package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The bottom of the frame: the breathing room above the box, the chip that says
// the reader has scrolled away from the live edge, and the box's own window.

// blankRow reports whether a drawn row carries nothing but spaces.
func blankRow(s string) bool { return strings.TrimRight(plain(s), " ") == "" }

// ruleAt is the index of the frame's one horizontal line, or -1.
func ruleAt(rows []string) int {
	for i, r := range rows {
		if strings.Contains(plain(r), "──") {
			return i
		}
	}
	return -1
}

// chipAtRow is the index of the row the jump chip was drawn on, or -1.
func chipAtRow(rows []string) int {
	for i, r := range rows {
		if strings.Contains(plain(r), "latest") {
			return i
		}
	}
	return -1
}

// scrolledApp is a conversation long enough to scroll, parked away from its
// live edge.
func scrolledApp(t *testing.T, height int) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.height = height
	for i := 0; i < 60; i++ {
		a.note("a line of transcript")
	}
	a.touch()
	return a
}

// ── 1. the breathing room ───────────────────────────────────────────────────

// THE GAP IS A LADDER AND EVERY RUNG IS COUNTED. Two blank rows on a window
// with the height to lend them, one on the everyday window, none on a short one
// — and whatever it draws, [app.chromeHeight] charges the conversation for
// exactly the rows [app.chrome] drew.
func TestTheBreathingGapStepsDownWithTheWindow(t *testing.T) {
	for _, c := range []struct {
		name   string
		height int
		gap    int
	}{
		{name: "airy", height: 24, gap: 2},
		{name: "roomy", height: 10, gap: 1},
		{name: "short", height: 5, gap: 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.height = c.height
			for i := 0; i < 40; i++ {
				a.note("a line of transcript")
			}
			a.touch()

			if got := a.breathingRows(); got != c.gap {
				t.Fatalf("the ladder gives %d rows at height %d, want %d", got, c.height, c.gap)
			}
			rows, marks, _, _ := a.chrome(a.width)
			// The three answers that must never disagree: what was drawn, what was
			// marked for the pointer, and what the conversation was charged.
			if len(rows) != len(marks) || len(rows) != a.chromeHeight() {
				t.Fatalf("the chrome drew %d rows, marked %d and charged %d",
					len(rows), len(marks), a.chromeHeight())
			}

			frameRows := strings.Split(frame(a), "\n")
			rule := ruleAt(frameRows)
			if c.gap == 0 {
				if rule >= 0 {
					t.Fatalf("a short window still drew the rule:\n%s", plain(frame(a)))
				}
				return
			}
			if rule < 1 {
				t.Fatalf("the rule is at row %d:\n%s", rule, plain(frame(a)))
			}
			// One blank below the rule whatever the ladder says — the row the box
			// has always stood on.
			if !blankRow(frameRows[rule+1]) {
				t.Fatalf("the row under the rule is %q, want the gap above the draft",
					plain(frameRows[rule+1]))
			}
			if !strings.Contains(plain(frameRows[rule+2]), "›") {
				t.Fatalf("the draft is not two rows under the rule:\n%s", plain(frame(a)))
			}
			// And the SECOND helping goes above it, where the conversation stops.
			above := blankRow(frameRows[rule-1])
			if want := c.gap == 2; above != want {
				t.Fatalf("the row above the rule is %q (blank=%v), want blank=%v",
					plain(frameRows[rule-1]), above, want)
			}
			if c.gap == 2 && blankRow(frameRows[rule-2]) {
				t.Fatalf("the gap above the rule is more than one row:\n%s", plain(frame(a)))
			}
		})
	}
}

// ── 2. the jump chip ────────────────────────────────────────────────────────

// A READER AT THE LIVE EDGE IS SHOWN NOTHING. Absence renders as nothing on
// this surface, and the chip is the state it is in.
func TestTheJumpChipOnlyShowsWhileTheReaderHasScrolledAway(t *testing.T) {
	a := scrolledApp(t, 24)
	if a.jumpShowing() {
		t.Fatal("a stuck transcript is offering to jump to where it already is")
	}
	if got := chipAtRow(strings.Split(frame(a), "\n")); got >= 0 {
		t.Fatalf("the chip is on row %d of a frame nobody scrolled:\n%s", got, plain(frame(a)))
	}

	a.scroll(-5)
	if !a.jumpShowing() {
		t.Fatal("a transcript scrolled off its live edge says nothing about it")
	}
	rows := strings.Split(frame(a), "\n")
	at := chipAtRow(rows)
	if at < 0 {
		t.Fatalf("the chip was not drawn:\n%s", plain(frame(a)))
	}
	// It rides the FIRST row of the gap, which on an airy window is the row above
	// the rule — and it takes no row of its own: the frame is as tall as it was.
	if rule := ruleAt(rows); at != rule-1 {
		t.Fatalf("the chip is on row %d and the rule is on %d", at, rule)
	}
	if len(rows) != a.height {
		t.Fatalf("the frame is %d rows tall, want %d", len(rows), a.height)
	}
	// The chip names the key, and it names the key the router actually binds.
	if got := plain(rows[at]); !strings.HasSuffix(got, "↓ latest · "+jumpKey) {
		t.Fatalf("the chip row is %q, want it right-aligned and naming %s", got, jumpKey)
	}

	// A conversation SHORTER than its window has nothing below it, so a released
	// stick is not enough on its own.
	b := newTestApp(&fakeAgent{model: "m"})
	b.note("one line")
	b.stick = false
	if b.jumpShowing() {
		t.Fatal("a transcript with nothing below the window offered to jump to it")
	}
}

