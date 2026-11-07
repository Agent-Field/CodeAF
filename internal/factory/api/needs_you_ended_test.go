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
