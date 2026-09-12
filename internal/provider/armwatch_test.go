package provider

import (
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
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
// `refused: plan cannot pay`. The purse declined the arm,
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

// ── A STREAM TEACHES THE LEDGER WHILE IT IS STILL RUNNING ──────────────────

// partialLedger records every sighting the ledger is handed, so a test can see
// what a running stream taught and when. The belief reads it does not answer —
// this seam only ever writes.
type partialLedger struct {
	mu      sync.Mutex
	sighted []lanes.Sighting
}

func (l *partialLedger) Note(s lanes.Sighting) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sighted = append(l.sighted, s)
}
func (l *partialLedger) NoteOutcome(lanes.Outcome)            {}
func (l *partialLedger) Belief(lanes.ID) (lanes.Belief, bool) { return lanes.Belief{}, false }
func (l *partialLedger) Beliefs(string) []lanes.Belief        { return nil }
func (l *partialLedger) Prime(lanes.Row, float64)             {}
func (l *partialLedger) noted() []lanes.Sighting {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]lanes.Sighting(nil), l.sighted...)
}

// TestASlowStreamTeachesTheLedgerMidStream is the 14:29–14:32 incident of
// 2026-09-11: one Morph endpoint wrote for 86 seconds and then 363, and the
// belief the NEXT pick ranked on still described that machine's morning, so the
// retry chose the same collapsed endpoint the first call was at that moment
// still being throttled by.
//
// THE DEFECT WAS ONE DOOR OPENING ONLY AT THE END. Every lane.Sighting was
// built at settlement — [Client.noteVelocity] from the epilogue, or
// [streamWatch.sighting] for a losing arm — so a stream producing tokens
// throughout taught the ledger nothing until it closed. The chooser's refusal
// (beyondThePatience → provider.ignore) reads the belief, and the belief had
// not been told.
//
// THE FIX IS A CHAIN OF NON-OVERLAPPING WINDOWS. The read loop already holds
// the live figures; it now folds a partial sighting in at most once a minute,
// each window covering only the span since the one before it, and the
// settlement sighting is rebased to cover only what no partial claimed — so one
// stream is one measurement, told in chapters, never the same span twice.
func TestASlowStreamTeachesTheLedgerMidStream(t *testing.T) {
	ledger := &partialLedger{}
	previous := lanes.Default().Ledger()
	lanes.Default().SetLedger(ledger)
	t.Cleanup(func() { lanes.Default().SetLedger(previous) })

	began := time.Now()
	race := &hedgeRace{model: "deepseek/deepseek-v4.1-flash"}
	watch := &streamWatch{race: race, began: began}
	// A running arm always has its controller; install the scripted one rather
	// than driving a whole race for what is a read-loop measurement.
	watch.control = scriptedController{}
	watch.served = "Morph"
	watch.first = began.Add(4 * time.Second) // ttft 3993ms in the incident

	// THE STREAM WRITES FOR MINUTES. Eighty-six seconds of steady output —
	// sixty rated tokens a minute apart, so both the cadence and the rate floor
	// are crossed — with no settle anywhere in it.
	at := watch.first
	feed := func(tokens int, span time.Duration) {
		at = at.Add(span)
		watch.note(control.Reading{Visible: tokens, At: at})
	}
	feed(ratedFloor, 30*time.Second) // 0:30 — under the cadence, teaches nothing
	feed(ratedFloor, 31*time.Second) // 1:01 — a minute of stream: first window
	feed(ratedFloor, 61*time.Second) // 2:02 — second minute: second window

	taught := ledger.noted()
	if len(taught) != 2 {
		t.Fatalf("a stream writing for two minutes taught the ledger %d times, want the two minute-windows", len(taught))
	}

	// EACH WINDOW IS ITS OWN SPAN, NOT A CUMULATIVE RE-MEASUREMENT. The first
	// covers first-token to one minute in; the second covers the next minute.
	// Overlapping windows would tell the belief one slow minute two and three
	// times — which is exactly the double-count settlement is rebased to avoid.
	first, second := taught[0], taught[1]
	if first.ID.Lane != "Morph" {
		t.Fatalf("the partial is filed against %q, want the machine that is writing", first.ID.Lane)
	}
	if first.Gen <= 0 || second.Gen <= 0 {
		t.Fatalf("a window with no span teaches nothing: %s and %s", first.Gen, second.Gen)
	}
	if first.Gen > 61*time.Second {
		t.Fatalf("the first window claims %s — it may cover only its own minute, not the whole stream", first.Gen)
	}
	if !second.At.After(first.At) {
		t.Fatalf("the windows are not in stream order: %s then %s", first.At, second.At)
	}
	// The two windows together account for the whole generation window and no
	// more: disjoint spans of one stream.
	if gap := second.At.Sub(first.At); gap < time.Minute {
		t.Fatalf("the second window opened %s after the first — under the one-minute cadence", gap)
	}

	// AND SETTLEMENT DOES NOT TEACH THE SAME SPAN TWICE. The stream now closes,
	// and its final sighting must cover only the tail no partial claimed.
	watch.last = at
	settled, ok := watch.sighting("deepseek/deepseek-v4.1-flash", watch.tokens)
	if !ok {
		t.Fatal("a stream that wrote for two minutes produced no settlement sighting")
	}
	// The whole stream ran ~2 minutes past its first token; the partials already
	// claimed that span, so the settlement's generation window is the empty tail.
	if settled.Gen > 2*time.Second {
		t.Fatalf("settlement re-taught %s the partials already covered — one stream counted twice", settled.Gen)
	}
	if settled.Tokens > watch.tokens {
		t.Fatalf("settlement claims %d tokens of a %d-token stream", settled.Tokens, watch.tokens)
	}
	// And it is still one honest sighting: the wait to the first token is told
	// once, here, because no partial could measure it before it happened.
	if settled.TTFT != 4*time.Second {
		t.Fatalf("the settlement lost the first-token wait: %s", settled.TTFT)
	}
}

// TestAHealthyFastStreamTeachesNothingEarly is the case the cadence exists to
// protect: a lane that answers in nine seconds is measured once, at settle,
// exactly as it always was. The mid-stream door is for the stream that runs
// long enough to be worth interrupting the belief over — never for the ordinary
// quick answer, which would only add noise.
func TestAHealthyFastStreamTeachesNothingEarly(t *testing.T) {
	ledger := &partialLedger{}
	previous := lanes.Default().Ledger()
	lanes.Default().SetLedger(ledger)
	t.Cleanup(func() { lanes.Default().SetLedger(previous) })

	began := time.Now()
	race := &hedgeRace{model: "sim/model"}
	watch := &streamWatch{race: race, began: began}
	watch.control = scriptedController{}
	watch.served = "quickmachine"
	watch.first = began.Add(800 * time.Millisecond)

	// A normal fast stream: eighty tokens over nine seconds, well under the
	// one-minute cadence.
	at := watch.first
	for i := 0; i < 8; i++ {
		at = at.Add(time.Second)
		watch.note(control.Reading{Visible: 10, At: at})
	}

	if got := ledger.noted(); len(got) != 0 {
		t.Fatalf("a nine-second answer taught the ledger %d times mid-stream — the cadence gate is off", len(got))
	}
	// And settlement measures it whole, unchanged.
	watch.last = at
	settled, ok := watch.sighting("sim/model", 80)
	if !ok || settled.Gen != 8*time.Second {
		t.Fatalf("the fast stream's settlement is wrong: ok=%v gen=%s", ok, settled.Gen)
	}
}
