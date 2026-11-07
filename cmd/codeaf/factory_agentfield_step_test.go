package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// afStepJob files a job routed to a pipeline of one agentfield step.
func afStepJob(t *testing.T) (*store.Store, factory.Item, factory.Stage) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stage := factory.Stage{Name: "build", Label: "Build with SWE-AF", Kind: factory.StageAgentField, Target: "swe-planner.build_issue", On: true,
		Input: map[string]string{"title": "{title}", "body": "{body}", "repo_url": "{repo_url}", "issue_number": "{issue_number}"}}
	it, err := st.Create(factory.Item{Title: "Add a Len() method to Ledger", Body: "ledger.go needs Len", Num: 12, Repo: "santoshkumarradha/factory-demo",
		State: factory.StateRunning, Stages: []factory.Stage{stage}})
	if err != nil {
		t.Fatal(err)
	}
	return st, it, stage
}

// afUntil waits for the job to read as want says.
func afUntil(t *testing.T, st *store.Store, id int, what string, want func(factory.Item) bool) factory.Item {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		it, _ := st.Get(id)
		if want(it) {
			return it
		}
		if time.Now().After(deadline) {
			t.Fatalf("never %s: state %s %q %+v", what, it.State, it.Question, it.AgentFieldSteps["build"])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *afFakeCP) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

// PATTERN A, END TO END ON A FAKE CONTROL PLANE: a job whose pipeline is one
// agentfield step starts the step's reasoner with the job mapped into its
// input, folds the run under the step, shows the plan in Needs you in full,
// shows the NEW plan after a change was asked for, takes the question down
// when it is answered anywhere, and ends with the pull request the run opened.
func TestAnAgentFieldStepRunsTheReasonerThroughItsPlanToAPullRequest(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })

	st, it, stage := afStepJob(t)
	waiting := afWaitingForApproval
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "running", "2026-11-07T12:00:00Z", "",
		afNode("e-plan", "plan", "running", "2026-11-07T12:00:01Z", ""))}
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	var logged []string
	done := make(chan error, 1)
	var result factory.StageResult
	go func() {
		var err error
		result, err = r.Step(context.Background(), factoryrun.Job{Item: it, Stage: stage, Log: func(l string) { logged = append(logged, l) }})
		done <- err
	}()

	// The reasoner was started with the job's facts, numbers as numbers.
	afUntil(t, st, it.ID, "started", func(i factory.Item) bool { return i.AgentFieldSteps["build"] != nil })
	cp.set(func() {
		if len(cp.dispatched) != 1 || cp.dispatched[0]["_target"] != "swe-planner.build_issue" || cp.dispatched[0]["issue_number"] != float64(12) ||
			cp.dispatched[0]["repo_url"] != "https://github.com/santoshkumarradha/factory-demo" || cp.owner != "workaf-node" {
			t.Errorf("dispatched %+v owner %q", cp.dispatched, cp.owner)
		}
	})

	// Round 1: the plan pauses for a person.
	pause := func(id, body string) {
		cp.set(func() {
			cp.tree.Status, cp.tree.StatusReason = "waiting", &waiting
			cp.tree.Children[0].Status = "succeeded"
			if cp.approval == nil {
				cp.approval = map[string]agentfield.Approval{}
			}
			cp.approval["exec-root"] = agentfield.Approval{Status: "pending", RequestID: id, Kind: "plan_review",
				Question: "Approve the plan for #12: Add a Len() method to Ledger?", Body: body, Options: []string{"approve", "request_changes", "reject"}}
		})
	}
	resume := func() {
		cp.set(func() { cp.tree.Status, cp.tree.StatusReason = "running", nil })
	}
	pause("swe-plan-exec-root-r1", "# Plan\n\n1. add Len to ledger.go")
	got := afUntil(t, st, it.ID, "asked for the plan", func(i factory.Item) bool { return i.State == factory.StateNeedsYou })
	w := factory.AgentFieldWaiting(got)
	if got.QKind != factory.QKindAgentField || w == nil || w.Body != "# Plan\n\n1. add Len to ledger.go" || w.Step != "build" || w.RequestID != "swe-plan-exec-root-r1" {
		t.Fatalf("round 1 = %q %+v", got.QKind, w)
	}
	if run := got.AgentFieldSteps["build"]; len(run.Stages) != 1 || run.Stages[0].Name == "build" || len(got.Stages) != 1 {
		t.Fatalf("the run's steps are not under the agentfield step: job %+v run %+v", got.Stages, run.Stages)
	}

	// Asked for changes (anywhere): the question comes down while the
	// architect re-plans, and the new plan is what round 2 shows.
	cp.set(func() { a := cp.approval["exec-root"]; a.Status = "request_changes"; cp.approval["exec-root"] = a })
	resume()
	afUntil(t, st, it.ID, "cleared after an answer", func(i factory.Item) bool { return i.State == factory.StateRunning && i.QKind == "" })
	pause("swe-plan-exec-root-r2", "# Plan\n\n1. add Len, with a test")
	got = afUntil(t, st, it.ID, "asked for the new plan", func(i factory.Item) bool {
		w := factory.AgentFieldWaiting(i)
		return i.State == factory.StateNeedsYou && w != nil && w.RequestID == "swe-plan-exec-root-r2"
	})
	if w := factory.AgentFieldWaiting(got); w.Body != "# Plan\n\n1. add Len, with a test" || len(got.AgentFieldSteps["build"].Asks) != 2 {
		t.Fatalf("round 2 = %+v asks %d", w, len(got.AgentFieldSteps["build"].Asks))
	}

	// Approved; the run codes and opens a pull request.
	cp.set(func() { a := cp.approval["exec-root"]; a.Status = "approved"; cp.approval["exec-root"] = a })
	resume()
	afUntil(t, st, it.ID, "cleared after approval", func(i factory.Item) bool { return i.State == factory.StateRunning })
	cp.set(func() {
		cp.result = json.RawMessage(`{"success":true,"summary":"done","pr_results":[{"pr_url":"https://github.com/santoshkumarradha/factory-demo/pull/31","pr_number":31}]}`)
		cp.tree.Status = "succeeded"
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the step failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the step never ended")
	}
	if !result.Done || !strings.Contains(result.Output, "pull/31") {
		t.Fatalf("result = %+v", result)
	}
	end, _ := st.Get(it.ID)
	if end.PullRequest == nil || end.PullRequest.Number != 31 || end.Publish != "pr" {
		t.Fatalf("the job does not hold the run's pull request: %+v", end.PullRequest)
	}
	if len(logged) == 0 || !strings.Contains(logged[0], "started swe-planner.build_issue") {
		t.Fatalf("log = %v", logged)
	}
}

