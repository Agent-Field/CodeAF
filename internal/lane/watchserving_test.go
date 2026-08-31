package lane

import (
	"testing"
	"time"
)

// ── THE MACHINE THAT REALLY ANSWERED ────────────────────────────────────────
//
// A router is free to honour any of an order, and it says which way it went on
// every chunk. Until this pair the watch went on measuring gaps against the
// belief of the lane at the HEAD of the order, so a stream served by somebody
// else was judged by a rate belonging to a machine that was never asked — and
// a lane the ledger knows nothing about left the drift test switched off
// entirely, which is the state the reported stall sat in.

func TestTheWatchFollowsTheLaneTheStreamNames(t *testing.T) {
	start := time.Now()
	// Asked for A, believed quick and prolific; served by B, believed slow.
	watch := NewWatch(
		Choice{Order: []string{"A"}, Alt: "C", Deadline: time.Second},
		beliefOf(400, 1000),
		start,
	)
	watch.Serving("B", beliefOf(400, 1000), start)
	watch.Token(1, 1, msIn(start, 400))
	// One millisecond is B's believed gap, so sixty of them is a drift alarm
	// about B — which is only reachable because the belief moved.
	if verdict := watch.Silence(msIn(start, 460)); !verdict.Hedge || verdict.Reason != "drift" {
		t.Fatalf("verdict = %+v, want the stall judged against the lane that is writing", verdict)
	}
}

func TestADeadlineIsRecomputedForTheLaneThatAnswers(t *testing.T) {
	start := time.Now()
	// The chooser solved a deadline for A. B is a much slower machine, and a
	// stream that landed on it must not be called late at A's figure.
	watch := NewWatch(
		Choice{Order: []string{"A"}, Alt: "C", Deadline: 900 * time.Millisecond},
		beliefOf(400, 50),
		start,
	)
	watch.Serving("B", beliefOf(4000, 50), start)
	if watch.Deadline() <= 900*time.Millisecond {
		t.Fatalf("Deadline = %s, want B's own believed wait rather than A's", watch.Deadline())
	}
	if verdict := watch.Silence(msIn(start, 1000)); verdict.Hedge {
		t.Fatalf("called B late at A's deadline: %+v", verdict)
	}
}

func TestTheDeadlineIsNotReopenedOnceTheAnswerHasStarted(t *testing.T) {
	start := time.Now()
	watch := NewWatch(
		Choice{Order: []string{"A"}, Alt: "C", Deadline: 900 * time.Millisecond},
		beliefOf(400, 50),
		start,
	)
	watch.Token(1, 1, msIn(start, 300))
	before := watch.Deadline()
	watch.Serving("B", beliefOf(4000, 50), msIn(start, 300))
	if watch.Deadline() != before {
		t.Fatalf("Deadline moved to %s after the first token; a window that has closed does not reopen", watch.Deadline())
	}
}

func TestALaneNobodyBelievesAnythingAboutLeavesTheBeliefAlone(t *testing.T) {
	start := time.Now()
	watch := NewWatch(
		Choice{Order: []string{"A"}, Alt: "C", Deadline: time.Second},
		beliefOf(400, 1000),
		start,
	)
	// An empty belief is the honest empty answer and it must not overwrite the
	// one thing the watch had to measure against.
	watch.Serving("B", Belief{}, start)
	watch.Token(1, 1, msIn(start, 400))
	if verdict := watch.Silence(msIn(start, 460)); !verdict.Hedge {
		t.Fatalf("the drift test went quiet when an unknown lane named itself: %+v", verdict)
	}
}
