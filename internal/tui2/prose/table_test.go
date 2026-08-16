package prose

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// 13.3's second finding, tested at the frame: "tables wrap to soup".
//
// The property that makes a table a table is that one source row is one screen
// row. Every test here is a way of asking that question — at a width that fits,
// at a width that does not, and at a width where nothing could.

const wideTable = "" +
	"| id | title | owner | status | notes |\n" +
	"| -- | ----- | ----- | ------ | ----- |\n" +
	"| 1 | a reasonably long title for a row | santosh | running | this cell is deliberately far longer than any terminal is wide, so that fitting it is impossible and truncation is the only honest answer |\n" +
	"| 2 | short | ada | done | fine |\n"

// tableRows returns the plain rows a table rendered to, and the count of source
// rows it should have produced: header, hairline, and one row per body row.
func tableRowCount(body int) int { return body + 2 }

// TestTableRowsStayRows is the whole finding in one assertion. Whatever the
// width, four source rows come out as four screen rows plus the header's
// hairline — never five, never nine.
func TestTableRowsStayRows(t *testing.T) {
	for _, width := range []int{20, 34, 60, 80, 120, 200} {
		rows := plain(render(t, wideTable, Options{Width: width, Styler: styler(tokens.TrueColor)}))
		if got, want := len(rows), tableRowCount(2); got != want {
			t.Errorf("width %d: %d rows, want %d\n%s", width, got, want, strings.Join(rows, "\n"))
		}
	}
}

// TestTableNeverExceedsWidth walks the sweep. A table is the shape most likely
// to overflow, because it is the only one that lays columns out arithmetically
// rather than by wrapping.
func TestTableNeverExceedsWidth(t *testing.T) {
	for _, p := range profiles {
		for width := 20; width <= 140; width++ {
			for _, row := range Render(wideTable, Options{Width: width, Styler: styler(p)}) {
				if got := ansi.StringWidth(row); got > width {
					t.Fatalf("%s at width %d: row is %d cells: %q", p, width, got, row)
				}
			}
		}
	}
}

// TestTableTruncatesRatherThanWraps: the over-long cell ends in the static
// overflow ellipsis, and its text does not reappear on a row of its own.
func TestTableTruncatesRatherThanWraps(t *testing.T) {
	rows := plain(render(t, wideTable, Options{Width: 60}))
	joinedRows := strings.Join(rows, "\n")
	if !strings.Contains(joinedRows, "…") {
		t.Errorf("nothing was marked as cut:\n%s", joinedRows)
	}
	if strings.Contains(joinedRows, "truncation is the only honest answer") {
		t.Errorf("the long cell wrapped instead of truncating:\n%s", joinedRows)
	}
}

// TestTableColumnsAlign is the reason the ellipsis is worth paying: the columns
// are still columns, so the eye can run down one.
func TestTableColumnsAlign(t *testing.T) {
	src := "| a | b |\n| - | - |\n| one | two |\n| three | four |\n"
	rows := plain(render(t, src, Options{Width: 40}))
	if len(rows) != tableRowCount(2) {
		t.Fatalf("want %d rows, got %d", tableRowCount(2), len(rows))
	}
	col := strings.Index(rows[0], "b")
	for i, row := range rows[2:] {
		second := strings.Fields(row)
		if len(second) != 2 {
			t.Fatalf("body row %d did not split into two cells: %q", i, row)
		}
		if got := strings.Index(row, second[1]); got != col {
			t.Errorf("body row %d starts column 2 at %d, header starts it at %d: %q",
				i, got, col, row)
		}
	}
}

// TestTableRightAlignment pins the one alignment that changes what a table
// MEANS: a column of figures compares only when it shares a right edge.
func TestTableRightAlignment(t *testing.T) {
	src := "| item | cost |\n| --- | ---: |\n| a | 8 |\n| b | 1024 |\n"
	rows := plain(render(t, src, Options{Width: 30}))
	first := strings.TrimRight(rows[2], " ")
	second := strings.TrimRight(rows[3], " ")
	if !strings.HasSuffix(first, "8") || !strings.HasSuffix(second, "1024") {
		t.Fatalf("figures are not right-aligned:\n%q\n%q", first, second)
	}
	if ansi.StringWidth(first) != ansi.StringWidth(second) {
		t.Errorf("right-aligned rows do not share an edge: %d vs %d",
			ansi.StringWidth(first), ansi.StringWidth(second))
	}
}

// TestTableTooNarrowToFitIsScrollableShaped is the give-up case, and the point
// is that it gives up in the RIGHT direction: rows stay one row each and are
// cut at the right edge, so a caller with a horizontal viewport can crop. The
// alternative — dropping columns — would be a table that lies about its shape.
func TestTableTooNarrowToFitIsScrollableShaped(t *testing.T) {
	var b strings.Builder
	cols := 20
	for i := 0; i < cols; i++ {
		fmt.Fprintf(&b, "| column %d ", i)
	}
	b.WriteString("|\n")
	for i := 0; i < cols; i++ {
		b.WriteString("| --- ")
	}
	b.WriteString("|\n")
	for r := 0; r < 3; r++ {
		for i := 0; i < cols; i++ {
			fmt.Fprintf(&b, "| value %d-%d ", r, i)
		}
		b.WriteString("|\n")
	}
	for _, width := range []int{20, 40, 80} {
		rows := plain(render(t, b.String(), Options{Width: width}))
		if got, want := len(rows), tableRowCount(3); got != want {
			t.Errorf("width %d: %d rows, want %d", width, got, want)
		}
		for i, row := range rows {
			if got := ansi.StringWidth(row); got > width {
				t.Errorf("width %d: row %d is %d cells", width, i, got)
			}
		}
	}
}

// TestTableHeaderIsQuieterThanItsBody holds 5.13's type hierarchy on a grid: a
// header is a LABEL and labels sit at the secondary tier, so the data outranks
// the words naming it.
func TestTableHeaderIsQuieterThanItsBody(t *testing.T) {
	rows := render(t, "| a | b |\n| - | - |\n| one | two |\n",
		Options{Width: 40, Styler: styler(tokens.TrueColor)})
	head := tokens.TextSecondary.Fg(tokens.TrueColor, tokens.FocusNormal)
	bodyTier := tokens.TextPrimary.Fg(tokens.TrueColor, tokens.FocusNormal)
	if !strings.Contains(rows[0], head) {
		t.Errorf("header is not at the secondary tier: %q", rows[0])
	}
	if !strings.Contains(rows[2], bodyTier) {
		t.Errorf("body is not at the primary tier: %q", rows[2])
	}
}

// TestRaggedTableDoesNotPanic: a model writes markdown by hand, and a row with
// the wrong number of cells is the most ordinary way for it to be wrong.
func TestRaggedTableDoesNotPanic(t *testing.T) {
	src := "| a | b | c |\n| - | - | - |\n| one |\n| one | two | three | four | five |\n"
	rows := plain(render(t, src, Options{Width: 40}))
	if len(rows) == 0 {
		t.Fatal("a ragged table rendered nothing at all")
	}
}
