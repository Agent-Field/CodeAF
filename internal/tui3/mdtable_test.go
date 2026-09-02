package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Openable tables (mdtable.go).
//
// The load-bearing assertion in this file is the FIRST one: a closed table is
// prose's own bytes and one row this surface added, and everything else in the
// document is untouched. Every other case here is a layout question and can be
// argued about; that one is the contract markdownwrap_test.go's
// TestWideTiersUnchanged is written against, and the whole splice architecture
// exists to keep it true.

// The table used almost everywhere below: three columns, two of them far wider
// than any frame this surface draws, and prose above and below it so the splice
// has a document to be careful inside of.
const tblCut = "Here is what landed.\n\n" +
	"| Task | Model | Notes |\n" +
	"| --- | --- | --- |\n" +
	"| refactor the parser into something a person can read | opus | it needed doing and it is done now |\n" +
	"| write the tests | sonnet | green on the first run |\n\n" +
	"And that is the whole of it.\n"

// tblFits is the same shape at a size no frame has to cut.
const tblFits = "Before.\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |\n\nAfter.\n"

// tableLab is a surface with one settled answer on it, wide enough that the
// window holds the whole transcript.
func tableLab(t *testing.T, said string) (*app, *entry) {
	t.Helper()
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 80, 60
	a.entries = append(a.entries, entry{kind: entryAssistant, text: said, settled: true})
	a.touch()
	return a, &a.entries[len(a.entries)-1]
}

// footAt is the index of the row carrying an affordance, and what it says.
func footAt(rows []string) (int, string) {
	for i, row := range rows {
		switch plain(row) {
		case mdOpenWord, mdTuckWord:
			return i, plain(row)
		}
	}
	return -1, ""
}

// footRow is the first row on the FRAME that carries a foot, and the screen row
// it landed on — resolved exactly as [app.rowAt] does, so a click built from it
// is the click a person makes.
func footRow(a *app) (row, int, bool) {
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	for i, r := range body {
		if r.foot.span.pressable() {
			return r, a.bodyTop() + i, true
		}
	}
	return row{}, 0, false
}

// A CLOSED TABLE IS PROSE'S OWN BYTES AND ONE MORE ROW. Nothing else in the
// document moves, and the table itself is copied rather than redrawn.
func TestAClosedTableIsProsesOwnBytesAndOneMoreRow(t *testing.T) {
	a, e := tableLab(t, tblCut)
	// Every width under the table's natural size — which is where the cutting
	// happens, and so where the door belongs.
	for _, width := range []int{60, 61, 72, 80, 90} {
		want := trimBlanks(renderMarkdownWith(a.styler(), e.text, width))
		got := a.settledMarkdown(0, e, width)
		at, word := footAt(got)
		if at < 0 {
			t.Fatalf("at %d cells a cut table grew no foot:\n%s", width, strings.Join(got, "\n"))
		}
		if word != mdOpenWord {
			t.Fatalf("at %d cells a closed table says %q", width, word)
		}
		rest := append(append([]string{}, got[:at]...), got[at+1:]...)
		if strings.Join(rest, "\n") != strings.Join(want, "\n") {
			t.Fatalf("at %d cells the closed render is no longer prose's:\n%s\n---\n%s",
				width, strings.Join(rest, "\n"), strings.Join(want, "\n"))
		}
		// The foot sits directly under the table's last row, not at the end of the
		// document: it is the table's foot and not the answer's.
		if at == len(got)-1 {
			t.Fatalf("at %d cells the foot drifted to the end of the answer:\n%s",
				width, strings.Join(got, "\n"))
		}
		if e.feet[at].table != 0 || !e.feet[at].span.pressable() {
			t.Fatalf("at %d cells the foot recorded no columns: %+v", width, e.feet)
		}
	}
}

