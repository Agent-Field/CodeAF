package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE STANDING PLACE'S READING, ON ITS OWN.
//
// Everything here is asserted against plain data and a palette — no app, no
// clock, no disk — because that is what the reading layer IS. What a person
// does to the place, and which seam answered which row, is place_standing_test.go's.

// standingNow is the pinned instant every fixture here is read at. A clause
// counted in hours cannot be tested by waiting.
var standingNow = time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)

// standingView is one order as a row needs it.
func standingView(id, title string, level standing.Altitude) StandingItemView {
	return StandingItemView{Item: standing.Item{
		ID:       id,
		Words:    title + ", every time, without me asking",
		Brief:    standing.Brief{Title: title},
		Altitude: level,
		When:     standing.When{Kind: standing.WhenEvery, Words: "Mondays at 9am", Every: "168h"},
		Status:   standing.StatusActive,
	}}
}

// standingFixture is one plausible reading: something over this conversation,
// something over its project, something everywhere, one order kept out of here,
// and two more the machine holds somewhere else.
func standingFixture() (stand, excepted, elsewhere []StandingItemView) {
	stand = []StandingItemView{
		standingView("c1", "keep the tests green", standing.AltitudeConversation),
		standingView("p1", "draft the weekly update", standing.AltitudeProject),
		standingView("m1", "never touch the public API", standing.AltitudeMachine),
	}
	excepted = []StandingItemView{
		standingView("x1", "post the standup", standing.AltitudeMachine),
	}
	elsewhere = []StandingItemView{
		standingView("f1", "watch the release feed", standing.AltitudeProject),
		standingView("f2", "keep the changelog index fresh", standing.AltitudeProject),
		// The machine's list holds this conversation's own orders as well — they
		// are documents in folders like every other — so the reading has to sift
		// them out rather than trusting its caller to.
		standingView("p1", "draft the weekly update", standing.AltitudeProject),
	}
	return stand, excepted, elsewhere
}

// standingPaint is the reading painted into `room` rows at `width`, as plain
// text with the ink stripped.
func standingPaint(t *testing.T, rows []standRow, cursor, width, room int) ([]string, []int) {
	t.Helper()
	lines, owner, _, _ := standingLines(rows, session.UsageWindow{}, cursor, 0, width, room, -1,
		newPalette(tokens.NoColor, false), standingNow)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.TrimRight(plain(line), " "))
	}
	return out, owner
}

// ── the shelves ─────────────────────────────────────────────────────────────

// FOUR SHELVES, IN THE ORDER A PERSON READS OUTWARD FROM WHERE THEY STAND: this
// conversation, this project, everywhere, and then everything else the machine
// is holding.
func TestTheReadingFilesFourShelvesOutward(t *testing.T) {
	rows := standingShelves(standingFixture())
	var order []string
	for _, row := range rows {
		if row.kind == standRowShelf {
			order = append(order, row.shelf)
		}
	}
	want := []string{standInHereWord, standProjectWord, standEverywhereWord, standOtherWord}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("the shelves are %v, want %v", order, want)
	}
}

// AN EMPTY SHELF DRAWS NOTHING AT ALL — not the heading, not a line saying it is
// empty. It is the emptiness law at its most literal, and it is what keeps the
// place one line long on the ordinary conversation with one order over it.
func TestTheReadingDropsAnEmptyShelfWhole(t *testing.T) {
	rows := standingShelves(
		[]StandingItemView{standingView("p1", "draft the weekly update", standing.AltitudeProject)},
		nil, nil)
	if len(rows) != 2 || rows[0].kind != standRowShelf || rows[0].shelf != standProjectWord {
		t.Fatalf("one order made %d rows: %+v", len(rows), rows)
	}
	// AND A READING WITH NOTHING IN IT IS NO ROWS AT ALL, which is what lets the
	// place refuse to open rather than opening onto a heading.
	if got := standingShelves(nil, nil, nil); len(got) != 0 {
		t.Fatalf("an empty reading made %d rows", len(got))
	}
}

