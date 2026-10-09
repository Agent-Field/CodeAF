package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
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
// peek's, and the divider's cell (-1 for a row with none). On an item page
// wide enough for the verbs on the right, the right column stops at the
// second rule: it is the pane, and [factoryVerbsAt] is the column past it.
func factorySplitAt(row string) (left, right string, div int) {
	r := []rune(row)
	for i, c := range r {
		if string(c) == "│" {
			right := string(r[i+1:])
			if pane, _, ok := strings.Cut(right, "│"); ok {
				right = pane
			}
			return string(r[:i]), right, i
		}
	}
	return row, "", -1
}

// factoryVerbsAt is the verbs' column of an item page's body row, past its
// second rule, and false for a row with no second rule.
func factoryVerbsAt(row string) (string, bool) {
	_, rest, ok := strings.Cut(row, "│")
	if !ok {
		return "", false
	}
	_, verbs, ok := strings.Cut(rest, "│")
	return verbs, ok
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
		railW := a.fp.rowsW - factoryMargins
		if a.fp.rowsW < width {
			railW = a.fp.rowsW - factoryMargin - (factoryDividerW - factoryRuleW)
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
			if g.factsN >= 0 && (r[factsX-1] != ' ' || r[factsX] == ' ') {
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
			// A PARKED ITEM'S SECOND ROW IS ITS QUESTION, where every other
			// item's chips stand; with the verbs on the right the chips are
			// the column's, and the second row draws none.
			it, _ := a.factoryCursorItem()
			second := wordBudget
			switch {
			case it.State == factory.StateNeedsYou:
				second = "?"
			case a.factoryVerbsDrawn():
				second = ""
			}
			if !strings.HasPrefix(strings.TrimSpace(body[0]), "Factory ›") || !strings.HasPrefix(strings.TrimSpace(body[1]), second) || strings.TrimSpace(body[2]) != "" {
				t.Errorf("at %d item %d's head is not crumbs, chips, blank:\n%s", width, id, strings.Join(body[:4], "\n"))
			}
			if a.factoryVerbsDrawn() && it.State != factory.StateNeedsYou && strings.Contains(body[1], wordBudget) {
				t.Errorf("at %d item %d's second row keeps the chips beside the verbs: %q", width, id, body[1])
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
			for _, label := range []string{wordBudget, wordThinking} {
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

// (h) THE LOG'S TIMES STAND AT THE PANE'S MARGIN AND ITS MARKS ONE STAMP
// LATER on every line, and (i) THE PROOF SHEET'S MEDIUM CHIPS START IN ONE
// COLUMN down the sheet, at 160, 120 and 100.
func TestFactoryAlignLogAndSheet(t *testing.T) {
	for _, width := range factoryAlignWidths {
		f := &factoryFake{}
		a := factoryVerbLab(t, f)
		a.pal = newPalette(tokens.NoColor, false)
		a.width = width
		factoryOn(t, a, 2)
		drive(t, a, key("enter"))
		factoryRowNamed(t, a, "log")
		lines := 0
		for _, row := range factoryBodyPlain(a, width, 30)[3:] {
			_, right, div := factorySplitAt(row)
			if div < 0 || !strings.Contains(right, ":") || strings.Contains(right, " · enter") {
				continue
			}
			r := []rune(right)
			if x := factoryFirstInk(right); x != factoryMargin || r[factoryMargin+factoryStampW-1] != ' ' || r[factoryMargin+factoryStampW] == ' ' {
				t.Errorf("at %d a log line's time or mark is out of its column: %q", width, right)
			}
			lines++
		}
		if lines == 0 {
			t.Fatalf("at %d the log pane drew no line", width)
		}
		drive(t, a, key("esc"))

		factoryOn(t, a, 9)
		drive(t, a, key("enter"))
		col := -1
		for _, row := range factoryBodyPlain(a, width, 30)[3:] {
			_, right, div := factorySplitAt(row)
			if div < 0 {
				continue
			}
			for _, medium := range []string{"test", "policy", "screenshot"} {
				trimmed := strings.TrimRight(right, " ")
				if !strings.HasSuffix(trimmed, " "+medium) {
					continue
				}
				x := len([]rune(trimmed)) - len([]rune(medium))
				if col < 0 {
					col = x
				} else if x != col {
					t.Errorf("at %d a medium chip starts at %d, not %d: %q", width, x, col, right)
				}
			}
		}
		if col < 0 {
			t.Fatalf("at %d the sheet drew no medium chip", width)
		}
	}
}

// ── THE REVIEW'S FIXES, EACH AS THE FRAME IT DRAWS ──────────────────────────
//
// A fresh-eyes review of the floor at 160, 120 and 100 columns found a dozen
// places where the frame was not what the grid meant (2026-10-08). Each test
// below renders the case it found and asserts the frame.

// factoryPeekTitleRow is the body row the peek's first words stand on, and
// how many of the peek's rows carry words, -1 and 0 with no peek.
func factoryPeekTitleRow(body []string) (int, int) {
	at, inked := -1, 0
	for i, row := range body {
		_, right, div := factorySplitAt(row)
		if div < 0 || strings.TrimSpace(right) == "" {
			continue
		}
		if at < 0 {
			at = i
		}
		inked++
	}
	return at, inked
}

// (1) THE PEEK STANDS ON ONE FIXED ROW: walked to the foot of a long list, its
// title is on the row it stood on at the top, and it keeps its height.
func TestFactoryAlignPeekKeepsItsRowWithTheCursorLow(t *testing.T) {
	a := factoryBigLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	a.width, a.height = 160, 50
	body := factoryBodyPlain(a, 160, 44)
	topAt, topInk := factoryPeekTitleRow(body)
	if topAt < 0 || topInk < 6 {
		t.Fatalf("at the top the peek is not drawn whole:\n%s", strings.Join(body, "\n"))
	}
	a.fp.cursor = len(a.factoryWalkNow()) - 1
	body = factoryBodyPlain(a, 160, 44)
	if a.fp.top == 0 {
		t.Fatal("the cursor at the foot did not scroll the rows")
	}
	at, inked := factoryPeekTitleRow(body)
	if at != topAt {
		t.Errorf("with the cursor low the peek's title is on row %d, not %d:\n%s", at, topAt, strings.Join(body, "\n"))
	}
	if inked < 6 {
		t.Errorf("with the cursor low the peek collapsed to %d rows:\n%s", inked, strings.Join(body, "\n"))
	}
	if left, _, _ := factorySplitAt(body[at]); !strings.Contains(left, "───") && strings.TrimSpace(left) != "" {
		isRow := false
		for _, it := range a.fp.snap.Items {
			if factoryRowOf([]string{left}, it.Ref()) != "" {
				isRow = true
				break
			}
		}
		if !isRow {
			t.Errorf("the rows beside the peek's title are not rows: %q", left)
		}
	}
}

// (2) THE LIST'S TOP COMES BACK: under a repo filter, walked down and back up
// to the first row, the window is at its top and the first heading shows.
func TestFactoryAlignListTopComesBackUnderAFilter(t *testing.T) {
	a := factoryBigLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	a.width, a.height = 160, 50
	drive(t, a, key("]"))
	n := len(a.factoryWalkNow())
	if n < 30 {
		t.Fatalf("the filtered floor holds %d items, too few to scroll", n)
	}
	for i := 0; i < n; i++ {
		drive(t, a, key("down"))
		factoryBodyPlain(a, 160, 44)
	}
	for i := 0; i < n; i++ {
		drive(t, a, key("up"))
		factoryBodyPlain(a, 160, 44)
	}
	if a.fp.cursor != 0 {
		t.Fatalf("the cursor is on %d, not the first row", a.fp.cursor)
	}
	body := factoryBodyPlain(a, 160, 44)
	if a.fp.top != a.fp.pinned {
		t.Errorf("with the cursor on the first row the window starts at %d, not at its top (%d)", a.fp.top, a.fp.pinned)
	}
	heading := a.fp.headRows + a.factoryPeekTop()
	if left, _, _ := factorySplitAt(body[heading]); !strings.Contains(left, "───") {
		t.Errorf("the first heading is not on row %d:\n%s", heading, strings.Join(body[:heading+2], "\n"))
	}
}

// (7) NO ORPHAN HEADING: at every height, with the cursor on the first row,
// the rows' last row is never a heading whose rows are under the edge; and
// walking onto a section's first row brings its heading with it.
func TestFactoryAlignNoOrphanHeading(t *testing.T) {
	a, _ := factoryAlignLab(t, 160)
	isHeading := func(s string) bool { return strings.Contains(s, "───") }
	for room := 8; room < 30; room++ {
		a.fp.cursor, a.fp.top = 0, 0
		body := factoryBodyPlain(a, 160, room)
		last := ""
		for _, row := range body {
			if l, _, _ := factorySplitAt(row); strings.TrimSpace(l) != "" {
				last = l
			}
		}
		if isHeading(last) {
			t.Errorf("at %d rows a heading is the rows' last row, its rows off screen:\n%s", room, strings.Join(body, "\n"))
		}
	}
	// Walk down a short window: every time the cursor stands on a section's
	// first row, the row above it is that section's heading.
	a.fp.cursor, a.fp.top = 0, 0
	rows := a.factoryRows()
	for walk := 0; walk < len(a.factoryWalkNow()); walk++ {
		a.fp.cursor = walk
		body := factoryBodyPlain(a, 160, 10)
		for line, r := range rows {
			if r.kind != factoryRowItem || r.walk != walk || line == 0 || rows[line-1].kind != factoryRowHeading {
				continue
			}
			at := a.fp.headRows + a.fp.pinned + line - a.fp.top
			if l, _, _ := factorySplitAt(body[at-1]); !isHeading(l) {
				t.Errorf("on a section's first row (walk %d) its heading is not above it:\n%s", walk, strings.Join(body, "\n"))
			}
		}
	}
}

// (3) EVERY CELL OF ONE STRIP IS ONE WIDTH, the first as wide as the rest; a
// name longer than the cap ends in an ellipsis, and a strip that lost cells
// says so with one.
func TestFactoryAlignStripCellsShareOneWidth(t *testing.T) {
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	more := a.icon(tokens.GEllipsis)
	it := factory.Item{ID: 99, Stream: &factory.Stream{Phases: []factory.Phase{
		{Name: "read", State: factory.PhaseDone},
		{Name: "checks", State: factory.PhaseDone},
		{Name: "screenshot when the page moves", State: factory.PhaseRunning, Round: 1},
		{Name: "review", State: factory.PhasePending},
		{Name: "proof", State: factory.PhasePending},
	}}}
	strip := a.factoryPeekStrip(it, 200)
	r := []rune(strip)
	var marks []int
	for i, c := range r {
		if i == 0 || r[i-1] == ' ' && i+1 < len(r) && r[i+1] == ' ' && c != ' ' && (i < 2 || r[i-2] == ' ') {
			marks = append(marks, i)
		}
	}
	if len(marks) != 5 {
		t.Fatalf("the strip's marks are at %v: %q", marks, strip)
	}
	step := marks[1] - marks[0]
	if want := factoryStripCellMax + factoryStripGap; step != want {
		t.Errorf("the first cell is %d wide with its gap, not %d: %q", step, want, strip)
	}
	for i := 2; i < len(marks); i++ {
		if marks[i]-marks[i-1] != step {
			t.Errorf("cell %d steps %d, not %d like the first: %q", i, marks[i]-marks[i-1], step, strip)
		}
	}
	if !strings.Contains(strip, more) {
		t.Errorf("the cut name carries no ellipsis: %q", strip)
	}
	narrow := a.factoryPeekStrip(it, 40)
	if !strings.HasSuffix(narrow, more) || ansi.StringWidth(narrow) > 40 {
		t.Errorf("a strip that dropped cells does not end in an ellipsis, or is too wide: %q", narrow)
	}
	if !strings.Contains(narrow, "screenshot") {
		t.Errorf("the narrow strip lost its running cell: %q", narrow)
	}
	// AND THE ITEM PAGE'S STAGE RAIL cuts a long name with the ellipsis.
	cells := []factoryRailCell{{mark: "○ ", markPaint: a.pal.dim, rest: "screenshot when the page moves", paint: a.pal.dim}}
	rail, _ := a.factoryCellRail(cells, 0, 1)
	if got := strings.TrimRight(ansi.Strip(rail[0]), " "); !strings.HasSuffix(got, more) || ansi.StringWidth(rail[0]) != factoryRailW {
		t.Errorf("the rail's long name is %q, not cut with an ellipsis in %d cells", got, factoryRailW)
	}
}

// (4) THE PROOF SHEET'S EVIDENCE NEVER REPEATS ITS MEDIUM, and the medium is a
// column of its own at the right.
func TestFactoryAlignProofSheetEvidenceAndMedium(t *testing.T) {
	for _, width := range factoryAlignWidths {
		f := &factoryFake{}
		a := factoryVerbLab(t, f)
		a.pal = newPalette(tokens.NoColor, false)
		a.width = width
		factoryOn(t, a, 9)
		drive(t, a, key("enter"))
		seen := 0
		for _, row := range factoryBodyPlain(a, width, 30)[3:] {
			_, right, div := factorySplitAt(row)
			if div < 0 {
				continue
			}
			for _, medium := range []string{"test", "screenshot"} {
				if strings.HasSuffix(strings.TrimRight(right, " "), " "+medium) {
					seen++
					if strings.Contains(right, medium+" · ") || strings.Count(right, medium) > 1 {
						t.Errorf("at %d the evidence repeats the medium %q: %q", width, medium, right)
					}
				}
			}
		}
		if seen == 0 {
			t.Fatalf("at %d the sheet drew no test or screenshot row", width)
		}
	}
	if got := factoryEvidence("test · 0.3s", "test"); got != "0.3s" {
		t.Errorf("`test · 0.3s` under `test` is %q, want 0.3s", got)
	}
	if got := factoryEvidence("screenshot", "screenshot"); got != "" {
		t.Errorf("evidence that is only the medium is %q, want none", got)
	}
}

// (5) THE RIGHT MARGIN: at 160, 120 and 100 the rows, the peek and the item
// page stop [factoryMargin] before the frame's edge. The handover's hairline
// is the one row drawn to the edge.
func TestFactoryAlignRightMarginAtEveryWidth(t *testing.T) {
	inkEnd := func(s string) int { return len([]rune(strings.TrimRight(s, " "))) }
	for _, width := range factoryAlignWidths {
		a, body := factoryAlignLab(t, width)
		for i, row := range body[a.fp.headRows:] {
			if end := inkEnd(row); end > width-factoryMargin {
				t.Errorf("at %d floor row %d ends at %d, past %d:\n%q", width, i, end, width-factoryMargin, row)
			}
		}
		for _, id := range []int{1, 2, 9} {
			factoryOn(t, a, id)
			drive(t, a, key("enter"))
			for i, row := range factoryBodyPlain(a, width, 40) {
				if end := inkEnd(row); end > width-factoryMargin {
					t.Errorf("at %d item %d's page row %d ends at %d, past %d:\n%q", width, id, i, end, width-factoryMargin, row)
				}
			}
			drive(t, a, key("esc"))
		}
	}
}

// (6) KEYS LAST: a box opened on the peek stands above its key line, which
// stays the column's last row, one blank between them.
func TestFactoryAlignTextBoxOpensAboveTheKeys(t *testing.T) {
	for _, width := range factoryAlignWidths[:2] {
		f := &factoryFake{}
		a := factoryVerbLab(t, f)
		a.pal = newPalette(tokens.NoColor, false)
		a.width = width
		factoryOn(t, a, 1)
		drive(t, a, key("a"))
		var pane []string
		for _, row := range factoryBodyPlain(a, width, 36) {
			if _, right, div := factorySplitAt(row); div >= 0 {
				pane = append(pane, strings.TrimSpace(right))
			}
		}
		last := len(pane) - 1
		if !strings.HasPrefix(pane[last], "enter open") {
			t.Errorf("at %d the key line is not the last row:\n%s", width, strings.Join(pane, "\n"))
			continue
		}
		if pane[last-1] != "" || !strings.Contains(pane[last-2], "›") {
			t.Errorf("at %d the box does not stand above the keys with one blank between:\n%s", width, strings.Join(pane[last-3:], "\n"))
		}
	}
}

// (8) LANDED SITS BESIDE NEEDS YOU, above what runs and the backlog.
func TestFactoryAlignLandedSitsBesideNeedsYou(t *testing.T) {
	var words []string
	for _, g := range factoryGroups {
		words = append(words, g.word)
	}
	if got := strings.Join(words, ","); got != "needs you,landed,streams,new,shipped" {
		t.Fatalf("the sections stand %q", got)
	}
}

// (9) THE TITLE KEEPS ITS WIDTH: at every width a row with facts gives the
// title at least [factoryTitleMinW], the facts drop as whole columns (every
// row of the floor carries the same count of them, or all it has), and the
// comfortable density's second line is as wide as its first.
func TestFactoryAlignTitleKeepsItsWidthAndFactsDropAsAColumn(t *testing.T) {
	for name, lab := range map[string]func(*testing.T) *app{"fixture": factoryPlaceLab} {
		a := lab(t)
		a.pal = newPalette(tokens.NoColor, false)
		a.fp.columns = true
		for width := 70; width <= 200; width++ {
			a.factoryGridForget()
			g := a.factoryGridAt(width)
			if g.factsN >= 0 && g.titleW < factoryTitleMinW {
				t.Fatalf("%s at %d: the title is %d cells while the row keeps %d fact columns", name, width, g.titleW, g.factsN)
			}
			for _, it := range a.fp.snap.Items {
				if !factoryOnFloor(it) || it.State == factory.StateNeedsYou {
					continue
				}
				parts := a.factoryRowFacts(it)
				k := len(parts)
				switch {
				case g.factsN < 0:
					k = 0
				case g.factsN > 0:
					k = min(g.factsN, k)
				}
				var plains []string
				for _, p := range parts[:k] {
					plains = append(plains, p.plain)
				}
				want := strings.Join(plains, rowSep)
				got := ansi.Strip(a.factoryFactsLine(it, g.factsW, g.factsN))
				if got != want && !(k == 1 && ansi.StringWidth(want) > g.factsW) {
					t.Fatalf("%s at %d: %s carries %q, not its first %d facts %q", name, width, it.Ref(), got, k, want)
				}
			}
		}
	}
	// AT 120 THE TITLE IS NEVER CUT TO 18 while the facts keep their width.
	a, body := factoryAlignLab(t, 120)
	g := a.factoryGridAt(a.fp.rowsW - factoryMargin - (factoryDividerW - factoryRuleW))
	if g.titleW < factoryTitleMinW {
		t.Errorf("at 120 the title column is %d cells:\n%s", g.titleW, strings.Join(body, "\n"))
	}
	// The comfortable second line wraps at the first line's width.
	a.fp.comfy = true
	a.factoryGridForget()
	comfy := factoryBodyPlain(a, 120, 40)
	if a.fp.titleCols != a.factoryGridAt(a.fp.rowsW-factoryMargin-(factoryDividerW-factoryRuleW)).titleW {
		t.Errorf("the comfortable title wraps at %d, not the first line's width", a.fp.titleCols)
	}
	_ = comfy
}

// (10) THE PEEK'S STRIP SHEDS FROM ITS RIGHT: at 120 and at 160 the strip on
// a new item's peek is a whole-clause prefix of its five verbs, `enter open`
// first, so a narrow peek loses `t ask me at …` before `r run`.
func TestFactoryAlignStripShedsFromTheRight(t *testing.T) {
	for _, width := range []int{120, 160} {
		a := factoryBigLab(t)
		a.width, a.height = width, 40
		factoryOn(t, a, 8)
		it, _ := a.factoryCursorItem()
		if it.State != factory.StateNew {
			t.Fatalf("item 8 is %s, not new", it.State)
		}
		full := strings.Join(a.factoryVerbRail(it), rowSep)
		strip := ""
		for _, line := range factoryFrameLines(a) {
			if at := strings.Index(line, "enter open"); at >= 0 {
				strip = strings.TrimSpace(strings.TrimRight(line[at:], " │"))
			}
		}
		if strip == "" || !strings.HasPrefix(full, strip) {
			t.Errorf("at %d the peek's strip %q is not a prefix of %q", width, strip, full)
		}
	}
}

// (11) `U` SPINS ONE ROW: only an item being read now draws the spinner in its
// priority cell, never one waiting its turn.
func TestFactoryAlignOnlyTheReadRowSpins(t *testing.T) {
	a := factoryPlaceLab(t)
	a.fp.snap.Busy = map[int]string{1: "refreshing", 2: "queued"}
	spin := a.factorySpin()
	if cell := a.factoryPrioCell(*factoryPaneItem(t, a, 1)); !strings.Contains(cell, spin) {
		t.Errorf("the row being read does not spin: %q", cell)
	}
	if cell := a.factoryPrioCell(*factoryPaneItem(t, a, 2)); strings.Contains(cell, spin) {
		t.Errorf("a queued row spins: %q", cell)
	}
}

// (13) AN ESTIMATE WEARS THE FACTS' MUTED TONE; money's green is for spend.
func TestFactoryAlignEstimateIsMutedAndSpendIsMoney(t *testing.T) {
	a := factoryPlaceLab(t)
	for _, it := range a.fp.snap.Items {
		money := factoryMoneyFact(it)
		if money == "" {
			continue
		}
		for _, p := range a.factoryRowFacts(it) {
			if p.plain != money {
				continue
			}
			want := placeMoneyInk(a.pal)(money)
			if strings.HasPrefix(money, "~") {
				want = a.pal.muted(money)
			}
			if p.painted != want {
				t.Errorf("%s's %q is painted %q, want %q", it.Ref(), money, p.painted, want)
			}
		}
	}
	if a.pal.muted("~$2") == placeMoneyInk(a.pal)("~$2") {
		t.Skip("the palette paints muted and money alike")
	}
}

// (14) AN EMPTY FLOOR DRAWS NO DIVIDER: with nothing to show in a peek there is
// no `│` and no column of air beside the rows.
func TestFactoryAlignEmptyFloorHasNoDivider(t *testing.T) {
	f := &factoryFake{shape: func(s *factory.Snapshot) { s.Items, s.Repos = nil, nil }}
	a := factoryVerbLab(t, f)
	a.pal = newPalette(tokens.NoColor, false)
	for _, width := range factoryAlignWidths {
		a.width = width
		body := factoryBodyPlain(a, width, 30)
		for i, row := range body {
			if strings.Contains(row, "│") {
				t.Fatalf("at %d an empty floor draws the divider on row %d: %q", width, i, row)
			}
		}
		if a.fp.rowsW != width {
			t.Errorf("at %d an empty floor's rows are %d wide, not the whole width", width, a.fp.rowsW)
		}
	}
}

// (7) THE REPO PICKER STANDS ON ITS OWN NAMED GRID at 80, 120 and 150: every
// heading and mark at the margin, every name at one cell past the mark, every
// fact column ending at one cell on every row that has it, the last ending
// [factoryMargin] before the frame's edge.
func TestFactoryAlignRepoPicker(t *testing.T) {
	for _, width := range []int{80, 120, 150} {
		a := factorySettingsLab(t, newPickerFake(), width)
		a.pal = newPalette(tokens.NoColor, false)
		drive(t, a, key("R"))
		body := factoryExactBody(t, a, width, 30)
		t.Logf("picker at %d:\n%s", width, strings.Join(body, "\n"))
		ends := map[string]int{}
		rows := 0
		for _, row := range body {
			r := []rune(row)
			if !strings.Contains(row, "[x]") && !strings.Contains(row, "[ ]") {
				if strings.Contains(row, " ─") && factoryFirstInk(row) != factoryMargin {
					t.Errorf("at %d a heading starts at %d, not %d:\n%q", width, factoryFirstInk(row), factoryMargin, row)
				}
				continue
			}
			rows++
			if factoryFirstInk(row) != factoryMargin {
				t.Errorf("at %d a mark starts at %d:\n%q", width, factoryFirstInk(row), row)
			}
			if at := factoryMargin + factoryPickMarkW; len(r) <= at || r[at-1] != ' ' || r[at] == ' ' {
				t.Errorf("at %d a name does not start at %d:\n%q", width, at, row)
			}
			if end := len(r); end > width-factoryMargin {
				t.Errorf("at %d a row ends at %d, past %d:\n%q", width, end, width-factoryMargin, row)
			}
			for _, fact := range []string{" open", "private", "here", "pushed "} {
				i := strings.LastIndex(row, fact)
				if i < 0 {
					continue
				}
				end := len([]rune(row[:i])) + len([]rune(fact))
				if fact == "pushed " {
					end = len(r)
				}
				if was, ok := ends[fact]; ok && was != end {
					t.Errorf("at %d %q ends at %d, not %d:\n%q", width, fact, end, was, row)
				}
				ends[fact] = end
			}
		}
		if rows == 0 {
			t.Fatalf("at %d no picker row was found", width)
		}
		if end, ok := ends["pushed "]; ok && end != width-factoryMargin {
			t.Errorf("at %d the last column ends at %d, not %d", width, end, width-factoryMargin)
		}
	}
}
