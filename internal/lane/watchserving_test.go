package lane

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── THE MACHINE THAT REALLY ANSWERED ────────────────────────────────────────
//
// A router is free to honour any of an order, and it says which way it went on
// every chunk. Until this pair the watch went on judging the stream against the
// belief of the lane at the HEAD of the order, so a stream served by somebody
// else was measured against a machine that was never asked — and a lane the
// ledger knows nothing about left the test switched off entirely, which is the
// state the reported stall sat in.

// acted is the moment a watch first says to do something, in milliseconds from
// the request, or −1 when nothing is ever done about it.
func acted(watch *Watch, start time.Time) int {
	for ms := 0; ms <= int(RoleTalk.Ceiling()/time.Millisecond); ms += 10 {
		if watch.Quiet(msIn(start, ms)).Kind != control.None {
			return ms
		}
	}
	return -1
}

// TestTheWaitIsJudgedAgainstTheLaneThatIsWriting: a stream the router landed
// somewhere else is judged by the machine that is really answering, and the
// proof is that it is judged EXACTLY as one sent to that machine in the first
// place would have been.
func TestTheWaitIsJudgedAgainstTheLaneThatIsWriting(t *testing.T) {
	start := time.Now()
	asked := NewWatch(raced(), beliefOf(400, 50), start)
	moved := NewWatch(raced(), beliefOf(400, 50), start)
	moved.Serving("B", beliefOf(4000, 50), start)
	sent := NewWatch(raced(), beliefOf(4000, 50), start)

	head, served, direct := acted(asked, start), acted(moved, start), acted(sent, start)
	if head < 0 || served < 0 || direct < 0 {
		t.Fatalf("one of the three was never acted on: %d, %d, %d", head, served, direct)
	}
	if served == head {
		t.Fatalf("naming a machine believed ten times slower left the answer at %dms", head)
	}
	if served != direct {
		t.Fatalf("the stream was acted on at %dms; a request sent to that machine would be at %dms",
			served, direct)
	}
}

// TestAWindowThatHasClosedDoesNotReopen: past the first token the first-token
// belief has nothing left to bound, and a lane named after the answer started
// must not reopen a window that has already shut.
func TestAWindowThatHasClosedDoesNotReopen(t *testing.T) {
	start := time.Now()
	watch := NewWatch(raced(), beliefOf(400, 50), start)
	watch.Token(1, 1, msIn(start, 300))
	before := watch.DeadlineAt()
	watch.Serving("B", beliefOf(60_000, 50), msIn(start, 300))
	if got := watch.DeadlineAt(); !got.Equal(before) {
		t.Fatalf("the deadline moved to %s after the answer had started", got.Sub(start))
	}
}

// TestTheGapIsJudgedAgainstTheLaneThatIsWriting is the same claim about the
// other clock: a stall mid-answer is a surprise about the machine writing, and
// judging parasail's stream against friendli's believed rate is a surprise
// about nobody.
func TestTheGapIsJudgedAgainstTheLaneThatIsWriting(t *testing.T) {
	start := time.Now()
	stall := func(build func() *Watch) int {
		watch := build()
		watch.Token(1, 1, msIn(start, 400))
		for ms := 410; ms <= 120_000; ms += 10 {
			if watch.Quiet(msIn(start, ms)).Kind != control.None {
				return ms
			}
		}
		return -1
	}
	head := stall(func() *Watch { return NewWatch(raced(), beliefOf(400, 1000), start) })
	moved := stall(func() *Watch {
		watch := NewWatch(raced(), beliefOf(400, 1000), start)
		watch.Serving("B", beliefOf(400, 4), start)
		return watch
	})
	direct := stall(func() *Watch { return NewWatch(raced(), beliefOf(400, 4), start) })
	if head < 0 || moved < 0 || direct < 0 {
		t.Fatalf("one of the three was never acted on: %d, %d, %d", head, moved, direct)
	}
	if moved == head {
		t.Fatalf("naming a machine believed to write two hundred times slower left the answer at %dms", head)
	}
	if moved != direct {
		t.Fatalf("the stall was acted on at %dms; on that machine's own belief it is %dms", moved, direct)
	}
}

// TestALaneNobodyBelievesAnythingAboutLeavesTheBeliefAlone: an empty belief is
// the honest empty answer and it must not overwrite the one thing the watch had
// to measure against.
func TestALaneNobodyBelievesAnythingAboutLeavesTheBeliefAlone(t *testing.T) {
	start := time.Now()
	kept := NewWatch(raced(), beliefOf(400, 1000), start)
	kept.Serving("B", Belief{}, start)
	bare := NewWatch(raced(), beliefOf(400, 1000), start)
	for ms := 0; ms <= int(RoleTalk.Ceiling()/time.Millisecond); ms += 50 {
		mine, theirs := kept.Quiet(msIn(start, ms)), bare.Quiet(msIn(start, ms))
		if mine.Kind != theirs.Kind {
			t.Fatalf("at %dms an unknown lane changed the answer: %d against %d", ms, mine.Kind, theirs.Kind)
		}
		if mine.Kind != control.None {
			return
		}
	}
	t.Fatal("neither watch was ever acted on")
}

// TestTheLaneOnTheActIsTheOneItWouldGoTo, whichever machine answered.
func TestTheLaneOnTheActIsTheOneItWouldGoTo(t *testing.T) {
	start := time.Now()
	watch := NewWatch(raced(), beliefOf(400, 50), start)
	watch.Serving("A", beliefOf(400, 50), start)
	for ms := 0; ms <= int(RoleTalk.Ceiling()/time.Millisecond); ms += 50 {
		if act := watch.Quiet(msIn(start, ms)); act.Kind == control.Hedge {
			if act.Lane != "B" {
				t.Fatalf("the rescue went to %q, want the alternative the frontier named", act.Lane)
			}
			return
		}
	}
	t.Fatal("nothing was acted on")
}
