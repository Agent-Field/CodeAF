package rail

import (
	"strings"
	"testing"
)

// 8.1.7: a live list keeps its running rows, and the fold line goes at the TOP
// so the live edge stays where the reader is looking.
func TestLiveOverflowKeepsRunningRows(t *testing.T) {
	m := New(crowd(30, LifeQueued, LifeWorking, LifeSettled))
	lines := plainView().Render(m, ModeRail, 28, 9)
	if !strings.HasPrefix(lines[2], "…") {
		t.Fatalf("a live list did not fold from the top: %q", lines[2])
	}
	body := lines[3:]
	for _, l := range body {
		if !strings.Contains(l, "◐") {
			t.Fatalf("a live fold kept a row that is not running: %q\n%s", l, strings.Join(lines, "\n"))
		}
	}
	if !strings.Contains(lines[2], "more") || !strings.Contains(lines[2], "pending") {
		t.Fatalf("the fold line carries no breakdown: %q", lines[2])
	}
}

// 8.1.7: a finalized list gives the slots to the failures first, and its fold
// line goes at the bottom.
func TestFinalizedOverflowKeepsFailures(t *testing.T) {
	m := New(crowd(30, LifeSettled, LifeSettled, LifeFailed))
	lines := plainView().Render(m, ModeRail, 28, 9)
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "…") {
		t.Fatalf("a finalized list did not fold at the bottom: %q", last)
	}
	broken := 0
	for _, l := range lines[2 : len(lines)-1] {
		if strings.Contains(l, "✕") {
			broken++
		}
	}
	if broken != len(lines)-3 {
		t.Fatalf("a finalized fold spent slots on rows that did not fail:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(last, "done") {
		t.Fatalf("the fold line does not account for the settled rows: %q", last)
	}
}

// A question outranks everything: a blocked human is the most expensive state
// this product has (5.9, and the footer table says the same thing).
func TestOverflowNeverFoldsAwayAQuestion(t *testing.T) {
	src := crowd(30, LifeWorking)
	home := src.scopes[HomeScopeID]
	home.Rows[1].Questions = 1
	src.set(home)
	m := New(src)
	lines := plainView().Render(m, ModeRail, 28, 6)
	if !strings.Contains(strings.Join(lines, "\n"), "?") {
		t.Fatalf("the open question was folded away:\n%s", strings.Join(lines, "\n"))
	}
}

// The cursor is never folded away: a map that hides where you are standing is
// a map lying. This walks every row of a crowded scope at every height a rail
// might get.
func TestOverflowAlwaysShowsTheCursor(t *testing.T) {
	m := New(crowd(30, LifeQueued, LifeWorking, LifeFailed, LifeSettled))
	v := plainView()
	for cursor := 1; cursor < m.Len(); cursor++ {
		m.Select(cursor)
		name := m.Selected().Name
		for _, height := range []int{3, 4, 6, 10, 20} {
			lines := v.Render(m, ModeRail, 28, height)
			if !strings.Contains(strings.Join(lines, "\n"), name) {
				t.Fatalf("cursor %d (%s) vanished at height %d:\n%s",
					cursor, name, height, strings.Join(lines, "\n"))
			}
		}
	}
}

// Row 0 is the scope's conversational surface and never folds: a scope you
// cannot speak into is not a scope (5.15).
func TestTheSurfaceRowNeverFolds(t *testing.T) {
	m := New(crowd(30, LifeWorking))
	v := plainView()
	for _, height := range []int{1, 2, 3, 8} {
		lines := v.Render(m, ModeRail, 28, height)
		if len(lines) == 0 || !strings.Contains(lines[0], "aforge") {
			t.Fatalf("height %d lost the surface row: %v", height, lines)
		}
	}
}

// A fold hides rows; it never re-sorts them (7.2).
func TestFoldPreservesDisplayOrder(t *testing.T) {
	m := New(crowd(30, LifeQueued, LifeWorking, LifeFailed))
	m.Select(15)
	lines := plainView().Render(m, ModeRail, 28, 12)
	want := names(m.Rows())
	last := -1
	for _, l := range lines {
		for i, n := range want {
			if strings.Contains(l, n) {
				if i < last {
					t.Fatalf("row %q came after %q:\n%s", n, want[last], strings.Join(lines, "\n"))
				}
				last = i
			}
		}
	}
}

// The fold line is rebuilt only when the breakdown changes: a rail that
// repaints on a timer must not rebuild an unchanged string every frame.
func TestFoldLineIsCachedUntilTheBreakdownMoves(t *testing.T) {
	m := New(crowd(30, LifeQueued, LifeWorking))
	v := plainView()
	first := copyOf(v.Render(m, ModeRail, 28, 9))
	before := v.fold.fold
	second := copyOf(v.Render(m, ModeRail, 28, 9))
	assertLines(t, second, first)
	if v.fold.fold != before {
		t.Fatalf("the fold line was rebuilt: %q then %q", before, v.fold.fold)
	}
}

func TestFoldFitEdges(t *testing.T) {
	var f folder
	rows := []Row{{Name: "a"}, {Name: "b"}}
	heights := []int{2, 2}
	if p := f.fit(nil, nil, 10, 0, true); len(p.shown) != 0 || p.fold != "" {
		t.Fatalf("an empty list produced %+v", p)
	}
	if p := f.fit(rows, heights, 0, 0, true); len(p.shown) != 0 || p.hidden != 2 {
		t.Fatalf("a zero budget produced %+v", p)
	}
	if p := f.fit(rows, heights, 4, 0, true); len(p.shown) != 2 || p.fold != "" {
		t.Fatalf("an exact budget folded anyway: %+v", p)
	}
	// One line of budget buys the fold line and nothing else — which is the
	// honest answer rather than half a row.
	p := f.fit(rows, heights, 1, -1, false)
	if len(p.shown) != 0 || p.hidden != 2 || p.fold == "" {
		t.Fatalf("a one-line budget produced %+v", p)
	}
	// The cursor is kept even when it does not fit; the renderer clips it.
	p = f.fit(rows, heights, 2, 1, true)
	if len(p.shown) != 1 || p.shown[0] != 1 {
		t.Fatalf("the cursor was not kept: %+v", p)
	}
}
