package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// deckApp is a session at PHONE WIDTH with something on every part of the
// status line: a name, a bill, a context reading and a model.
func deckApp(t *testing.T) (*app, *fakeAgent) {
	t.Helper()
	a, agent, _ := hudApp(t)
	a.width, a.height = 44, 20
	a.title, a.cost = "Fix the nil-map crash", 0.31
	a.ctxWindow, a.ctxTokens = 200_000, 24_000
	a.touch()
	return a, agent
}

// deckRows is the last [deckHeight] rows of the frame — the deck itself, as a
// reader sees it.
func deckRowsOf(t *testing.T, a *app) []string {
	t.Helper()
	lines := strings.Split(plain(frame(a)), "\n")
	if len(lines) != a.height {
		t.Fatalf("the frame is %d rows, want the terminal's %d", len(lines), a.height)
	}
	return lines[len(lines)-deckHeight:]
}

// ── THE DECK ────────────────────────────────────────────────────────────────

// At forty-four columns the status is TWO ROWS, and the two carry the four
// facts a phone-width frame can answer at a glance: what this is and what it
// cost, then what is answering and what is still moving.
func TestThePhoneStatusIsATwoRowDeck(t *testing.T) {
	a, _ := deckApp(t)

	if got := a.statusHeight(a.width); got != deckHeight {
		t.Fatalf("the phone status is %d rows, want the deck's %d", got, deckHeight)
	}
	if got := len(a.statusRows(a.width)); got != deckHeight {
		t.Fatalf("the row builder drew %d rows, want %d", got, deckHeight)
	}

	deck := deckRowsOf(t, a)
	top, model := deck[0], deck[1]
	for _, want := range []string{"Fix the nil-map crash", "$0.31", "12%"} {
		if !strings.Contains(top, want) {
			t.Fatalf("row 1 is missing %q:\n%q", want, top)
		}
	}
	if !strings.Contains(top, deckMore) {
		t.Fatalf("row 1 carries no affordance for the sheet:\n%q", top)
	}
	// The model is its BASENAME on the deck — the routing address is the sheet's,
	// where the whole id is recorded.
	if !strings.Contains(model, "deepseek-v4-flash") {
		t.Fatalf("row 2 is missing the model chip:\n%q", model)
	}
	if strings.Contains(model, "deepseek/deepseek") {
		t.Fatalf("row 2 spent nine cells on a vendor prefix:\n%q", model)
	}
	if !strings.Contains(model, "idle") {
		t.Fatalf("row 2 dropped the state word, which is the last thing to go:\n%q", model)
	}
	for i, line := range deck {
		if w := ansi.StringWidth(line); w > a.width {
			t.Fatalf("deck row %d is %d cells wide, want at most %d:\n%q", i, w, a.width, line)
		}
	}
}

// The deck is two rows in EVERY state, not two rows when it happens to wrap:
// the chrome height, the frame and the hit-testing all read one constant.
func TestTheDeckIsAlwaysTwoRowsAndTheChromeCountsBoth(t *testing.T) {
	a, agent := deckApp(t)

	states := []func(){
		func() {},
		func() { a.title, a.cost = "", 0 },
		func() { a.title, a.cost = "a much longer session name than this frame can hold", 148.02 },
		func() { a.state = stateWorking; a.turnBegan = a.now().Add(-4 * time.Second) },
	}
	for i, set := range states {
		set()
		a.touch()
		_ = agent
		if got := a.statusHeight(a.width); got != deckHeight {
			t.Fatalf("state %d: statusHeight says %d, want %d", i, got, deckHeight)
		}
		if got := len(a.statusRows(a.width)); got != deckHeight {
			t.Fatalf("state %d: the row builder drew %d rows, want %d", i, got, deckHeight)
		}
		lines := strings.Split(plain(frame(a)), "\n")
		if len(lines) != a.height {
			t.Fatalf("state %d: the frame is %d rows, want %d", i, len(lines), a.height)
		}
		chrome, marks, _, _ := a.chrome(a.width)
		if len(chrome) != len(marks) {
			t.Fatalf("state %d: the chrome has %d rows and %d marks", i, len(chrome), len(marks))
		}
		status := 0
		for _, mark := range marks {
			if mark.kind == chromeStatus {
				status++
			}
		}
		if status != deckHeight {
			t.Fatalf("state %d: the chrome marked %d status rows, want %d", i, status, deckHeight)
		}
	}
}

