// Package effort is the reasoning-depth ladder: five named rungs, one resolver,
// and nothing else.
//
// It exists because "how hard should the model think about this" was already
// being answered in half a dozen unrelated places — a level held per model id on
// the session, a `:high` suffix on a crew tier value, a hardcoded effort at
// every errand and sentinel — and each of them was a different vocabulary for
// one question. A person who dialled their conversation up found their tasks
// unchanged, and a lane adding the next spawn site had nowhere to look for the
// answer.
//
// So the ladder is written down ONCE, here, and the answer is computed ONCE, by
// [Resolve]. Every model call in this process reads its depth through that
// function; a spawn site that decides for itself is the defect this package was
// written to end.
//
// THIS PACKAGE HOLDS NO PROVIDER VOCABULARY AND IMPORTS NOTHING. A rung is a
// word about how hard to think, not a request field: translating one into the
// shape a wire accepts is the adapter's job and happens nowhere above it
// (internal/provider's effortladder.go). That is what lets the ladder grow a
// rung the wire has no word for.
package effort

import "strings"

// Rung is one step of the ladder.
type Rung string

const (
	// None is absence rather than a rung. Nothing is asked for and the model
	// thinks however it thinks, which is what an unconfigured install sends and
	// the only value that leaves a request byte-for-byte what it was.
	//
	// It is NOT "think as little as possible" — that is a different request, it
	// suppresses the thinking pass outright, and some endpoints refuse it. The
	// adapter owns that shape and this ladder never reaches for it.
	None Rung = ""

	Low    Rung = "low"
	Medium Rung = "medium"
	High   Rung = "high"

	// XHigh and Max are the two rungs above the word ladder every provider
	// shares. "high" is the top of that vocabulary and there is nothing above it
	// to SAY, so these two say it with a thinking budget instead — see the
	// constants in internal/provider for the figures and why they are those.
	XHigh Rung = "xhigh"
	Max   Rung = "max"
)

// Rungs is the ladder, cheapest first.
//
// THERE ARE FIVE AND THERE WILL NEVER BE A SIXTH. A ladder somebody can add a
// step to is a ladder nobody can learn: every surface that draws it, every
// setting that stores it and every person who has memorised where their work
// sits would move together for one more shade of the same idea. A new rung has
// to displace an existing one, in this slice, in one commit.
var Rungs = []Rung{Low, Medium, High, XHigh, Max}

// Ship leaves reasoning to the selected model unless a person chooses a rung.
// Keeping this as absence lets provider defaults evolve without a local model
// table or an instruction that enables, disables, or budgets thinking.
const Ship = None

// Valid reports whether a rung is one of [Rungs]. [None] is not: it is absence,
// and a caller asking "is this a rung" about absence wants no.
func (r Rung) Valid() bool {
	for _, rung := range Rungs {
		if r == rung {
			return true
		}
	}
	return false
}

// String is the rung as a person and a config file both spell it, and "" for
// [None] — which is what an empty setting looks like on disk and what the
// emptiness law asks a surface to draw as nothing at all.
func (r Rung) String() string { return string(r) }

// Parse normalizes one operator-supplied word and reports whether it is one.
//
// "auto", the legacy alias "off", and "" land on [None]. At a scoped dial
// this clears the override; at the install default it leaves reasoning to the
// provider. An unrecognized word is refused rather than quietly downgraded:
// a typo that
// silently drops a rung somebody paid for is worse than a message saying the
// word is not one.
func Parse(value string) (Rung, bool) {
	switch rung := Rung(strings.ToLower(strings.TrimSpace(value))); rung {
	case None, "auto", "off":
		return None, true
	case Low, Medium, High, XHigh, Max:
		return rung, true
	default:
		return None, false
	}
}
