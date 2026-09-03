package provider

import (
	"context"
	"strings"
	"sync"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── WHAT A PERSON SAID ABOUT THE MACHINE ────────────────────────────────────
//
// `internal/lane` decides which endpoint a request should prefer. This file is
// the one place a PERSON'S answer to the same question overrides it: the
// `lane.<slot>` row and the `lane.guard` row (internal/config's settings.go),
// resolved once by the surface and handed down.
//
// IT IS A PROCESS-WIDE KNOB AND NOT A FIELD ON Config, for [SetHedgeBudget]'s
// reason and one more. The reason it shares: the row is about a SESSION and not
// about an adapter, and two clients in one process — the conversation's and a
// tool loop's own — must not hold two different answers to "which machine did
// they ask for". The reason of its own: the row is written WHILE THE PROCESS IS
// RUNNING, by the picker (internal/tui3's pinLane), and a pin that only took
// effect at the next launch would be a promise this build did not keep. A
// settings read per request would keep it too — at the cost of a disk read in
// front of every first token, which is the one thing the lane law forbids
// hardest — so the surface WRITES the answer here when it writes the row.
//
// The read is a mutex over two words on a path that already takes several, and
// the write happens when somebody presses enter in a list.

// LanePin is a person's answer to "which machine serves this model", already
// resolved from the settings row.
//
// The three states of the row are three states here, and the zero value is
// `auto` — nobody said, and the belief chooses per answer.
//
//	auto          Lane empty, OpenRouter false   the belief chooses
//	pinned        Lane named, Borrow false       exactly that machine, or nothing
//	pinned+borrow Lane named, Borrow true        that machine first, and a rescue may leave it
//	openrouter    OpenRouter true                no lane choice at all; the router balances
type LanePin struct {
	// Lane is the machine named by the row, as the wire spells it. Empty is a
	// row that names none.
	Lane string
	// Borrow is whether a PINNED lane may still be left when it goes slow. It
	// is false unless somebody said so: a pin means the machine they named, and
	// widening it on their behalf is not this build's to do.
	Borrow bool
	// OpenRouter is the row that asks for NO lane at all and lets the router
	// balance on price, which is what this build did before it held an opinion.
	// It is a different fact from `auto`, which asks the belief to choose.
	OpenRouter bool
}

// pinned reports whether this row names one machine.
func (p LanePin) pinned() string { return strings.TrimSpace(p.Lane) }

var (
	lanePinMu sync.RWMutex
	lanePin   LanePin
	// laneGuard is the `lane.guard` row: whether a slow answer is worth one
	// extra call to rescue. It defaults to ON, which is the row's own default
	// (internal/config's DefaultLaneGuard) said once more where the mechanism
	// that reads it lives.
	laneGuard = true
)

// SetLanePin states which machine this process's conversation asked for. It is
// called from wherever the routing row is resolved — the surface, once, at
// launch — and again by the picker when somebody pins from it.
func SetLanePin(pin LanePin) {
	pin.Lane = strings.TrimSpace(pin.Lane)
	lanePinMu.Lock()
	defer lanePinMu.Unlock()
	// A ROW THAT CHANGED FORGETS EVERY REFUSAL THIS RUN COLLECTED, and a row
	// RESTATED forgets nothing. See the retirement block at the foot of this
	// file: a person naming a machine is them stating the instruction afresh,
	// and a build that answered "no, the router said no an hour ago" would be
	// arguing with somebody who has just told it to try again.
	//
	// THE SECOND HALF OF THAT SENTENCE IS THE ONE THAT WAS MEASURED. This
	// setter is not only the picker: it is also every place that RESOLVES the
	// row — the door at launch, and the standing ticker, which rebuilds a whole
	// posture every five minutes for as long as the window lives
	// (cmd/aforge's v3StandingTicker). An experiment build that cleared here
	// unconditionally therefore forgot the refusal every five minutes and paid
	// the identical 404 again: one at launch, one at 16:30:02, one at 16:35:00,
	// in one process, in one measured run. Nobody had touched the row.
	if pin != lanePin {
		retiredPins = map[string]struct{}{}
	}
	lanePin = pin
}

// CurrentLanePin is the pin in force.
func CurrentLanePin() LanePin {
	lanePinMu.RLock()
	defer lanePinMu.RUnlock()
	return lanePin
}

// SetLaneGuard turns the speed guard on or off, and it is the ONE switch: it
// moves the hedge budget and the probe together, because both are the same
// promise to a person — that this build may spend a little extra to keep an
// answer moving — and a row that turned off half of it would be a row nobody
// could reason about.
//
// OFF IS A BUDGET THAT ALLOWS NOTHING rather than a flag the race consults. The
// budget is already the one gate every hedge passes through ([hedgeRace.hedge]),
// so a zero allowance is the whole of "do not rescue" with no second path to
// keep in step. The probe reads the flag directly, because a probe is not
// budgeted in dollars — it is gated on whether anybody is waiting.
func SetLaneGuard(on bool) {
	lanePinMu.Lock()
	laneGuard = on
	lanePinMu.Unlock()
	if on {
		SetHedgeBudget(nil)
		return
	}
	SetHedgeBudget(lanes.NewBudget(0, 0))
}

// LaneGuardOn reports whether the speed guard is on.
func LaneGuardOn() bool {
	lanePinMu.RLock()
	defer lanePinMu.RUnlock()
	return laneGuard
}

// ── A PIN THE WIRE HAS ALREADY REFUSED ──────────────────────────────────────
//
// Issue #456, and the ruling #375 asked for. As shipped, a strict pin puts
// `provider.only: [name]` on EVERY request; the router answers the
// `permits only:` 404; and the next request demands the same machine again.
// Every turn of a session pays a 404 round trip to learn a fact this process
// already knows — and on the turn's own unhedged call, with nowhere to walk to,
// the turn simply ended and the person read the router's sentence.
//
// A PIN IS RETIRED FOR THE MODEL IT WAS REFUSED FOR. A `permits only:` refusal
// is not an outage and not a slow lane: it is the router saying the PAIRING is
// impossible, which is exactly the one refusal class the object next door calls
// Terminal. So the pair is written down here, and every later request for that
// model routes on auto — exactly as if the row read `auto`.
//
// FOUR THINGS IT DELIBERATELY DOES NOT DO. It does not touch the settings file:
// the row a person wrote is theirs and stays as they wrote it. It does not
// retire the pin for any OTHER model, because the refusal was about a pairing.
// It does not outlive the process. And it does not survive somebody CHANGING
// the row — [SetLanePin] forgets every retirement when the answer moves,
// because pinning is a person stating the instruction afresh and this build
// does not get to remember an argument they have just re-opened.
//
// THE POLICY LIVES HERE AND NOT IN refusalobject.go. That file answers what a
// refusal IS — a fact about the wire — and what a person's standing instruction
// should do about it is a fact about the ROW, which is this file's subject.

// retiredPins is the set of (pinned lane, ledger model) pairs the router has
// refused this run, guarded by [lanePinMu] alongside the knob itself because
// the two are read together on the same request path and a second lock would be
// a second thing to reason about in front of somebody's first token.
//
// IT IS KEYED BY THE PAIR AND NOT BY THE LANE. "cloudflare cannot serve this
// model" is a fact about one model, and a person pinning cloudflare for a
// second model is asking a question this refusal did not answer.
//
// AND IT IS PER RUN RATHER THAN PER TURN OR PER SESSION, which is the whole of
// what makes the 404 get paid once. A conversation, the errands beside it and
// every task node it starts are one process and read this one map; a build that
// kept the fact on an agent would re-pay the round trip for every node it ran.
var retiredPins = map[string]struct{}{}

// retiredPinLines are the sentences a person is owed and has not been told yet.
//
// A LINE IS PARKED BECAUSE THE CALL THAT LEARNED THE FACT IS OFTEN NOT A CALL
// ANYBODY IS READING. The first `permits only:` refusal of a run is collected
// by the errand that runs at launch (internal/session's auxiliary.go), whose
// context carries no stream observer and no report slot at all — so the news an
// experiment build posted there went nowhere, and every later request was
// already retired and had nothing to say. The fact is learned once, wherever it
// is learned; the sentence waits for the next request a person is actually
// watching ([tellRetiredPins]).
var retiredPinLines []string

// retiredPinKey folds one pairing into the key both halves of this file use.
//
// THE MODEL IS FOLDED THROUGH [laneModel] for the reason the serving set is
// (refusalobject.go's refuseServing): the bare id and the dated slug are two
// spellings of one model, and a pair written under one and read under the other
// would rebuild that disagreement one layer down.
func retiredPinKey(lane, model string) string {
	return strings.ToLower(strings.TrimSpace(lane)) + "\x00" + laneModel(model)
}

// retirePin writes one terminal refusal of a person's own pin down, and answers
// whether THIS call is the one that retired the pair.
//
// The answer is what keeps the sentence a person reads to exactly one: a turn
// that refuses twice, or a ladder that refuses on its way up, must not say the
// same thing twice about the same pairing.
//
// IT REFUSES TO ACT ON A MACHINE NOBODY PINNED. A rescue arm demands a lane of
// its own, and a refusal of that is the race's business (hedge.go) rather than
// a person's row. Only a refusal of the lane the pin in force NAMES, and only
// while that pin is strict, retires anything: a borrowable pin sends no demand
// of its own, so a refusal naming it was never the row speaking.
func retirePin(lane, model string) bool {
	lane, model = strings.TrimSpace(lane), strings.TrimSpace(model)
	if lane == "" || model == "" {
		return false
	}
	// The key is folded OUTSIDE the lock: [laneModel] reaches into the ledger's
	// own normaliser, and calling a neighbouring package while holding this
	// file's lock is how a deadlock is built by somebody else's later edit.
	key := retiredPinKey(lane, model)
	lanePinMu.Lock()
	defer lanePinMu.Unlock()
	if lanePin.Borrow || !strings.EqualFold(lanePin.pinned(), lane) {
		return false
	}
	if _, already := retiredPins[key]; already {
		return false
	}
	retiredPins[key] = struct{}{}
	return true
}

// pinRetired reports whether the pin in force has already been refused for this
// model, in which case the request goes out on auto.
func pinRetired(lane, model string) bool {
	if strings.TrimSpace(lane) == "" {
		return false
	}
	key := retiredPinKey(lane, model)
	lanePinMu.RLock()
	defer lanePinMu.RUnlock()
	_, retired := retiredPins[key]
	return retired
}

// forgetRetiredPins empties the set and anything parked. It is for tests, which
// must not inherit one another's refusals — the shipped path forgets through
// [SetLanePin], and only when the row it is handed is a different row.
func forgetRetiredPins() {
	lanePinMu.Lock()
	defer lanePinMu.Unlock()
	retiredPins = map[string]struct{}{}
	retiredPinLines = nil
}

// RescueRetired is the word a retirement travels under: the machine a person
// pinned said it will not serve this model, so nothing more is demanded of it
// and this model routes on auto until they pin again.
//
// IT IS A REFUSED REASON AND NOT A THIRD KIND OF WAIT. [RescueRefused] is the
// same wire fact about a machine the RACE demanded; this one is that fact about
// a machine a PERSON demanded, and only a person's own row earns a sentence
// about their own row. It is spelled here rather than beside its two neighbours
// in refusalobject.go because it is the pin's word and that file holds no pin
// policy.
const RescueRetired = "retired"

// retiredPinLine is the whole sentence, and it is spelled ONCE, here.
//
// Two surfaces say it — the status line while the answer is in flight
// (internal/tui3's laneRider) and the conversation, which keeps it — and a
// sentence spelled in two places is a sentence that gets reworded in one. The
// machine is named the way the person spelled it when they pinned it, because
// that is the row they will go and look at.
// RetiredPinLine is that sentence for a surface that has to draw it beside its
// own furniture (internal/tui3's laneRider). It is exported rather than copied
// for the reason the two words above it are constants: three packages would
// otherwise spell one sentence, and a sentence spelled in three places is a
// sentence that gets reworded in one.
func RetiredPinLine(lane string) string { return retiredPinLine(lane) }

func retiredPinLine(lane string) string {
	return lane + " cannot serve this model; routing on auto for this model until you pin again"
}

// retirePinnedLane is what a TERMINAL refusal does to a person's own pin, and
// it answers whether this call is the one that retired the pairing.
//
// IT IS CALLED FROM THE FORK EVERY ROUTING REFUSAL PASSES THROUGH
// (client.go's [Client.sendRecovered]), beside the strike and before either
// recovery runs: the walk takes one road out of that line and the ladder the
// other, and a policy written on one of the two roads would be a policy half
// the refusals never met. It asks the one classifier through the entrance for a
// caller that holds a request (refusalobject.go), so nothing about what this
// refusal MEANS is decided twice.
//
// AND THE PERSON IS TOLD ONCE. Two sentences leave here for one fact, and they
// are two GRAINS of it rather than two claims: the rider is what the status
// line shows while the answer is still coming, and the parked line is the one
// that stays in the conversation afterwards — which is the one that was
// missing. [retirePin] answers whether this call is the one that retired the
// pairing, so a run that collects the same refusal twice says nothing the
// second time.
func retirePinnedLane(ctx context.Context, model string, refusal laneRefusal) bool {
	if !refusal.Terminal || !retirePin(refusal.Lane, model) {
		return false
	}
	HedgeReportFrom(ctx).tell(RescueNews{Alt: refusal.Lane, Reason: RescueRetired, Failed: true})
	lanePinMu.Lock()
	retiredPinLines = append(retiredPinLines, retiredPinLine(refusal.Lane))
	lanePinMu.Unlock()
	tellRetiredPins(ctx)
	return true
}

// tellRetiredPins hands whatever is parked to a call somebody is reading, and
// does nothing at all on one they are not.
//
// TWO CONDITIONS, AND BOTH ARE ABOUT THE READER RATHER THAN THE CALL. There has
// to be a stream to say it on ([Emit] is silent without one), and the errand
// this call belongs to has to be one a person is watching — a naming errand and
// a memory reflex both answer during an ordinary talk turn, and a line about
// somebody's own row delivered into a stream nobody reads is a line that was
// never said. So the sentence waits, and the very next request of the
// conversation carries it (client.go's [Client.sendShaped]).
func tellRetiredPins(ctx context.Context) {
	if ctx == nil || streamObserverFrom(ctx) == nil || !RoleFrom(ctx).Visible() {
		return
	}
	lanePinMu.Lock()
	owed := retiredPinLines
	retiredPinLines = nil
	lanePinMu.Unlock()
	for _, line := range owed {
		Emit(ctx, StreamNotice, line)
	}
}