// The running count and the background jobs are what row 2 says beside the
// model, and the state word survives both.
func TestTheDeckCountsWhatIsStillMoving(t *testing.T) {
	a, _ := deckApp(t)
	drive(t, a, streamEventMsg{gen: a.gen,
		ev: update(7, "port the parser", session.TaskRunning, session.TaskNotice{})})

	row := deckRowsOf(t, a)[1]
	if !strings.Contains(row, "1 running") {
		t.Fatalf("row 2 does not say what is running:\n%q", row)
	}
}

// ── THE SHEET ───────────────────────────────────────────────────────────────

// TAPPING ROW 1 OPENS THE SHEET, and the sheet lists every fact the status line
// can carry — including the ones the deck's two rows had no cells for.
func TestTappingTheDeckOpensTheFullscreenStatusSheet(t *testing.T) {
	a, _ := deckApp(t)
	_ = frame(a)

	drive(t, a, clickAt(6, a.height-deckHeight))
	drive(t, a, releaseAt(6, a.height-deckHeight))
	if !a.deck.open {
		t.Fatal("a press on the deck's first row did not open the status sheet")
	}

	lines := strings.Split(plain(frame(a)), "\n")
	if len(lines) != a.height {
		t.Fatalf("the sheet is %d rows, want the terminal's %d", len(lines), a.height)
	}
	body := strings.Join(lines, "\n")
	if !strings.HasPrefix(lines[0], " status") || !strings.Contains(lines[0], "esc close") {
		t.Fatalf("the sheet has no head:\n%q", lines[0])
	}
	// Everything the deck kept, everything it moved, and the two the legend
	// holds — one line each.
	for _, want := range []string{
		"session", "Fix the nil-map crash",
		"model", "deepseek/deepseek-v4-flash",
		"spend", "$0.31",
		"context", "24k/200k · 12%",
		"state", "idle",
		"place", "chat-v3-task*",
		"keys", microcopy,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the sheet never says %q:\n%s", want, body)
		}
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > a.width {
			t.Fatalf("sheet row %d is %d cells wide, want at most %d:\n%q", i, w, a.width, line)
		}
	}

	// esc closes it and leaves the deck exactly as it was.
	drive(t, a, key("esc"))
	if a.deck.open {
		t.Fatal("esc did not close the status sheet")
	}
	if got := len(deckRowsOf(t, a)); got != deckHeight {
		t.Fatalf("the deck came back as %d rows", got)
	}
}

// A press on the chrome — the head, the rules, the empty rows under a short
// list — is a press OUTSIDE the list, and that is how a finger closes it.
func TestTappingOutsideTheSheetsListClosesIt(t *testing.T) {
	a, _ := deckApp(t)
	a.openStatusSheet()
	_ = frame(a)

	drive(t, a, clickAt(2, 0))
	drive(t, a, releaseAt(2, 0))
	if a.deck.open {
		t.Fatal("a press on the sheet's head did not close it")
	}
}

