package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
// down at once, and Reject — either door, factory.job_reject or
// factory.job_reject_plan — ends the job rejected with the person's words. Ask for changes without words is
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

	// park files a job waiting on its run's plan. Its worktree is another
	// computer's, so this machine's runner does not take it to follow the
	// run (a live run with no runner is owed one): this test is the answer
	// doors on a job no runner holds.
	park := func(round string) int {
		t.Helper()
		id, err := host.floor.Add(context.Background(), factory.Item{Title: "Add Len", Tier: factory.TierOwner})
		if err != nil {
			t.Fatal(err)
		}
		if err := host.floor.Update(id, func(it *factory.Item) error {
			it.State, it.QKind, it.Question = factory.StateNeedsYou, factory.QKindAgentField, "Approve the plan?"
			it.WorktreeMachine = "another-computer"
			it.Stages = []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "swe-planner.build_issue", On: true}}
			it.AgentFieldSteps = map[string]*factory.AgentFieldRun{"build": {ExecutionID: "exec-" + round, RunID: "run-" + round,
				Waiting: &factory.AgentFieldWait{Execution: "exec-" + round, Step: "build", RequestID: "swe-plan-" + round, Question: "Approve the plan?", Body: "# Plan", At: time.Now()}}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// lastCan is the job's verbs as factory.job last read them.
	var lastCan struct {
		RequestChanges bool `json:"request_changes"`
		RejectPlan     bool `json:"reject_plan"`
		Reject         bool `json:"reject"`
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
				Reject         bool `json:"reject"`
			} `json:"can"`
		}
		_ = json.Unmarshal(afCall(s, "factory.job", map[string]any{"factory": st.Factory, "job": job}), &read)
		lastCan = read.Can
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
		len(listed.Reasoners[0].Input) != 4 || listed.Reasoners[0].Input[3].Name != "title" || !listed.Reasoners[0].Input[3].Required ||
		listed.Reasoners[0].Input[1].Type != "integer" || listed.Reasoners[0].Input[2].Type != "boolean" || len(listed.Placeholders) == 0 {
		t.Fatalf("reasoners = %+v", listed)
	}

	a := park("a")
	// ALL THREE ANSWERS ARE OFFERED on the job the desk reads: a desk leaves
	// a verb off when can says no, so a missing reject_plan hides Reject.
	if version(a); !lastCan.RequestChanges || !lastCan.RejectPlan || !lastCan.Reject {
		t.Fatalf("a run's pause offers can = %+v, want request_changes, reject_plan and reject", lastCan)
	}
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

	// A REJECT ENDS THE JOB REJECTED, through either door: done, with the
	// person's words, never "Stopped by you" offering to carry on.
	rejected := func(method, round, words string) {
		t.Helper()
		job := park(round)
		if code, msg := answer(method, job, map[string]any{"words": words}); code != "" {
			t.Fatalf("%s: %s %s", method, code, msg)
		}
		eventually(t, "the rejected job ends rejected", func() bool {
			it, _ := host.floor.Get(job)
			return factory.JobState(it) == "done" && it.Rejection != nil && it.Rejection.Words == words
		})
	}
	rejected("factory.job_reject_plan", "c", "not this plan")
	rejected("factory.job_reject", "f", "wrong repo")

	// A step's question is answered with factory.job_answer: the option and
	// the person's words reach the factory's answer door as one answer.
	d, err := host.floor.Add(context.Background(), factory.Item{Title: "Add Len", Tier: factory.TierOwner})
	if err != nil {
		t.Fatal(err)
	}
	_ = host.floor.Update(d, func(it *factory.Item) error {
		it.State, it.QKind, it.Question = factory.StateNeedsYou, "step", "Which storage shape?"
		it.Asking = &factory.Asked{Stage: "build", Question: "Which storage shape?", Options: []string{"sqlite", "jsonl"}, Pick: "sqlite", With: factory.AskedYou, At: time.Now()}
		return nil
	})
	if code, msg := answer("factory.job_answer", d, map[string]any{"question_id": "q" + strconv.Itoa(d), "option": "jsonl", "words": "it is append-only"}); code != "" {
		t.Fatalf("job_answer: %s %s", code, msg)
	}
	answered, _ := host.floor.Get(d)
	if answered.State == factory.StateNeedsYou {
		t.Fatalf("the step's question is still waiting: %+v", answered.Asking)
	}
	if !strings.Contains(strings.Join(answered.Notes, "\n")+fmt.Sprint(answered.Stream), "jsonl: it is append-only") {
		t.Fatalf("the answer did not reach the job: notes %q", answered.Notes)
	}
	// An approval is never answered there.
	e := park("e")
	if code, _ := answer("factory.job_answer", e, map[string]any{"question_id": "q" + strconv.Itoa(e) + ":swe-plan-e", "option": "Approve"}); code != appwire.CodeBadParams {
		t.Fatalf("job_answer on an approval = %q", code)
	}
	if code, _ := answer("factory.job_answer", d, map[string]any{"question_id": "q1"}); code != appwire.CodeBadParams {
		t.Fatalf("an answer with neither option nor words = %q", code)
	}

	cp.mu.Lock()
	resolved := strings.Join(cp.resolved, "\n")
	cp.mu.Unlock()
	if resolved != "exec-a: request_changes: use sqlite\nexec-b: approved: \nexec-c: rejected: not this plan\nexec-f: rejected: wrong repo" {
		t.Fatalf("the control plane was answered:\n%s", resolved)
	}
}
