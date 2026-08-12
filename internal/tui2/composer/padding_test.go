package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// §19's padding law and §20's grid, measured on the surface the reader stares at
// most.
//
// THE DEFECT these pin, in the reader's own words: "aforge text input does not
// seem to be proper padding for input text nor thinking part etc.. it seems to
// be really bad". The typed sentence, the ghost line and the streaming prompt
// all sat welded to the field's own edge, and a wrapped line ran into its last
// column. Every test here asserts a COLUMN rather than a constant, because a
// padding law that only holds where a constant is read is a law with one caller.

// plainStyler is the profile these tests measure in: no colour, plain glyph
// tier, so a column is a column and not an escape sequence.
func plainStyler() *tokens.Styler {
	return tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, tokens.Plain)
}

// rowsAt renders and strips, so every assertion below counts cells.
func rowsAt(m *Model, width, height int) []string {
	return strings.Split(ansi.Strip(m.Render(width, height)), "\n")
}

// colOf is the CELL column a substring sits at, which is the only unit any of
// this is stated in. strings.Index answers in bytes, and the hug edge is a
// three-byte glyph one cell wide — measuring the wrong one is how a test about
// columns passes on a frame that has none.
func colOf(row, sub string) int {
	at := strings.Index(row, sub)
	if at < 0 {
		return -1
	}
	return ansi.StringWidth(row[:at])
}

// TestTheGridPutsContentAtTheEdgeAfterATwoCellGutter is §20 stated as
// arithmetic: the field's edge, one cell of §19 padding, a two-cell gutter
// holding the marker and its space, and the content edge after them.
//
// It is the assertion the other tests in this file lean on, so it is written
// once and read as a number rather than re-derived per case.
func TestTheGridPutsContentAtTheEdgeAfterATwoCellGutter(t *testing.T) {
	for _, width := range []int{20, 40, 60, 80, 120} {
		if got := padAt(width); got != innerPad {
			t.Fatalf("width %d: inner pad = %d, want %d", width, got, innerPad)
		}
		if got := TextColumn(width); got != hugEdgeCells+innerPad+gridGutter {
			t.Fatalf("width %d: content edge = %d, want %d",
				width, got, hugEdgeCells+innerPad+gridGutter)
		}
		// The indent STEP the chrome rows hang from is the same edge, reached by
		// a different route (the pad outside, hintIndent inside), which is the
		// thing that can silently drift.
		if got := padAt(width) + ansi.StringWidth(hintIndent); got != TextColumn(width) {
			t.Fatalf("width %d: chrome edge = %d, content edge = %d",
				width, got, TextColumn(width))
		}
	}
}

// TestTypedGhostAndStreamingShareOneInnerEdge is the defect's sentence turned
// into a measurement: three different things can occupy the composer's first
// row, and §19 says all three hang from the same inner edge.
func TestTypedGhostAndStreamingShareOneInnerEdge(t *testing.T) {
	for _, width := range []int{24, 40, 60, 100} {
		edge := TextColumn(width)

		// 1. TYPED TEXT.
		typed := New(Options{Styler: plainStyler()})
		typed.Focus(true)
		typeString(typed, "hello")
		row := rowsAt(typed, width, 1)[0]
		if at := colOf(row, "hello"); at != edge {
			t.Fatalf("width %d: typed text starts at column %d, want %d (%q)",
				width, at, edge, row)
		}

		// 2. THE GHOST HINT. It follows the caret cell, which itself sits on the
		// content edge — so the edge is where the caret is.
		ghost := New(Options{Styler: plainStyler()})
		ghost.Focus(true)
		row = rowsAt(ghost, width, 1)[0]
		if at := colOf(row, caretBlock); at != edge {
			t.Fatalf("width %d: the ghost row's caret is at column %d, want the edge %d (%q)",
				width, at, edge, row)
		}
		if at := colOf(row, hintCells[HintIdle][0]); at < edge {
			t.Fatalf("width %d: the ghost hint opened at column %d, before the edge %d (%q)",
				width, at, edge, row)
		}
		if n := ansi.StringWidth(row); n > width-padAt(width) {
			t.Fatalf("width %d: the ghost line ran to %d cells, past the right pad (%q)",
				width, n, row)
		}

		// 3. THE STREAMING INDICATOR. §8 puts it AT the prompt, so it takes the
		// marker's cell and the content edge does not move when a reply starts.
		live := New(Options{Styler: plainStyler()})
		live.Focus(true)
		typeString(live, "hello")
		live.Streaming(true, 4)
		row = rowsAt(live, width, 1)[0]
		if at := colOf(row, tokens.Spinner(4)); at != hugEdgeCells+innerPad {
			t.Fatalf("width %d: the streaming glyph sits at column %d, want the gutter at %d (%q)",
				width, at, hugEdgeCells+innerPad, row)
		}
		if at := colOf(row, "hello"); at != edge {
			t.Fatalf("width %d: the content edge moved to %d when the reply started (%q)",
				width, at, row)
		}
	}
}

