package factory

import (
	"errors"
	"testing"
	"time"
)

// A task the main agent sends to a job that stands at a stop is one more
// round: it counts against the same limit as its other decisions, and once
// that is used up the task is the person's to send.
func TestAManagersTaskOnAStopCountsAsARound(t *testing.T) {
	it := handStored(handRun(AutonomyAuto))
	at := watchT0
	stop := func() {
		before := it
		outOfRounds(&it)
		at = at.Add(time.Minute)
		SettleWait(before, &it, at)
	}
	resume := func() {
		before := it
		it.State, it.Question, it.QKind, it.Asking = StateRunning, "", "", nil
		it.Stream = &Stream{Cur: 1, Phases: []Phase{it.Stream.Phases[0], {Name: "test", Kind: StageChat, State: PhaseRunning, Round: it.Stream.Phases[1].Round + 1}}}
		at = at.Add(time.Minute)
		SettleWait(before, &it, at)
	}
	for round := 1; round <= ManagerRoundsMost; round++ {
		stop()
		if err := ManagerTaskRefusal(it); err != nil {
			t.Fatalf("task %d refused: %v", round, err)
		}
		if !Offered(it, OfferByMainAgent, DoTask) {
			t.Fatalf("stop %d does not offer the main agent a task: %v", round, it.Offers)
		}
		NoteManagerTask(&it)
		resume()
		if got := it.Handoff.Rounds["test"]; got != round {
			t.Fatalf("after task %d the count is %d", round, got)
		}
	}
	stop()
	if it.Wait.key() != "you:answer" {
		t.Fatalf("the third stop reads %s, want the person", it.Wait.key())
	}
	if err := ManagerTaskRefusal(it); !errors.Is(err, ErrManagerRoundsUsed) {
		t.Fatalf("a third task from the main agent: %v", err)
	}
	if Offered(it, OfferByMainAgent, DoTask) || !Offered(it, OfferByYou, DoTask) {
		t.Fatalf("at the limit the task is offered to %v; want the person only", it.Offers)
	}
	// A job on no stop counts nothing.
	free := handStored(handRun(AutonomyAuto))
	NoteManagerTask(&free)
	if free.Handoff != nil {
		t.Fatalf("a task on a running job left a count: %+v", free.Handoff)
	}
}
