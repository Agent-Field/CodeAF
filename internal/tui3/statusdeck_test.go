package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// deckApp is the phone-tier harness: a session with a name, a model, a bill
// and a meter, at a width the deck answers for. The welcome box is dismissed
// the way a first keystroke dismisses it, because the deck keeps the row's own
// quiet law ([app.statusQuiet]) and a greeting over it would empty the numbers
// these tests are reading.
func deckApp(t *testing.T) *app {
	t.Helper()
	a, _, _ := hudApp(t)
	a.width, a.height = 44, 24
	a.cost = 0.42
	a.ctxTokens, a.ctxWindow = 12400, 128000
	a.dismissWelcome()
	return a
}

// deckRowsOf is the deck as a reader sees it: the two rows, plain.
func deckRowsOf(a *app) []string {
	rows := a.statusRows(a.width)
	if len(rows) != deckHeight {
		panic("the deck is not two rows")
	}
	return []string{plain(rows[0]), plain(rows[1])}
}

// THE DECK IS TWO ROWS, ALWAYS. The wide row's second row is wrap-driven — it
// exists only when the clusters would collide — and the deck's is not: a phone
// frame gets the same two rows in every state, which is what lets the chrome
// height be a constant ([app.statusHeight]).
func TestStatusDeckIsTwoRowsInEveryState(t *testing.T) {
	a := deckApp(t)
	if got := a.statusHeight(a.width); got != deckHeight {
		t.Fatalf("statusHeight(%d) = %d, want the deck's constant %d", a.width, got, deckHeight)
	}
	if rows := a.statusRows(a.width); len(rows) != deckHeight {
		t.Fatalf("statusRows(%d) returned %d rows, want %d", a.width, len(rows), deckHeight)
	}
	// And the same answer while a turn is running, when the state word and the
	// burn are on the line.
	a.state = stateWorking
	a.turnBegan = a.now().Add(-4 * time.Second)
	if rows := a.statusRows(a.width); len(rows) != deckHeight {
		t.Fatalf("statusRows(%d) mid-turn returned %d rows, want %d", a.width, len(rows), deckHeight)
	}
}

// ROW 1 IS THE CRUMB'S OWN STEP AGAINST THE SPEND AND THE METER. ISSUE-126
// moved the identity to the top bar's crumb, and the deck's first row is that
// crumb's current step: the session's name in the conversation, the room's
// chip in a room. The ▸ on its right end is the door to the sheet.
func TestStatusDeckTopRowIsTheCrumbStepAgainstTheSpend(t *testing.T) {
	a := deckApp(t)
	rows := deckRowsOf(a)
	if !strings.Contains(rows[0], "aforge-v2") {
		t.Fatalf("row 1 = %q, want the session's name", rows[0])
	}
	if !strings.Contains(rows[0], "$0.42") {
		t.Fatalf("row 1 = %q, want the bill", rows[0])
	}
	if !strings.Contains(rows[0], "10%") {
		t.Fatalf("row 1 = %q, want the meter's percent", rows[0])
	}
	if !strings.Contains(rows[0], deckMore) {
		t.Fatalf("row 1 = %q, want the sheet's ▸", rows[0])
	}
}

// ROW 2 IS WHAT IS ANSWERING AGAINST WHAT IS STILL MOVING. The model is its
// basename — the rider and the full routing address are the sheet's — and the
// right end is the state word, which is the last segment to go on the wide row
// and the last here.
func TestStatusDeckModelRowIsTheBasenameAgainstTheState(t *testing.T) {
	a := deckApp(t)
	a.state = stateWorking
	a.turnBegan = a.now().Add(-4 * time.Second)
	rows := deckRowsOf(a)
	if !strings.Contains(rows[1], "deepseek-v4-flash") {
		t.Fatalf("row 2 = %q, want the model's basename", rows[1])
	}
	if strings.Contains(rows[1], "deepseek/deepseek-v4-flash") {
		t.Fatalf("row 2 = %q, want the basename, not the full routing address", rows[1])
	}
	if !strings.Contains(rows[1], "working") {
		t.Fatalf("row 2 = %q, want the state word", rows[1])
	}
}

