package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE DECISION, PUT WHERE THE PERSON IS STANDING — AND ASKED BY THE ONE THING
// THAT ASKS EVERYTHING.
//
// A task that finishes with nobody able to say whether it holds lands as the
// person's call. It is neither done nor incomplete, its branch is kept, and
// everything queued behind it waits. This file used to draw that question
// itself: its own two rows, its own five letters, its own receipts, its own
// pointer, and three copies of the same routing for the conversation, the room
// and the roster's focused row.
//
// IT DOES NOT ANY MORE. A landed `your call` is a [session.Question] — the
// engine builds it over [session.TaskAsk] itself (internal/session's
// question.go, `landingQuestion`), publishes it the moment the node lands and
// again every time the node moves (task_landing_question.go), and the question
// block draws it in whichever form its evidence asks for, on every page, with
// the one key grammar (question.go, questionkeys.go, questionroom.go). The keys
// are still task-states' own — `[a] <yes> · [n] <no> · [s] tell it`
// (docs/design/task-states/DESIGN.md, [session.LandingYesKey] and its
// neighbours) — and they are now answered through [session.Agent.ResolveQuestion]
// like every other decision this engine hands a person.
//
// ── WHY THE ROWS HAD TO GO RATHER THAN STAND BESIDE THE BLOCK ──
//
// A question drawn twice on one screen is worse than a question drawn in the
// older place: a person answering the second copy of a decision they already
// made is the exact failure the one-renderer wave exists to end. So when the
// block claimed this lane (question.go's [app.questionDrawnHere]) these rows had
// to leave with it.
//
// ── WHAT IS LEFT HERE ──
//
// The landing CARD is still this surface's — the head, the facts and the reason
// row are the record of how work came home and are not an answer to anything
// (taskdone.go) — and finding that card by a node's id is what the room, the
// roster and the record page all do. That lookup, and the redraw after a card
// changes, are the whole of this file now.

// doneEntryFor is the index of the LATEST landed card for one node, or -1. The
// latest, because the engine lands a node more than once — once needing a look,
// and again as done or incomplete once somebody decided — and the newest card is
// the one that says where the work stands now.
func (a *app) doneEntryFor(id uint64) int {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if e := a.entries[i]; e.kind == entryDone && e.done != nil && e.done.id == id {
			return i
		}
	}
	return -1
}

// doneCardFor is the latest landed card for one node, or nil.
func (a *app) doneCardFor(id uint64) *taskDone {
	if i := a.doneEntryFor(id); i >= 0 {
		return a.entries[i].done
	}
	return nil
}

// settleTouched is the redraw after a card changed, wherever the change came
// from: the conversation's copy of the row is dropped, and so is the room's
// page, which caches its rows as one list (room.go) and would otherwise keep
// drawing the question the landing asked an hour ago.
func (a *app) settleTouched(card *taskDone) {
	if card == nil {
		return
	}
	if i := a.doneEntryFor(card.id); i >= 0 {
		a.entries[i].stale = true
	}
	if a.room != nil && a.room.id == card.id {
		a.room.dirty = true
	}
	a.touch()
}

// landingAsked reports whether ONE NODE'S landing is a question standing right
// now — the object, not a reading of the card.
//
// IT ASKS THE BLOCK AND NOT THE CARD, and that is the whole point of the
// migration: the card is a record of a landing and the question is what is being
// asked about it, they move at different times, and a surface that worked the
// second out of the first is how a re-landed node came to draw the first
// landing's frozen answers (#767). Both lanes are asked because a landing whose
// merge was refused is a `conflict` and every other one is a `landing`, and one
// node can move from the second to the first without settling.
func (a *app) landingAsked(id uint64) bool {
	if id == 0 {
		return false
	}
	for _, kind := range []session.QuestionKind{session.QuestionLanding, session.QuestionConflict} {
		if a.questionIsOpen(string(kind) + ":" + itoa64(id)) {
			return true
		}
	}
	return false
}

// roomLandingAsking reports that the OPEN ROOM's node is one of those.
//
// It is what keeps the room's foot from saying `this task has finished — say it
// to main` over a node that is waiting on somebody: the work has stopped, but
// what the page owes a person there is the question, and the question is on the
// block above the box (room.go's foot).
func (a *app) roomLandingAsking() bool {
	if a.room == nil || !a.room.done || a.room.orch != nil || a.roomIsGuest() {
		return false
	}
	return a.landingAsked(a.room.id)
}

