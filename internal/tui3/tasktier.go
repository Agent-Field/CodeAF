package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// tasktier.go is THE ONE PLACE THIS SURFACE TURNS A READING INTO A CELL AND A
// ROW.
//
// Every task row, rail line and roster entry answers ONE question before it says
// anything else: do I need to do anything? There are three answers
// ([session.TaskTier]), each has one glyph and one word, and a person who has
// learned the glyphs has learned the whole system
// (docs/design/task-states/DESIGN.md).
//
// BEFORE THIS FILE THE SURFACE HAD FOUR TABLES. The rail worked a cell out of
// the state and the merge word, the roster worked one out of the presence, home
// worked out a third, and the record page spelled its own words for all of them
// — so a task a person stopped drew ⊘ on the roster and ✗ on its own page, and
// one landing was called `needs your look`, `awaiting review` and `unverified`
// on three screens a keypress apart. The tables disagreed because there were
// four of them. There is one now, and it reads [session.TaskStatus.Tier]: NO
// SURFACE WORKS A TIER OUT OF A STATE FOR ITSELF.
//
// The words are internal/session's and are never respelled here. What belongs to
// this file is the DRAWING: which cell, which hue, and how a row that will not
// fit gives ground.

// tierGlyph is the tier in one cell, in both glyph tiers — the Unicode mark and
// the stand-in a terminal with no Unicode gets.
//
// FIVE CELLS AND NO SIXTH. Moving is `◌` while nothing is turning and `▸` while
// something is; over is `✓`, `⊘` or `✗` for the three ways work ends; your call
// is `?` and only ever `?`. The `!` this surface used to draw for unfinished
// work is gone: it was a fourth answer to a question that has three, and it left
// a person deciding whether a `!` was louder than a `✗`.
func tierGlyph(status session.TaskStatus) (glyph, ascii string) {
	switch status.Tier {
	case session.TaskTierYourCall:
		// "?" is already a character a screen with no Unicode has, so there is
		// nothing for the linear tier to stand in for.
		return glyphAsk, glyphAsk
	case session.TaskTierOver:
		switch status.Presence {
		case session.TaskPresenceDone:
			return glyphDone, glyphDoneASCII
		case session.TaskPresenceStopped:
			// ⊘ AND NOT THE CROSS. A cross is a finding, and nobody found anything
			// wrong with work the person ended themselves (stop.go states it).
			return glyphStopped, glyphStoppedASCII
		}
		return glyphBad, glyphBadASCII
	case session.TaskTierMoving:
		switch status.Presence {
		case session.TaskPresenceWorking, session.TaskPresenceFinishing:
			return glyphRunning, glyphRunningASCII
		}
		return glyphQueued, glyphQueuedASCII
	}
	// A reading with no tier at all is a node this build has heard nothing about,
	// which is the hollow circle: nothing has started, and nothing is claimed.
	return glyphQueued, glyphQueuedASCII
}

// tierInk is the hue that cell is said in — the paint half of [tierGlyph], asked
// separately because the hue is needed on its own: the composer's room segment
// says the name of the task you are typing to in the state's own colour
// (room.go's [app.roomLead]), and a second table of which state is which colour
// would be a segment that disagreed with the glyph beside the same name.
//
// THE ROW HAS ONE LIT ELEMENT AND IT IS THE QUESTION. A your-call row is the one
// thing on the column somebody has to act on and it is the one thing brought up
// out of the dim; everything over is dim or muted, and the accent is spent on
// work in flight because that is the surface's word for "this is happening now".
// The amber is styles.go's [hueWarn], which the design gives to A PERSON BEING
// WAITED ON everywhere it appears — the places wave settled that, and moving the
// reading onto a second colour would put two hues back on one fact.
func tierInk(pal palette, status session.TaskStatus) func(string) string {
	switch status.Tier {
	case session.TaskTierYourCall:
		return pal.warn
	case session.TaskTierOver:
		switch status.Presence {
		case session.TaskPresenceDone:
			return pal.muted
		case session.TaskPresenceStopped:
			return pal.dim
		}
		// DIM UNLESS SOMETHING ACTUALLY BROKE. A dropped connection, a spent
		// threshold and a check that named gaps are work that did not finish, and
		// painting any of them in the failure hue reports a finding nobody made.
		if status.Fault {
			return pal.bad
		}
		return pal.dim
	}
	switch status.Presence {
	case session.TaskPresenceWorking, session.TaskPresenceFinishing:
		return pal.accent
	}
	return pal.dim
}

