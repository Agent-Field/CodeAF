package footer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The breadcrumb's one law, asserted as a width sweep because that is the shape
// the defect had: nothing was wrong at any single width, and everything was
// wrong on the way down.

const (
	crumbAncestor = "untitled room"
	crumbCurrent  = "Higher-order investment angles"
	crumbTail     = tokens.GlyphScopeUp + " " + crumbAncestor + " " +
		tokens.GlyphScopeUp + " " + crumbCurrent
)

// crumbContext is one entered room's real footer: the trail where the tabs
// would be, a turn in flight, and the standing facts. It is deliberately
// lighter than [fullContext] — a task room does not usually have every state at
// once — because this test is about the width the trail runs out of room at, and
// a synthetic row full of zones would move that width somewhere no reader has
// ever been.
func crumbContext() FocusContext {
	return FocusContext{
		Places:        places(),
		EscInterrupts: true,
		Spend:         1.42,
		HaveSpend:     true,
		ScopeTail:     crumbTail,
	}
}

// TestTheBreadcrumbKeepsWhereYouAreLongest is the elision ORDER, swept.
//
// THE DEFECT: the trail was one column and one width, so `untitled room ‹
// Higher-order investment angles` either fit whole or left the row whole — and
// when it left, the one thing on screen naming where the reader was standing
// went with it. The cells were not missing; they were being spent on the room
// the task is inside of, which is the half a reader can answer with esc.
//
// Under the three-zone row the law splits in two, because "where am I" now has
// two answers on one line. The TABS are the last thing standing (§7's
// degradation order) and say which page; the TRAIL says how deep inside it, and
// outlives every telemetry cell before it goes. So the sweep asserts the trail's
// own elision order while it is present, and asserts that the page word is
// present on every row that exists at all.
func TestTheBreadcrumbKeepsWhereYouAreLongest(t *testing.T) {
	m := New(Options{})
	ctx := crumbContext()

	sawElidedAncestor, sawCutName := false, false
	for width := 200; width >= 1; width-- {
		row := ansi.Strip(m.Render(ctx, width))
		if got := ansi.StringWidth(row); got > width {
			t.Fatalf("at width %d the row ran %d cells: %q", width, got, row)
		}
		if row == "" {
			continue
		}
		ancestor := strings.Contains(row, crumbAncestor)
		whole := strings.Contains(row, crumbCurrent)
		// "High" is the head of the current name and the least the zone can
		// carry and still be an answer: while there is a row at all, this much
		// of where the reader IS has to be on it.
		some := strings.Contains(row, "High")

		// The page word is on every row that exists at all.
		if !strings.Contains(row, "chat") {
			t.Fatalf("at width %d the row exists and names no page: %q", width, row)
		}
		if !some {
			// The trail has left entirely, which is legal below its floor —
			// what is illegal is a trail that named an ancestor and cut the
			// reader's own place, and that is asserted below.
			if ancestor {
				t.Fatalf("at width %d the ancestor outlived the reader's own place: %q", width, row)
			}
			continue
		}
		if ancestor && !whole {
			t.Fatalf("at width %d the row named an ancestor and elided the reader's own place: %q",
				width, row)
		}
		if !whole {
			sawCutName = true
		}
		if !ancestor {
			sawElidedAncestor = true
		}
	}
	if !sawElidedAncestor {
		t.Fatal("no width dropped the ancestor while keeping the reader's own place")
	}
	if !sawCutName {
		t.Fatal("no width cut the current name; the last resort is unreachable")
	}
}

// The order, stated once against the fitter itself: whole trail, then the
// ancestors as one mark, then the name from the middle. Every form still opens
// with the way out, because the breadcrumb is a door before it is a label
// (5.15).
func TestTheTrailElidesAncestorsBeforeTheNameItIsOn(t *testing.T) {
	whole := fitScopeTail(crumbTail, 200)
	if whole != crumbTail {
		t.Fatalf("a generous width shortened the trail: %q", whole)
	}

	elided := fitScopeTail(crumbTail, ansi.StringWidth(crumbTail)-1)
	if strings.Contains(elided, crumbAncestor) {
		t.Fatalf("one cell short, the ancestor stayed: %q", elided)
	}
	if !strings.Contains(elided, crumbCurrent) {
		t.Fatalf("one cell short, the reader's own place went first: %q", elided)
	}
	if !strings.HasPrefix(elided, tokens.GlyphScopeUp+" "+scopeElided) {
		t.Fatalf("the elided trail lost the way out or the mark: %q", elided)
	}

	cut := fitScopeTail(crumbTail, ansi.StringWidth(elided)-1)
	if strings.Contains(cut, crumbCurrent) {
		t.Fatalf("the name did not give when nothing else was left to: %q", cut)
	}
	// A title is recognized by its head and disambiguated by its tail, so the
	// cut is in the middle and both ends survive it.
	if !strings.Contains(cut, "High") || !strings.Contains(cut, "angles") {
		t.Fatalf("the cut took an end off the name: %q", cut)
	}
	if got := ansi.StringWidth(cut); got > ansi.StringWidth(elided)-1 {
		t.Fatalf("the cut trail ran %d cells: %q", got, cut)
	}
}

// A trail with one name in it is a room at home. It never grows the mark for
// ancestors it does not have — the affordance never lies, and `‹ … ‹ aforge`
// would be claiming a depth the reader is not at.
func TestAOneNameTrailNeverGrowsAnElisionMark(t *testing.T) {
	const tail = tokens.GlyphScopeUp + " untitled room"
	for width := 1; width <= 30; width++ {
		got := fitScopeTail(tail, width)
		if strings.Contains(got, scopeElided) {
			t.Fatalf("at width %d a one-name trail claimed ancestors: %q", width, got)
		}
		if ansi.StringWidth(got) > width {
			t.Fatalf("at width %d the trail ran %d cells: %q", width, ansi.StringWidth(got), got)
		}
	}
}

// Every form the fitter can return fits the budget it was given, at every width
// and for every depth — including the pathological ones, where the answer is
// the empty string and the column leaves.
func TestTheTrailNeverExceedsItsBudget(t *testing.T) {
	tails := []string{
		crumbTail,
		tokens.GlyphScopeUp + " untitled room",
		tokens.GlyphScopeUp + " a " + tokens.GlyphScopeUp + " b " +
			tokens.GlyphScopeUp + " c " + tokens.GlyphScopeUp + " d",
		"no separators at all",
		"",
	}
	for _, tail := range tails {
		for width := -2; width <= 60; width++ {
			got := fitScopeTail(tail, width)
			if width > 0 && ansi.StringWidth(got) > width {
				t.Fatalf("%q at width %d ran %d cells: %q",
					tail, width, ansi.StringWidth(got), got)
			}
			if width <= 0 && got != "" {
				t.Fatalf("%q at width %d rendered %q", tail, width, got)
			}
		}
	}
}
