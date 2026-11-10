package main

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// afPausedStep is a job copy on a runner whose agentfield step "build" waits
// on its run's plan, and a control plane that holds that pause.
func afPausedStep(t *testing.T, decisions ...factory.AgentFieldDecision) (*store.Store, int, *afFakeCP, agentfield.TreeNode) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wait := &factory.AgentFieldWait{Execution: "exec-plan", Step: "build", RequestID: "plan-r1", Question: "Approve the plan?", Body: "# Plan", At: time.Now()}
	it, err := st.Create(factory.Item{Title: "Add Len", State: factory.StateNeedsYou, QKind: factory.QKindAgentField, Question: "Approve the plan?", Stream: &factory.Stream{},
		Stages:          []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "planner.build", On: true}},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {ExecutionID: "exec-root", RunID: "run-1", Waiting: wait, Decisions: decisions}}})
	if err != nil {
		t.Fatal(err)
	}
	reason := afWaitingForApproval
	plan := afNode("exec-plan", "plan", "waiting", "2026-11-07T12:00:01Z", "")
	plan.StatusReason = &reason
	cp := &afFakeCP{approval: map[string]agentfield.Approval{"exec-plan": {Status: "pending", RequestID: "plan-r1", Question: "Approve the plan?", Body: "# Plan"}}}
	return st, it.ID, cp, afNode("exec-root", "build", "running", "2026-11-07T12:00:00Z", "", plan)
}

// THE RUNNER THAT STARTED THE RUN CARRIES A PERSON'S ANSWER. A factory that
// took the answer signals it to this runner, which sends it to the control
// plane as the run's owner, with the person named beside the words, and takes
// the question down on its own copy so its next reading does not ask again.
func TestTheRunnerCarriesAnAnswerGivenOnAnyFactory(t *testing.T) {
	st, job, cp, _ := afPausedStep(t)
	r := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return cp, "org-runner", true }}
	d := factory.AgentFieldDecision{RequestID: "plan-r1", Execution: "exec-plan", Decision: agentfield.DecisionRequestChanges, Words: "use sqlite", By: "sam"}
	if err := r.Answer(context.Background(), job, d); err != nil {
		t.Fatal(err)
	}
	if want := "exec-plan: request_changes: use sqlite (by sam)"; !slices.Contains(cp.resolved, want) {
		t.Fatalf("the control plane was told %q, want %q", cp.resolved, want)
	}
	it, _ := st.Get(job)
	if it.State == factory.StateNeedsYou || it.AgentFieldSteps["build"].Waiting != nil {
		t.Fatalf("the runner's copy still asks: state %s waiting %+v", it.State, it.AgentFieldSteps["build"].Waiting)
	}
}

// AN ANSWER SURVIVES A RESTART. The factory kept the person's decision on the
// job; the runner that takes the job again (a fresh process, nothing in
// memory) reads the control plane's pause, finds the decision it still owes
// for that very pause, sends it, and does not put the question up again.
func TestARunnerThatTakesTheJobAgainSendsTheAnswerItStillOwes(t *testing.T) {
	d := factory.AgentFieldDecision{RequestID: "plan-r1", Execution: "exec-plan", Decision: agentfield.DecisionApproved, Words: "go", By: "sam", At: time.Now()}
	st, job, cp, tree := afPausedStep(t, d)
	r := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return cp, "org-runner", true }}
	if _, err := r.syncTo(context.Background(), cp, st, job, tree, afPlace{step: "build"}); err != nil {
		t.Fatal(err)
	}
	if want := "exec-plan: approved: go (by sam)"; !slices.Contains(cp.resolved, want) {
		t.Fatalf("the owed answer was not sent: %q", cp.resolved)
	}
	it, _ := st.Get(job)
	if it.State == factory.StateNeedsYou || it.QKind == factory.QKindAgentField {
		t.Fatalf("the answered pause was asked again: state %s qkind %q", it.State, it.QKind)
	}
	// Read again at once: the control plane already has it, so it is not
	// sent twice in a row.
	if _, err := r.syncTo(context.Background(), cp, st, job, tree, afPlace{step: "build"}); err != nil {
		t.Fatal(err)
	}
	if n := len(cp.resolved); n != 1 {
		t.Fatalf("the answer was sent %d times: %q", n, cp.resolved)
	}
}

// A REFUSED ANSWER IS SAID, NEVER RETRIED FOREVER. The control plane refuses
// the answer outright (another node owns the run): the runner writes the
// refusal on the run record, so the question stands again on the factory
// with its words, and the signal is done.
func TestAnAnswerTheControlPlaneRefusesIsWrittenOnTheRun(t *testing.T) {
	st, job, cp, _ := afPausedStep(t)
	cp.resolveErr = &agentfield.Error{Code: 403, Op: "answer the approval", Message: "this node does not own the run"}
	r := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return cp, "org-runner", true }}
	d := factory.AgentFieldDecision{RequestID: "plan-r1", Execution: "exec-plan", Decision: agentfield.DecisionApproved, By: "sam", At: time.Now()}
	if err := r.Answer(context.Background(), job, d); err != nil {
		t.Fatalf("a refusal is not worth trying again: %v", err)
	}
	it, _ := st.Get(job)
	f := it.AgentFieldSteps["build"].Refused
	if f == nil || f.RequestID != "plan-r1" || !strings.Contains(f.Words, "does not own the run") {
		t.Fatalf("refusal = %+v", f)
	}
	if factory.AgentFieldAnswerFor(it.AgentFieldSteps["build"], "exec-plan", "plan-r1") != nil {
		t.Fatal("a refused answer still reads as owed")
	}
}

// A STOP NO RUNNER HELD IS CARRIED BY THE NEXT ONE. Job 71 of the third live
// run: the person's Stop landed while no runner held the job, so its run went
// on at the control plane and opened a pull request four seconds later. The
// runner that next takes the stopped job cancels the whole run and writes its
// end on the factory's own job, so the job is owed nothing more.
func TestARunnerCarriesAStopThatLandedWithNoRunnerHoldingTheJob(t *testing.T) {
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	floor, id, cp, tree := afPausedStep(t)
	stopped := true
	if err := floor.Update(id, func(it *factory.Item) error {
		it.PersonStopped = &stopped
		it.State, it.QKind, it.Question = factory.StateNew, "", ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cp.tree = tree
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote := &afRemote{items: mirror, floor: floor, conn: func() (afClient, string, bool) { return cp, "workaf", true }}
	it, _ := floor.Get(id)
	if err := remote.CarryStop(context.Background(), it); err != nil {
		t.Fatal(err)
	}
	cp.mu.Lock()
	cancelled := append([]string(nil), cp.cancelled...)
	cp.mu.Unlock()
	if len(cancelled) != 1 || cancelled[0] != "run-1" {
		t.Fatalf("the stop did not reach the control plane: cancelled %v", cancelled)
	}
	got, _ := floor.Get(id)
	if run := got.AgentFieldSteps["build"]; run.Ended.IsZero() || run.Status != "cancelled" {
		t.Fatalf("the run's end is not on the factory's job: %+v", run)
	}
	if !factory.StoppedByPerson(got) || factory.AgentFieldAnyRunLive(got) {
		t.Fatalf("after the carry the job still owes its run a runner: stopped %v", factory.StoppedByPerson(got))
	}
}