// ONE ROW OF THE GAP, WHICHEVER ONE THERE IS. The everyday window keeps a single
// blank and the chip rides that instead of asking for a row of its own.
func TestTheJumpChipFallsBackToTheGapAboveTheDraft(t *testing.T) {
	a := scrolledApp(t, 10)
	a.scroll(-3)
	rows := strings.Split(frame(a), "\n")
	at, rule := chipAtRow(rows), ruleAt(rows)
	if at < 0 || rule < 0 || at != rule+1 {
		t.Fatalf("the chip is on row %d and the rule on %d:\n%s", at, rule, plain(frame(a)))
	}
	if len(rows) != a.height {
		t.Fatalf("the chip took a row: the frame is %d tall, want %d", len(rows), a.height)
	}

	// And a window with no gap at all draws no chip. The key still works, which
	// is the next test.
	short := scrolledApp(t, 5)
	short.scroll(-2)
	if got := chipAtRow(strings.Split(frame(short), "\n")); got >= 0 {
		t.Fatalf("a short window spent a row on the chip:\n%s", plain(frame(short)))
	}
}

// PRESSING IT REJOINS THE LIVE EDGE, and the chip is gone on the next frame.
func TestPressingTheJumpChipReturnsToTheLiveEdge(t *testing.T) {
	a := scrolledApp(t, 24)
	a.scroll(-6)
	rows := strings.Split(frame(a), "\n") // the layout is what writes the span
	at := chipAtRow(rows)
	if at < 0 || !a.jumpSpan.pressable() {
		t.Fatalf("the chip drew no target: row %d, span %+v", at, a.jumpSpan)
	}
	// A press to the LEFT of the chip is a press on empty space, which is
	// nothing on this surface.
	drive(t, a, clickAt(0, at))
	drive(t, a, releaseAt(0, at))
	if a.stick {
		t.Fatal("a press on the empty half of the gap row jumped the conversation")
	}
	drive(t, a, clickAt(a.jumpSpan.from+1, at))
	drive(t, a, releaseAt(a.jumpSpan.from+1, at))
	if !a.stick {
		t.Fatal("pressing the chip did not rejoin the live edge")
	}
	if a.jumpShowing() {
		t.Fatal("the chip is still up after the jump")
	}
	if got := chipAtRow(strings.Split(frame(a), "\n")); got >= 0 {
		t.Fatalf("the chip is still drawn on row %d", got)
	}
}

// THE KEY IS THE PATH THAT ALWAYS EXISTS, because the mouse is opt-in — and it
// has to work with a sentence in the box, which is what rules every other key
// out.
func TestTheJumpKeyReturnsToTheLiveEdgeWithADraftInTheBox(t *testing.T) {
	a := scrolledApp(t, 24)
	typeInto(t, a, "half a sentence")
	a.scroll(-6)
	if a.stick {
		t.Fatal("the transcript never left its live edge")
	}
	drive(t, a, key(jumpKey))
	if !a.stick {
		t.Fatalf("%s did not rejoin the live edge", jumpKey)
	}
	if a.input.String() != "half a sentence" {
		t.Fatalf("the draft is now %q — the key typed into the box", a.input.String())
	}
}

// A FROZEN VIEWPORT MANAGES ITS OWN EDGE (copymode.go), so the chip stays off
// while it is up rather than offering to scroll a snapshot.
func TestTheJumpChipStaysOffInCopyMode(t *testing.T) {
	a := scrolledApp(t, 24)
	a.scroll(-6)
	if !a.jumpShowing() {
		t.Fatal("the chip was never up to begin with")
	}
	a.enterCopy()
	if a.jumpShowing() {
		t.Fatal("the chip is up over a frozen viewport")
	}
	if got := chipAtRow(strings.Split(frame(a), "\n")); got >= 0 {
		t.Fatalf("copy mode drew the chip on row %d", got)
	}
	// Leaving copy mode rejoins the edge on its own, so the chip stays off.
	a.exitCopy()
	if a.jumpShowing() {
		t.Fatal("thawing left the reader off the live edge")
	}
}