// itoa64 is one node id as the token a question is known by. The block spells
// its own tokens from [session.Question.Token], and this is the same string
// built from the id a surface is holding.
func itoa64(id uint64) string {
	return strconv.FormatUint(id, 10)
}

// landingOpen is the standing landing question for one node, and false where
// there is none.
func (a *app) landingOpen(id uint64) (session.Question, bool) {
	if id == 0 {
		return session.Question{}, false
	}
	for _, kind := range []session.QuestionKind{session.QuestionLanding, session.QuestionConflict} {
		token := string(kind) + ":" + itoa64(id)
		for _, open := range a.questions {
			if open.token() == token {
				return open.question, true
			}
		}
	}
	return session.Question{}, false
}

// landingHintAt is the legend's share of a your-call node, spelled FROM THE
// QUESTION'S OWN ANSWERS: `a resolve it · n drop it · s tell it`.
//
// EVERY RUNG IS A RANKED PREFIX OF THE ONE ABOVE IT, which is rowfit.go's law 3
// said about a sentence: what a narrow frame shows is the top of what a wide one
// shows, in the same order, with a count of what went rather than a silence.
//
// tail is what the caller hangs off the end of every rung — the roster's own
// hold hint adds ` · esc`, because out there `esc` gives the column back and
// nothing else on the frame says so (taskeffort.go); the room's slot adds
// nothing, because the legend's left end already carries that key. It is the
// last thing given up: a frame too narrow for even the first answer keeps the
// way out and drops the answers.
func (a *app) landingHintAt(id uint64, width int, tail string) string {
	q, ok := a.landingOpen(id)
	if !ok {
		return strings.TrimPrefix(tail, railSep)
	}
	for keep := len(q.Options); keep > 0; keep-- {
		said := ""
		for _, option := range q.Options[:keep] {
			word := strings.TrimSpace(option.Label)
			if word == "" {
				continue
			}
			if said != "" {
				said += landingHintGap
			}
			said += strings.TrimSpace(option.Key) + " " + word
		}
		if said == "" {
			continue
		}
		if hid := len(q.Options) - keep; hid > 0 {
			said += landingHintHid + itoa(hid)
		}
		full := said + tail
		left, _ := a.legendLeft(width, legendRoom(width, full))
		if ansi.StringWidth(left)+ansi.StringWidth(full)+legendFurniture <= width {
			return full
		}
	}
	return strings.TrimPrefix(tail, railSep)
}

// The two joiners that hint is built with: this surface's one separator, and its
// own grammar for a fold that hid something (`▸ +1`, `holds 3 more`).
//
// A NARROW SLOT MAY DROP AN ANSWER; IT MAY NOT DROP IT SILENTLY.
const (
	landingHintGap = railSep
	landingHintHid = " · +"
)

// legendFurniture is what [app.legendLine] spends on a line with a label at
// both ends before either label starts: `─ ` and one space after the left, one
// space before the right and ` ─` after it, and the one fill cell that line
// refuses to draw without.
const legendFurniture = 7

// landingTell is the landing question's third column, and it is the one answer
// on it that RESOLVES NOTHING.
//
// It opens the node's own page and points the box at it (room.go's
// [app.openRoom] retargets the composer to that task, recipient.go). That page
// is the existing place a person says something to one piece of work: it carries
// the address in its header, it keeps its own unsent draft, and its box is the
// steer surface this program already has (steer.go). "Looks good" typed there
// must not silently become an accept, which is why this is a third column rather
// than a third answer, and why the question is left standing behind it.
func (a *app) landingTell(q session.Question, key string) (tea.Cmd, bool) {
	if key != session.LandingTellKey || q.ID == 0 {
		return nil, false
	}
	if q.Kind != session.QuestionLanding && q.Kind != session.QuestionConflict {
		return nil, false
	}
	if _, ok := a.roomDoors(); !ok {
		return nil, false
	}
	a.openRoom(q.ID, strings.TrimSpace(q.Head))
	return a.takeRoomPump(), true
}