// AN AFFORDANCE FOR A PROBLEM THAT DID NOT HAPPEN IS NOISE. A table that fits
// renders exactly as it always has, and there is no row under it at all.
func TestATableThatFitsGrowsNoFoot(t *testing.T) {
	for _, src := range []string{tblFits, tblCut} {
		a, e := tableLab(t, src)
		// 200 cells is wider than either table's natural size, so neither of them
		// was cut and neither has anything to offer.
		for _, width := range []int{120, 200} {
			want := trimBlanks(renderMarkdownWith(a.styler(), e.text, width))
			got := a.settledMarkdown(0, e, width)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("at %d cells a fitting table was touched:\n%s\n---\n%s",
					width, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			if len(e.feet) != 0 {
				t.Fatalf("at %d cells a fitting table grew %d feet", width, len(e.feet))
			}
		}
	}
	a, e := tableLab(t, tblFits)
	for _, width := range []int{60, 80, 120, 200} {
		want := trimBlanks(renderMarkdownWith(a.styler(), e.text, width))
		got := a.settledMarkdown(0, e, width)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("at %d cells a fitting table was touched:\n%s\n---\n%s",
				width, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		if len(e.feet) != 0 {
			t.Fatalf("at %d cells a fitting table grew %d feet", width, len(e.feet))
		}
	}
}

// AN OPENED TABLE IS THE SAME GRID WITH ITS CELLS WRAPPED: every word the model
// wrote is on screen, no row is over the column, and the header keeps its
// hairline.
func TestAnOpenedTableShowsEveryCell(t *testing.T) {
	a, e := tableLab(t, tblCut)
	e.tables = map[int]bool{0: true}
	for _, width := range []int{72, 80, 100, 120} {
		rows := a.settledMarkdown(0, e, width)
		joined := strings.Join(rows, "\n")
		flat := plain(joined)
		if strings.Contains(flat, glyphMore+" ") && !strings.Contains(flat, mdTuckWord) {
			t.Fatalf("at %d cells an opened table still carries a cut:\n%s", width, flat)
		}
		// EVERY WORD OF EVERY CELL IS ON SCREEN. The wrap puts them on rows of
		// their own and the padding of the columns beside them stands in between,
		// so what is asserted is the words and not the phrases they came in.
		for _, word := range strings.Fields(strings.ReplaceAll(
			strings.ReplaceAll(tblCut, "|", " "), "-", " ")) {
			if !strings.Contains(flat, word) {
				t.Fatalf("at %d cells an opened table lost %q:\n%s", width, word, flat)
			}
		}
		if !strings.Contains(flat, strings.Repeat(tokens.GlyphTreeDash, 8)) {
			t.Fatalf("at %d cells an opened table lost its header hairline:\n%s", width, flat)
		}
		if _, word := footAt(rows); word != mdTuckWord {
			t.Fatalf("at %d cells an opened table does not offer the way back: %q", width, word)
		}
		for _, row := range rows {
			if w := ansi.StringWidth(row); w > width {
				t.Fatalf("at %d cells an opened row is %d cells: %q", width, w, row)
			}
			if strings.ContainsAny(row, "\n\r") {
				t.Fatalf("an opened row carries a newline: %q", row)
			}
		}
	}
}

// A RECORD THAT SPANS ROWS IS SEPARATED FROM THE NEXT, and one that does not is
// left alone: a blank line between single rows would be spacing a problem
// nobody has.
func TestAnOpenedTableSeparatesRecordsThatSpanRows(t *testing.T) {
	a, e := tableLab(t, tblCut)
	e.tables = map[int]bool{0: true}
	rows := a.settledMarkdown(0, e, 80)
	from, to := -1, -1
	for i, row := range rows {
		flat := plain(row)
		if strings.HasPrefix(flat, "refactor") {
			from = i
		}
		if strings.HasPrefix(flat, "write the tests") {
			to = i
		}
	}
	if from < 0 || to < 0 || to <= from {
		t.Fatalf("the two records are not where they should be (%d/%d):\n%s", from, to, strings.Join(rows, "\n"))
	}
	if to-from < 3 {
		t.Fatalf("the first record did not wrap at all:\n%s", strings.Join(rows, "\n"))
	}
	if strings.TrimSpace(plain(rows[to-1])) != "" {
		t.Fatalf("two wrapped records were left wedged together:\n%s", strings.Join(rows, "\n"))
	}
}

// A COLUMN OF FIGURES STAYS A COLUMN OF FIGURES. Alignment is what the source
// asked for, and a table that lost it on the way open would be a different
// table.
func TestAnOpenedTableKeepsTheAlignmentTheSourceAsked(t *testing.T) {
	const src = "| Task | Cost |\n| --- | ---: |\n" +
		"| refactor the parser into something a person can read at all | 1.20 |\n" +
		"| write the tests and then some more of them for good measure | 12.05 |\n"
	a, e := tableLab(t, src)
	e.tables = map[int]bool{0: true}
	rows := a.settledMarkdown(0, e, 72)
	var ends []int
	for _, row := range rows {
		flat := strings.TrimRight(plain(row), " ")
		if strings.HasSuffix(flat, "1.20") || strings.HasSuffix(flat, "12.05") {
			ends = append(ends, ansi.StringWidth(flat))
		}
	}
	if len(ends) != 2 || ends[0] != ends[1] {
		t.Fatalf("a right-aligned column lost its shared edge (%v):\n%s", ends, plain(strings.Join(rows, "\n")))
	}
}

// A TABLE TOO WIDE EVEN FOR WRAPPING OPENS AS RECORDS: `header: value` a line
// at a time, which is what the phone tier already does with every table.
func TestAnAbsurdlyWideTableOpensAsRecords(t *testing.T) {
	var b strings.Builder
	b.WriteString("| One | Two | Three | Four | Five | Six |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	b.WriteString("| supercalifragilistic | expialidocious | antidisestablishment | floccinaucinihilipilification | pneumonoultramicroscopic | incomprehensibilities |\n")
	a, e := tableLab(t, b.String())
	e.tables = map[int]bool{0: true}
	rows := a.settledMarkdown(0, e, 72)
	flat := plain(strings.Join(rows, "\n"))
	for _, want := range []string{
		"One: supercalifragilistic",
		"Four: floccinaucinihilipilification",
		"Six: incomprehensibilities",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("a stacked record is missing %q:\n%s", want, flat)
		}
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 72 {
			t.Fatalf("a stacked row is %d cells: %q", w, row)
		}
	}
}

// TWO TABLES IN ONE ANSWER ARE TWO TABLES: the ordinals are the document's, and
// opening one leaves the other exactly as it was.
func TestTwoTablesInOneAnswerOpenIndependently(t *testing.T) {
	src := tblCut + "\nAnd another:\n\n" + strings.TrimPrefix(tblCut, "Here is what landed.\n\n")
	a, e := tableLab(t, src)
	closed := a.settledMarkdown(0, e, 80)
	if len(e.feet) != 2 {
		t.Fatalf("two cut tables grew %d feet:\n%s", len(e.feet), strings.Join(closed, "\n"))
	}
	e.tables = map[int]bool{1: true}
	rows := a.settledMarkdown(0, e, 80)
	words := []string{}
	for _, row := range rows {
		if plain(row) == mdOpenWord || plain(row) == mdTuckWord {
			words = append(words, plain(row))
		}
	}
	if len(words) != 2 || words[0] != mdOpenWord || words[1] != mdTuckWord {
		t.Fatalf("opening the second table moved the first: %v", words)
	}
	// The first table is still prose's own bytes, cut and all.
	if !strings.Contains(plain(strings.Join(rows[:len(rows)/2], "\n")), glyphMore) {
		t.Fatalf("the first table stopped being cut when the second one opened:\n%s", plain(strings.Join(rows, "\n")))
	}
}

// PRESSING THE FOOT OPENS THE TABLE, AND PRESSING IT AGAIN TUCKS IT BACK — the
// whole of the gesture, on the frame, through the same click a person makes.
func TestClickingTheFootOpensAndTucksTheTable(t *testing.T) {
	a, _ := tableLab(t, tblCut)
	r, y, ok := footRow(a)
	if !ok {
		t.Fatalf("the answer grew no foot:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if plain(r.text) != mdOpenWord {
		t.Fatalf("the foot says %q", plain(r.text))
	}

	drive(t, a, tea.MouseClickMsg{X: r.foot.span.from, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: r.foot.span.from, Y: y, Button: tea.MouseLeft})
	said := strings.Join(plainRows(a), "\n")
	if !strings.Contains(said, mdTuckWord) {
		t.Fatalf("the press did not open the table:\n%s", said)
	}
	for _, word := range []string{"person", "read", "done"} {
		if !strings.Contains(said, word) {
			t.Fatalf("the opened table is still cut — no %q in it:\n%s", word, said)
		}
	}

	r, y, ok = footRow(a)
	if !ok {
		t.Fatalf("the opened table lost its way back:\n%s", said)
	}
	drive(t, a, tea.MouseClickMsg{X: r.foot.span.to - 1, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: r.foot.span.to - 1, Y: y, Button: tea.MouseLeft})
	if said := strings.Join(plainRows(a), "\n"); !strings.Contains(said, mdOpenWord) {
		t.Fatalf("the second press did not tuck the table back:\n%s", said)
	}
}

// A PRESS IN THE MARGIN BESIDE THE WORDS IS NOT A PRESS ON THE FOOT. The
// affordance is the phrase, exactly as a task reference is.
func TestAPressBesideTheFootDoesNothing(t *testing.T) {
	a, _ := tableLab(t, tblCut)
	r, y, ok := footRow(a)
	if !ok {
		t.Fatalf("the answer grew no foot:\n%s", strings.Join(plainRows(a), "\n"))
	}
	drive(t, a, tea.MouseClickMsg{X: r.foot.span.to + 4, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: r.foot.span.to + 4, Y: y, Button: tea.MouseLeft})
	if said := strings.Join(plainRows(a), "\n"); !strings.Contains(said, mdOpenWord) {
		t.Fatalf("a press in the margin opened the table anyway:\n%s", said)
	}
}

// AN OPEN TABLE STAYS OPEN ACROSS A RESIZE, and lands inside every width it is
// re-laid out in: the choice is the person's and the layout is the surface's.
func TestAnOpenTableSurvivesAResize(t *testing.T) {
	a, _ := tableLab(t, tblCut)
	r, y, ok := footRow(a)
	if !ok {
		t.Fatal("the answer grew no foot")
	}
	drive(t, a, tea.MouseClickMsg{X: r.foot.span.from, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: r.foot.span.from, Y: y, Button: tea.MouseLeft})

	for _, width := range []int{120, 100, 72, 64, 80} {
		a.width = width
		a.touch()
		rows := plainRows(a)
		joined := strings.Join(rows, "\n")
		if !strings.Contains(joined, mdTuckWord) {
			t.Fatalf("at %d cells the table closed itself:\n%s", width, joined)
		}
		for _, row := range rows {
			if w := ansi.StringWidth(row); w > width {
				t.Fatalf("at %d cells a row is %d cells: %q", width, w, row)
			}
		}
	}
}

// NO FOOT UNDER A TABLE THAT IS STILL ARRIVING. An offer to open something the
// model has not finished writing is a promise the surface cannot keep.
func TestNoFootBeforeTheAnswerSettles(t *testing.T) {
	a, e := tableLab(t, tblCut)
	e.settled, e.built, e.stale = false, false, true
	e.mdCut = len(e.text)
	a.touch()
	if r, _, ok := footRow(a); ok {
		t.Fatalf("a streaming answer grew a foot: %+v", r.foot)
	}
	if len(e.feet) != 0 {
		t.Fatalf("a streaming answer recorded %d feet", len(e.feet))
	}
}

// THE PHONE TIER IS UNTOUCHED: its tables are already stacked whole, so there
// is nothing to open and no row to spend saying so.
func TestThePhoneTierGrowsNoFoot(t *testing.T) {
	a, e := tableLab(t, tblCut)
	rows := a.settledMarkdown(0, e, phoneCols)
	if at, word := footAt(rows); at >= 0 {
		t.Fatalf("a phone-width table grew %q:\n%s", word, plain(strings.Join(rows, "\n")))
	}
	if strings.Join(rows, "\n") != strings.Join(trimBlanks(renderMarkdownWith(a.styler(), e.text, phoneCols)), "\n") {
		t.Fatal("the phone tier no longer renders what renderMarkdown renders")
	}
}

// THE FOOT ANSWERS THE POINTER like every other pressable thing on this
// surface, and it answers it in the jump chip's vocabulary rather than the
// hover band's: three words at the end of a table are not a row.
func TestTheFootBrightensUnderThePointer(t *testing.T) {
	a, _ := tableLab(t, tblCut)
	r, y, ok := footRow(a)
	if !ok {
		t.Fatal("the answer grew no foot")
	}
	cold := r.text
	drive(t, a, tea.MouseMotionMsg{X: r.foot.span.from, Y: y})
	if a.hot.kind != hoverTable || a.hot.index != 0 {
		t.Fatalf("the pointer on the foot resolved to %+v", a.hot)
	}
	warm, _, _ := footRow(a)
	if warm.text == cold {
		t.Fatalf("the foot did not answer the pointer: %q", plain(warm.text))
	}
	if plain(warm.text) != plain(cold) {
		t.Fatalf("the hover changed what the foot SAYS: %q", plain(warm.text))
	}

	// And the pointer in the margin of the same row is on nothing at all.
	drive(t, a, tea.MouseMotionMsg{X: r.foot.span.to + 4, Y: y})
	if a.hot.kind == hoverTable {
		t.Fatalf("the margin beside the foot claimed the pointer: %+v", a.hot)
	}
}
