package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE ONE FRAME IS A RECTANGLE AT EVERY WIDTH, IN BOTH FLOORS.
//
// Every row it draws is exactly the width it was asked for, so the right edge
// lands in one column; the aside is given up before the title is cut; and a
// terminal refused box drawing gets two plain rules and blank sides with the
// rows in the same columns (frame.go's four decisions).
func TestTheOneFrameIsARectangleAtEveryWidth(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		pal := newPalette(tokens.TrueColor, ascii)
		pieces := framePiecesOf(pal)
		for _, width := range []int{8, 20, 40, 84, 120} {
			f := framed{
				title:     pal.ink("? Which storage for the session index?"),
				aside:     pal.dim("model asks"),
				keys:      pal.dim("↑↓ choose · enter take it · esc later"),
				keysAside: pal.dim("c change"),
			}
			rows, span := f.draw(pal, width, []string{pal.ink("▸ 1  SQLite"), "", pal.ink(strings.Repeat("x", 200))})
			if len(rows) != 5 {
				t.Fatalf("ascii=%v width %d: %d rows, want the two edges and three rows", ascii, width, len(rows))
			}
			for i, row := range rows {
				if got := ansi.StringWidth(row); got != width {
					t.Fatalf("ascii=%v width %d: row %d is %d cells: %q", ascii, width, i, got, ansi.Strip(row))
				}
			}
			top, bottom := ansi.Strip(rows[0]), ansi.Strip(rows[len(rows)-1])
			if !strings.HasPrefix(top, pieces.tl) || !strings.HasSuffix(top, pieces.tr) ||
				!strings.HasPrefix(bottom, pieces.bl) || !strings.HasSuffix(bottom, pieces.br) {
				t.Fatalf("ascii=%v width %d: an edge lost a corner:\n%s\n%s", ascii, width, top, bottom)
			}
			if strings.Contains(top, "model asks") && !strings.Contains(top, "Which") {
				t.Fatalf("ascii=%v width %d: the aside outlived the title: %q", ascii, width, top)
			}
			if span.pressable() && ansi.Cut(rows[len(rows)-1], span.from, span.to) != pal.dim("c change") &&
				ansi.Strip(ansi.Cut(rows[len(rows)-1], span.from, span.to)) != "c change" {
				t.Fatalf("ascii=%v width %d: the bottom aside is not where the span says: %v in %q",
					ascii, width, span, bottom)
			}
			for _, row := range rows[1 : len(rows)-1] {
				plainRow := ansi.Strip(row)
				if !strings.HasPrefix(plainRow, pieces.side) || !strings.HasSuffix(plainRow, pieces.side) {
					t.Fatalf("ascii=%v width %d: a row leaks out of its frame: %q", ascii, width, plainRow)
				}
			}
		}
	}
	// AND THE ASCII FLOOR IS TWO RULES AND NO SIDES — never `+` corners and `|`
	// sides, which read as a table drawn in punctuation.
	rows, _ := framed{title: "t"}.draw(newPalette(tokens.NoColor, true), 12, []string{"row"})
	if rows[0] != "-- t -------" || rows[1] != " row        " || rows[2] != "------------" {
		t.Fatalf("the ASCII frame is not two plain rules and blank sides:\n%s", strings.Join(rows, "\n"))
	}
}
