package serve_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// parkOnAgentFieldPause makes a team factory's job held by a runner and
// waiting in Needs you on its AgentField run's plan, as the runner's report
// leaves it, and returns the job and the runner's lease.
func parkOnAgentFieldPause(t *testing.T, f *v2Jobs) (factory.Item, *lease.Desk, lease.Lease) {
	t.Helper()
	it := f.create(t, factory.Item{State: factory.StateQueued, Assignee: "sam"})
	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	held, err := desk.Pull(lease.Worker{ID: "runner", Machine: "server", Handle: "sam", Team: true, Slots: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.State, cur.QKind, cur.Question = factory.StateNeedsYou, factory.QKindAgentField, "Approve the plan?"
		cur.Stream = &factory.Stream{}
		cur.Stages = []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "planner.build", On: true}}
		cur.AgentFieldSteps = map[string]*factory.AgentFieldRun{"build": {ExecutionID: "exec-root", RunID: "run-1", Status: "running",
			Waiting: &factory.AgentFieldWait{Execution: "exec-plan", Step: "build", Node: "planner", RequestID: "plan-r1", Question: "Approve the plan?", Body: "# Plan", Kind: "plan_review"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it, err = f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	return it, desk, held
}

// ANY FACTORY TAKES AN ANSWER TO AN AGENTFIELD RUN'S PAUSE. A team factory —
// one on a server, or another computer's that a desk is attached to — is
// answered with the same three verbs as the desk's own: it lets only the job's
// person answer, refusing anyone else in words; it keeps the decision with who
// gave it; it takes the question down; and it signals the runner holding the
// job, which started the run and carries the answer to the control plane.
func TestAnyFactoryTakesAnAnswerToARunsPauseAndSignalsItsRunner(t *testing.T) {
	f := newV2Jobs(t)
	it, desk, held := parkOnAgentFieldPause(t, f)

	if j := f.read(t, it); j.NeedsYou == nil || j.NeedsYou.Question == nil || j.NeedsYou.Question.Kind != factory.QKindAgentField || !j.Can.Approve || !j.Can.RequestChanges || !j.Can.RejectPlan {
		t.Fatalf("the job's person is not offered the run's answers: %+v", j.Can)
	}
	f.person = "mei"
	if j := f.read(t, it); j.Can.Approve || j.Can.RequestChanges || j.Can.RejectPlan {
		t.Fatalf("someone who is not the job's person is offered the run's answers: %+v", j.Can)
	}
	for _, verb := range []string{"approve", "request_changes", "reject_plan"} {
		w := f.post(t, it, verb, `{"words":"looks fine"}`, 403)
		if !strings.Contains(w.Body.String(), "only @sam can answer this") {
			t.Fatalf("%s by another person was not refused in words: %s", verb, w.Body.String())
		}
	}
	f.person = "sam"
	if w := f.post(t, it, "request_changes", `{}`, 422); !strings.Contains(w.Body.String(), "say what to change") {
		t.Fatalf("Ask for changes with no words was not refused in words: %s", w.Body.String())
	}
	if w := f.post(t, it, "answer", `{"yes":true,"words":"ok"}`, 422); !strings.Contains(w.Body.String(), "answer it with Approve, Ask for changes or Reject") {
		t.Fatalf("a step's answer to the run's approval was not refused in words: %s", w.Body.String())
	}

	f.post(t, it, "request_changes", `{"words":"use sqlite, not postgres"}`, 200)

	got, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := got.AgentFieldSteps["build"]
	if len(run.Decisions) != 1 {
		t.Fatalf("decisions = %+v", run.Decisions)
	}
	if d := run.Decisions[0]; d.Decision != "request_changes" || d.Words != "use sqlite, not postgres" || d.By != "sam" || d.RequestID != "plan-r1" || d.Execution != "exec-plan" {
		t.Fatalf("decision = %+v", d)
	}
	if got.State != factory.StateRunning || got.QKind != "" || run.Waiting != nil {
		t.Fatalf("the answered question is still up: state %s qkind %q waiting %+v", got.State, got.QKind, run.Waiting)
	}
	signals, err := desk.DrainLive(held.ID, held.Worker)
	if err != nil {
		t.Fatal(err)
	}
	var carried *lease.Signal
	for i := range signals {
		if signals[i].Verb == lease.SignalAgentField {
			carried = &signals[i]
		}
	}
	if carried == nil || carried.ID != "plan-r1" || carried.Execution != "exec-plan" || carried.Decision != "request_changes" || carried.Words != "use sqlite, not postgres" || carried.By != "sam" {
		t.Fatalf("the runner was not handed the answer: %+v", signals)
	}
	// The pause is answered; a second answer is refused as the job not
	// waiting, never sent.
	f.post(t, it, "approve", `{}`, 409)
}

// A REJECT ENDS THE JOB AFTER THE RUNNER CARRIED IT. A stop lets go of the
// runner's lease and every signal it has not collected, so the job stops as
// the person's Stop only once its runner acknowledges carrying the reject to
// the control plane; until then the runner still holds the job.
func TestAnyFactorysRejectStopsTheJobOnceItsRunnerCarriedIt(t *testing.T) {
	f := newV2Jobs(t)
	it, desk, held := parkOnAgentFieldPause(t, f)
	f.post(t, it, "reject_plan", `{}`, 200)
	signals, err := desk.DrainLive(held.ID, held.Worker, []string{})
	if err != nil {
		t.Fatal(err)
	}
	var reject *lease.Signal
	for i := range signals {
		if signals[i].Verb == lease.SignalAgentField && signals[i].Decision == "rejected" {
			reject = &signals[i]
		}
	}
	if reject == nil || reject.Delivery == "" {
		t.Fatalf("the runner was not handed the reject: %+v", signals)
	}
	if got, _ := f.st.Get(it.ID); factory.StoppedByPerson(got) {
		t.Fatal("the job stopped before its runner carried the reject")
	}
	if _, err := desk.DrainLive(held.ID, held.Worker, []string{reject.Delivery}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.Get(it.ID)
	if !factory.StoppedByPerson(got) {
		t.Fatalf("a rejected run's job was not stopped as the person's Stop: state %s", got.State)
	}
	if again, err := desk.DrainLive(held.ID, held.Worker, []string{reject.Delivery}); err == nil && !(len(again) == 1 && again[0].Verb == lease.SignalStop) {
		t.Fatalf("the runner still holds a stopped job: %+v", again)
	}
}
