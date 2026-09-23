package tui3

// ── WHAT MOVING A CONVERSATION LOOKS LIKE, WHERE THE EYE ALREADY IS ─────────
//
// takeover.go is the mechanism — a question, a request on the disk, a beat on
// the flock. This file is the only thing a person sees of it, and it exists
// because the mechanism used to speak in the one place nobody was looking.
//
// THE COMPLAINT THIS FILE ANSWERS. The whole account of the door lived on the
// foot line: a dim sentence at the bottom of a forty-five-row frame, while the
// eye was on the row under the cursor and on the card beside it. And after the
// second enter the surface said NOTHING anywhere — no state on the row, nothing
// on the card — which is indistinguishable from a key that did not work. A
// surface must not go quiet at the exact moment somebody is waiting on it.
//
// SO THE STORY IS TOLD IN THREE PLACES, IN DECREASING WIDTH, AND NEVER TWICE:
//
//	the row     one word in the right margin, where every other row of this
//	            surface says what it is: `another window` at rest becomes
//	            `coming here` while the claim is out. And the row takes THE ONE
//	            SPINNER (homespinner.go) — a claim is by construction the most
//	            recent thing anybody did on this machine, which is that law's
//	            own rule for which row moves, not an exception to it. On the ONE
//	            ROW THE CURSOR IS ON the resting word grows into the whole door,
//	            `another window · enter brings it here`, and gives that clause
//	            back to the width before it would cut a word in half
//	            ([takeoverHeldDoorWord]).
//	the card    the sentences. What the far window is doing, why the wait is a
//	            wait, how long it has been, and the key that ends it.
//	the foot    an echo and no more: the short line, for the case the card
//	            cannot cover — the cursor has walked off the claimed row and the
//	            card is about something else.
//
// ── THE STATES, AND THE LAW THAT NONE OF THEM IS SILENCE ───────────────────
//
//	rest         another window has it, and enter would bring it here
//	armed        the question is up on the card (homeconfirm.go): what moving it
//	             costs, the two answers, and the cursor on the one that does not
//	moving       asked, and the far window has nothing in flight
//	holding      asked, and the far window is mid-reply — what it is doing
//	unanswered   asked, and the request died of old age unanswered
//	came free    it let go while nobody was on home to walk through the door
//
// The last two are the ones that did not exist. A request expires after
// [session.TakeoverStale] and the window that wrote it used to go on beating at
// a flock that nothing was ever going to release — a wait with no end and no
// word for it. And a conversation that came free while the person was on
// another page used to arrive as nothing at all.

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/reltime"
)

// ── the words ───────────────────────────────────────────────────────────────

const (
	// takeoverDoorWord is the whole of the door at rest, and it is on the CARD
	// rather than on the foot. `enter` and not `enter twice`: the first press
	// says what the second one does, in its own words, and a door that
	// advertised its own confirmation step would be spending the calm state's
	// one line on machinery.
	takeoverDoorWord = "enter brings it here"
	// takeoverHeldDoorWord is the whole right margin of the held row THE CURSOR
	// IS ON: where the conversation is, and the way to bring it back, in one dim
	// sentence on the line the eye is already reading.
	//
	// IT IS THE SAME COMPLAINT THIS FILE WAS WRITTEN FOR, ONE STATE EARLIER. The
	// live states were moved onto the row and the resting one was left behind:
	// `another window` is a fact with no door in it, and the door was told only
	// on the card three cells away and on the foot line thirty rows down. The
	// cursor is on the row; so is the sentence.
	//
	// It is BUILT out of the two spellings that already exist rather than
	// written a third time, so the row and the card cannot drift apart — the
	// margin's own word for where it is, then the card's own word for the door.
	//
	// ONLY THE ROW UNDER THE CURSOR GROWS IT, for two reasons that agree. Seven
	// held rows each carrying the same instruction is not seven answers, it is
	// noise; and the instruction is only TRUE of the row enter would act on,
	// which is the cursor's row and never the pointer's — the card follows a
	// hovered row without moving the cursor (home.go's [homeView.previewLine]).
	takeoverHeldDoorWord = homeHeldShort + " · " + takeoverDoorWord
	// takeoverComingWord is the row's word and the card's headline while the
	// claim is out. It is what is HAPPENING and not what was asked for:
	// `asked for` would be a fact about a file, and this is a fact about a
	// conversation that is on its way.
	takeoverComingWord = "coming here"
	// takeoverMidReplyWord is the honest reason, and it is what the far window is
	// DOING rather than a wait: it is mid-reply, it stops there, and it hands the
	// conversation over on its next heartbeat (session's takeover.go). It used to
	// say `it comes the moment the reply ends`, which was true of a road that
	// held every request back until the turn ended — and that wait was the defect
	// the road was reported for.
	takeoverMidReplyWord = "that window is mid-reply — it stops there and hands it over"
	// takeoverQuietWord is what a LONG wait says when the far window is not
	// mid-reply at all. It states the fact and claims nothing about the cause,
	// because there is nothing here that knows one: a window wedged on a disk,
	// a build too old to hear the question, and a machine that went to sleep
	// all look exactly like this from here.
	takeoverQuietWord = "that window has not answered yet"
	// takeoverStopWord is the key out. It sits under the state rather than
	// beside it: the state is the news and this is what to do about it.
	takeoverStopWord = "esc stops waiting"
	// takeoverUnansweredWord is the end of a wait nothing was ever going to
	// end. It says what is true NOW — the conversation is still over there —
	// rather than reporting a failure, because "it did not work" is not a thing
	// a person can act on and "it is still in the other window" is.
	takeoverUnansweredWord = "that window did not answer — it still has it"
	// takeoverRetryWord is the way out of that, and it is the same key that
	// started it — which means it raises the QUESTION again (takeover.go's card)
	// and not the request. A row that had been through the door once used to get
	// the ask on a single press, and that shortcut was the one place where one
	// keystroke could still end another window.
	takeoverRetryWord = "enter asks again"
	// takeoverFreeWord is the conversation that came free while nobody was on
	// home. Nothing was opened under the person — that is [app.takeoverTick]'s
	// standing rule — so the news waits on the card for them to come back.
	takeoverFreeWord = "it came free — enter opens it"
)