// TestEveryComposerRowKeepsBothPads: the pad is not a property of the first row.
// A wrapped draft, a candidate list and a chip all stop one cell short of the
// field's right edge and open one cell inside its left one.
func TestEveryComposerRowKeepsBothPads(t *testing.T) {
	const width = 34
	m := New(Options{Styler: plainStyler(), NewlineKeys: []string{"alt+enter"}})
	m.Focus(true)
	typeString(m, strings.Repeat("wordword ", 9))
	rows := rowsAt(m, width, 1+m.GrowRows(width))
	if len(rows) < 3 {
		t.Fatalf("the fixture did not wrap: %v", rows)
	}
	for i, row := range rows {
		if n := ansi.StringWidth(row); n > width-innerPad {
			t.Fatalf("row %d runs to %d cells, past the right pad at %d: %q",
				i, n, width-innerPad, row)
		}
		if !strings.HasPrefix(row, tokens.GlyphHugEdge) {
			t.Fatalf("row %d lost the field's edge: %q", i, row)
		}
		// Row 0 carries the marker in the gutter; every row under it leaves that
		// cell blank and opens on the content edge. Both are measured as "the
		// first inked cell after the field's edge", which is the one question a
		// reader's eye is actually asking.
		rest := strings.TrimPrefix(row, tokens.GlyphHugEdge)
		ink := hugEdgeCells + ansi.StringWidth(rest) - ansi.StringWidth(strings.TrimLeft(rest, " "))
		want := TextColumn(width)
		if i == 0 {
			want = hugEdgeCells + innerPad
		}
		if ink != want {
			t.Fatalf("row %d opens at column %d, want %d: %q", i, ink, want, row)
		}
	}
}

// TestWrapMeasuresThePaddedWidth: the padded width is what wraps. A wrap taken
// against the rectangle would put one character per line into the cell the
// padding was supposed to be keeping empty.
func TestWrapMeasuresThePaddedWidth(t *testing.T) {
	for _, width := range []int{20, 33, 48, 77} {
		want := width - 2*padAt(width) - gutterAt(innerWidth(width))
		if got := usable(width); got != want {
			t.Fatalf("width %d: draft measure = %d, want %d", width, got, want)
		}
		m := New(Options{Styler: plainStyler()})
		m.Focus(true)
		typeString(m, strings.Repeat("x", want*2))
		rows := layoutRows([]rune(m.Value()), usable(width))
		if len(rows) != 2 {
			t.Fatalf("width %d: %d cells of text wrapped to %d rows, want 2",
				width, want*2, len(rows))
		}
		// And the picture agrees with the arithmetic.
		for i, row := range rowsAt(m, width, 1+m.GrowRows(width)) {
			if n := ansi.StringWidth(row); n > width-padAt(width) {
				t.Fatalf("width %d row %d: %d cells, past the right pad: %q", width, i, n, row)
			}
		}
	}
}

