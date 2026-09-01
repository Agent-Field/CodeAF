package tui3

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE ONE KEY A WAIT OFFERS ───────────────────────────────────────────────
//
// A pinned lane is a person's instruction, so when it goes quiet this build asks
// rather than overrides: the phase clock says `coreweave is slow · switch to
// auto? (y)` and `y` fires the rescue the controller would have fired anyway,
// to the lane the frontier already named (docs/design/waiting/DESIGN.md §E).
//
// IT IS A LETTER, AND THAT IS THE WHOLE OF WHY THIS FILE IS CAREFUL. Somebody
// typing a sentence is writing their next message, not answering a question, and
// a surface that stole a `y` out of the middle of a word would be worse than the
// wait it was trying to end. So the key is taken only when BOTH halves are true:
// an offer is live for the model on screen, and the composer is empty. Either
// one alone is not enough — an empty box with no question is an ordinary `y`,
// and a live question under a half-written sentence belongs to the sentence.
//
// It is read in [app.key] under every overlay, list and card, and over the plain
// switch that would otherwise type it into the box. That is the same rung the
// settled card's four letters take, for the same reason and with the same kind
// of guard on it (tasksettle.go).

// offerKeyWord is the key itself, spelled once so the surface that draws the
// question and the router that answers it cannot disagree about it.
const offerKeyWord = "y"

// offerAgent is the OPTIONAL half of [Agent]: an engine that can answer an open
// offer.
//
// It is a separate interface rather than a method on [Agent] for the reason the
// typing door is one (lanes.go's typingAgent): answering an offer is a thing a
// session with a real transport under it has, and a door that does not offer one
// — a connection to another machine, a test double — makes the capability ABSENT
// rather than present and failing. A surface over a door that cannot answer
// simply never takes the key, and the letter types.
type offerAgent interface{ AnswerLaneOffer(yes bool) bool }

// offerLive reports whether a question is standing over this conversation's own
// model right now.
//
// IT IS READ OFF THE PHASE DESK AND NOWHERE ELSE. The engine keeps its own note
// of the open offer, fed by the same news this desk is fed by, so the two cannot
// disagree about whether one is standing — and this side holds no state of its
// own to go stale (phase.go's [app.livePhase] carries the freshness rule and the
// role rule with it).
func (a *app) offerLive() bool {
	news, ok := a.livePhase()
	return ok && news.Phase == session.PhaseAsking
}

// offerKey answers a standing offer, and reports whether it took the key.
func (a *app) offerKey(msg tea.KeyPressMsg) bool {
	if msg.String() != offerKeyWord || !a.input.empty() || !a.offerLive() {
		return false
	}
	engine, ok := a.agent.(offerAgent)
	if !ok {
		return false
	}
	engine.AnswerLaneOffer(true)
	// THE QUESTION IS NOT REDRAWN AS ANSWERED. The rescue withdraws the offer
	// the moment it starts — the phase moves to the switch that is now in
	// flight, and the clock says that instead — so a receipt printed here would
	// be this surface narrating a thing the engine is about to say properly.
	a.touch()
	return true
}