// THE IDLE WORD IS NOTHING, ON THE DECK AS ON THE WIDE ROW. A session that is
// doing nothing says nothing where the state word would stand — the emptiness
// law's plainest case, and the one the old row broke with a permanent dim
// "idle".
func TestStatusDeckSaysNothingWhenNothingIsHappening(t *testing.T) {
	a := deckApp(t)
	rows := deckRowsOf(a)
	for i, row := range rows {
		if strings.Contains(row, "idle") {
			t.Fatalf("row %d = %q, want no state word for a session at rest", i+1, row)
		}
	}
}

// A ROOM RENAMES BOTH ROWS, which is the wide row's own law at phone width:
// row 1 takes the room's chip — the crumb's current step — and row 2 takes the
// task's own model, because a page about a task that still named the
// conversation's engine would be the deck's half of the same lie.
func TestStatusDeckInARoomNamesTheRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width, a.height = 44, 24
	a.cost = 0.42
	a.ctxTokens, a.ctxWindow = 12400, 128000
	a.dismissWelcome()
	a.openRoom(7, "Fix the nil-map crash")
	if !a.roomOpen() {
		t.Fatal("the room did not open")
	}
	rows := deckRowsOf(a)
	if !strings.Contains(rows[0], "Fix the nil-map crash") {
		t.Fatalf("row 1 in a room = %q, want the room's chip", rows[0])
	}
	if strings.Contains(rows[0], "aforge-v2") {
		t.Fatalf("row 1 in a room = %q, want the room's step, not the session's", rows[0])
	}
}

// THE SHEET IS WHAT THERE IS. The deck is what fits; the sheet is every item,
// one per line, and it keeps the facts the rows gave up: the full routing
// address, the served rider, the crew, the watch.
func TestStatusDeckSheetCarriesWhatTheRowsGaveUp(t *testing.T) {
	a := deckApp(t)
	a.openStatusSheet()
	if !a.deck.open {
		t.Fatal("the sheet did not open")
	}
	var lines []string
	for _, item := range a.deckItems() {
		lines = append(lines, item.label+": "+item.value)
	}
	sheet := strings.Join(lines, "\n")
	if !strings.Contains(sheet, "deepseek/deepseek-v4-flash") {
		t.Fatalf("the sheet does not carry the full routing address:\n%s", sheet)
	}
	if !strings.Contains(sheet, "session: aforge-v2") {
		t.Fatalf("the sheet does not carry the session's name:\n%s", sheet)
	}
}

// THE WIDE ROW IS TWO CLUSTERS NOW (ISSUE-126): the presence cluster on the
// left, dim — jobs, watches, what is keeping watch — and the ticking cluster
// on the right, hard against the edge. The identity that used to open this row
// is the top bar's crumb, and the facts that left the row are one press away
// in the sheet.
func TestStatusRowIsTwoClusters(t *testing.T) {
	a, _, _ := hudApp(t)
	a.cost = 0.42
	a.ctxTokens, a.ctxWindow = 12400, 128000
	a.dismissWelcome()
	rows := a.statusRows(a.width)
	if len(rows) != 1 {
		t.Fatalf("statusRows(%d) returned %d rows, want one", a.width, len(rows))
	}
	row := plain(rows[0])
	if !strings.Contains(row, "$0.42") {
		t.Fatalf("the row = %q, want the bill in the ticking cluster", row)
	}
	if !strings.Contains(row, "10%") {
		t.Fatalf("the row = %q, want the meter in the ticking cluster", row)
	}
	// The identity is gone from the row: the session's name and the model's
	// full address are the top bar's and the sheet's now.
	if strings.Contains(row, "aforge-v2") {
		t.Fatalf("the row = %q, want the identity off it — the crumb carries it", row)
	}
	if strings.Contains(row, "deepseek/deepseek-v4-flash") {
		t.Fatalf("the row = %q, want the model's full address off it", row)
	}
}

