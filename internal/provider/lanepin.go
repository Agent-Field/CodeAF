package provider

import (
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