// A PLACE AN ORDER DOES NOT REACH IS ONE LINE UNDER THE SHELF IT WOULD HAVE BEEN
// ON, and it is not a row: no mark about what it is doing, no clause about where
// it stands, and no cursor.
func TestTheReadingFilesAnExceptedOrderUnderItsShelf(t *testing.T) {
	stand, excepted, _ := standingFixture()
	rows := standingShelves(stand, excepted, nil)
	at := -1
	for i, row := range rows {
		if row.kind == standRowNotHere {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the excepted order is not on the page")
	}
	if rows[at].view.Item.ID != "x1" {
		t.Fatalf("the not-here line is about %q", rows[at].view.Item.ID)
	}
	pal := newPalette(tokens.NoColor, false)
	if got, want := standRowNotHereLine(rows[at], pal),
		standNotHereGlyph+" "+standNotHereWord+": post the standup"; got != want {
		t.Fatalf("the line reads %q, want %q", got, want)
	}
	// It has no tail and no label, which is what makes it one line at every
	// width and unreachable by the cursor.
	if note := standRowNote(rows[at], 200, standingNow); note != "" {
		t.Fatalf("a not-here line grew a tail: %q", note)
	}
	if label := standRowLabel(rows[at], pal); label != "" {
		t.Fatalf("a not-here line grew a label: %q", label)
	}
}

// THE MACHINE'S SHELF IS DEDUPLICATED AGAINST THE THREE ABOVE IT, BY ITEM. An
// order over this conversation is a document in a folder like any other, so
// filing it twice would put one order on two shelves of one screen under two
// different claims about where it stands.
func TestTheReadingDeduplicatesTheMachineAgainstTheThreeShelves(t *testing.T) {
	rows := standingShelves(standingFixture())
	seen := map[string]int{}
	for _, row := range rows {
		if row.kind != standRowShelf {
			seen[row.view.Item.ID]++
		}
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("%q is on the page %d times", id, n)
		}
	}
	var far []string
	for _, row := range rows {
		if row.elsewhere {
			far = append(far, row.view.Item.ID)
		}
	}
	if strings.Join(far, ",") != "f1,f2" {
		t.Fatalf("the machine's shelf holds %v", far)
	}
}

// AND AN ORDER KEPT OUT OF HERE IS ACCOUNTED FOR TOO. The person excepted it on
// purpose; pushing it down to "in other projects" would answer that gesture by
// redrawing the thing they had just pushed away.
func TestAnExceptedOrderIsNotRefiledOnTheMachinesShelf(t *testing.T) {
	excepted := []StandingItemView{standingView("x1", "post the standup", standing.AltitudeMachine)}
	rows := standingShelves(nil, excepted, []StandingItemView{
		standingView("x1", "post the standup", standing.AltitudeMachine),
	})
	for _, row := range rows {
		if row.elsewhere {
			t.Fatalf("an excepted order came back as another project's: %+v", row)
		}
	}
}

// THE CURSOR STOPS ONLY ON ORDERS. A heading and a `not here` line are things to
// READ, and a selection on one is a selection no verb has anything to do with.
func TestTheReadingStopsOnlyOnOrders(t *testing.T) {
	rows := standingShelves(standingFixture())
	stops := standRowStops(rows)
	if len(stops) != 5 {
		t.Fatalf("%d rows are cursor-legal, want 5", len(stops))
	}
	for _, at := range stops {
		if rows[at].kind != standRowItem {
			t.Fatalf("row %d is a %v and is offered as a stop", at, rows[at].kind)
		}
	}
}

// ── the paint ───────────────────────────────────────────────────────────────

// EVERY ROW IS DRAWN IN ONE GRAMMAR: the mark every aforge screen agrees on
// ([standing.Item.Glyph]), what the order is called, and home's own clause
// ([standRollup]) — on the machine's shelf exactly as on the three above it.
func TestTheReadingDrawsEveryShelfInOneGrammar(t *testing.T) {
	rows := standingShelves(standingFixture())
	lines, _ := standingPaint(t, rows, -1, 120, 20)
	screen := strings.Join(lines, "\n")
	for _, want := range []string{
		standHeading,
		standWaitGlyph + " keep the tests green",
		standWaitGlyph + " watch the release feed",
		"Mondays at 9am",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the reading does not draw %q:\n%s", want, screen)
		}
	}
	// AND NOTHING ON IT SPEAKS THE MACHINERY'S OWN WORDS.
	for _, banned := range []string{"altitude", "scope", "machine", "conversation altitude"} {
		if strings.Contains(strings.ToLower(screen), banned) {
			t.Fatalf("the reading says the machinery's word %q:\n%s", banned, screen)
		}
	}
}