// THE POINTER HAS TO BE ON THE CHIP, not merely on its row: it is the one target
// on this surface narrower than the line it is drawn on.
func TestTheJumpChipBrightensUnderThePointerAndNowhereElse(t *testing.T) {
	a := scrolledApp(t, 24)
	a.scroll(-6)
	rows := strings.Split(frame(a), "\n")
	at := chipAtRow(rows)
	dim := rows[at]

	a.setHover(0, at)
	if a.hot.kind == hoverJump {
		t.Fatal("the chip claimed a pointer forty columns away from it")
	}
	a.setHover(a.jumpSpan.from+1, at)
	if a.hot.kind != hoverJump {
		t.Fatalf("the pointer on the chip is %v", a.hot.kind)
	}
	bright := strings.Split(frame(a), "\n")[at]
	if bright == dim {
		t.Fatalf("the chip did not brighten under the pointer: %q", plain(bright))
	}
	if plain(bright) != plain(dim) {
		t.Fatalf("hovering changed the TEXT: %q → %q", plain(dim), plain(bright))
	}
}

// ── 3. the top-anchored draft ───────────────────────────────────────────────

// TYPING STARTS AT THE TOP AND FLOWS DOWN. The prompt is the block's first row
// and it stays there until the caret walks past the cap.
func TestTheDraftBlockAnchorsAtTheTop(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	lines := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		lines = append(lines, string(rune('a'+i)))
	}
	e := &editor{}
	e.setText(strings.Join(lines[:draftRows], "\n"))

	// Exactly at the cap: every line is on screen, the first with its prompt.
	rows, _, caretRow := draftBlock(e, pal, 20, draftRows, "", "")
	if len(rows) != draftRows {
		t.Fatalf("a draft at the cap drew %d rows: %q", len(rows), rows)
	}
	if got := plain(rows[0]); got != "› a" {
		t.Fatalf("the first row is %q, want the prompt on the draft's first line", got)
	}
	if caretRow != draftRows-1 {
		t.Fatalf("the caret is on row %d, want %d", caretRow, draftRows-1)
	}

	// Over the cap with the caret at the END: the window follows the caret, which
	// is the only thing that makes it scroll at all.
	e.setText(strings.Join(lines, "\n"))
	rows, _, caretRow = draftBlock(e, pal, 20, draftRows, "", "")
	if len(rows) != draftRows || caretRow != draftRows-1 {
		t.Fatalf("a scrolled draft drew %d rows with the caret on %d", len(rows), caretRow)
	}
	if got := plain(rows[0]); !strings.HasPrefix(got, glyphMore) {
		t.Fatalf("the scrolled block's first row is %q, want the ellipsis lead", got)
	}
	if got := plain(rows[len(rows)-1]); got != "  j" {
		t.Fatalf("the last row is %q, want the draft's last line", got)
	}

	// Over the cap with the caret back at the TOP: the block is anchored there,
	// prompt and all. This is the case that used to show the draft's tail.
	e.cursor = 0
	rows, caretX, caretRow := draftBlock(e, pal, 20, draftRows, "", "")
	if got := plain(rows[0]); got != "› a" {
		t.Fatalf("the first row is %q — the block is still bottom-anchored", got)
	}
	if caretRow != 0 || caretX != ansi.StringWidth(prompt) {
		t.Fatalf("the caret is at %d,%d, want %d,0", caretX, caretRow, ansi.StringWidth(prompt))
	}

	// And in the MIDDLE, still inside the cap: nothing scrolls, so the prompt is
	// still the first row and the caret is where the person put it.
	e.cursor = 6 // "a\nb\nc\nd" — the fourth line
	rows, _, caretRow = draftBlock(e, pal, 20, draftRows, "", "")
	if got := plain(rows[0]); got != "› a" {
		t.Fatalf("the first row is %q — the window moved without being pushed", got)
	}
	if caretRow != 3 {
		t.Fatalf("the caret is on row %d, want 3", caretRow)
	}

	// Past the cap, the window scrolls by exactly as much as it must: the caret
	// is on the block's LAST row and never off it.
	e.cursor = len(e.value) - 2 // the ninth line
	rows, _, caretRow = draftBlock(e, pal, 20, draftRows, "", "")
	if caretRow != draftRows-1 {
		t.Fatalf("the caret is on row %d of %d", caretRow, len(rows))
	}
	if got := plain(rows[caretRow]); got != "  i" {
		t.Fatalf("the caret's row is %q, want the line the caret is on", got)
	}
}

// THE CARET THE TERMINAL IS POSITIONED FROM IS THE ONE THE BLOCK DREW. The frame
// counts it back through the chrome (view.go's [app.frameOut]), so a top-anchored
// window that lied about its row would put the cursor in the transcript.
func TestTheCaretLandsOnTheDraftsFirstRowWhenTheDraftIsLong(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.ANSI256, false)
	for i := 0; i < 10; i++ {
		a.input.insert("line\n")
	}
	a.input.cursor = 0
	a.touch()

	painted, caretX, caretY := a.frame()
	rows := strings.Split(painted, "\n")
	if caretY < 0 || caretY >= len(rows) {
		t.Fatalf("the caret is on row %d of a %d-row frame", caretY, len(rows))
	}
	if got := plain(rows[caretY]); !strings.Contains(got, "›") {
		t.Fatalf("the caret is on %q, want the draft's prompt row", got)
	}
	if want := len(inputPad) + ansi.StringWidth(prompt); caretX != want {
		t.Fatalf("the caret is at column %d, want %d", caretX, want)
	}
}
