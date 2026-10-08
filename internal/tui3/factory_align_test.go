package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE ALIGNMENT AUDIT ─────────────────────────────────────────────────────
//
// PADDING, POSITIONING AND ALIGNMENT ARE DELIBERATE (owner ruling,
// 2026-10-08), and this proves it the way a person would check it by eye: it
// renders the fixture at 160, 120 and 100 columns and scans the frame's text
// for every column that should stand at one cell and every gap that should be
// one row. factory_gridlaw_test.go proves the other half, that every width
// it measures comes from factory_grid.go.

// factoryAlignWidths are the three widths the audit renders: two with a peek,
// one without.
var factoryAlignWidths = []int{160, 120, 100}

// factoryAlignLab is the fixture floor at width on the plain floor, its body
// drawn once so the page's measurements are the frame's.
func factoryAlignLab(t *testing.T, width int) (*app, []string) {
	t.Helper()
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	a.width, a.height = width, 50
	body := factoryBodyPlain(a, width, 40)
	return a, body
}

// factorySplitAt is a body row cut at the divider: the rows' column and the
// peek's, and the divider's cell (-1 for a row with none).
func factorySplitAt(row string) (left, right string, div int) {
	r := []rune(row)
	for i, c := range r {
		if string(c) == "│" {
			return string(r[:i]), string(r[i+1:]), i
		}
	}
	return row, "", -1
}

// factoryFirstInk is the cell of a row's first character that is not air, -1
// for a blank row.
func factoryFirstInk(s string) int {
	for i, c := range []rune(s) {
		if c != ' ' {
			return i
		}
	}
	return -1
}

// (a) EVERY ITEM ROW OF A SECTION HAS ITS KIND COLUMN AT ONE CELL AND ITS AGE
// ENDING AT ONE CELL; the priority stands in one cell; the refs right-align.
func TestFactoryAlignRowsColumns(t *testing.T) {
	for _, width := range factoryAlignWidths {
		a, body := factoryAlignLab(t, width)
		railW := a.fp.rowsW - factoryMargin
		if a.fp.rowsW < width {
			railW -= factoryDividerW - factoryRuleW
		}
		g := a.factoryGridAt(railW)
		titleX := factoryMargin + a.factoryTitleCol()
		factsX := titleX + g.titleW + factoryGutter + factoryRepoW + factoryGutter
		ageEnd := -1
		refEnd := factoryMargin + factoryLeadW + factoryPriorityW + max(a.fp.refW, factoryRefW)
		for _, row := range body {
			left, _, _ := factorySplitAt(row)
			r := []rune(left)
			if len(r) <= factsX || !strings.ContainsRune(left, '#') && !strings.Contains(left, " ci ") {
				continue
			}
			if r[factsX-1] != ' ' || r[factsX] == ' ' {
				t.Errorf("at %d the facts do not start at cell %d:\n%q", width, factsX, left)
			}
			if r[refEnd-1] == ' ' || r[refEnd] != ' ' {
				t.Errorf("at %d the ref does not end at cell %d:\n%q", width, refEnd, left)
			}
			end := len([]rune(strings.TrimRight(left, " ")))
			if ageEnd < 0 {
				ageEnd = end
			} else if end != ageEnd {
				t.Errorf("at %d an age ends at %d, not %d:\n%q", width, end, ageEnd, left)
			}
		}
		if ageEnd < 0 {
			t.Fatalf("at %d no item row was found:\n%s", width, strings.Join(body, "\n"))
		}
	}
}

// (b) THE PEEK'S BLOCKS START [factoryMargin] PAST THE DIVIDER, and (e) THE
// ACTION LINE IS THE PEEK'S LAST ROW with one blank above it, and (c) no two
// blank rows stand together inside the peek's content.
func TestFactoryAlignPeek(t *testing.T) {
	for _, width := range factoryAlignWidths[:2] {
		_, body := factoryAlignLab(t, width)
		var pane []string
		for _, row := range body {
			_, right, div := factorySplitAt(row)
			if div < 0 {
				continue
			}
			pane = append(pane, right)
		}
		if len(pane) < 3 {
			t.Fatalf("at %d there is no peek", width)
		}
		for _, p := range pane {
			if x := factoryFirstInk(p); x >= 0 && x != factoryMargin {
				t.Errorf("at %d a peek row starts at %d past the divider, not %d: %q", width, x, factoryMargin, p)
			}
		}
		last := len(pane) - 1
		if !strings.HasPrefix(strings.TrimSpace(pane[last]), "enter open") || strings.TrimSpace(pane[last-1]) != "" {
			t.Errorf("at %d the action line is not the last row with a blank above:\n%s", width, strings.Join(pane, "\n"))
		}
		first, end := -1, last-1
		for i, p := range pane {
			if strings.TrimSpace(p) != "" && first < 0 {
				first = i
			}
		}
		for end > 0 && strings.TrimSpace(pane[end]) == "" {
			end--
		}
		for i := first + 1; i <= end; i++ {
			if strings.TrimSpace(pane[i]) == "" && strings.TrimSpace(pane[i-1]) == "" {
				t.Errorf("at %d two blank rows stand together in the peek at row %d:\n%s", width, i, strings.Join(pane[first:end+1], "\n"))
			}
		}
	}
}