// AND IT DRAWS NO FOLD. The list showed four rows and hid the rest behind a
// `▸ N more` line no key answered — a door onto nothing. A place takes the whole
// terminal, so the window is whatever the frame had room for and the cursor
// walks the rest.
func TestTheReadingDrawsNoFold(t *testing.T) {
	var elsewhere []StandingItemView
	for _, one := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		elsewhere = append(elsewhere, standingView(one, "watch the "+one+" thing", standing.AltitudeProject))
	}
	rows := standingShelves(nil, nil, elsewhere)
	lines, _ := standingPaint(t, rows, 1, 120, 20)
	screen := strings.Join(lines, "\n")
	if strings.Contains(screen, "▸") || strings.Contains(screen, " more") {
		t.Fatalf("the reading folded rows behind a line no key answers:\n%s", screen)
	}
	for _, one := range []string{"a", "g"} {
		if !strings.Contains(screen, "watch the "+one+" thing") {
			t.Fatalf("a frame with room for every order did not draw %q:\n%s", one, screen)
		}
	}
}

// THE WINDOW FOLLOWS THE CURSOR, so a cursor past the bottom of the window
// scrolls the list rather than walking off the screen.
func TestTheReadingScrollsToTheRowTheCursorIsOn(t *testing.T) {
	var elsewhere []StandingItemView
	for _, one := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		elsewhere = append(elsewhere, standingView(one, "watch the "+one+" thing", standing.AltitudeProject))
	}
	rows := standingShelves(nil, nil, elsewhere)
	last := len(rows) - 1
	lines, _, top, shown := standingLines(rows, session.UsageWindow{}, last, 0, 120, 4, -1,
		newPalette(tokens.NoColor, false), standingNow)
	if shown < 1 || top == 0 {
		t.Fatalf("a four-row frame did not scroll (top %d, window %d)", top, shown)
	}
	screen := ""
	for _, line := range lines {
		screen += plain(line) + "\n"
	}
	if !strings.Contains(screen, "watch the g thing") {
		t.Fatalf("the row under the cursor is not on the frame:\n%s", screen)
	}
}

// ONE LAYOUT ANSWERS BOTH THE PAINT AND THE POINTER. Every line records the row
// it belongs to, in the order it was emitted, so a press can never land on the
// row above — and a heading, a `not here` line and a `last look` paragraph
// answer to no row at all.
func TestTheReadingsOwnerMapMatchesTheLinesItPainted(t *testing.T) {
	rows := standingShelves(standingFixture())
	lines, owner := standingPaint(t, rows, 1, 120, 20)
	if len(owner) != len(lines) {
		t.Fatalf("%d lines and %d owners", len(lines), len(owner))
	}
	for i, at := range owner {
		if at < 0 {
			continue
		}
		if at >= len(rows) || rows[at].kind != standRowItem {
			t.Fatalf("line %d claims row %d, which is not an order", i, at)
		}
		if name := strings.TrimSpace(rows[at].view.Item.Title()); !strings.Contains(lines[i], name) {
			t.Fatalf("line %d is owned by %q but reads %q", i, name, lines[i])
		}
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == standHeading || strings.TrimSpace(line) == standOtherWord {
			if owner[i] != -1 {
				t.Fatalf("the heading on line %d answers to row %d", i, owner[i])
			}
		}
	}
}

// THE LAST LOOK FOLLOWS THE CURSOR AND SAYS NOTHING WHEN THERE IS NOTHING TO
// SAY: no heading, no blank line, no "never run".
func TestTheReadingsLastLookFollowsTheCursor(t *testing.T) {
	looked := standingView("looked", "the 6am watch", standing.AltitudeProject)
	looked.Item.When = standing.When{Kind: standing.WhenProbe, Words: "daily"}
	looked.Item.LastChecked = standingNow.Add(-3 * time.Hour)
	looked.Item.LastCheckLine = "nothing had changed since yesterday"
	never := standingView("never", "watch the release feed", standing.AltitudeProject)
	rows := standingShelves(nil, nil, []StandingItemView{looked, never})

	lines, _ := standingPaint(t, rows, 1, 120, 20)
	screen := strings.Join(lines, "\n")
	for _, want := range []string{
		"the 6am watch, last look",
		"looked 3h ago · nothing had changed since yesterday, so nothing was done",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the reading does not say %q:\n%s", want, screen)
		}
	}
	lines, _ = standingPaint(t, rows, 2, 120, 20)
	if screen = strings.Join(lines, "\n"); strings.Contains(screen, "last look") {
		t.Fatalf("an order that has never been looked at drew a last look:\n%s", screen)
	}
}