// takeoverElapsedAfter is how long a move has to have taken before the card
// says how long it has taken.
//
// THE EMPTINESS LAW, APPLIED TO A CLOCK. A move that lands in under a second is
// a door opening, and `0s` beside it would be the surface reporting the absence
// of a delay as though it were one. Past this the number is news, because past
// this something is genuinely being waited for.
const takeoverElapsedAfter = 2 * time.Second

// ── which state a row is in ─────────────────────────────────────────────────

// takeoverPhase is what this window has to say about moving one conversation.
type takeoverPhase int

const (
	// takeoverNone is every row this door has nothing to do with, which is
	// almost all of them.
	takeoverNone takeoverPhase = iota
	takeoverRest
	takeoverArmed
	takeoverMoving
	takeoverHolding
	takeoverUnanswered
	takeoverCameFree
)

// takeoverPhaseOf is the ONE place the state of this door is decided, and every
// place that draws it asks here.
//
// THE ROW AND THE CARD CANNOT DISAGREE, which is the whole reason this is a
// function rather than three conditions in three paints. They say different
// AMOUNTS about the same state — a word, a paragraph — and a surface where the
// margin says `coming here` over a card that has gone back to `open in another
// window` is a surface a person stops believing.
func (a *app) takeoverPhaseOf(row session.SessionRow) takeoverPhase {
	file := strings.TrimSpace(row.Transcript)
	if file == "" || a.hosted() {
		// Over --host the holder is a window on this laptop and the journal is
		// on the far machine, so there is nobody to ask and nothing to say
		// (takeover.go's header).
		return takeoverNone
	}
	if a.waitingToTakeOver() && a.takeover.file == file {
		if a.takeoverHeldUp(row) {
			return takeoverHolding
		}
		return takeoverMoving
	}
	if a.takeover.about == file {
		switch a.takeover.outcome {
		case takeoverEndedUnanswered:
			return takeoverUnanswered
		case takeoverEndedFree:
			return takeoverCameFree
		}
	}
	if !a.homeHeld(row) || strings.TrimSpace(row.Dir) == "" {
		return takeoverNone
	}
	if a.home.armed == file {
		return takeoverArmed
	}
	return takeoverRest
}

// takeoverHeldUp reports that the far window has something in flight, which is
// the difference between a move that is merely happening and a move that has a
// reason to be taking a while.
//
// IT IS THE CONVERSATION'S OWN WORD, read off the presence file every other
// state on this screen is read off ([session.SessionRow.Doing]) and not a second
// judgement about it. A window on a build too old to say anything says nothing,
// and the honest reading of nothing is that there is no reason to report.
func (a *app) takeoverHeldUp(row session.SessionRow) bool {
	doing := row.Doing()
	return doing != "" && doing != string(session.PresenceIdle)
}

// claiming reports that this window has a claim out on this row, for the two
// paints that only need the yes or no.
func (a *app) claiming(row session.SessionRow) bool {
	switch a.takeoverPhaseOf(row) {
	case takeoverMoving, takeoverHolding:
		return true
	}
	return false
}

// ── the row ─────────────────────────────────────────────────────────────────

// takeoverRowWord is what the right margin says, and "" for every row this door
// is not the news on.
//
// ONLY THE LIVE STATE TAKES THE MARGIN AWAY FROM THE ROW'S OWN WORD. At rest
// the margin is the row's to spell — `another window`, and on the cursor's row
// the whole door ([takeoverHeldDoorWord], drawn by home.go's [app.homeRowNote]
// and by switcher.go's paint). The failure states say nothing here at all,
// because `open in another window` remains true of a conversation nobody let go
// of and a second word for it in the width the NAME needed would be the row
// spending cells to repeat itself.
func (a *app) takeoverRowWord(row session.SessionRow) string {
	if a.claiming(row) {
		return takeoverComingWord
	}
	return ""
}