// (c) NO TWO BLANK ROWS STAND TOGETHER in the rows' content, and (d) no row is
// wider than the frame; and the vertical rhythm: the handover on row 0, a
// blank, the repo line, a blank, the first heading, with the peek's title
// level with it.
func TestFactoryAlignRhythm(t *testing.T) {
	for _, width := range factoryAlignWidths {
		_, body := factoryAlignLab(t, width)
		for i, row := range body {
			if w := ansi.StringWidth(row); w > width {
				t.Errorf("at %d row %d is %d cells", width, i, w)
			}
		}
		left := func(i int) string { l, _, _ := factorySplitAt(body[i]); return strings.TrimSpace(l) }
		if !strings.HasPrefix(left(0), "◆") || left(1) != "" || left(2) != "all repos" || left(3) != "" || !strings.HasPrefix(left(4), "NEEDS YOU") {
			t.Errorf("at %d the floor's first rows are not handover, blank, repos, blank, heading:\n%s", width, strings.Join(body[:6], "\n"))
		}
		if _, right, div := factorySplitAt(body[4]); div >= 0 && !strings.HasPrefix(strings.TrimSpace(right), "#1538") {
			t.Errorf("at %d the peek's title is not level with the first heading: %q", width, body[4])
		}
		last := 0
		for i := range body {
			if left(i) != "" {
				last = i
			}
		}
		for i := 1; i <= last; i++ {
			if left(i) == "" && left(i-1) == "" {
				t.Errorf("at %d two blank rows stand together in the rows at %d", width, i)
			}
		}
	}
}

// (f) THE ITEM PAGE'S RAIL AND PANE SHARE THEIR TOP ROW, row 3: the crumbs on
// row 0, the chips on row 1, a blank between.
func TestFactoryAlignItemPage(t *testing.T) {
	for _, width := range factoryAlignWidths {
		a, _ := factoryAlignLab(t, width)
		for _, id := range []int{1, 2, 9, 10} {
			factoryOn(t, a, id)
			drive(t, a, key("enter"))
			body := factoryBodyPlain(a, width, 40)
			if !strings.HasPrefix(strings.TrimSpace(body[0]), "Factory ›") || !strings.HasPrefix(strings.TrimSpace(body[1]), "gate") || strings.TrimSpace(body[2]) != "" {
				t.Errorf("at %d item %d's head is not crumbs, chips, blank:\n%s", width, id, strings.Join(body[:4], "\n"))
			}
			left, right, div := factorySplitAt(body[3])
			if div < 0 || strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
				t.Errorf("at %d item %d's rail and pane do not both start on row 3: %q", width, id, body[3])
			}
			if x := factoryFirstInk(right); div >= 0 && x != factoryMargin {
				t.Errorf("at %d item %d's pane starts %d past the rule, not %d", width, id, x, factoryMargin)
			}
			drive(t, a, key("esc"))
		}
	}
}

// (g) THE CHIPS' VALUES START AT ONE CELL across the fixture's items, on the
// peek and on the item page.
func TestFactoryAlignChips(t *testing.T) {
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	for _, keys := range []bool{false, true} {
		at := map[string]int{}
		for _, id := range []int{1, 2, 4, 9} {
			row := ansi.Strip(a.factoryChipRow(*factoryPaneItem(t, a, id), keys, 120))
			for _, label := range []string{"gate", "cap", "effort"} {
				x := strings.Index(row, label+"  ")
				if x < 0 {
					t.Fatalf("item %d's chips have no %s: %q", id, label, row)
				}
				if was, ok := at[label]; ok && was != x {
					t.Errorf("keys=%v: item %d's %s stands at %d, not %d: %q", keys, id, label, x, was, row)
				}
				at[label] = x
			}
		}
	}
}
