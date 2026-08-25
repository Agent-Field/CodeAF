package tui3

// THE STANDING PLACE'S READING: WHAT IS TRUE, TURNED INTO ROWS AND THEN INTO
// LINES.
//
// This file is PURE. It never sees the surface itself, never asks the clock,
// and never touches the disk — everything it needs arrives as data assembled by
// place_standing.go, so a cursor move can redraw the whole screen without
// reaching a seam and two rows of one frame can never be two ages apart.
//
//	  standing orders
//	  in this conversation
//	› ▲ keep the tests green        needs your look · the fix touches migrations
//	  for this project
//	  ◦ draft the weekly update                                  Mondays at 9am
//	  everywhere
//	  ◦ never touch the public API                                        holds
//	  ─ not here: post the standup
//	  in other projects
//	  ◦ watch the release feed                                   Mondays at 9am
//
// Four decisions, and three of them are borrowed rather than invented:
//
//   - THE SHELVES ARE THE THREE REACHES, in the order a person reads outward
//     from where they are standing: this conversation, this project,
//     everywhere. Each one is a heading and no more — the rows under it are what
//     the shelf is about, and a heading with a count beside it would be the
//     screen counting what a person can see.
//   - AND A FOURTH SHELF IS THE REST OF THE MACHINE. The three reaches answer
//     "what is true HERE"; a person who came to this screen to find the order
//     they set up last week in another project was shown a page that did not
//     have it and did not say so. So what stands outside this conversation is
//     drawn under a heading of its own, at the bottom, after everything that
//     reaches where the person is sitting — the same outward reading carried one
//     step further.
//   - AN EMPTY SHELF DRAWS NOTHING AT ALL. Not the heading, not a "nothing
//     here", not a rule where the rows would have been. That is the emptiness
//     law at its most literal, and it is what makes this page one line long on
//     the ordinary conversation with one order over it.
//   - THE STATUS CLAUSE IS HOME'S OWN ([standRollup]). An item's tail — needs
//     your look, checking now, paused, the cadence, what the last look found —
//     is one derivation with one set of words, and a page that wrote a second
//     one would be two screens disagreeing about the same item in front of the
//     same person.
//
// AND THERE IS NO FOLD. The list showed four rows and hid the rest behind a
// `▸ N more` line no key answered; a place takes the whole terminal, so the
// window is whatever the frame had room for and the cursor walks the rest.

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// The mark a "not here" line leads with, and its stand-in on a terminal that
// cannot draw it. It is a RULE and not a glyph out of [standing.Item.Glyph]'s
// four: those four say what an order is doing, and this line is about an order
// that is doing nothing at all here — the shelf it would have been on, with the
// place struck through.
const (
	standNotHereGlyph = "─"
	standNotHereASCII = "-"
)

// standRowKind is what one line of the page is. A shelf heading and a "not
// here" line are drawn and never chosen; only an order is a row a verb can act
// on.
type standRowKind uint8

const (
	standRowShelf standRowKind = iota
	standRowItem
	standRowNotHere
)

// standRow is one line of the page: a shelf's heading, an order, or an order
// that deliberately does not reach here.
type standRow struct {
	kind standRowKind
	// shelf is the heading's words, on a heading row and nowhere else.
	shelf string
	// view is the order as a row needs it, on the other two kinds. It is
	// [StandingItemView] and not a bare item so that the clause this page draws
	// is the clause home draws, from the same input.
	view StandingItemView
	// elsewhere marks a row on the FOURTH shelf: an order this machine holds
	// that does not reach this conversation at all.
	//
	// IT DECIDES WHICH VERBS THE ROW HAS AND NOTHING ELSE. Everything a person
	// reads on such a row — the mark, the name, the clause — is drawn by the
	// same three functions every other row is drawn by, because one grammar is
	// the whole point of filing them on one page. What cannot be the same is
	// what a key can do to it: the conversation seam has never heard of this
	// order, so its three writes are not available and the store's two are
	// ([standPage.verbs]).
	elsewhere bool
}