// A plan rejected anywhere ends the run cleanly with {success:false}; the step
// ends with the run's own words, never as work done.
func TestARejectedPlanEndsTheStepWithTheRunsWords(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "succeeded", "2026-11-07T12:00:00Z", "2026-11-07T12:00:05Z"),
		result: json.RawMessage(`{"success":false,"summary":"Plan rejected: not now"}`)}
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	_, err := r.Step(context.Background(), factoryrun.Job{Item: it, Stage: stage})
	if err == nil || !strings.Contains(err.Error(), "Plan rejected: not now") {
		t.Fatalf("err = %v", err)
	}
}

// A HARNESS STEP'S QUESTION REACHES THE JOB AND ITS ANSWER REACHES THE STEP:
// the node relays the step's ask line; the factory keeps it out of the
// step's conversation, puts it to the job (its manager first, then the
// person: Job.Ask), and sends the answer back down the step's steer channel
// as an answer frame.
func TestAHarnessStepsQuestionIsAnsweredOverSteer(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "running", "2026-11-07T12:00:00Z", "",
		afNode("e-code", "coder", "running", "2026-11-07T12:00:01Z", ""))}
	cp.relayLines("e-code", [][]byte{
		[]byte(`{"type":"session","version":1,"id":"nodecoder","cwd":"/node","model":"m","timestamp":"2026-11-07T12:00:01Z"}`),
		[]byte(`{"type":"message","role":"user","content":"add Len","timestamp":"2026-11-07T12:00:01Z"}`),
		[]byte(`{"type":"ask","id":"ask-1","question":"Which storage shape?","options":["sqlite","jsonl"],"pick":"sqlite","timestamp":"2026-11-07T12:00:02Z"}`),
	}, false)
	// The node closes the step's record once the run is cancelled.
	cp.afterCancel = func() {
		cp.set(func() {
			cp.held["e-code"] = append(cp.held["e-code"], agentfield.RelayEntry{Seq: 100, Kind: "ended", Data: json.RawMessage(`{}`)})
		})
	}
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	asked := make(chan factory.Asked, 1)
	ask := func(_ context.Context, q factory.Asked) (string, error) {
		asked <- q
		return "the manager answered: jsonl", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stepped := make(chan struct{})
	go func() { defer close(stepped); _, _ = r.Step(ctx, factoryrun.Job{Item: it, Stage: stage, Ask: ask}) }()
	t.Cleanup(func() { cancel(); <-stepped })
	select {
	case q := <-asked:
		if q.Question != "Which storage shape?" || strings.Join(q.Options, ",") != "sqlite,jsonl" || q.Pick != "sqlite" {
			t.Fatalf("asked %+v", q)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the question never reached the job")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		cp.mu.Lock()
		steers := strings.Join(cp.steers, "\n")
		cp.mu.Unlock()
		if steers == `e-code: {"answer":"ask-1","text":"jsonl","by":"manager"}` {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("steers = %q", steers)
		}
		time.Sleep(10 * time.Millisecond)
	}
	chat := ""
	afUntil(t, st, it.ID, "a conversation for the harness step", func(i factory.Item) bool {
		for _, ph := range factory.AllPhases(i) {
			if ph.Chat != "" {
				chat = ph.Chat
				return true
			}
		}
		return false
	})
	raw, _ := os.ReadFile(chat)
	if strings.Contains(string(raw), `"type":"ask"`) || !strings.Contains(string(raw), "add Len") {
		t.Fatalf("the step's conversation is wrong:\n%s", raw)
	}
}

// CALLS THAT FAN OUT READ APART: three investigate calls that ran at once are
// each named by their own subject from their input, and the group they make
// is still "Investigate"; SWE-AF's coder calls are named by their issue.
func TestFannedOutCallsAreNamedByTheirSubject(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`{"question":"which json lib?","option":{"name":"goccy/go-json","why":"fast"}}`, "goccy/go-json"},
		{`{"subject":"encoding/json","plan":{"options":[]}}`, "encoding/json"},
		{`{"issue":{"name":"len-method","title":"add Len() to Ledger"},"iteration":1}`, "add Len() to Ledger"},
		{`{"goal":"# Make the ledger report its size\n\nmore words"}`, "Make the ledger report its size"},
		{`{"n":3}`, ""},
	} {
		if got := afSubjectOf(json.RawMessage(tc.input)); got != tc.want {
			t.Errorf("afSubjectOf(%s) = %q, want %q", tc.input, got, tc.want)
		}
	}
	if got := afSubjectLabel("Coder", "add Len() to Ledger"); got != "Coder · add Len() to Ledger" {
		t.Errorf("phrase label = %q", got)
	}

	inv := func(id, start string) agentfield.TreeNode {
		return afNode(id, "investigate", "succeeded", start, "2026-11-07T12:00:05Z")
	}
	root := afNode("exec-root", "research", "running", "2026-11-07T12:00:00Z", "",
		inv("e-1", "2026-11-07T12:00:01Z"), inv("e-2", "2026-11-07T12:00:01.1Z"), inv("e-3", "2026-11-07T12:00:01.2Z"),
		afNode("e-s", "synthesize", "running", "2026-11-07T12:00:09Z", ""))
	cp := &afFakeCP{tree: root, inputs: map[string]string{
		"e-1": `{"option":{"name":"goccy/go-json"}}`, "e-2": `{"option":{"name":"encoding/json"}}`, "e-3": `{"subject":"json-iterator/go"}`,
	}}
	r := &afRemote{}
	steps := afFlattenNamed(root, r.subjectsFor(context.Background(), cp, root))
	var labels []string
	for _, s := range steps[:3] {
		labels = append(labels, s.label)
		if s.groupLabel != "Investigate" || s.group != steps[0].name {
			t.Errorf("%s is grouped as %q/%q", s.label, s.group, s.groupLabel)
		}
	}
	if strings.Join(labels, " | ") != "Investigate goccy/go-json | Investigate encoding/json | Investigate json-iterator/go" || steps[3].label != "Synthesize" {
		t.Fatalf("labels = %v, %q", labels, steps[3].label)
	}
	// The input is read once per call, and never for a reasoner called once.
	_ = r.subjectsFor(context.Background(), cp, root)
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if strings.Join(cp.inputReads, ",") != "e-1,e-2,e-3" {
		t.Fatalf("inputs read: %v", cp.inputReads)
	}
}
