package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// plainInk paints nothing, so an assertion here is about cells and not colour.
func plainInk(text string) string { return text }

// TestACardRowNeverPricesANameItHadToCut is rowfit.go's law 1 on the card's own
// row fitter.
//
// The tail was measured first and given every cell it asked for, and the label
// took what was left with an ellipsis in it — `✓ Port the picker onto the ne…
// $0.31`, which names no piece of work and prices it precisely.
func TestACardRowNeverPricesANameItHadToCut(t *testing.T) {
	const label = "Port the picker onto the new fitter"
	const tail = "$0.31"

	rows := bandSides(40, 2, 8, label, tail, plainInk, plainInk)
	if len(rows) != 2 {
		t.Fatalf("a 40-cell row drew\n\t%q\nand the name is %d cells beside a %d-cell price — a name that had to be cut takes the row, and the fact goes under it",
			strings.Join(rows, "\n\t"), ansi.StringWidth(label), ansi.StringWidth(tail))
	}
	if !strings.Contains(rows[0], label) {
		t.Fatalf("the name was still cut on its own row:\n\t%q", rows[0])
	}
	if !strings.Contains(rows[1], tail) {
		t.Fatalf("the fact did not get the row under the name:\n\t%q", strings.Join(rows, "\n\t"))
	}
	for i, row := range rows {
		if ansi.StringWidth(row) > 40 {
			t.Fatalf("row %d overran the card:\n\t%q", i, row)
		}
	}

	// AND A ROW THAT CAN HOLD BOTH STILL HOLDS BOTH. A second line spent where
	// one would do is the same defect with the halves swapped.
	wide := bandSides(60, 2, 8, label, tail, plainInk, plainInk)
	if len(wide) != 1 || !strings.Contains(wide[0], label) || !strings.Contains(wide[0], tail) {
		t.Fatalf("a 60-cell row did not keep the name and the fact together:\n\t%q", strings.Join(wide, "\n\t"))
	}

	// AND A NAME TOO LONG FOR ANY ROW SHARES AGAIN, down to the caller's floor:
	// it is going to be cut whichever shape the row takes, and a second line for
	// the fact would be a line spent for nothing.
	huge := strings.Repeat("a very long deliverable name ", 3)
	if got := bandSides(40, 2, 8, huge, tail, plainInk, plainInk); len(got) != 1 {
		t.Fatalf("a name longer than the whole card took %d rows, want the one it shares:\n\t%q", len(got), strings.Join(got, "\n\t"))
	}
}
