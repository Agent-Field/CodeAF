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
	it = waitOnRunPause(t, f, it.ID)
	return it, desk, held
}

// waitOnRunPause leaves job id waiting in Needs you on its AgentField run's
// plan, as the runner's report leaves it.
func waitOnRunPause(t *testing.T, f *v2Jobs, id int) factory.Item {
	t.Helper()
	if err := f.st.Update(id, func(cur *factory.Item) error {
		cur.State, cur.QKind, cur.Question = factory.StateNeedsYou, factory.QKindAgentField, "Approve the plan?"
		cur.Stream = &factory.Stream{}
		cur.Stages = []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "planner.build", On: true}}
		cur.AgentFieldSteps = map[string]*factory.AgentFieldRun{"build": {ExecutionID: "exec-root", RunID: "run-1", Status: "running",
			Waiting: &factory.AgentFieldWait{Execution: "exec-plan", Step: "build", Node: "planner", RequestID: "plan-r1", Question: "Approve the plan?", Body: "# Plan", Kind: "plan_review"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it, err := f.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return it
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

// A REJECT ENDS THE JOB, REJECTED, AFTER THE RUNNER CARRIED IT. Either door
// a desk has for a no — factory.job_reject (POST reject) and Ask for
// changes' sibling Reject (POST reject_plan) — is carried to the control plane
// by the runner holding the job. A stop lets go of the runner's lease and
// every signal it has not collected, so the job ends only once that runner
// acknowledges carrying it; then it is done as rejected with the person's
// words, never "Stopped by you" with a Carry on.
func TestAnyFactorysRejectEndsTheJobRejectedOnceItsRunnerCarriedIt(t *testing.T) {
	for _, verb := range []string{"reject", "reject_plan"} {
		t.Run(verb, func(t *testing.T) {
			f := newV2Jobs(t)
			it, desk, held := parkOnAgentFieldPause(t, f)
			if j := f.read(t, it); !j.Can.Reject {
				t.Fatalf("a job waiting on its run's approval does not offer Reject: %+v", j.Can)
			}
			f.post(t, it, verb, `{"words":"not this plan"}`, 200)
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
			if reject == nil || reject.Delivery == "" || reject.Words != "not this plan" {
				t.Fatalf("the runner was not handed the reject with its words: %+v", signals)
			}
			if got, _ := f.st.Get(it.ID); factory.JobState(got) == "done" || factory.StoppedByPerson(got) {
				t.Fatal("the job ended before its runner carried the reject")
			}
			if _, err := desk.DrainLive(held.ID, held.Worker, []string{reject.Delivery}); err != nil {
				t.Fatal(err)
			}
			got, _ := f.st.Get(it.ID)
			if factory.JobState(got) != "done" || got.Rejection == nil || got.Rejection.Words != "not this plan" {
				t.Fatalf("a rejected run's job did not end rejected with the words: state %s, rejection %+v", factory.JobState(got), got.Rejection)
			}
			if j := f.read(t, got); j.Can.Reject || j.Can.Resume || j.Can.Approve {
				t.Fatalf("a rejected job still offers an answer or a carry-on: %+v", j.Can)
			}
			if again, err := desk.DrainLive(held.ID, held.Worker, []string{reject.Delivery}); err == nil && !(len(again) == 1 && again[0].Verb == lease.SignalStop) {
				t.Fatalf("the runner still holds a rejected job: %+v", again)
			}
		})
	}
}

// A REJECT OF A RUN NO RUNNER HOLDS ENDS THE JOB AT ONCE, rejected with the
// person's words: nothing could carry it until a runner came back.
func TestARejectOfAnUnheldRunEndsTheJobRejected(t *testing.T) {
	f := newV2Jobs(t)
	it := waitOnRunPause(t, f, f.create(t, factory.Item{State: factory.StateQueued, Assignee: "sam"}).ID)
	f.post(t, it, "reject", `{"words":"wrong repo"}`, 200)
	got, _ := f.st.Get(it.ID)
	if factory.JobState(got) != "done" || got.Rejection == nil || got.Rejection.Words != "wrong repo" {
		t.Fatalf("an unheld rejected run's job did not end rejected: state %s, rejection %+v", factory.JobState(got), got.Rejection)
	}
}
