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

// THE MAIN AGENT'S OWN MOVE NEVER WIPES ITS COUNT. A stop already with the
// person (the main agent's turn ended without settling it) that the main agent
// then moves, by a task or a decision, counts one more round: the count does
// not start again, so the bound still brings the next stop to the person. The
// person's own move still wipes it.
func TestTheMainAgentsMoveOnAStopWithThePersonKeepsTheCount(t *testing.T) {
	// stopWithPerson is a job out of rounds on test that the main agent has
	// put back to work once, whose stop is now with the person.
	stopWithPerson := func() (Item, time.Time) {
		it := handStored(handRun(AutonomyAuto))
		before := it
		outOfRounds(&it)
		at := watchT0.Add(time.Minute)
		SettleWait(before, &it, at)
		if it.Wait.Who != WaitMainAgent {
			t.Fatalf("the stop reads %s", it.Wait.key())
		}
		before = it
		next := it.Handoff.Ended(HandoffLate)
		next.Rounds = map[string]int{"test": 1}
		it.Handoff = &next
		at = at.Add(time.Minute)
		SettleWait(before, &it, at)
		if it.Wait.key() != "you:answer" || it.Handoff.Rounds["test"] != 1 {
			t.Fatalf("the stop with the person: %s %+v", it.Wait.key(), it.Handoff)
		}
		return it, at
	}
	moveOn := func(it *Item, at time.Time) {
		it.State, it.Question, it.QKind, it.Asking = StateQueued, "", "", nil
		it.Stream = &Stream{Cur: 1, Phases: []Phase{it.Stream.Phases[0], {Name: "test", Kind: StageChat, State: PhasePending, Round: it.Stream.Phases[1].Round + 1}}}
	}

	// A task: counted and moved in the one write.
	it, at := stopWithPerson()
	if err := ManagerTaskRefusal(it); err != nil {
		t.Fatalf("the task was refused: %v", err)
	}
	before := it
	NoteManagerTask(&it)
	moveOn(&it, at)
	SettleWait(before, &it, at.Add(time.Minute))
	if it.Handoff == nil || it.Handoff.Rounds["test"] != 2 {
		t.Fatalf("after the main agent's task the count is %+v, want 2", it.Handoff)
	}
	// The bound holds: the next stop is the person's.
	before = it
	outOfRounds(&it)
	SettleWait(before, &it, at.Add(2*time.Minute))
	if it.Wait.key() != "you:answer" || !errors.Is(ManagerTaskRefusal(it), ErrManagerRoundsUsed) {
		t.Fatalf("the stop after the bound reads %s", it.Wait.key())
	}

	// A decision: the door moves the job in a write of its own, then the
	// decision is counted with the count it had kept.
	it, at = stopWithPerson()
	held := it.Handoff
	kind, step := ManagerStop(it), ManagerStopStep(it)
	before = it
	moveOn(&it, at)
	SettleWait(before, &it, at.Add(time.Minute))
	before = it
	KeepManagerRounds(&it, held)
	NoteManagerDecision(&it, kind, step, DecideAnotherRound, "Fix the import cycle first.", at)
	SettleWait(before, &it, at.Add(time.Minute))
	if it.Handoff == nil || it.Handoff.Rounds["test"] != 2 {
		t.Fatalf("after the main agent's decision the count is %+v, want 2", it.Handoff)
	}

	// The person's own move wipes it.
	it, at = stopWithPerson()
	before = it
	moveOn(&it, at)
	SettleWait(before, &it, at.Add(time.Minute))
	if it.Handoff != nil {
		t.Fatalf("the person's move kept the count: %+v", it.Handoff)
	}
}