// TestGrowRowsCountsThePaddedWrap: the region asks the layout for the rows the
// PADDED draft needs. Asking for the unpadded count is how a draft's last line
// ends up drawn under the bottom of its own rectangle.
func TestGrowRowsCountsThePaddedWrap(t *testing.T) {
	const width = 30
	m := New(Options{Styler: plainStyler()})
	m.Focus(true)
	typeString(m, strings.Repeat("y", usable(width)+1))
	if got := m.GrowRows(width); got != 1 {
		t.Fatalf("a draft one cell past the padded measure asked for %d extra rows, want 1", got)
	}
	if rows := rowsAt(m, width, 1+m.GrowRows(width)); len(rows) != 2 {
		t.Fatalf("the region drew %d rows for a two-row draft: %v", len(rows), rows)
	}
}

// TestTheCaretLandsOnTheCellTheGlyphIsDrawnIn: the terminal's real cursor is
// placed from [Model.CaretAt], and the painted caret from the same plan. §19
// moved the content edge, so a caret derived by subtracting the draft's measure
// from the rectangle would now sit one cell right of the letter it is on — the
// exact class of bug two spellings of one number produce.
func TestTheCaretLandsOnTheCellTheGlyphIsDrawnIn(t *testing.T) {
	for _, width := range []int{18, 40, 90} {
		m := New(Options{Styler: plainStyler()})
		m.Focus(true)
		typeString(m, "abc")
		x, y, ok := m.CaretAt(width, 1)
		if !ok {
			t.Fatalf("width %d: the caret is not on screen", width)
		}
		if y != 0 {
			t.Fatalf("width %d: the caret is on row %d", width, y)
		}
		// The caret sits past the three typed letters, at the content edge plus
		// their cells — and the row's own picture puts the block there too.
		if want := TextColumn(width) + 3; x != want {
			t.Fatalf("width %d: caret column %d, want %d", width, x, want)
		}
		row := rowsAt(m, width, 1)[0]
		if at := colOf(row, caretBlock); at != x {
			t.Fatalf("width %d: the painted caret is at column %d, the reported one at %d (%q)",
				width, at, x, row)
		}
		// And a pointer put on the glyph's cell resolves to the same position.
		m2 := New(Options{Styler: plainStyler()})
		m2.Focus(true)
		typeString(m2, "abc")
		if !m2.ClickCaret(width, 1, TextColumn(width), 0) {
			t.Fatalf("width %d: clicking the content edge moved nothing", width)
		}
		m2.insert("|")
		if got := m2.Value(); got != "|abc" {
			t.Fatalf("width %d: a click on the content edge landed at %q", width, got)
		}
	}
}

// TestTheNarrowestComposerSpendsThePadFirst: the pad is rhythm, and rhythm is
// the first thing a terminal too narrow for a word cannot pay for. Below the
// threshold the row is exactly what it was before this law existed — edge,
// marker, space — and never a row with padding and no prompt.
func TestTheNarrowestComposerSpendsThePadFirst(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	m.Focus(true)
	for width := 1; width <= 12; width++ {
		row := rowsAt(m, width, 1)[0]
		if n := ansi.StringWidth(row); n > width {
			t.Fatalf("width %d overran to %d cells: %q", width, n, row)
		}
		if !strings.HasPrefix(row, tokens.GlyphHugEdge) {
			t.Fatalf("width %d dropped the field's edge: %q", width, row)
		}
		switch {
		case width < promptGutter+2*innerPad+1:
			if padAt(width) != 0 {
				t.Fatalf("width %d kept the pad with no room for a letter", width)
			}
			if !strings.HasPrefix(row, tokens.GlyphHugEdge+tokens.GlyphPromptChat) &&
				gutterAt(innerWidth(width)) >= 2 {
				t.Fatalf("width %d shed the prompt before the pad: %q", width, row)
			}
		default:
			if padAt(width) != innerPad {
				t.Fatalf("width %d dropped the pad it could afford", width)
			}
			if !strings.HasPrefix(row, edged(width, tokens.GlyphPromptChat)) {
				t.Fatalf("width %d: %q does not open edge · pad · prompt", width, row)
			}
		}
	}
}
