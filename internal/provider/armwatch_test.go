package provider

import (
	"testing"
	"time"
)

// ── WHAT ONE ARM'S ROW SAYS ABOUT ITSELF ────────────────────────────────────

// TestTheRowSaysWhichBoundReallyEndedTheAttempt is `deadline_ms` stopping being
// fiction (`docs/design/recovery/DESIGN.md` §7, and the seam R0's call log
// reads).
//
// WHAT THE FIELD MEANT AND WHAT EVERY READER TOOK IT TO MEAN were two different
// things, and the 2026-09-10 census is the bill: 2,720 of 11,841 finished
// attempts ran more than TWICE their recorded deadline, the field reads 10,000
// on 7,937 rows, and the worst pairs `deadline_ms 10000` with `ms 937777`. It
// was never a bound on the call — it is [streamWatch.armed], the moment the
// hazard was going to start THINKING about a second machine — so a census
// reading it as "when this call was going to be ended" concluded that six
// hundred streams had been guillotined by a wall that never touched them.
//
// The two facts now travel as two fields: what was planned, and what happened.
func TestTheRowSaysWhichBoundReallyEndedTheAttempt(t *testing.T) {
	race := &hedgeRace{}
	watch := &streamWatch{race: race, armed: 10 * time.Second}

	facts, ok := watch.facts()
	if !ok {
		t.Fatal("an arm with a race has no facts to record")
	}
	if facts.deadline != 10*time.Second {
		t.Fatalf("the planned deadline reads %s, want the hazard's own figure", facts.deadline)
	}
	if facts.applied != 0 || facts.appliedWord != "" {
		t.Fatalf("an attempt no bound of ours ended claims one anyway: %s %q; an empty here is the honest reading",
			facts.applied, facts.appliedWord)
	}

	watch.boundApplied(&StreamCut{Reason: CutStalled, Waited: 45 * time.Second})
	// A SECOND BOUND IS A TIMER UNWINDING BEHIND THE FIRST, never a second
	// decision, so the row keeps the one that actually ended the stream.
	watch.boundApplied(&StreamCut{Reason: CutOverrun, Waited: 20 * time.Minute})

	facts, _ = watch.facts()
	if facts.applied != 45*time.Second || facts.appliedWord != "stalled" {
		t.Fatalf("the row says the attempt ended at %s (%q), want the mid-stream bound that fired",
			facts.applied, facts.appliedWord)
	}
	if facts.deadline != 10*time.Second {
		t.Fatalf("recording what happened overwrote what was planned (%s); they are two questions", facts.deadline)
	}
}

// TestALosingArmIsExhaustAndNotAFailure is the largest single cause family in
// the census turning out not to be a cause at all.
//
// 1,204 of 3,906 bad rows in ten days are `context canceled`, and most of them
// are the arms of a race another arm won. Nothing went wrong in any of them: the
// request was made on purpose, cut off on purpose, and the answer it was racing
// for arrived. A log that files them beside a provider's refusal is a log
// measuring this build's own hedging policy and reporting it as provider health,
// which is how "self-inflicted" came to be 57% of our failures.
func TestALosingArmIsExhaustAndNotAFailure(t *testing.T) {
	watch := &streamWatch{race: &hedgeRace{}}

	facts, _ := watch.facts()
	if facts.exhaust || facts.note != "" {
		t.Fatalf("an arm that has lost nothing is already marked exhaust (%v, %q)", facts.exhaust, facts.note)
	}

	watch.lostRace()
	facts, _ = watch.facts()
	if !facts.exhaust {
		t.Fatal("an arm cancelled because another answered first is not marked as exhaust, " +
			"so the census goes on counting a won race as a failure")
	}
	if facts.note != exhaustNote {
		t.Fatalf("the row says %q, want %q; a census keys on the sentence until the outcome field lands",
			facts.note, exhaustNote)
	}
}

// TestARaceWithSomethingOfItsOwnToSayKeepsSayingIt is the narrow half of the
// rule above: the exhaust sentence is a DEFAULT and never an override. A race
// that recorded something specific about this call is saying the more useful
// thing, and a generic note written over the top of it would lose the only row
// that had it.
func TestARaceWithSomethingOfItsOwnToSayKeepsSayingIt(t *testing.T) {
	const own = "the account excludes this machine"
	watch := &streamWatch{race: &hedgeRace{note: own}}
	watch.lostRace()
	facts, _ := watch.facts()
	if facts.note != own {
		t.Fatalf("the row says %q, want the race's own sentence kept", facts.note)
	}
	if !facts.exhaust {
		t.Fatal("the arm still lost the race; only the sentence belongs to somebody else")
	}
}
