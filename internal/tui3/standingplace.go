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
//	› ▲ keep the tests green        your call · the fix touches migrations
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

	"github.com/Agent-Field/aforge-v2/internal/session"
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
	// ([standingPlace.verbs]).
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

// ── the window: WHEN IT FIRED ───────────────────────────────────────────────
//
// Screen 3d gives every place that has a time axis the same four keys, and names
// what each place's axis is: tasks is when it ran, memory is when it was
// learned, and standing is WHEN IT FIRED.
//
// THE WINDOW SCOPES WHAT IS LISTED, AND IT OPENS HOLDING EVERYTHING. A reference
// page whose whole job is to say what is standing over you may not arrive having
// quietly dropped an order — so [standingOpenWindow] measures the oldest firing
// the machine has and opens on a span that reaches it, and narrowing is then the
// person's own deliberate act rather than a default they never chose.
//
// AND AN ORDER THAT HAS NEVER FIRED IS NEVER SCOPED AWAY. A rule that only holds
// is never examined and never fires; a watch whose first moment has not come has
// no firing either. Neither has a date to be outside a window, and filtering a
// record by a fact it does not have is the emptiness law broken from the far
// side — it would answer "not in this fortnight" about something that was never
// anywhere.

// standingOpenWindow is the span the place opens on: from the oldest firing it
// can see to today, by the day, so that the first frame hides nothing.
//
// A MACHINE THAT HAS NEVER FIRED ANYTHING OPENS ON TODAY. There is no history to
// span, and a window invented backwards over an empty calendar would be arrows
// steering nothing.
func standingOpenWindow(now time.Time, groups ...[]StandingItemView) session.UsageWindow {
	oldest := time.Time{}
	for _, views := range groups {
		for _, view := range views {
			at := view.Item.LastFired
			if at.IsZero() {
				continue
			}
			if oldest.IsZero() || at.Before(oldest) {
				oldest = at
			}
		}
	}
	if oldest.IsZero() || oldest.After(now) {
		oldest = now
	}
	return session.UsageWindow{From: oldest, To: now, Grain: session.GrainDay}.Normalized()
}

// standingInWindow keeps the orders whose last firing the window holds, and
// every order that has never fired at all.
func standingInWindow(views []StandingItemView, win session.UsageWindow) []StandingItemView {
	if win.Label() == "" {
		return views
	}
	kept := make([]StandingItemView, 0, len(views))
	for _, view := range views {
		if view.Item.LastFired.IsZero() || win.Holds(view.Item.LastFired) {
			kept = append(kept, view)
		}
	}
	return kept
}

// standingShelvesIn is [standingShelves] over only the orders the window holds.
// It is one function because scoping is one rule, and a caller that filtered its
// own slice before laying them out would be a second place deciding what "when
// it fired" means.
func standingShelvesIn(win session.UsageWindow, stand, excepted, elsewhere []StandingItemView) []standRow {
	return standingShelves(
		standingInWindow(stand, win),
		standingInWindow(excepted, win),
		standingInWindow(elsewhere, win))
}

// standingWindowRoom is whether this frame has room to draw the control beside
// the page's name, asked of the ONE predicate every windowed place asks
// (placeprose.go's [placeWindowFits]). It answers the paint and the keys alike:
// the arrows are never bound where they are not drawn.
func standingWindowRoom(width int, win session.UsageWindow) bool {
	arrows, _ := placeWindowFits(width, standingHeadWords, win)
	return arrows
}

// standingGrainRoom is the same question about the SECOND axis: `shift+↑↓` is
// bound only where the clause naming it is on the header.
func standingGrainRoom(width int, win session.UsageWindow) bool {
	_, grain := placeWindowFits(width, standingHeadWords, win)
	return grain
}

// standingHeadWords is the page's name as the header lays it out, indent and
// all. It is measured as well as drawn, so it is one string.
const standingHeadWords = "  " + standHeading

