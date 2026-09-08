package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── WHAT A TAB IS ALLOWED TO SAY ABOUT ITS CONVERSATION ─────────────────────
//
// The tab strip's own law is that a tab is a conversation this window has been
// in, and NOT a promise that it is running (chattabs.go). This file is the
// narrowest possible widening of that: one mark, three states, and every one of
// them read off something this process already knows for certain.
//
// ── THE TWO READINGS THAT ARE FORBIDDEN, AND WHY ────────────────────────────
//
// [session.Agent.TaskIndex] READS A FILE. It is how the switcher counts running
// work, and the switcher is drawn on a keystroke; the strip is laid out on every
// frame, so the same reading here would be a disk scan per tab per frame — or,
// over `--host`, a call to another machine (keeper.go's [behindWatch] states the
// same law from the other end).
//
// [session.Agent.NeedsPerson] TAKES THE AGENT'S MUTEX AND ALLOCATES A MAP
// (session's taskpresence.go). It is cheap once and it is not cheap eight times
// a frame, and it can block behind a turn that is holding the lock.
//
// So the far side's two facts are CACHED BY THE WATCHER THAT WAS ALREADY
// WATCHING, at the transitions it was already computing (keeper.go's
// [behindWatch.waits] and [behindWatch.turning]), and reading them here is two
// atomic loads per tab. The near side's are read off the surface itself.
//
// ── WHAT COUNTS AS NEEDING A PERSON ─────────────────────────────────────────
//
// A QUESTION SOMEBODY HAS TO ANSWER, and nothing else. The engine's own
// definition is the list in [session.Agent.waitingOnPerson] — a consent card, a
// sign-in, a subharness offer, a task proposal WITH NO DEADLINE, a run out of
// fuel — and the exclusion in it is the one that matters here: an automatic
// proposal carrying a deadline is a countdown, which proceeds whether or not
// anybody looks at it, and is not a request.
//
// The front tab is answered from the surface instead of from the agent, and its
// two states are written to mean the same thing: a consent card on screen, and a
// task proposal that is genuinely waiting rather than counting down. INTERNAL
// WAITS ARE NOT ON EITHER LIST. A tool checking something, a node waiting on a
// dependency, a provider being retried — all of those are WORK, and a strip that
// spelled them `?` would put an amber question mark on every tab at once and
// teach a person to ignore the one that means them.
//
// ── AND WORK IS NOT ONLY THE TURN ───────────────────────────────────────────
//
// A TASK OUTLIVES THE TURN THAT PROPOSED IT. The node runs in its own worktree
// under its own agent; the conversation's turn ends the moment the proposal is
// answered, and the work goes on for minutes after that. So a strip that asked
// only whether a turn was in flight drew the busiest conversation in the window
// at rest — which is the reading this file used to have, and the one it does
// not have now.
//
// Both sides answer that from something already in memory. The conversation in
// front reads its OWN ROSTER ([app.taskSeen], the last state seen per node,
// which task.go keeps for the de-dup); a held one reads the watcher's cached
// fold of the same notices (keeper.go's [behindWatch.tasking]). Neither opens
// [session.Agent.TaskIndex], which reads a file.
//
// IT IS THE WORKING MARK AND NEVER THE QUESTION, on both sides. A queued node
// waiting on a dependency and a running one are the same thing to a person —
// something is happening and nobody is being asked — and an UNVERIFIED node,
// which really is waiting on a decision, is deliberately not raised as one
// either: the engine's own list of what needs a person does not carry it
// (session's [Agent.waitingOnPerson]), and the strip does not get to disagree
// with the engine about what a question is.
//
// ── AND WHAT IDLE LOOKS LIKE ────────────────────────────────────────────────
//
// Nothing. The emptiness law: unknown or at rest draws no mark, never a dot and
// never a dimmed placeholder. There is no clock here either — the marks change
// when a stir wakes the surface (keeper.go) or when the conversation in front
// changes state, both of which already paint a frame.

// tabSignal is what one conversation is doing, as far as this window can
// honestly say. Its zero value is the honest answer for a conversation nothing
// is known about.
type tabSignal uint8

const (
	// tabIdle is at rest, or not known. The two are one state on purpose: this
	// window cannot tell them apart for a conversation it is only remembering,
	// and a mark that meant "probably" would be the strip inventing a claim.
	tabIdle tabSignal = iota
	// tabWorking is a turn in flight.
	tabWorking
	// tabNeedsPerson is a question waiting for an answer, and it OUTRANKS
	// working: a conversation can be both, and the half a person can act on is
	// the half worth the cell.
	tabNeedsPerson
)

// tabSignalFor is one conversation's state, by its canonical key ([app.convKey])
// and whether it is the one in front.
//
// IT OPENS NO FILE AND CROSSES NO WIRE, which is the strip's own law and the
// whole reason this function exists rather than a call to the agent.
func (a *app) tabSignalFor(key string, here bool) tabSignal {
	if here {
		return a.frontSignal()
	}
	held := a.behind[key]
	if held == nil || held.watch == nil {
		// A conversation this window remembers but does not hold — one closed, one
		// the engine ended on a swap over a shared handle, one that was only ever
		// a name on the recency stack. NOTHING IS KNOWN ABOUT IT, and the strip
		// says nothing rather than guessing that a tab somebody visited an hour
		// ago is still live.
		return tabIdle
	}
	switch {
	case held.watch.waits.Load():
		return tabNeedsPerson
	case held.watch.turning.Load(), held.watch.tasking.Load(), held.watch.jobbing.Load():
		// A TURN IN FLIGHT, OR WORK THAT OUTLIVED ONE. The second is the whole
		// reason this is two loads rather than one: the watcher's turn flag goes
		// false the moment the conversation's own stream ends, and the nodes it
		// started keep running afterwards (keeper.go's [behindWatch.tasking]).
		return tabWorking
	}
	return tabIdle
}

