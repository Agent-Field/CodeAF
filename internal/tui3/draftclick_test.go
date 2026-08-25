package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// arrowChord builds a modified arrow press the way the terminal reports it.
func arrowChord(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// THE WORD KEYS AND THE KILL KEY AGREE ON WHAT A WORD IS: spaces first, then
// the run of non-spaces, in both directions.
func TestWordMotionWalksTheSameBoundariesTheKillDoes(t *testing.T) {
	e := &editor{}
	e.setText("fix the   flaky test")
	e.cursor = len(e.value)

	e.wordLeft()
	if got := string(e.value[e.cursor:]); got != "test" {
		t.Fatalf("one word back from the end left the caret before %q, want %q", got, "test")
	}
	e.wordLeft()
	if got := string(e.value[e.cursor:]); got != "flaky test" {
		t.Fatalf("two words back left the caret before %q, want %q", got, "flaky test")
	}
	e.wordRight()
	if got := string(e.value[:e.cursor]); got != "fix the   flaky" {
		t.Fatalf("a word forward left %q behind the caret, want %q", got, "fix the   flaky")
	}
	// The ends clamp rather than wrap.
	e.cursor = 0
	e.wordLeft()
	if e.cursor != 0 {
		t.Fatalf("word-left at the start moved to %d", e.cursor)
	}
	e.cursor = len(e.value)
	e.wordRight()
	if e.cursor != len(e.value) {
		t.Fatalf("word-right at the end moved to %d", e.cursor)
	}
}

// The chords reach the editor through the key router, and an empty box keeps
// its navigation meaning — a modifier held by accident moves nobody to another
// page.
func TestWordChordsMoveTheCaretAndSpareTheEmptyBox(t *testing.T) {
	a, _ := sheetApp(t)
	a.input.setText("hello world")
	a.input.cursor = len(a.input.value)

	a.key(arrowChord(tea.KeyLeft, tea.ModAlt))
	if got := string(a.input.value[a.input.cursor:]); got != "world" {
		t.Fatalf("alt+left left the caret before %q, want %q", got, "world")
	}
	a.key(arrowChord(tea.KeyRight, tea.ModAlt))
	if a.input.cursor != len(a.input.value) {
		t.Fatalf("alt+right did not return to the end: %d", a.input.cursor)
	}
	a.key(arrowChord(tea.KeyLeft, tea.ModSuper))
	if a.input.cursor != 0 {
		t.Fatalf("super+left did not reach the line start: %d", a.input.cursor)
	}
	a.key(arrowChord(tea.KeyRight, tea.ModSuper))
	if a.input.cursor != len(a.input.value) {
		t.Fatalf("super+right did not reach the line end: %d", a.input.cursor)
	}
	// And under the OTHER name the same chord arrives by, which is the one every
	// terminal actually sends it under (inputguard_test.go says why).
	a.key(arrowChord(tea.KeyLeft, tea.ModMeta))
	if a.input.cursor != 0 {
		t.Fatalf("meta+left did not reach the line start: %d", a.input.cursor)
	}
	a.key(arrowChord(tea.KeyRight, tea.ModMeta))
	if a.input.cursor != len(a.input.value) {
		t.Fatalf("meta+right did not reach the line end: %d", a.input.cursor)
	}

	a.input.reset()
	a.key(arrowChord(tea.KeyLeft, tea.ModAlt))
	if a.input.cursor != 0 || !a.input.empty() {
		t.Fatal("alt+left over an empty box did something")
	}
}

// The click inverse names the rune under the pointer through the same wrap the
// frame drew, and every miss lands on the nearest legal caret.
func TestDraftClickIndexInvertsTheLayout(t *testing.T) {
	value := []rune("alpha beta gamma")
	// Room of 6 wraps as "alpha ", "beta ", "gamma" (wrapLine breaks after
	// spaces where it can).
	room, rows := 6, 6

	if got := draftClickIndex(value, 0, 0, 2, room, rows); string(value[got:got+3]) != "pha" {
		t.Fatalf("row 0 col 2 resolved to %d (%q)", got, string(value[got:]))
	}
	if got := draftClickIndex(value, 0, 1, 0, room, rows); string(value[got:got+4]) != "beta" {
		t.Fatalf("row 1 col 0 resolved to %d (%q)", got, string(value[got:]))
	}
	// A column past the row's end is that row's end; a row past the window is
	// the draft's end; a negative column is the row's start.
	rowEnd := draftClickIndex(value, 0, 0, 99, room, rows)
	if rowEnd < 5 || rowEnd > 6 {
		t.Fatalf("a far-right click on row 0 resolved to %d", rowEnd)
	}
	if got := draftClickIndex(value, 0, 99, 0, room, rows); got != len(value) {
		t.Fatalf("a click past the last row resolved to %d, want the end %d", got, len(value))
	}
	if got := draftClickIndex(value, 0, 1, -3, room, rows); string(value[got:got+4]) != "beta" {
		t.Fatalf("a click left of the text resolved to %d", got)
	}
}

// AND THEY AGREE ON A WIDE-RUNE DRAFT, which is where they used to disagree.
//
// The wrap counted RUNES and the click counted CELLS. On a Japanese draft in a
// box six cells wide that put six runes on a row and painted twelve — the draft
// walked out of its own box — and every click below the first row landed on a
// letter the person had not aimed at, because the two halves of the arithmetic
// were measuring different things.
func TestTheWrapAndTheClickAgreeOnAWideRuneDraft(t *testing.T) {
	value := []rune("日本語のテキスト") // eight runes, sixteen cells
	room, rows := 6, 6

	// Three cells to the rune-pair, so the box holds three runes to the row and
	// no row may paint wider than the box it is drawn in.
	segments := wrapLine(value, 0, len(value), room)
	for i, seg := range segments {
		row := string(value[seg.from:seg.to])
		if w := ansi.StringWidth(row); w > room {
			t.Fatalf("row %d paints %d cells in a box %d wide: %q", i, w, room, row)
		}
	}
	if len(segments) != 3 {
		t.Fatalf("eight wide runes laid out %d rows in a box six cells wide, want 3: %v", len(segments), segments)
	}

	// And the click names the rune under the pointer on every one of them, in
	// the same cells the row was painted in.
	for _, tc := range []struct {
		row, col int
		want     string
	}{
		{0, 0, "日"}, {0, 4, "語"},
		{1, 2, "テ"},
		{2, 0, "ス"}, {2, 2, "ト"},
	} {
		got := draftClickIndex(value, 0, tc.row, tc.col, room, rows)
		if got >= len(value) || string(value[got]) != tc.want {
			t.Fatalf("row %d col %d resolved to %d (%q), want %q",
				tc.row, tc.col, got, string(value[min(got, len(value)-1)]), tc.want)
		}
	}
}