// ── every width ─────────────────────────────────────────────────────────────

// THE READING HOLDS AT EVERY TIER, phone included: no line wider than the frame,
// exactly the rows it was promised, and the headings and the orders still
// readable.
func TestTheReadingHoldsAtEveryWidth(t *testing.T) {
	rows := standingShelves(standingFixture())
	for _, width := range []int{44, 60, 80, 120, 200} {
		for _, room := range []int{6, 20} {
			lines, owner := standingPaint(t, rows, 1, width, room)
			if len(lines) != room {
				t.Fatalf("at %d×%d the reading drew %d lines", width, room, len(lines))
			}
			if len(owner) != room {
				t.Fatalf("at %d×%d the map has %d entries for %d lines", width, room, len(owner), room)
			}
			for _, line := range lines {
				if got := ansi.StringWidth(line); got > width {
					t.Fatalf("at %d a line is %d cells: %q", width, got, line)
				}
			}
			if room < 20 {
				continue
			}
			screen := strings.Join(lines, "\n")
			for _, want := range []string{standHeading, standInHereWord, standOtherWord, "keep the tests green"} {
				if !strings.Contains(screen, want) {
					t.Fatalf("at %d the reading is missing %q:\n%s", width, want, screen)
				}
			}
		}
	}
}

// AND SO DOES A ROW WHOSE AUTHOR WROTE IT IN WIDE CHARACTERS WITH AN UNBOUNDED
// CADENCE. The person's own words are kept whole in the record; what a narrow
// frame does is clip them for the drawing, never overflow.
func TestTheReadingFitsAuthoredUnicodeAndAnUnboundedCadence(t *testing.T) {
	wide := StandingItemView{Item: standing.Item{
		ID: "wide", Words: strings.Repeat("東京🧭", 30), Status: standing.StatusActive,
		When: standing.When{Kind: standing.WhenEvery, Words: strings.Repeat("界", 250)},
	}}
	rows := standingShelves(nil, nil, []StandingItemView{wide})
	for _, width := range []int{44, 60, 80, 120, 200} {
		lines, _ := standingPaint(t, rows, 1, width, 12)
		for _, line := range lines {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("width %d drew %d cells: %q", width, got, line)
			}
		}
	}
}

// THE EMPTINESS LAW REACHES EVERY FIGURE THE READING CAN DRAW — no `$0.00`, no
// count of what a person can already see beside the heading, and no shelf over
// nothing.
func TestTheReadingObeysTheEmptinessLaw(t *testing.T) {
	rows := standingShelves(nil, nil, []StandingItemView{
		standingView("f1", "watch the release feed", standing.AltitudeProject),
	})
	lines, _ := standingPaint(t, rows, 1, 120, 20)
	screen := strings.Join(lines, "\n")
	for _, banned := range []string{
		"$0.00", "0 tok", "1 standing", "waiting to be stood up",
		standInHereWord, standProjectWord, standEverywhereWord,
	} {
		if strings.Contains(screen, banned) {
			t.Fatalf("the reading draws %q over nothing:\n%s", banned, screen)
		}
	}
	if !strings.Contains(screen, standHeading) {
		t.Fatalf("the reading lost its heading:\n%s", screen)
	}
}

// ── the rope column ─────────────────────────────────────────────────────────