// frontSignal is the same question about the conversation on screen, which has
// no watcher — [app.bringForward] stops it when a conversation comes forward —
// and needs none: everything it would have cached is on this surface.
//
// THE TASK PROPOSAL IS ASKED ABOUT ITS DEADLINE, which is how the engine's own
// reading tells a question from a countdown (session's [Agent.waitingOnPerson]).
// A proposal that will proceed on its own is work, and it wears the working mark
// or none.
func (a *app) frontSignal() tabSignal {
	if run := a.orchOf(); run != nil && run.gate != nil {
		return tabNeedsPerson
	}
	if a.asking() || (a.awaitingTask() && a.task != nil && a.task.deadline.IsZero()) {
		return tabNeedsPerson
	}
	if a.state == stateWorking || a.tasksInFlight() || a.jobsRunning() > 0 {
		return tabWorking
	}
	return tabIdle
}

// tasksInFlight says this conversation has a task node still going, which is the
// front tab's half of the fact keeper.go caches for every other tab.
//
// IT WALKS A MAP THIS SURFACE ALREADY KEEPS. [app.taskSeen] is the last state
// heard for each node — task.go maintains it for the update de-dup — so this is
// a walk over the nodes of ONE conversation, once per frame for the ONE tab in
// front, with no lock and no allocation. The alternative is
// [session.Agent.TaskIndex], which reads a file, or [session.Agent.NeedsPerson],
// which takes the agent's mutex; the strip's law is that it does neither.
//
// THE TWO LIVE STATES ARE NAMED, not the settled ones inverted, so a state this
// surface has not heard of never counts as work. It is the same reading the
// watcher makes of the same notices, and the two have to agree: a conversation
// must not change what it claims by being switched to.
func (a *app) tasksInFlight() bool {
	for _, state := range a.taskSeen {
		if state == session.TaskRunning || state == session.TaskQueued {
			return true
		}
	}
	return false
}

// tabSignalGlyph is the mark itself, or "" for a tab with nothing to say.
//
// THE TWO MARKS ARE THE ONES THIS PROGRAM ALREADY USES for these two states —
// the switcher's rows, home's rows and the task roster all spell them this way
// (hop.go, home.go's own block about the two marks the design re-spelled) — so
// the strip and the card a person opens with ctrl+k cannot disagree about what a
// conversation is doing.
//
// `?` IS ASCII AND NEEDS NO FALLBACK. `◐` is not, so it takes the stand-in this
// file's neighbours take for a running thing ([glyphRunASCII]), on the same gate:
// a terminal that cannot draw the box characters gets the meaning by name rather
// than mojibake (styles.go's block states the law).
func tabSignalGlyph(sig tabSignal, ascii bool) string {
	switch sig {
	case tabNeedsPerson:
		return tokens.GlyphNeedsHuman
	case tabWorking:
		if ascii {
			return glyphRunASCII
		}
		return tokens.GlyphWorking
	}
	return ""
}

// tabSignalSlot is the mark in a FIXED CELL, which is what the strip draws.
//
// THE SLOT IS THE SAME WIDTH IN ALL THREE STATES, and that is the whole reason
// it is a function rather than a concatenation at the call site. A mark that
// appeared and disappeared would move every name to its right by one cell each
// time a turn started — on a row a person reaches for by POSITION, which is the
// one thing tabs may not do (chattabs.go's law about the order never moving).
//
// It is [tabSignalWidth] cells wide, always, and empty for idle.
func tabSignalSlot(sig tabSignal, ascii bool) string {
	glyph := tabSignalGlyph(sig, ascii)
	if glyph == "" {
		return " "
	}
	return glyph
}

// tabSignalWidth is what the slot costs a tab's label, and it is what
// [tabWordFloor] has to rise by: a strip that spent its last six cells on a name
// and then drew a mark over them would trade the word for the mark.
const tabSignalWidth = 1

// tabSignalWord is the state in words, for anywhere with room for them — a
// hover, a card, a line of help. It is the surface's own vocabulary and not the
// machinery's: work is running or it needs you, and nothing here is `verified`,
// `pending` or `blocked`.
func tabSignalWord(sig tabSignal) string {
	switch sig {
	case tabNeedsPerson:
		return tabNeedsPersonWord
	case tabWorking:
		return tabWorkingWord
	}
	return ""
}

// The two words, said once. They are the sentences the switcher's own card and
// home's rows use for the same two states.
const (
	tabNeedsPersonWord = "needs you"
	tabWorkingWord     = "working"
)

// tabSignalInk paints a mark, and it paints ONLY the mark — the name beside it
// keeps whatever the strip gives it, because the tab's own paint says which
// conversation is up and this says what one is doing. Two claims, two inks, and
// neither is allowed to overwrite the other.
//
// The hues are the ones both other surfaces use: amber for a question, the
// accent for work in flight (hop.go's [hopLine]).
func (p palette) tabSignalInk(sig tabSignal, mark string) string {
	if mark == "" {
		return mark
	}
	switch sig {
	case tabNeedsPerson:
		return p.warn(mark)
	case tabWorking:
		return p.accent(mark)
	}
	return p.dim(mark)
}