// THE MODEL IS THE ONE LINE A TAP ACTS ON, at both ends: the chip on the deck's
// second row, and the model line inside the sheet.
func TestTheModelIsPressableOnTheDeckAndInTheSheet(t *testing.T) {
	a, _ := deckApp(t)
	_ = frame(a)

	if !a.modelSpan.pressable() {
		t.Fatal("the deck recorded no columns for the model chip")
	}
	if a.modelSpan.to-a.modelSpan.from < deckTouch {
		t.Fatalf("the model target is %d cells wide, want at least %d",
			a.modelSpan.to-a.modelSpan.from, deckTouch)
	}
	drive(t, a, clickAt(a.modelSpan.from+1, a.height-1))
	drive(t, a, releaseAt(a.modelSpan.from+1, a.height-1))
	if !a.pick.open {
		t.Fatal("pressing the model chip on the deck did not open the picker")
	}
	drive(t, a, key("esc"))

	// And from the sheet, by the keyboard: the model line, then enter.
	a.openStatusSheet()
	items := a.deckItems()
	at := -1
	for i, item := range items {
		if item.act == deckActModel {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the sheet lists no model line")
	}
	a.deck.cursor = at
	drive(t, a, key("enter"))
	if a.deck.open {
		t.Fatal("answering the model line left the sheet up under the picker")
	}
	if !a.pick.open {
		t.Fatal("enter on the sheet's model line did not open the picker")
	}
}

// A frame that GREW out of the phone tier has its whole status row back, and
// the sheet standing in for it goes away with the tier that needed it.
func TestTheSheetClosesWhenTheFrameLeavesThePhoneTier(t *testing.T) {
	a, _ := deckApp(t)
	a.openStatusSheet()
	_ = frame(a)

	a.width = 120
	a.touch()
	lines := strings.Split(plain(frame(a)), "\n")
	if a.deck.open {
		t.Fatal("the status sheet survived the frame growing past the phone tier")
	}
	if strings.HasPrefix(lines[0], " status") {
		t.Fatalf("the sheet is still drawn at 120 columns:\n%q", lines[0])
	}
}

// ── AND EVERY WIDER FRAME IS UNTOUCHED ──────────────────────────────────────

// The deck is the PHONE tier's status and nothing else's. At sixty columns and
// up the row is the one it has always been — one row, or the wrapped two — and
// the height is still answered by the layout rather than by the tier.
func TestTheWiderTiersKeepTodaysStatusRow(t *testing.T) {
	a, _ := deckApp(t)

	for _, width := range []int{200, 120, 100, 80, 70, 60} {
		a.width = width
		a.touch()
		rows := a.statusRows(width)
		_, _, _, wrapped := a.statusLayout(width)
		want := 1
		if wrapped {
			want = 2
		}
		if len(rows) != want {
			t.Fatalf("at %d columns the status is %d rows, want the layout's %d",
				width, len(rows), want)
		}
		if got := a.statusHeight(width); got != len(rows) {
			t.Fatalf("at %d columns statusHeight says %d and the row builder drew %d",
				width, got, len(rows))
		}
		// The wide row keeps its two clusters on one line, which is the thing the
		// deck replaces and must not have replaced here.
		line := plain(strings.Join(rows, "\n"))
		if !strings.Contains(line, "Fix the nil-map crash · deepseek-v4-flash") {
			t.Fatalf("at %d columns the identity cluster is not the wide row's:\n%q", width, line)
		}
	}
}

// THE BYTES, PINNED. The wave's promise is that a frame wide enough for the
// status row renders exactly what it rendered before the deck existed, escape
// sequences and all — so the row is asserted against its literal self.
//
// The crew joined the row in #315, which is the one deliberate change to this
// literal since the deck: the segment was written to stand at the head of the
// telemetry and drew for nobody, because it was guarded on an empty profile
// directory — the ordinary launch (crew.go's [app.crewReading]).
//
// AND THE BILL GAINED ITS RESERVATION, which is the second. The money segment
// holds one width for every spelling a turn walks through, so the row does not
// shove sideways as the figure grows a place and loses it again (render.go's
// [costCell]). `$0.31` is five cells inside an eight-cell segment, so three of
// the cells the gap used to hold moved to the other side of the crew word — the
// row is the same length and the same segments, and the two right-hand clusters
// stand exactly where they stood.
func TestTheWideStatusRowIsByteForByteWhatItWas(t *testing.T) {
	a, _ := deckApp(t)
	a.width = 120
	a.touch()

	const want = "Fix the nil-map crash · deepseek-v4-flash" +
		"                               crew balanced ·    $0.31 · 24k/200k · 12% · idle"
	if got := plain(strings.Join(a.statusRows(120), "\n")); got != want {
		t.Fatalf("the wide status row changed:\n got %q\nwant %q", got, want)
	}
}

// The status row is one cell short of the frame's width at no width: the deck's
// rows fill theirs the way the wide row fills its own.
func TestTheDeckRowsFillTheFrame(t *testing.T) {
	a, _ := deckApp(t)
	for _, width := range []int{30, 44, 59} {
		a.width = width
		a.touch()
		for i, line := range a.statusRows(width) {
			if w := ansi.StringWidth(plain(line)); w > width {
				t.Fatalf("at %d columns deck row %d is %d cells:\n%q", width, i, w, plain(line))
			}
		}
	}
}
