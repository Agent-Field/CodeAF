package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE PERSON'S ANSWER, THROUGH THE REAL DOORS: on a real local factory, a job
// whose AgentField run waits for approval is answered with the job verbs the
// desk already has — Ask for changes with words, Approve, Reject — and each
// is carried to the control plane as approval-response, the question comes
// down at once, and Reject stops the job. Ask for changes without words is
// refused before anything is sent.
func TestARunsPauseIsAnsweredWithTheJobVerbs(t *testing.T) {
	s := newLocalSetup(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "running", "2026-11-07T12:00:00Z", "")}
	afTestConn = func() (afClient, string, bool) { return cp, "codeaf-factory", true }
	t.Cleanup(func() { afTestConn = nil })
	st := s.start()
	eventually(t, "the runner holds the worker lock", func() bool { return runnerHeld(s) })
	s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret})
	s.setup("factory.setup_select", map[string]any{"repos": []string{"acme/api"}})
	s.until("checkouts", func(v appwire.FactorySetupView) bool { return v.State == "checkouts" })
	s.setup("factory.setup_skip", map[string]any{"repo": "acme/api"})
	s.setup("factory.setup_done", map[string]any{})
	s.backend.local.mu.Lock()
	host := s.backend.local.host
	s.backend.local.mu.Unlock()

	// park files a job waiting on its run's plan.
	park := func(round string) int {
		t.Helper()
		id, err := host.floor.Add(context.Background(), factory.Item{Title: "Add Len", Tier: factory.TierOwner})
		if err != nil {
			t.Fatal(err)
		}
		if err := host.floor.Update(id, func(it *factory.Item) error {
			it.State, it.QKind, it.Question = factory.StateNeedsYou, factory.QKindAgentField, "Approve the plan?"
			it.Stages = []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "swe-planner.build_issue", On: true}}
			it.AgentFieldSteps = map[string]*factory.AgentFieldRun{"build": {ExecutionID: "exec-" + round, RunID: "run-" + round,
				Waiting: &factory.AgentFieldWait{Execution: "exec-" + round, Step: "build", RequestID: "swe-plan-" + round, Question: "Approve the plan?", Body: "# Plan", At: time.Now()}}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	version := func(job int) int {
		var read struct {
			Version  int
			NeedsYou *struct {
				Question *struct{ Kind string } `json:"question"`
			} `json:"needs_you"`
			Can struct {
				RequestChanges bool `json:"request_changes"`
				RejectPlan     bool `json:"reject_plan"`
			} `json:"can"`
		}
		_ = json.Unmarshal(afCall(s, "factory.job", map[string]any{"factory": st.Factory, "job": job}), &read)
		return read.Version
	}
	answer := func(method string, job int, extra map[string]any) (string, string) {
		params := map[string]any{"factory": st.Factory, "job": job, "expect": map[string]any{"version": version(job), "state": "needs_you"}}
		for k, v := range extra {
			params[k] = v
		}
		_, code, msg := s.call(method, params)
		return code, msg
	}

	// The reasoners an agentfield step may name, with their input fields.
	var listed appwire.FactoryAgentFieldReasoners
	_ = json.Unmarshal(afCall(s, "factory.agentfield_reasoners", map[string]any{"factory": st.Factory}), &listed)
	if len(listed.Reasoners) != 1 || listed.Reasoners[0].Target != "swe-planner.build_issue" || !listed.Reasoners[0].Online ||
		len(listed.Reasoners[0].Input) != 2 || listed.Reasoners[0].Input[1].Name != "title" || !listed.Reasoners[0].Input[1].Required ||
		listed.Reasoners[0].Input[0].Type != "integer" || len(listed.Placeholders) == 0 {
		t.Fatalf("reasoners = %+v", listed)
	}

	a := park("a")
	if code, _ := answer("factory.job_request_changes", a, nil); code != appwire.CodeBadParams {
		t.Fatalf("Ask for changes with no words = %q", code)
	}
	if code, msg := answer("factory.job_request_changes", a, map[string]any{"words": "use sqlite"}); code != "" {
		t.Fatalf("Ask for changes: %s %s", code, msg)
	}
	got, _ := host.floor.Get(a)
	if got.State != factory.StateRunning || got.QKind != "" || len(got.AgentFieldSteps["build"].Decisions) != 1 || got.AgentFieldSteps["build"].Decisions[0].Words != "use sqlite" {
		t.Fatalf("after Ask for changes: %s %q %+v", got.State, got.QKind, got.AgentFieldSteps["build"])
	}

	b := park("b")
	if code, msg := answer("factory.job_approve", b, nil); code != "" {
		t.Fatalf("Approve: %s %s", code, msg)
	}

	c := park("c")
	if code, msg := answer("factory.job_reject_plan", c, nil); code != "" {
		t.Fatalf("Reject: %s %s", code, msg)
	}
	eventually(t, "the rejected job stops", func() bool {
		it, _ := host.floor.Get(c)
		return it.State != factory.StateNeedsYou && it.State != factory.StateRunning
	})

	cp.mu.Lock()
	resolved := strings.Join(cp.resolved, "\n")
	cp.mu.Unlock()
	if resolved != "exec-a: request_changes: use sqlite\nexec-b: approved: \nexec-c: rejected: " {
		t.Fatalf("the control plane was answered:\n%s", resolved)
	}
}
