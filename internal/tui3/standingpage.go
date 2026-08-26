package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE THREE REACHES, IN THE THREE VOICES A PERSON MEETS THEM IN.
//
// A standing order reaches exactly one of three distances — this conversation,
// this project, every project on the machine — and four surfaces say so: the
// ratification card in the transcript (standing.go), the shelves of the standing
// place (standingplace.go), the column down the right (margin.go) and home's own
// rows (homestanding.go). What they say is spelled once in placeprose.go; what
// lives here is WHICH of those spellings each voice uses, because a shelf
// heading, a card's band and a two-cell tail are three different sentences about
// one fact and a fourth written somewhere else is how a surface ends up calling
// one thing two things.
//
// AND NONE OF THEM SAYS `altitude`, `scope` OR `machine`. Those are this
// codebase's words for the three reaches; the person reads `in this
// conversation`, `for this project` and `everywhere`, which are the words the
// card asked them in.

// standShelfWord is one reach as its shelf is headed.
func standShelfWord(level standing.Altitude) string {
	switch level {
	case standing.AltitudeConversation:
		return standInHereWord
	case standing.AltitudeMachine:
		return standEverywhereWord
	}
	return standProjectWord
}

// standLevelWord is one reach as the RATIFICATION CARD names it. It is a second
// spelling of one fact on purpose; [standJustHereWord] says why.
func standLevelWord(level standing.Altitude) string {
	switch level {
	case standing.AltitudeConversation:
		return standJustHereWord
	case standing.AltitudeMachine:
		return standEverywhereWord
	}
	return standProjectWord
}

// standScopeTail is one reach as a ROW'S DIM TAIL says it, and NOTHING at the
// default.
//
// THE DEFAULT IS SILENT, which is the emptiness law applied to a fact rather
// than to a figure. An order said in a conversation governs the project
// ([standing.AltitudeProject] is the zero value and D6's default), so a tail
// saying so on every row would be a column printing what is already true of
// nearly everything on it. The two reaches that are NOT the default earn their
// word, because those are the ones a person would be surprised by.
func standScopeTail(level standing.Altitude) string {
	switch level {
	case standing.AltitudeMachine:
		return standEverywhereWord
	case standing.AltitudeConversation:
		return standJustHereTag
	}
	return ""
}

// ── the engine under the place ──────────────────────────────────────────────

// standingHereAgent is the slice of the engine the standing place needs, and it
// is asserted rather than added to [Agent] — the standing side is OPTIONAL,
// exactly as the card's own contract is ([standingAgent] says why at more
// length). A scripted agent that has never heard of a standing order is a
// session with the ambient side off, and it must stay representable.
type standingHereAgent interface {
	// StandingHere answers what stands over this conversation — its own, its
	// project's and the machine's, in that order — and separately what the
	// person excepted from here (internal/session's standing_orders.go).
	StandingHere() (stand []standing.Item, excepted []standing.Item)
	// StandingExcept records that one order does not reach this place.
	StandingExcept(id string) error
	// StandingStandDown retires one order, recording that the person stopped it.
	StandingStandDown(id string) error
	// StandingPause pauses an active order or starts a paused one again, and
	// answers the status it now has.
	StandingPause(id string) (standing.Status, error)
}

// standingSeam is the engine under this surface, when it has one that can
// answer about standing orders.
func (a *app) standingSeam() (standingHereAgent, bool) {
	if a.agent == nil {
		return nil, false
	}
	agent, ok := a.agent.(standingHereAgent)
	return agent, ok
}
