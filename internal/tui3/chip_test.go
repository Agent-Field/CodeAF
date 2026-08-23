package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE INPUT BAR NAMES ONE KEY. "@" still opens the completion and always will;
// what changed is that the legend stopped printing it, because a slot beside a
// live prompt holds one affordance before it becomes a cheatsheet (render.go).
func TestTheLegendNamesTheCommandKeyAndNotTheFileKey(t *testing.T) {
	a, _, _ := hudApp(t)
	legend := plain(a.legend(120))
	if !strings.Contains(legend, "/ commands") {
		t.Fatalf("the legend lost the one key it names: %q", legend)
	}
	if strings.Contains(legend, "@ files") || strings.Contains(legend, "@") {
		t.Fatalf("the legend still prints the file key: %q", legend)
	}
	// AND THE KEY ITSELF IS UNTOUCHED: an "@" and the letters after it still open
	// the completion. This is a chrome declutter, not a feature removal.
	b, _, _ := attachLab(t, map[string]int{"shot.png": 12})
	drive(t, b, key("@"), key("s"), key("h"))
	if !b.comp.open {
		t.Fatal("@ stopped opening the completion when the legend stopped naming it")
	}
}

// ── THE TASK STRIP'S CHIPS ──────────────────────────────────────────────────

// A CHIP IS PADDED AND THE PADDING IS THE CHIP'S. The band needs cells to fill
// before it reads as a tab rather than as a highlighted word, and the pointer
// gets those cells too — a one-cell miss beside a name is a miss people make.
func TestAStripChipIsPaddedAndItsPaddingOpensTheRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width = 80 // no rail: the strip is the only door
	a.touch()
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Write the auth tests",
		session.TaskRunning, session.TaskNotice{})})

	text := stripText(a)
	if !strings.HasPrefix(text, " ") {
		t.Fatalf("the first chip opens hard against the frame's edge: %q", text)
	}
	var chip stripSpan
	for _, span := range a.stripSpans {
		if span.id == 9 {
			chip = span
		}
	}
	if !chip.span.pressable() {
		t.Fatalf("the second node has no chip on the strip:\n%q", text)
	}
	// The chip's own columns are the glyph, a space, the name and one cell of air
	// on each side.
	node := a.tasks[9]
	body := ansi.StringWidth(plain(a.stripGlyph(node))) + 1 + ansi.StringWidth(fit(node.title, stripTitleCap))
	if got := chip.span.to - chip.span.from; got != body+stripPadCols {
		t.Fatalf("the chip is %d cells wide, want %d:\n%q", got, body+stripPadCols, text)
	}
	// THE LAST CELL OF THE CHIP IS PADDING, and pressing it is pressing the chip.
	drive(t, a, tea.MouseClickMsg{X: chip.span.to - 1, Y: a.headHeight(), Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: chip.span.to - 1, Y: a.headHeight(), Button: tea.MouseLeft})
	if !a.roomOpen() || a.room.id != 9 {
		t.Fatalf("a press on the chip's padding did not open its room: open=%v", a.roomOpen())
	}
}

// THE OPEN ROOM'S CHIP IS BANDED, AND IT IS THE ROW'S ONLY MARK. The band is
// what makes this a tab bar rather than a list of doors; the roster's cursor is
// not on it, because the strip and the roster are never on screen together
// (taskstrip.go's [app.stripShowing]).
func TestTheOpenChipIsBandedAndTheStripCarriesNoCursor(t *testing.T) {
	a, _, _ := roomApp(t)
	// A frame with NO roster on it, which is the only frame this row is drawn on.
	a.width = 80
	a.touch()
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Write the auth tests",
		session.TaskRunning, session.TaskNotice{})})

	band := "\x1b[48;5;" + itoa(int(hueBand.idx)) + "m"
	const underline = "\x1b[4m"

	// Nothing open: a row of dim chips and no marks.
	if row := a.stripRow(a.width); strings.Contains(row, band) || strings.Contains(row, underline) {
		t.Fatalf("an idle strip wore a mark:\n%q", row)
	}

	// THE OPEN ROOM'S CHIP TAKES THE BAND.
	a.openRoom(9, "Write the auth tests")
	row := a.stripRow(a.width)
	if !strings.Contains(row, band) {
		t.Fatalf("the open room's chip is not banded:\n%q", row)
	}

	// AND THE ROSTER'S CURSOR NEVER REACHES THIS ROW. Asking for the roster on a
	// frame this narrow raises it over the body, which stands the strip down.
	a.railTake(true)
	if !a.railFull() {
		t.Fatal("ctrl+t did not raise the roster over the body")
	}
	if row := a.stripRow(a.width); row != "" {
		t.Fatalf("the strip drew under the roster it opens:\n%q", row)
	}
	a.railTake(false)
	if row := a.stripRow(a.width); strings.Contains(row, underline) {
		t.Fatalf("the strip drew a cursor of its own:\n%q", row)
	}
}