// THE ROPE LEADS THE ROW'S TAIL, IN THREE RUNGS, AND IT IS THE SHARED
// DERIVATION.
//
// Screen 2f says this is "the only fact that changes whether you have to watch
// it", so it goes at the head of the tail where a narrow frame's clip cannot
// reach it first. And the rung is [standing.RopeWord]'s answer: three rungs are
// a rule about a person's trust in a machine, and a surface that decided for
// itself which one a row was on would be a second authority on the one question
// this column exists to ask.
func TestTheRopeLeadsTheRowsTailInThreeRungs(t *testing.T) {
	rung := func(clean int, grant string) string {
		view := standingView("r", "watch the release feed", standing.AltitudeProject)
		view.Item.Grant, view.Item.CleanRuns = grant, clean
		rows := standingShelves(nil, nil, []StandingItemView{view})
		return standRowNote(rows[len(rows)-1], 200, standingNow)
	}
	for _, row := range []struct {
		clean int
		grant string
		want  string
	}{
		{0, "", standing.RopeAsksFirst},
		{99, "", standing.RopeAsksFirst},
		{0, "tell me without asking", "earning trust 0/5"},
		{4, "tell me without asking", "earning trust 4/5"},
		{5, "tell me without asking", standing.RopeTrusted},
	} {
		got := rung(row.clean, row.grant)
		if !strings.HasPrefix(got, row.want) {
			t.Fatalf("a row with grant %q and %d clean firings leads with %q, want %q",
				row.grant, row.clean, got, row.want)
		}
		// AND THE STATUS CLAUSE IS STILL BEHIND IT — home's own [standRollup] and
		// not a second set of words for the same record.
		if !strings.Contains(got, "Mondays at 9am") {
			t.Fatalf("the rope displaced the clause: %q", got)
		}
	}
}

// A RULE THAT ONLY HOLDS HAS NO ROPE TO REPORT. Nothing examines it, nothing
// fires it, and it cannot act unattended however long it stands — so `asks
// first` there would be answering a question the row does not raise. That is the
// emptiness law applied to a fact rather than to a figure.
func TestARuleThatOnlyHoldsHasNoRopeToReport(t *testing.T) {
	rule := standingView("h", "never touch the public API", standing.AltitudeProject)
	rule.Item.When = standing.When{Kind: standing.WhenHold}
	rows := standingShelves(nil, nil, []StandingItemView{rule})
	note := standRowNote(rows[len(rows)-1], 200, standingNow)
	if note != standHoldsWord {
		t.Fatalf("a rule's tail is %q, want %q", note, standHoldsWord)
	}
	for _, banned := range []string{standing.RopeAsksFirst, standing.RopeTrusted, "earning trust"} {
		if strings.Contains(note, banned) {
			t.Fatalf("a rule that only holds says %q", banned)
		}
	}
}

// AND THE ROPE SURVIVES A NARROW FRAME while the clause gives way, which is what
// putting it first is for.
func TestTheRopeOutlastsTheClauseOnANarrowRow(t *testing.T) {
	view := standingView("r", "watch every agentfield repo and summarise what merged",
		standing.AltitudeProject)
	view.Item.Grant, view.Item.CleanRuns = "summarise without asking", standing.TrustAfter
	rows := standingShelves(nil, nil, []StandingItemView{view})
	for _, width := range []int{60, 80, 120, 200} {
		note := standRowNote(rows[len(rows)-1], width, standingNow)
		if !strings.HasPrefix(note, standing.RopeTrusted) {
			t.Fatalf("at %d the tail is %q and does not lead with the rope", width, note)
		}
	}
}

// ── the window: when it fired ───────────────────────────────────────────────

// THE LABEL IS THE CONTROL AND THE READING AT ONCE, drawn BETWEEN ITS ARROWS on
// the header row — screen 3d's own move. A person reads the span they are
// looking at and the keys that move it in one glance, on the row that reports
// it.
func TestTheWindowIsDrawnBetweenItsArrowsOnTheHeader(t *testing.T) {
	// THE WINDOW'S OWN CALENDAR IS THE LOCAL ONE ([session.UsageWindow] buckets
	// on local days), so the fixture is built in it — a window handed UTC
	// midnights would label the day before wherever the machine sits west of it.
	win := session.UsageWindow{
		From:  time.Date(2026, 8, 12, 0, 0, 0, 0, time.Local),
		To:    time.Date(2026, 8, 25, 0, 0, 0, 0, time.Local),
		Grain: session.GrainDay,
	}
	head := plain(standingHeaderRow(120, win, newPalette(tokens.NoColor, false)))
	for _, want := range []string{standHeading, "shift+← ", "aug 12 – aug 25", " →"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the header does not draw %q: %q", want, head)
		}
	}
	if at, over := strings.Index(head, "shift+←"), strings.Index(head, "aug 12"); at < 0 || at > over {
		t.Fatalf("the label is not between its arrows: %q", head)
	}
	if got := ansi.StringWidth(head); got != 120 {
		t.Fatalf("the header is %d cells at a width of 120: %q", got, head)
	}
}

