package blocks_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// THE TITLE IS ON THE GRID, which is the whole reason this renderer exists.
//
// The seam it replaces led with TWO marks and put `execution` at column 3 — the
// one row on the record page that did not line up with the rows above and below
// it (§16 ALIGNMENT, §20's ladder). One mark and one space puts the word at
// [blocks.ContentEdge], where every other title on every other surface hangs.
func TestARuledLineHangsItsTitleAtTheContentEdge(t *testing.T) {
	floor := blocks.ContentEdge + blocks.RuleFloor + 2
	for width := floor; width <= 200; width++ {
		row := blocks.Ruled{Title: "execution"}.Render(width, nil)
		at := blocks.Width(row[:len(row)-len(strings.TrimLeft(row, blocks.RuleMark+" "))])
		if at != blocks.ContentEdge {
			t.Fatalf("at %d cells the title hangs at column %d, want %d:\n  %q",
				width, at, blocks.ContentEdge, row)
		}
	}
}

// A RULE NEVER OUTGROWS ITS ROW. Every width, every degradation rung, meta and
// no meta alike — the row measures exactly what it was given or less.
func TestARuledLineIsNeverWiderThanTheRow(t *testing.T) {
	rule := blocks.Ruled{
		Title: "wisp-parity-2",
		Meta:  []string{"28m", "$8.65", "41k tok", "claude-k3"},
	}
	for width := 0; width <= 200; width++ {
		row := rule.Render(width, nil)
		if got := blocks.Width(row); got > width {
			t.Fatalf("a %d-cell rule drew %d cells:\n  %q", width, got, row)
		}
		if strings.Contains(row, "\n") {
			t.Fatalf("a %d-cell rule drew a second row:\n  %q", width, row)
		}
	}
	if row := (blocks.Ruled{Title: "x"}).Render(0, nil); row != "" {
		t.Fatalf("a zero-width rule drew %q", row)
	}
}

// META SHEDS WHOLE FROM THE RIGHT. Half a receipt is not a smaller truth about a
// number, it is a false one — so a cell that does not fit leaves entirely, and
// the cells before it stay exactly as they were.
func TestARuledLineShedsItsMetaWhole(t *testing.T) {
	rule := blocks.Ruled{
		Title: "wisp-parity-2",
		Meta:  []string{"28m", "$8.65", "41k tok"},
	}
	wide := rule.Render(120, nil)
	for _, cell := range rule.Meta {
		if !strings.Contains(wide, cell) {
			t.Fatalf("a wide rule dropped %q:\n  %q", cell, wide)
		}
	}
	kept, dropped := 0, false
	for width := 120; width >= blocks.ContentEdge+blocks.RuleFloor+2; width-- {
		row := rule.Render(width, nil)
		here := 0
		for _, cell := range rule.Meta {
			if strings.Contains(row, cell) {
				here++
				continue
			}
			dropped = true
		}
		// The cells present are always a PREFIX of the list: a rule that kept
		// `41k tok` while dropping `$8.65` would be shedding from the middle.
		for i := 0; i < here; i++ {
			if !strings.Contains(row, rule.Meta[i]) {
				t.Fatalf("at %d cells the rule shed from the middle:\n  %q", width, row)
			}
		}
		if here > kept && kept != 0 {
			t.Fatalf("at %d cells the rule grew a meta cell back:\n  %q", width, row)
		}
		kept = here
		// Nothing is ever half a cell: what survives, survives whole.
		if here == 0 && strings.Contains(row, blocks.OverflowMark+" tok") {
			t.Fatalf("at %d cells a meta cell was cut rather than shed:\n  %q", width, row)
		}
	}
	if !dropped {
		t.Fatal("the meta never shed, so this test proves nothing")
	}
}

// A TITLE THAT CANNOT FIT DEGRADES TO THE BARE RULE. There is no rung below
// `one mark, one space, one cell of word, four marks of rule`; under it the row
// is a hairline, which is honest at any width (§16's EMPTINESS).
func TestAnImpossibleTitleDegradesToABareRule(t *testing.T) {
	floor := blocks.ContentEdge + blocks.RuleFloor + 2
	for width := 1; width < floor; width++ {
		row := blocks.Ruled{Title: "execution"}.Render(width, nil)
		if want := strings.Repeat(blocks.RuleMark, width); row != want {
			t.Fatalf("at %d cells the rule is %q, want the bare hairline %q",
				width, row, want)
		}
	}
	// And at the floor itself it is a titled rule again, with a tail that still
	// reads as one.
	row := blocks.Ruled{Title: "execution"}.Render(floor, nil)
	if !strings.HasSuffix(row, strings.Repeat(blocks.RuleMark, blocks.RuleFloor)) {
		t.Fatalf("the floor width kept fewer than %d trailing marks: %q",
			blocks.RuleFloor, row)
	}
	if strings.HasPrefix(row, blocks.RuleMark+blocks.RuleMark) {
		t.Fatalf("the rule led with more than %d mark: %q", blocks.RuleLead, row)
	}
}

// An empty title is a bare hairline and not a rule with a hole in it: [Rule] and
// [Ruled] agree, so a surface that computes its own word cannot draw a stray gap
// on the day the word comes back empty.
func TestARuledLineWithNoTitleIsTheBareRule(t *testing.T) {
	for _, width := range []int{1, 8, 40, 120} {
		if got, want := (blocks.Ruled{}).Render(width, nil), blocks.Rule(width, nil); got != want {
			t.Fatalf("at %d cells an untitled rule is %q, want %q", width, got, want)
		}
	}
}
