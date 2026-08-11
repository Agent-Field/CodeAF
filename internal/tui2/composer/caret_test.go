package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The caret's geometry is Render's own arithmetic run again, so the test that
// matters is not "does the formula agree with itself" — it is "does the caret
// land on the character the reader can SEE at that cell". Every case here finds
// its column in the painted frame rather than computing one.

// caretModel is a draft at a known size, with no chrome bound: no targets, no
// commands, so hintRows is empty and the rectangle is all draft.
func caretModel(t *testing.T, text string) *Model {
	t.Helper()
	m := New(Options{})
	m.Focus(true)
	for _, r := range text {
		m.insert(string(r))
	}
	return m
}

// cellOf finds the column a substring starts at on row y of the painted frame.
func cellOf(t *testing.T, m *Model, width, height, y int, needle string) int {
	t.Helper()
	rows := strings.Split(ansi.Strip(m.Render(width, height)), "\n")
	if y >= len(rows) {
		t.Fatalf("the frame has %d rows, not %d", len(rows), y+1)
	}
	at := strings.Index(rows[y], needle)
	if at < 0 {
		t.Fatalf("row %d does not carry %q: %q", y, needle, rows[y])
	}
	// A byte offset is not a column. The prompt glyph is three bytes and one
	// cell, and a pointer names cells.
	return ansi.StringWidth(rows[y][:at])
}

func TestClickingACharacterPutsTheCaretBeforeIt(t *testing.T) {
	const width, height = 40, 3
	m := caretModel(t, "hello world")
	x := cellOf(t, m, width, height, 0, "world")
	if !m.ClickCaret(width, height, x, 0) {
		t.Fatal("the click moved no caret")
	}
	m.insert("great ")
	if got := m.Value(); got != "hello great world" {
		t.Fatalf("the caret landed elsewhere: %q", got)
	}
}

// The right half of a character belongs to the gap after it, which is what
// every text surface on the machine does and what a reader aiming between two
// letters means.
func TestTheNearerEdgeOfACharacterWins(t *testing.T) {
	const width, height = 40, 3
	m := caretModel(t, "ab")
	start := cellOf(t, m, width, height, 0, "ab")
	if !m.ClickCaret(width, height, start, 0) {
		t.Fatal("clicking the left edge of `a` moved nothing")
	}
	m.insert("X")
	if got := m.Value(); got != "Xab" {
		t.Fatalf("the left edge gave %q", got)
	}
}

// A wrapped draft: the second display row is not a second logical line, and the
// caret has to know the difference. The row is found in the picture.
func TestClickingAWrappedRowLandsOnThatRowsCharacters(t *testing.T) {
	const width, height = 20, 4
	m := caretModel(t, "alpha beta gamma delta")
	frame := strings.Split(ansi.Strip(m.Render(width, height)), "\n")
	if len(frame) < 2 || !strings.Contains(frame[1], "a") {
		t.Fatalf("the fixture did not wrap: %q", frame)
	}
	// The first cell of the second display row.
	x := TextColumn(width)
	if !m.ClickCaret(width, height, x, 1) {
		t.Fatal("clicking the second row moved nothing")
	}
	m.insert("|")
	// Whatever the wrap point is, the marker must sit exactly where the second
	// row began — which is what re-rendering proves.
	rows := strings.Split(ansi.Strip(m.Render(width, height)), "\n")
	if !strings.HasPrefix(strings.TrimLeft(rows[1], " "), "|") {
		t.Fatalf("the caret did not land at the start of the wrapped row: %q", rows[1])
	}
}

// Clicking past the end of a line clamps to the end of THAT line, never to the
// end of the draft: a caret that jumped a paragraph because the reader clicked
// empty space would be an edit position nobody chose.
func TestClickingPastTheEndOfALineClampsToThatLine(t *testing.T) {
	const width, height = 40, 4
	m := caretModel(t, "one\ntwo")
	if !m.ClickCaret(width, height, width-1, 0) {
		t.Fatal("clicking past `one` moved nothing")
	}
	m.insert("!")
	if got := m.Value(); got != "one!\ntwo" {
		t.Fatalf("the clamp gave %q", got)
	}
}

// And a click on no row at all moves nothing.
func TestClickingBelowTheDraftMovesNoCaret(t *testing.T) {
	const width, height = 40, 4
	m := caretModel(t, "one")
	if m.ClickCaret(width, height, 3, 3) {
		t.Fatal("a click below the last drafted row moved the caret")
	}
	if m.ClickCaret(width, height, 3, -1) {
		t.Fatal("a click above the rectangle moved the caret")
	}
	if m.ClickCaret(0, height, 0, 0) {
		t.Fatal("a click into a zero-width rectangle moved the caret")
	}
}