// THE NARROW STRIP KEEPS THE PADDING AND THE OVERFLOW LAW. The chip row is the
// last tier that draws chips — at [tierPhone] the strip becomes one door with no
// chips to drop (taskphone.go) — so a narrow frame is where the +N does its work,
// and a chip that dropped its air there would be the tier where the row stops
// reading as tabs.
func TestTheNarrowStripKeepsItsChipsPaddedAndCountsTheRest(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = 66
	for i := 1; i <= 5; i++ {
		a.taskUpdate(update(uint64(i), "node number "+itoa(i), session.TaskRunning, session.TaskNotice{}))
	}
	if layoutTier(a.width) != tierNarrow {
		t.Fatalf("sixty-six columns is not the narrow tier")
	}
	text := stripText(a)
	if w := ansi.StringWidth(text); w > a.width {
		t.Fatalf("the strip is %d cells wide on a %d-column frame:\n%q", w, a.width, text)
	}
	if !strings.HasPrefix(text, " ") {
		t.Fatalf("the phone's first chip lost its padding: %q", text)
	}
	if len(a.stripSpans) == 0 || !a.stripMore.pressable() {
		t.Fatalf("the phone strip recorded no columns: chips=%d more=%+v", len(a.stripSpans), a.stripMore)
	}
	if want := stripMoreWord(5 - len(a.stripSpans)); !strings.Contains(text, want) {
		t.Fatalf("the overflow mark does not say %q:\n%q", want, text)
	}
	// Every chip carries its two cells of air, and no chip runs off the row.
	for _, chip := range a.stripSpans {
		node := a.tasks[chip.id]
		body := ansi.StringWidth(plain(a.stripGlyph(node))) + 1 + ansi.StringWidth(fit(node.title, stripTitleCap))
		if got := chip.span.to - chip.span.from; got != body+stripPadCols {
			t.Fatalf("chip %d is %d cells wide, want %d", chip.id, got, body+stripPadCols)
		}
		if chip.span.to > a.width {
			t.Fatalf("chip %d runs off a %d-column frame: %+v", chip.id, a.width, chip.span)
		}
	}
}

// ── THE SETTINGS PANEL'S TABS ───────────────────────────────────────────────

// THE SAME CHIP, IN THE OTHER PLACE IT IS A TAB. The panel's bar and the task
// strip are the same object drawn twice, so they are padded the same way and
// picked out the same way — one visual question, one answer.
func TestTheSettingsTabsAreChipsAndTheirPaddingIsClickable(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	bar := sheetTabBar(120, 1, pal)
	flat := plain(bar)
	for _, title := range settingTabs {
		if !strings.Contains(flat, " "+title+" ") {
			t.Fatalf("tab %q is not padded into a chip: %q", title, flat)
		}
	}
	// The open tab is banded, exactly as the open room's chip is.
	if band := "\x1b[48;5;" + itoa(int(hueBand.idx)) + "m"; !strings.Contains(bar, band) {
		t.Fatalf("the open tab is not banded: %q", bar)
	}

	// AND THE SPANS COVER THE PADDING. The first cell of a chip and its last are
	// that chip's, which is what makes a near miss land on the tab a person was
	// aiming at rather than on nothing.
	spans := tabSpans()
	if len(spans) != len(settingTabs) {
		t.Fatalf("the bar recorded %d spans for %d tabs", len(spans), len(settingTabs))
	}
	for i, span := range spans {
		if got := span.to - span.from; got != len(settingTabs[i])+tabPadCols {
			t.Fatalf("tab %q spans %d cells, want %d", settingTabs[i], got, len(settingTabs[i])+tabPadCols)
		}
		for _, x := range []int{span.from, span.to - 1} {
			at, ok := tabAtColumn(x)
			if !ok || at != i {
				t.Fatalf("column %d of tab %q resolved to %d (ok=%v)", x, settingTabs[i], at, ok)
			}
		}
	}
	// The bar's plain width agrees with the last span, so a click past the end
	// lands on nothing rather than on the tab before it.
	if _, ok := tabAtColumn(spans[len(spans)-1].to); ok {
		t.Fatal("the cell after the last chip answered as a tab")
	}
	if got, want := ansi.StringWidth(flat), spans[len(spans)-1].to; got != want {
		t.Fatalf("the bar is %d cells wide and its last span ends at %d:\n%q", got, want, flat)
	}
}
