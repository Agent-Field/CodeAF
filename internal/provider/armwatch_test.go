package provider

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/control"
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

// scriptedController answers every question with one act, so a test can put the
// watch in the exact state the reported defect was found in without staging a
// whole race. It is the controller's contract and nothing else.
type scriptedController struct{ act control.Act }

func (s scriptedController) Note(control.Reading) control.Act                              { return s.act }
func (s scriptedController) Quiet(time.Time) control.Act                                   { return s.act }
func (s scriptedController) Serving(string, control.Survival, control.Survival, time.Time) {}
func (s scriptedController) Deadline() time.Time                                           { return time.Time{} }
func (s scriptedController) Phase() control.Phase                                          { return control.Phase(0) }
func (s scriptedController) Acted(control.Kind) bool                                       { return false }

// TestACeilingThePurseRefusedStillActsOnADeadPath is the hole the coordinator
// found on 2026-09-10, closed.
//
// THE SHAPE: four quick tasks, one machine, first tokens at 260, 370, 375 and
// 428 seconds. On every one the hazard fired at its ten-second ceiling with
// `no heartbeat` — not a token, not a router comment — and the row says
// `refused: budget`. The purse (two rescues per twenty calls) declined the arm,
// and then NOTHING acted for another six minutes. Acting meant hedging, hedging
// was purse-gated, and so the waiting design's §A clause 1 — the ceiling is hard
// "regardless of belief" — quietly meant "when we can afford it".
//
// A hedge is the PAID way to act and a cut is the FREE one. The ceiling now
// picks one of them and never neither.
func TestACeilingThePurseRefusedStillActsOnADeadPath(t *testing.T) {
	guard := &stallWatch{cancel: func() {}, clock: time.Now}
	guard.born = guard.clock()
	guard.quietSince = guard.born
	watch := &streamWatch{
		race:    &hedgeRace{},
		control: scriptedController{act: control.Act{Kind: control.Report, Silence: 10 * time.Second}},
		began:   guard.born,
	}
	guard.arm = watch
	watch.guardedBy(guard)

	watch.quiet(guard.born.Add(10 * time.Second))

	cut := guard.cut()
	if cut == nil {
		t.Fatal("the ceiling fired, the purse refused the arm, and nothing at all happened — " +
			"which is six minutes of a person watching one silent machine")
	}
	if cut.Reason != CutSilent {
		t.Fatalf("the cut says %q, want a request that produced nothing", cut.Reason.word())
	}
	if cut.Waited != 10*time.Second {
		t.Fatalf("the cut says it waited %s, want the ceiling that fired", cut.Waited)
	}
	// AND THE ROW KNOWS WHICH BOUND IT WAS, so the next census can tell a
	// ceiling that acted from a silence bound that expired.
	facts, _ := watch.facts()
	if facts.applied != 10*time.Second || facts.appliedWord != "silent" {
		t.Fatalf("the row records %s (%q), want the ceiling", facts.applied, facts.appliedWord)
	}
}

// TestAPurseRefusalOnALiveWireIsLeftAlone is the other side, and the one that
// keeps the rule above from being a disaster.
//
// Of the 2,186 attempts whose ceiling fired and whose purse refused, those with
// a live wire under them — `drift`, `ceiling` — ended cleanly 92% and 75% of the
// time. Cutting those would throw away nine calls in ten that were about to
// answer and pay every prompt again. Only `no heartbeat` is worth cutting, and
// it is worth cutting because it ends cleanly 31% of the time against first
// tokens whose ninety-ninth percentile is 505 seconds.
func TestAPurseRefusalOnALiveWireIsLeftAlone(t *testing.T) {
	guard := &stallWatch{cancel: func() {}, clock: time.Now}
	guard.born = guard.clock()
	guard.quietSince = guard.born
	watch := &streamWatch{
		race:    &hedgeRace{},
		control: scriptedController{act: control.Act{Kind: control.Report, Silence: 10 * time.Second}},
		began:   guard.born,
	}
	guard.arm = watch
	watch.guardedBy(guard)
	// The router is speaking: a comment line arrived, so the path is alive and
	// the model is merely slow.
	watch.note(control.Reading{Beat: true, At: guard.born.Add(time.Second)})

	watch.quiet(guard.born.Add(10 * time.Second))

	if cut := guard.cut(); cut != nil {
		t.Fatalf("a stream whose router was still speaking was cut at the ceiling (%q) — "+
			"nine of those in ten were about to answer", cut.Error())
	}
}

// TestALateFirstTokenStillTeachesTheLedgerAboutItsMachine is the second half of
// the same evening.
//
// A sighting is dropped when the act was charged to the path, because there is
// no fact about a machine in a stream nothing ever reached. But the claim is
// made from what had arrived AT THE MOMENT OF THE ACT, and a stream that went on
// to write from a named machine has disproved it. Those four streams each named
// their machine and each eventually wrote — and every one of them was dropped
// here, so the sheet went on saying that machine answers in eight seconds while
// this build watched it take seven minutes, four times in a row.
func TestALateFirstTokenStillTeachesTheLedgerAboutItsMachine(t *testing.T) {
	began := time.Now()
	watch := &streamWatch{race: &hedgeRace{}, began: began}
	watch.served = "slowmachine"
	watch.fault = true // the act at the ceiling was charged to the path
	watch.first = began.Add(370 * time.Second)
	watch.last = watch.first.Add(5 * time.Second)

	sighting, ok := watch.sighting("sim/model", 200)
	if !ok {
		t.Fatal("a machine that named itself and then took six minutes to its first token taught the ledger nothing")
	}
	if sighting.ID.Lane != "slowmachine" {
		t.Fatalf("the sighting is filed against %q", sighting.ID.Lane)
	}
	if sighting.TTFT != 370*time.Second {
		t.Fatalf("the sighting says the first token took %s", sighting.TTFT)
	}
	if sighting.TTFT <= LagTTFT {
		t.Fatalf("a %s first token is not over the lag line of %s — this test is asking nothing", sighting.TTFT, LagTTFT)
	}
}