// tierMark is [tierGlyph] as this surface draws it right now, UNPAINTED: the
// stand-in on the linear tier, and THE SPINNER on the rows that animate today.
//
// The spinner is not a sixth cell — it is `▸` moving, which is this surface's
// one promise that something is happening this instant. A page that is redrawn
// only when something changes has no business claiming that, so the static
// glyph is what a roster row and a record row wear.
func (a *app) tierMark(status session.TaskStatus) string {
	glyph, ascii := tierGlyph(status)
	if glyph == glyphRunning {
		if a.linear {
			return glyphRunASCII
		}
		return tokens.Spinner(a.paints / spinnerStep)
	}
	if a.linear || a.pal.ascii {
		return ascii
	}
	return glyph
}

// tierCell is that mark in its own hue: the whole of what one cell says.
func (a *app) tierCell(status session.TaskStatus) string {
	return tierInk(a.pal, status)(a.tierMark(status))
}

// tierSep joins the two halves of a row, and it is the surface's own joiner
// (task.go's [railSep]): one punctuation down every list of work there is.
const tierSep = railSep

// tierTitleFloor is how little room a title may be left with before the row
// stops naming the work and starts being a state with an ellipsis in front of
// it. Eight cells is about one word — under that the title has told nobody which
// task this is, and the row is better spent on the word.
const tierTitleFloor = 8

// tierRow is the whole of what one row of work says, unpainted:
//
//	✓ Port the parser · done
//	▸ Port the parser · working
//	? Port the parser · your call · conflicts with your branch: parser.go
//	✗ Port the parser · incomplete · ran out of steps
//
// IT IS CUT FROM THE RIGHT AND THE VERB IS NEVER WHAT GOES. A row is read to
// find out whether it needs anything, so the half that answers that is the half
// that survives: the files a conflict names give way first, then the title down
// to [tierTitleFloor], and only a row with no room for either is cut through the
// word itself. A sentence that trails off before the one instruction a person
// needs has spent its cells saying nothing (docs/design/task-states/DESIGN.md).
func tierRow(pal palette, status session.TaskStatus, title string, width int) string {
	glyph, ascii := tierGlyph(status)
	if pal.ascii {
		glyph = ascii
	}
	lead := glyph + " "
	room := width - ansi.StringWidth(lead)
	if room <= 0 {
		return fit(lead, width)
	}
	return lead + tierRowBody(status, title, room)
}

// tierRowBody is that row without its cell: the title, the word, and the giving
// of ground between them.
func tierRowBody(status session.TaskStatus, title string, width int) string {
	title = strings.TrimSpace(title)
	word := strings.TrimSpace(status.RowWord())
	switch {
	case word == "":
		return fit(title, width)
	case title == "":
		return fit(word, width)
	}
	if row := title + tierSep + word; ansi.StringWidth(row) <= width {
		return row
	}
	// THE FILE LIST IS THE FIRST THING TO GO. `conflicts with your branch:
	// parser.go, parser_test.go` names the files as a courtesy and says what to
	// do as its whole point, and the card one keypress away has the list in full.
	if shed := tierWordShed(word); shed != word {
		if row := title + tierSep + shed; ansi.StringWidth(row) <= width {
			return row
		}
		word = shed
	}
	// THEN THE TITLE, DOWN TO ITS FLOOR. The word keeps whatever it needs; what
	// is left over, down to one word, is the title's.
	if room := width - ansi.StringWidth(tierSep+word); room >= tierTitleFloor {
		return fit(title, room) + tierSep + word
	}
	// AND ONLY THEN THE ROW ITSELF. A column this narrow cannot say both, and of
	// the two the word is the one that answers the question the row is read for.
	return fit(word, width)
}

// tierWordShed drops the list a reason carries after its colon — the files a
// conflict names, the gaps a check found — and leaves the sentence that says
// what happened. A reason with no list is returned exactly as it stands.
func tierWordShed(word string) string {
	if at := strings.Index(word, ": "); at > 0 {
		return word[:at]
	}
	return word
}

// tierYourCallWord is the word a your-call row wears, for the few rows on this
// surface that are NOT reading a [session.TaskStatus] — home's standing items
// and the news lines under them, which are the same fact about a different
// object: the machine has done what it can and somebody has to say something.
//
// IT IS THE ENGINE'S SPELLING AND NOT A SECOND ONE. internal/session keeps the
// word unexported because no engine caller needs it, so it is written here once
// and tasktier_test.go asserts against [session.ProjectTask]'s own answer that
// the two have not drifted. The words it replaced — `needs your look`, `awaiting
// review`, `unverified` — were three surfaces' private names for one reading.
const tierYourCallWord = "your call"

// tierReasonSep hangs a reason off that word, for the same rows: `your call ·
// the fix touches migrations`. It is [tierSep] under another name because a row
// composed by hand and a row composed by [session.TaskStatus.RowWord] must be
// punctuated identically or the eye reads two kinds of row.
const tierReasonSep = tierSep