// THE PRESENCE CLUSTER IS THE LEFT END, DIM, and it is what the session is
// doing in the background: jobs and watches this surface watched start, and
// the standing orders it is keeping.
func TestStatusRowPresenceClusterIsTheLeftEnd(t *testing.T) {
	a, _, _ := hudApp(t)
	a.dismissWelcome()
	a.hud = hudStats{jobs: 2, watches: 1}
	a.hudStale = false
	rows := a.statusRows(a.width)
	row := plain(rows[0])
	if !strings.Contains(row, "2 jobs") {
		t.Fatalf("the row = %q, want the jobs count on the left", row)
	}
	if !strings.Contains(row, "1 watch") {
		t.Fatalf("the row = %q, want the watch count on the left", row)
	}
	// The presence cluster stands left of the ticking one.
	jobs := strings.Index(row, "2 jobs")
	cost := strings.Index(row, "$")
	if cost >= 0 && jobs > cost {
		t.Fatalf("the row = %q, want the presence cluster left of the bill", row)
	}
}

// THE TICKING CLUSTER'S ORDER IS THE SPEC'S: the open count, the bill, the
// meter, the forecast, the state — each present only when it is true, and the
// state word never dropped.
func TestStatusRowTickingClusterReadsInOrder(t *testing.T) {
	a, _, _ := hudApp(t)
	a.dismissWelcome()
	a.cost = 0.42
	a.ctxTokens, a.ctxWindow = 12400, 128000
	a.state = stateWorking
	a.turnBegan = a.now().Add(-4 * time.Second)
	row := plain(a.statusRows(a.width)[0])
	cost := strings.Index(row, "$0.42")
	pct := strings.Index(row, "10%")
	state := strings.Index(row, "working")
	if cost < 0 || pct < 0 || state < 0 {
		t.Fatalf("the row = %q, want the bill, the meter and the state word", row)
	}
	if !(cost < pct && pct < state) {
		t.Fatalf("the row = %q, want bill → meter → state, left to right", row)
	}
}

// THE ROW'S BYTE-FOR-BYTE PIN, in the new form: one row, the presence cluster
// on the left, the ticking cluster hard against the right edge, and nothing
// where nothing is true. This is the pin that fails loudly when the row's
// shape moves, which is what a pin is for.
func TestStatusRowPinned(t *testing.T) {
	a, _, _ := hudApp(t)
	a.dismissWelcome()
	a.cost = 0.42
	a.ctxTokens, a.ctxWindow = 12400, 128000
	row := plain(a.statusRows(a.width)[0])
	// hudApp is 200 wide; the right cluster is right-aligned, so the row ends
	// with the meter and the bill is left of it. The left end is empty: no
	// jobs, no watches, nothing standing. The meter is A PERCENT and not the
	// fraction — the row wears the workings only past the crowding line, and
	// ten percent is a long way under it (render.go's [app.ctxAmbient]).
	// CELLS AND NOT BYTES: the ` · ` separator is two bytes wide and one column,
	// so a byte count pads the row one cell short of where it is drawn.
	const tail = "$0.42 · 10%"
	want := strings.Repeat(" ", 200-ansi.StringWidth(tail)) + tail
	if row != want {
		t.Fatalf("the pinned row moved:\n got %q\nwant %q", row, want)
	}
}

// THE DECK'S PRESS IS THE ROW'S PRESS: every cell of both rows opens
// something — the chip its picker, every other cell the sheet — so there is no
// part of either row a band would be promising a door it does not have.
func TestStatusDeckPressOpensTheSheet(t *testing.T) {
	a := deckApp(t)
	// The press lands on the deck's first row, off the model chip.
	a.deckPress(20, 0)
	if !a.deck.open {
		t.Fatal("pressing row 1 did not open the sheet")
	}
}