// standingShelves lays the whole page out: the three shelves over this
// conversation, and under them the fourth of what stands elsewhere on this
// machine. Each shelf is dropped whole when nothing is on it.
//
// THE CALLER'S ORDER IS KEPT INSIDE A SHELF. The engine answers recent first
// within each reach (internal/session's StandingHere) and the store's bands
// arrive in home's own triage order ([standTriage]); a reading that sorted
// again would be a second authority on rows two other surfaces have already
// ordered.
//
// THE FOURTH SHELF IS DEDUPLICATED AGAINST THE THREE, BY ITEM. An order over
// this conversation is a document in a project's folder like any other, so
// filing it twice would put one order on two shelves of one screen under two
// different claims about where it stands. A `not here` line accounts for its
// order too: the person excepted it from here on purpose, and pushing it down
// to "in other projects" would answer that gesture by redrawing the thing they
// had just pushed away.
func standingShelves(stand, excepted, elsewhere []StandingItemView) []standRow {
	var rows []standRow
	seen := make(map[string]bool, len(stand)+len(excepted))
	file := func(view StandingItemView) {
		if id := strings.TrimSpace(view.Item.ID); id != "" {
			seen[id] = true
		}
	}
	for _, level := range []standing.Altitude{
		standing.AltitudeConversation, standing.AltitudeProject, standing.AltitudeMachine,
	} {
		var shelf []standRow
		for _, view := range stand {
			if view.Item.Level() != level {
				continue
			}
			file(view)
			shelf = append(shelf, standRow{kind: standRowItem, view: view})
		}
		for _, view := range excepted {
			if view.Item.Level() != level {
				continue
			}
			// A PLACE AN ORDER DOES NOT REACH IS NOT A ROW, it is a footnote to
			// the shelf: nothing about it is running, nothing about it is due,
			// and the only fact worth a line is that it was excepted from here.
			file(view)
			shelf = append(shelf, standRow{kind: standRowNotHere, view: view})
		}
		if len(shelf) == 0 {
			// THE EMPTINESS LAW REACHES THE WHOLE SHELF, heading and all.
			continue
		}
		rows = append(rows, standRow{kind: standRowShelf, shelf: standShelfWord(level)})
		rows = append(rows, shelf...)
	}
	var far []standRow
	for _, view := range elsewhere {
		id := strings.TrimSpace(view.Item.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		far = append(far, standRow{kind: standRowItem, view: view, elsewhere: true})
	}
	if len(far) == 0 {
		return rows
	}
	return append(append(rows, standRow{kind: standRowShelf, shelf: standOtherWord}), far...)
}

// standRowStops is which rows a cursor may come to rest on: the orders, and
// nothing else. A heading, a "not here" line and a "last look" paragraph are
// things to READ — furniture the page chose — and a selection on one would be a
// selection no verb has anything to do with.
func standRowStops(rows []standRow) []int {
	var stops []int
	for at, row := range rows {
		if row.kind == standRowItem {
			stops = append(stops, at)
		}
	}
	return stops
}

// standRowLabel is an order's own half of its row: the mark every aforge screen
// agrees on ([standing.Item.Glyph] is the authority), and what the order is
// called.
func standRowLabel(row standRow, pal palette) string {
	if row.kind != standRowItem {
		return ""
	}
	glyph := standGlyph(row.view.Item, row.view.Running, false, pal.ascii)
	return glyph + " " + strings.TrimSpace(row.view.Item.Title())
}

// standRowNote is the dim tail: where the order stands, in [standRollup]'s words
// and never in a second set of them. A heading and a "not here" line have no
// tail at all, which is also what makes them one line at every width
// ([overlayItemLines] counts the tail).
//
// THE WORDS OUTRANK THE ROLLUP, exactly as they do on home's own row
// ([standFitNote] is that clip): the tail can be a whole sentence a run stopped
// on, and a row that spent all of a narrow frame on it would be a page of
// clauses with nothing to tell the orders apart. At [tierPhone] the tail has a
// line of its own and is left whole.
func standRowNote(row standRow, width int, now time.Time) string {
	if row.kind != standRowItem {
		return ""
	}
	note := standRollup(row.view, now)
	if phoneList(width) {
		return note
	}
	return standFitNote(note, width)
}

// standRowNotHereLine is the footnote under a shelf: one order that deliberately
// does not reach this place.
func standRowNotHereLine(row standRow, pal palette) string {
	glyph := standNotHereGlyph
	if pal.ascii {
		glyph = standNotHereASCII
	}
	return glyph + " " + standNotHereWord + ": " + strings.TrimSpace(row.view.Item.Title())
}

// standLastLook is what the order under the cursor did the last time it looked,
// in the sentence [standing.LastLookLine] composes from the three facts a firing
// leaves on the record.
//
// IT IS THE ONE THING A PLACE CAN SAY THAT AN OVERLAY COULD NOT. The row's own
// tail is a clause and has to stay one — it is drawn on every row and shares its
// line with the order's name — while this is the whole account of what was
// actually seen, which needs a paragraph and only ever belongs to the row a
// person stopped on.
//
// AN ORDER THAT WAS NEVER LOOKED AT SAYS NOTHING AT ALL: no heading, no blank
// line, no "never run". That is the emptiness law, and it is also what keeps a
// page of rules — which are never examined and never fire — exactly as short as
// it was before this paragraph existed.
func standLastLook(view StandingItemView, width int, pal palette, now time.Time) []string {
	line := standing.LastLookLine(view.Item, now)
	if line == "" {
		return nil
	}
	head := strings.TrimSpace(view.Item.Title()) + ", last look"
	if age := sinceAt(view.Item.LastFired, now); age != "" {
		head += " · " + age
	}
	return []string{"", pal.dim(fit("  "+head, width)), pal.ink(fit("  "+line, width))}
}

// standingLines paints the rows into exactly the `room` lines the frame reserved
// for them, and answers the map a click resolves against, the top it scrolled
// to, and the window it had room for.
//
// ONE LAYOUT ANSWERS BOTH THE PAINT AND THE POINTER. Every line emitted records
// the row it belongs to, in the order it was emitted, so a press can never land
// on the row above: the map is not a second walk of the list that has to agree
// with this one, it IS this one.
//
// AND THE WINDOW IS WHATEVER IS LEFT, which is the whole of what promotion
// bought this list: the frame says how many rows there are, the cursor is
// followed within them, and nothing is behind a fold.
func standingLines(rows []standRow, cursor, top, width, room, hover int, pal palette, now time.Time) (
	lines []string, owner []int, scrolled, shown int) {
	if room <= 0 || width < 1 {
		return nil, nil, top, 0
	}
	shown = overlayItems(room-1, width)
	scrolled = listTop(cursor, top, len(rows), shown)
	fill := newOverlayFill(width, room, pal, hover)
	// THE HEADINGS ARE [overlayFill.plain] LINES, which is what makes them
	// unpressable without anything downstream having to know they are headings:
	// plain records the line as belonging to row -1, and the pointer's own
	// resolver already swallows -1.
	fill.plain(pal.dim(fit("  "+standHeading, width)))
	for at := scrolled; at < len(rows) && fill.room(); at++ {
		row := rows[at]
		var fitted bool
		switch row.kind {
		case standRowShelf:
			fitted = fill.plain(pal.dim(fit("  "+row.shelf, width)))
		case standRowNotHere:
			fitted = fill.plain(pal.dim(fit("  "+standRowNotHereLine(row, pal), width)))
		default:
			fitted = fill.add(at, standRowLabel(row, pal), standRowNote(row, width, now), at == cursor, false)
			if fitted && at == cursor {
				// THE PARAGRAPH ANSWERS TO NO CURSOR. It is drawn about the row
				// above it and belongs to that row's selection, not to a line of
				// its own — so it goes in as a plain line and a click on it is
				// swallowed, exactly as a heading's is.
				for _, line := range standLastLook(row.view, width, pal, now) {
					if !fill.plain(line) {
						fitted = false
						break
					}
				}
			}
		}
		if !fitted {
			break
		}
	}
	lines, owner = fill.done()
	// THE BLOCK IS EXACTLY THE HEIGHT IT WAS PROMISED (palette.go's
	// [overlayFill.done] says why the frame depends on it). This reading pads it
	// itself because it draws lines the fill cannot count for it — the heading
	// above the list, the shelf headings between the rows, and the paragraph
	// under the cursor — so the rows can run out well before the frame's do.
	for len(lines) < room {
		lines = append(lines, "")
		owner = append(owner, -1)
	}
	return lines, owner, scrolled, shown
}
