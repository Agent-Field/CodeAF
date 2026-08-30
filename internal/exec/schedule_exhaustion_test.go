package exec

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// AN ENDING THAT IS EXHAUSTION IS NOT A VERDICT ON THE WORK, and until this the
// one-shot scheduler was the last place that answered it with a failure.
//
// The resident learned it from the ink run of 2026-08-29: a node abandoned on
// the clock was settled failed in the same second it was said to be going back
// on the queue, over a twenty-six kilobyte patch and seventy-three minutes of
// unspent wall. `aforge run` drives this scheduler instead, and it was making
// the identical mistake one package along — so the rule is [Requeued], asked by
// both, and this is that rule arriving here.
func TestAnExhaustedNodeGoesBackOnTheQueueWithItsRecord(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	id := graph.Add(plan.Node{Stage: 1, Title: "Long"})
	node := graph.Node(id)
	// The record an earlier attempt left on the row: this scheduler has no
	// transcript bank, so the node's own turns are what a requeue resumes from.
	node.State, node.Turns, node.Result = plan.StateRunning, 45, "half of it"

	// apply is the whole subject here, so the scheduler is the bare struct: it
	// reads no registry and launches nothing on this path.
	scheduler := &Scheduler{}
	retries := map[int]int{}
	scheduler.apply(graph, id, nil, &Abandoned{After: 17 * time.Minute}, time.Now(), retries)

	if node.State != plan.StatePending {
		t.Fatalf("the exhausted node is %s, want pending — a failed node is never offered again", node.State)
	}
	if node.Turns != 45 || node.Result != "half of it" {
		t.Errorf("the record did not survive the requeue: %d turns, result %q", node.Turns, node.Result)
	}
	if node.Failure != "" {
		t.Errorf("a requeue wrote a failure onto the node: %q", node.Failure)
	}
	if retries[id] != 1 {
		t.Errorf("the requeue counted %d attempts, want 1", retries[id])
	}

	// And it is bounded. The node's record cannot grow while nothing lands on
	// it, so a second abandonment reads what the first one read; without the
	// bound the node would go round forever.
	node.State = plan.StateRunning
	scheduler.apply(graph, id, nil, &Abandoned{After: 17 * time.Minute}, time.Now(), retries)
	if node.State != plan.StateFailed {
		t.Fatalf("a node requeued once and exhausted again is %s, want failed", node.State)
	}
	if node.Failure != "executor did not return within 17m0s; abandoned" {
		t.Errorf("the failure reads %q, want the sentence exec.Abandoned writes", node.Failure)
	}
}

// The other side of the gate: an attempt that reached nothing has told us the
// only thing it is going to, and requeueing it would be the same cold start
// again. This is the case the two watchdog tests beside it already cover
// end-to-end; it is stated here so the gate cannot be quietly widened.
func TestAnExhaustedNodeWithNoRecordIsStillFailed(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	id := graph.Add(plan.Node{Stage: 1, Title: "Cold"})
	node := graph.Node(id)
	node.State = plan.StateRunning

	scheduler := &Scheduler{}
	scheduler.apply(graph, id, nil, &Abandoned{After: 17 * time.Minute}, time.Now(), map[int]int{})

	if node.State != plan.StateFailed {
		t.Fatalf("a node that recorded nothing is %s, want failed", node.State)
	}
}

// Requeued is the whole rule, and both schedulers ask it rather than deciding
// for themselves. A failure that is not the clock is not exhaustion however
// much work is recorded, and a spent clock over an empty record is not either.
func TestRequeuedIsTheClockAndTheRecordTogether(t *testing.T) {
	abandoned := &Abandoned{After: 17 * time.Minute}
	if allowed, recorded, requeue := Requeued(abandoned, func() int { return 45 }); !requeue ||
		allowed != 17*time.Minute || recorded != 45 {
		t.Errorf("an exhaustion with a record = (%s, %d, %v), want (17m0s, 45, true)", allowed, recorded, requeue)
	}
	if _, _, requeue := Requeued(abandoned, func() int { return 0 }); requeue {
		t.Error("an exhaustion with an empty record was requeued; that is the same cold start again")
	}
	if _, _, requeue := Requeued(abandoned, nil); requeue {
		t.Error("a caller that cannot say what is recorded was answered yes")
	}
	if _, _, requeue := Requeued(errNotTheClock, func() int { return 45 }); requeue {
		t.Error("an ordinary failure was requeued as an exhaustion")
	}
}

// errNotTheClock stands for every ending that is a verdict on the work.
var errNotTheClock = &plainError{"the model refused"}

type plainError struct{ text string }

func (e *plainError) Error() string { return e.text }