// ── the card ────────────────────────────────────────────────────────────────

// takeoverCard is the state band's half of this door: the state, its reason,
// and the key. It is drawn ABOVE everything else the state band says
// (homebands.go's [drawStateBand]) because it is the only thing on that card
// that is about a keystroke somebody just made.
//
// THE STATE IS DIM AND THE REASON IS INK, which is the band's own arrangement
// and not a new one: a machine's account of itself is dim on this surface, and
// the one line a person has to READ is brought up out of it. Nothing here takes
// the accent — that is spoken for, on this very card, by a conversation that is
// stopped on a question.
func (a *app) takeoverCard(row session.SessionRow, width int, pal palette) []string {
	if width < 1 {
		return nil
	}
	phase := a.takeoverPhaseOf(row)
	var rows []string
	dim := func(said string) {
		for _, line := range wrap(said, width) {
			rows = append(rows, pal.dim(line))
		}
	}
	ink := func(said string) {
		for _, line := range wrap(said, width) {
			rows = append(rows, pal.ink(line))
		}
	}
	switch phase {
	case takeoverRest:
		dim(takeoverDoorWord)
	case takeoverArmed:
		// AN ARMED ROW IS A QUESTION, AND THE CARD IS THE QUESTION BLOCK'S OWN
		// (homeconfirm.go). This used to be two lines the band painted itself —
		// `enter again moves it here` over the two clauses saying what it costs
		// — which is a card describing a decision beside a block built to draw
		// one. What the answers cost is on the answers now, in a column, and the
		// keys are the ones every other question on this surface takes.
		return a.homeAskRows(width)
	case takeoverMoving, takeoverHolding:
		// THE STOP QUESTION IS THE CARD WHILE IT IS UP, the way the move
		// question is in the armed state: one decision, drawn by the block's
		// own renderer (homeconfirm.go).
		if ask, up := a.homeAsking(); up && ask.question.Kind == takeoverStopKind {
			return a.homeAskRows(width)
		}
		dim(a.takeoverHeadWord())
		switch {
		case phase == takeoverHolding:
			ink(takeoverMidReplyWord)
		case a.takeoverPatienceGone():
			// A MOVE THAT IS NOT WAITING FOR A REPLY AND STILL HAS NOT LANDED.
			// There is no reason to give, so the card gives the fact instead of
			// inventing one — and it only says it once the wait is long enough
			// that the fact is news (see [takeoverPatience]).
			ink(takeoverQuietWord)
		}
		// AND ONCE THE WAIT IS NEWS, THE WINDOW IS NAMED: its pid, its
		// terminal and its build, from its own presence record — and enter
		// offers to stop it. Nothing is left for a person to dig out of `ps`.
		if a.takeoverCanStop() {
			ink(takeoverHolderWord(a.takeover.holder))
			dim(takeoverStopOfferWord)
		}
		dim(takeoverStopWord)
	case takeoverUnanswered:
		ink(takeoverUnansweredWord)
		if a.takeover.holder.PID > 0 {
			ink(takeoverHolderWord(a.takeover.holder))
		}
		dim(takeoverRetryWord)
	case takeoverCameFree:
		ink(takeoverFreeWord)
	default:
		return nil
	}
	return rows
}

// takeoverHolderWord names the window holding the conversation, from its own
// presence record: `held by pid 58673 · ttys004 · a1b2c3d4 built …`.
func takeoverHolderWord(holder session.Holder) string {
	words := holder.Words()
	if words == "" {
		return ""
	}
	return "held by " + words
}

// takeoverStopOfferWord is the key that stops it, said under the name.
const takeoverStopOfferWord = "enter stops that window"

// takeoverStoppingWord is what the foot says once the stop was sent, and the
// second time says what the second signal does.
func takeoverStoppingWord(pid, times int) string {
	if times > 1 {
		return fmt.Sprintf("told pid %d to stop now — it exits without finishing", pid)
	}
	return fmt.Sprintf("asked pid %d to stop — the conversation comes here as it lets go", pid)
}

// takeoverHeadWord is the headline of a claim that is out, with how long it has
// been out once that is worth saying ([takeoverElapsedAfter]).
func (a *app) takeoverHeadWord() string {
	waited := a.now().Sub(a.takeover.since)
	if waited < takeoverElapsedAfter {
		return takeoverComingWord
	}
	return takeoverComingWord + " · " + reltime.Elapsed(waited)
}

// takeoverPatienceGone reports that the wait has gone on long enough to want
// explaining ([takeoverPatience]).
func (a *app) takeoverPatienceGone() bool {
	return a.now().Sub(a.takeover.since) >= takeoverPatience
}
