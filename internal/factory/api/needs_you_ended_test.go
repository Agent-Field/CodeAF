package api_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A JOB A PERSON ENDED ASKS NOTHING OF THEM. A person's Stop and a person's
// Reject are decisions already made: neither job is counted in Needs you, and
// neither reads needs_you, while a job really waiting on an approval does.
func TestStoppedAndRejectedJobsLeaveNeedsYou(t *testing.T) {
	now := time.Unix(1000, 0)
	yes := true
	waiting := factory.Item{ID: 1, State: factory.StateNeedsYou, QKind: factory.QKindApprove, Question: factory.ApproveQuestion("plan"),
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting}}}}
	stopped := factory.Item{ID: 2, State: factory.StateNew, PersonStopped: &yes,
		Failure: &factory.Failure{Step: "write", Code: factory.CodeStopped, Detail: "Stopped by you", Next: factory.FailureStopped},
		Stream:  &factory.Stream{Ended: now, Phases: []factory.Phase{{Name: "write", State: factory.PhaseFailed}}}}
	rejected := waiting
	rejected.ID = 3
	rejected.Stream = &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting}}}
	rejected.EndRejected("sam", "not this", now)
	q := api.FromSnapshot(factory.Snapshot{Items: []factory.Item{waiting, stopped, rejected}}, "sam", now)
	counted := map[int]bool{}
	for _, g := range q.NeedsYou {
		for _, id := range g.Jobs {
			counted[id] = true
		}
	}
	if !counted[1] || counted[2] || counted[3] {
		t.Fatalf("Needs you counts %v; want only the job waiting on its approval", counted)
	}
	for _, it := range []factory.Item{stopped, rejected} {
		if state := factory.JobState(it); state == "needs_you" {
			t.Fatalf("job %d, ended by its person, reads %s", it.ID, state)
		}
	}
}

// A STOPPED JOB OFFERS ONLY WHAT WORKS ON IT. Start, Run again and Run again
// as a new job start it again (the manual's "stop a job and start it again");
// Resume only continues a paused job and refuses a stopped one with "is not
// paused", so a stopped job does not offer it. Job 71 of the 2026-11-07 live
// run offered resume beside start_now, run_again and run_as_new.
func TestAStoppedJobDoesNotOfferResume(t *testing.T) {
	now := time.Unix(1000, 0)
	yes := true
	stopped := factory.Item{ID: 2, Assignee: "sam", State: factory.StateNew, PersonStopped: &yes,
		Failure: &factory.Failure{Step: "write", Code: factory.CodeStopped, Detail: "Stopped by you", Next: factory.FailureStopped},
		Stream:  &factory.Stream{Ended: now, Phases: []factory.Phase{{Name: "write", State: factory.PhaseFailed}}}}
	can := api.CanFor(stopped, "sam", now)
	if can.Resume || can.Stop {
		t.Fatalf("a stopped job offers a verb that refuses it: resume=%v stop=%v", can.Resume, can.Stop)
	}
	if !can.StartNow || !can.RunAgain || !can.RunAsNew {
		t.Fatalf("a stopped job does not offer the ways to start it again: %+v", can)
	}
}