// THE CHIP'S PRESS IS THE PICKER, and only the chip's: the model's basename on
// row 2 is the one cell that opens the model picker rather than the sheet.
func TestStatusDeckChipPressOpensThePicker(t *testing.T) {
	a := deckApp(t)
	// Lay the deck out once so the chip's columns are recorded.
	a.statusRows(a.width)
	// [app.deckPress] takes the DECK's row and not the frame's — the deck is two
	// rows and its second is 1, wherever on the screen the frame put it.
	a.deckPress(len(deckPad)+1, 1)
	if a.deck.open {
		t.Fatal("pressing the model chip opened the sheet, want the picker")
	}
	if !a.pick.open {
		t.Fatal("pressing the model chip opened nothing, want the picker")
	}
}

// THE SHEET'S OWN ROWS ARE DOORS WHERE THEY CARRY ONE: the model line opens
// the picker, and a line with no act is a fact and nothing else.
func TestStatusDeckSheetModelLineIsADoor(t *testing.T) {
	a := deckApp(t)
	a.openStatusSheet()
	items := a.deckItems()
	var modelAt = -1
	for i, item := range items {
		if item.act == deckActModel {
			modelAt = i
			break
		}
	}
	if modelAt < 0 {
		t.Fatal("the sheet has no model line")
	}
	// A press selects; the press on the row already selected answers it.
	a.deck.cursor = modelAt
	a.deckActivate(modelAt, items)
	if !a.pick.open {
		t.Fatal("answering the sheet's model line opened nothing, want the picker")
	}
	if a.deck.open {
		t.Fatal("the sheet is still open under the picker")
	}
}

// THE QUIET LAW IS THE ROW'S OWN: the greeting empties the wide row, and the
// deck keeps the same law — a phone frame over the welcome box draws the deck
// with the bill and the meter taken out, because nothing has been said, sent
// or spent yet.
func TestStatusDeckKeepsTheQuietLaw(t *testing.T) {
	a, _, _ := hudApp(t)
	a.width, a.height = 44, 24
	// The welcome box is still open: hudApp dismisses it, so open it again the
	// way a fresh session would.
	a.welcome.open = true
	if !a.statusQuiet() {
		t.Fatal("the welcome box is open and the row is not quiet")
	}
	rows := deckRowsOf(a)
	for i, row := range rows {
		if strings.Contains(row, "$") || strings.Contains(row, "%") {
			t.Fatalf("row %d over the greeting = %q, want the bill and the meter taken out", i+1, row)
		}
	}
}

// THE DECK'S ROWS ARE THE FRAME'S OWN ROWS: the frame pins them at the bottom,
// under the transcript, and the pointer's hit-testing resolves against the
// same two rows the layout drew.
func TestStatusDeckRowsAreTheFramesBottomRows(t *testing.T) {
	a := deckApp(t)
	frame, _, _ := a.frame()
	lines := strings.Split(frame, "\n")
	if len(lines) < a.height {
		t.Fatalf("the frame has %d lines, want %d", len(lines), a.height)
	}
	bottom := plain(lines[a.height-1])
	if !strings.Contains(bottom, "deepseek-v4-flash") {
		t.Fatalf("the frame's last line = %q, want the deck's model row", bottom)
	}
}

// hoverRow lights the whole row under the pointer, which is the deck's own
// bargain: every cell opens something, so the band never promises a door it
// does not have.
func TestStatusDeckHoverLightsTheWholeRow(t *testing.T) {
	a := deckApp(t)
	a.hot = hoverAt{kind: hoverDeck, index: 0}
	rows := a.statusRows(a.width)
	if len(rows) != deckHeight {
		t.Fatalf("statusRows returned %d rows, want %d", len(rows), deckHeight)
	}
	// THE BAND IS THE QUESTION, not "is there any paint on this row" — row 2's
	// model chip is drawn dim whether or not anything is hovered, so a row that
	// merely differs from its plain form proves nothing.
	if !strings.Contains(rows[0], hoverBg()) {
		t.Fatalf("the hovered row carries no band: %q", rows[0])
	}
	if strings.Contains(rows[1], hoverBg()) {
		t.Fatalf("the row NOT under the pointer carries the band: %q", rows[1])
	}
}