// A FRAME TOO NARROW TO DRAW THE CONTROL HAS NO WINDOW AT ALL — the arrows are
// not drawn and, because one predicate answers both, the keys are not bound
// either. A control bound but invisible is the defect the verb strip exists to
// end; a capability that cannot work is absent, not broken.
func TestANarrowFrameDrawsNoWindowControl(t *testing.T) {
	win := session.UsageWindow{
		From:  time.Date(2026, 8, 12, 0, 0, 0, 0, time.Local),
		To:    time.Date(2026, 8, 25, 0, 0, 0, 0, time.Local),
		Grain: session.GrainDay,
	}
	pal := newPalette(tokens.NoColor, false)
	if standingWindowRoom(44, win) {
		t.Fatal("a phone frame claims room for the window control")
	}
	head := plain(standingHeaderRow(44, win, pal))
	if strings.Contains(head, "shift+") {
		t.Fatalf("a narrow header drew the control anyway: %q", head)
	}
	if !strings.Contains(head, standHeading) {
		t.Fatalf("the narrow header lost the page's name: %q", head)
	}
	// AND A WINDOW NOBODY HAS CHOSEN DRAWS NOTHING AT ANY WIDTH: arrows around
	// no reading would be a control over nothing.
	if standingWindowRoom(200, session.UsageWindow{}) {
		t.Fatal("an unchosen window claims room")
	}
	if got := plain(standingHeaderRow(200, session.UsageWindow{}, pal)); strings.Contains(got, "shift+") {
		t.Fatalf("an unchosen window drew arrows: %q", got)
	}
}

// THE PLACE OPENS ON A WINDOW THAT HOLDS EVERY FIRING IT CAN SEE, so the first
// frame hides nothing. A reference page whose whole job is to say what stands
// over you may not arrive having quietly dropped an order; narrowing is the
// person's own deliberate act.
func TestTheWindowOpensHoldingEveryFiring(t *testing.T) {
	old := standingView("old", "watch the release feed", standing.AltitudeProject)
	old.Item.LastFired = standingNow.AddDate(0, -3, 0)
	recent := standingView("recent", "keep the changelog index fresh", standing.AltitudeProject)
	recent.Item.LastFired = standingNow.AddDate(0, 0, -1)
	never := standingView("never", "never touch the public API", standing.AltitudeProject)

	win := standingOpenWindow(standingNow, []StandingItemView{old, recent, never})
	for _, view := range []StandingItemView{old, recent} {
		if !win.Holds(view.Item.LastFired) {
			t.Fatalf("the window it opens on drops %q, fired %s", view.Item.ID, view.Item.LastFired)
		}
	}
	if kept := standingInWindow([]StandingItemView{old, recent, never}, win); len(kept) != 3 {
		t.Fatalf("the opening window scoped %d of 3 orders away", 3-len(kept))
	}
	// A MACHINE THAT HAS NEVER FIRED ANYTHING OPENS ON TODAY rather than on a
	// span invented backwards over an empty calendar.
	quiet := standingOpenWindow(standingNow, []StandingItemView{never})
	if !quiet.Holds(standingNow) {
		t.Fatalf("a machine with no firings opened on %q", quiet.Label())
	}
}

// AN ORDER THAT HAS NEVER FIRED IS NEVER SCOPED AWAY. A rule that only holds is
// never examined and never fires; a watch whose first moment has not come has no
// firing either. Neither has a date to be outside a window, and filtering a
// record by a fact it does not have would answer "not in this fortnight" about
// something that was never anywhere.
func TestTheWindowNeverScopesAwayAnOrderThatHasNeverFired(t *testing.T) {
	fortnight := session.LastDays(standingNow, 14)
	inside := standingView("inside", "keep the changelog index fresh", standing.AltitudeProject)
	inside.Item.LastFired = standingNow.AddDate(0, 0, -2)
	outside := standingView("outside", "watch the release feed", standing.AltitudeProject)
	outside.Item.LastFired = standingNow.AddDate(0, -3, 0)
	never := standingView("never", "never touch the public API", standing.AltitudeProject)

	kept := standingInWindow([]StandingItemView{inside, outside, never}, fortnight)
	var ids []string
	for _, view := range kept {
		ids = append(ids, view.Item.ID)
	}
	if strings.Join(ids, ",") != "inside,never" {
		t.Fatalf("a fortnight kept %v, want the recent one and the one that never fired", ids)
	}
}