// standingHeaderRow is the place's first line: what this page is on the left,
// and on the right the window that scopes it — drawn by the one head row every
// place with a time window shares ([placeHeadRow]).
func standingHeaderRow(width int, win session.UsageWindow, pal palette) string {
	return placeHeadRow(width, standingHeadWords, "", win, pal)
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

// standRowNote is the dim tail: HOW MUCH ROPE the order has, and then where it
// stands. A heading and a "not here" line have no tail at all, which is also
// what makes them one line at every width ([overlayItemLines] counts the tail).
//
// THE ROPE LEADS BECAUSE IT IS THE ONE COLUMN THAT EARNS ITS PLACE. Screen 2f
// says so outright — it is "the only fact that changes whether you have to watch
// it" — so it goes at the head of the tail, where the clip that shortens a
// narrow row eats the clause behind it and never the rope.
//
// AND IT IS [standing.RopeWord]'S ANSWER, never a second reading of
// [standing.Item.Grant] taken here. Three rungs are a rule about a person's
// trust in a machine, and a surface that decided for itself which rung a row was
// on would be a second authority on the one question this column exists to ask.
//
// THE STATUS CLAUSE BEHIND IT IS [standRollup]'S, exactly as it has always been:
// your call, checking now, paused, the cadence, what the last look found —
// one derivation with one set of words, shared with home's own rows.
//
// THE WORDS OUTRANK BOTH, exactly as they do on home's own row ([standFitNote]
// is that clip): the tail can be a whole sentence a run stopped on, and a row
// that spent all of a narrow frame on it would be a page of clauses with nothing
// to tell the orders apart. At [tierPhone] the tail has a line of its own and is
// left whole.
func standRowNote(row standRow, width int, now time.Time) string {
	if row.kind != standRowItem {
		return ""
	}
	note := joinDot(standRopeWord(row.view.Item), standRollup(row.view, now))
	if phoneList(width) {
		return note
	}
	return standFitNote(note, width)
}

// standRopeWord is how much rope an order has, and NOTHING AT ALL for one that
// can never act.
//
// A RULE THAT ONLY HOLDS HAS NO ROPE TO REPORT. Nothing examines it, nothing
// fires it, and it cannot do a thing unattended however long it stands — so
// `asks first` on such a row would be answering a question the row does not
// raise, which is the emptiness law applied to a fact rather than to a figure
// ([standRollup] keeps the same silence about a rule's cadence).
func standRopeWord(item standing.Item) string {
	if item.When.Kind == standing.WhenHold {
		return ""
	}
	return standing.RopeWord(item)
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
func standingLines(rows []standRow, win session.UsageWindow, cursor, top, width, room, hover int,
	pal palette, now time.Time) (lines []string, owner []int, scrolled, shown int) {
	if room <= 0 || width < 1 {
		return nil, nil, top, 0
	}
	shown = overlayItems(room-1, width)
	scrolled = listTop(cursor, top, len(rows), shown)
	fill := newOverlayFill(width, room, pal, hover)
	// THE HEADINGS ARE [overlayFill.plain] LINES, which is what makes them
	// unpressable without anything downstream having to know they are headings:
	// plain records the line as belonging to row -1, and the pointer's own
	// resolver already swallows -1. The window control rides the first of them
	// for the same reason it is drawn there at all — it is about the WHOLE list
	// and answers to no row on it.
	fill.plain(standingHeaderRow(width, win, pal))
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

// standingTeach spends an empty page on explaining the place rather than drawing
// a time control over a list with nothing in it.
//
// THE FIRST SENTENCE IS THE ONE THIS PLACE HAS ALWAYS SAID ([standNothingWord]),
// which is why it is not spelled again here: it was the refusal's own words back
// when a machine with nothing standing on it opened no page at all, and moving a
// sentence from a transcript line to the body of the place it was about is the
// whole of what that change was. The two under it are the same three-line shape
// tasks and memory teach in ([tasksTeach], [memoryTeaching]): what the place
// holds, where the things in it come from, and what a key does once there is
// something here.
func standingTeach(pal palette) []string {
	return []string{
		pal.dim(standNothingWord),
		pal.dim("an order stands until you stop it, and it can reach just this conversation, this project, or everywhere."),
		pal.dim("enter opens the conversation that made one, when there is one here."),
	}
}

// standingSummary is WHAT IS BEHIND THIS PLACE, in the one clause home's typed
// drop-up has room for: `6 orders, 1 fired today` (SCREEN 1g).
//
// THE NOUN IS `orders` AND NOT THE DESIGN'S `promises`. This place calls itself
// `standing orders` on its own heading ([standHeading]), the manual says it that
// way, and every other sentence on this surface about one of these rows uses
// that noun — so a second noun for one thing would be exactly the drift
// CLAUDE.md's one-source-of-truth law is written against. The row's left word is
// already `standing`, which is what lets the clause say `orders` and still read
// as a whole sentence.
//
// AND NOTHING IS SAID ABOUT NOTHING. A machine that holds no orders draws no
// clause at all — the row then says only what kind of thing it is — and the
// firing clause is absent on a day nothing fired, rather than drawn as a nought.
func standingSummary(views []StandingItemView, now time.Time) string {
	// ONE ORDER IS COUNTED ONCE, and it is counted by ID. An order that reaches
	// the whole machine stands over every project, so the bands this list is
	// built from hold it under each of them ([app.standingPlaceViews] walks the
	// keys of both maps) — and a clause that added those up would tell somebody
	// they had six promises on a machine holding four.
	held, fired := map[string]bool{}, map[string]bool{}
	for _, view := range views {
		if held[view.Item.ID] {
			continue
		}
		held[view.Item.ID] = true
		if sameDay(view.Item.LastFired, now) {
			fired[view.Item.ID] = true
		}
	}
	if len(held) == 0 {
		return ""
	}
	clause := groupedInt(len(held)) + " " + plural("order", len(held))
	if len(fired) > 0 {
		clause += ", " + groupedInt(len(fired)) + " fired today"
	}
	return clause
}
