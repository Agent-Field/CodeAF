package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// tableCatalog is a list with every shape of gap in it: a row that publishes
// everything, a free row with no price, a row with no score, and a row that
// publishes nothing but its window.
var tableCatalog = []Model{
	{
		ID: "anthropic/claude-sonnet-4.5", ContextLength: 1_000_000,
		PromptPrice: 0.000003, CompletionPrice: 0.000015,
		ArenaElo: 1243, Reasoning: true, Input: []string{"text", "image"},
	},
	{
		ID: "openai/gpt-4.1-mini", ContextLength: 128_000,
		PromptPrice: 0.00000008, CompletionPrice: 0.00000015,
	},
	{ID: "nex-agi/nex-n2.5-mini:free", ContextLength: 262_000, Input: []string{"text", "image", "audio"}},
	{ID: "moonshotai/kimi-k3"},
}

// tablePicker is that list, open, on a frame of the given width.
func tablePicker(width int) (*picker, modelTableFit) {
	p := &picker{}
	p.start(tableCatalog, "moonshotai/kimi-k3")
	return p, p.tableFit(width)
}

// tableRows is the drawn list with its colour taken off and the heading kept,
// each line's trailing blank left ON — the padding is what the alignment is
// made of, so a test that trimmed it would be testing a different string.
func tableRows(p *picker, width int) []string {
	out := []string{}
	for _, line := range p.rows(width, 8, newTestPalette(), -1, nil) {
		out = append(out, ansi.Strip(line))
	}
	return out
}

// EVERY ROW IS THE SAME WIDTH AND SO EVERY COLUMN IS IN THE SAME PLACE. This is
// the whole of what the table is for, and it is asserted as the thing a person
// would check: take the column a figure starts in on one row, and it is the
// column that figure starts in on every row.
func TestEveryTableRowPutsItsColumnsInTheSamePlace(t *testing.T) {
	for _, width := range []int{60, 80, 100, 140} {
		p, fit := tablePicker(width)
		if !fit.drawn() {
			t.Fatalf("at %d columns the list drew no table", width)
		}
		lines := tableRows(p, width)
		if len(lines) < len(tableCatalog)+1 {
			t.Fatalf("at %d columns the list drew %d lines for %d models and a heading",
				width, len(lines), len(tableCatalog))
		}
		// The block ends where the heading ends on every row. The selected row
		// is the one exception and not an exception to this: its ground is
		// painted across the whole frame, so what is asserted is that nothing
		// stands past the block's own edge.
		want := ansi.StringWidth(lines[0])
		for at, line := range lines {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("at %d columns line %d drew %d cells: %q", width, at, got, line)
			}
			if len(line) > want && strings.TrimSpace(line[want:]) != "" {
				t.Fatalf("at %d columns line %d runs past the heading's %d cells:\n%s",
					width, at, want, strings.Join(lines, "\n"))
			}
		}
		// And the figures land UNDER their heads rather than near them, which is
		// the whole claim: the cells of the window column all start in the cell
		// the word `window` starts in.
		head := lines[0]
		for _, cell := range []string{"1M", "128k", "262k"} {
			at := strings.Index(head, "window")
			if at < 0 {
				t.Fatalf("at %d columns there is no window column:\n%s", width, head)
			}
			if !columnHolds(lines, at, len("window"), cell) {
				t.Fatalf("at %d columns %q is not under the window head:\n%s",
					width, cell, strings.Join(lines, "\n"))
			}
		}
	}
}

// columnHolds reports whether some line carries text inside the cells [at,
// at+wide) and nothing of it outside them.
func columnHolds(lines []string, at, wide int, text string) bool {
	for _, line := range lines {
		if len(line) < at+wide {
			continue
		}
		if strings.TrimSpace(line[at:at+wide]) == text {
			return true
		}
	}
	return false
}

// A COLUMN NOBODY PUBLISHED IS NOT A NARROW COLUMN, IT IS NO COLUMN. Nothing on
// this list has ever been measured, so there is no machine to name, no wait to
// report and no throughput — and the table says so by not drawing three heads
// over three hundred blanks (law 4 of rowfit.go, down the page).
func TestATableDrawsNoHeadForAColumnNobodyPublished(t *testing.T) {
	_, fit := tablePicker(120)
	for _, head := range []string{"via", "first", "t/s"} {
		if strings.Contains(fit.header(), head) {
			t.Fatalf("nothing was measured, so %q has no column:\n%s", head, fit.header())
		}
	}
	for _, head := range []string{"in/M", "out/M", "window", "elo", "can"} {
		if !strings.Contains(fit.header(), head) {
			t.Fatalf("the catalog publishes %q, so the table has to draw it:\n%s", head, fit.header())
		}
	}
}

// AN EMPTY CELL IS A FACT. A row that published no price draws blank under the
// price heads — never a zero, and never a row that is simply shorter than its
// neighbours, which is what the ragged tail could not tell apart.
func TestARowWithNoPriceDrawsBlankCellsAndNotAZero(t *testing.T) {
	p, _ := tablePicker(120)
	lines := tableRows(p, 120)
	free := ""
	for _, line := range lines {
		if strings.Contains(line, "nex-n2.5-mini:free") {
			free = line
		}
	}
	if free == "" {
		t.Fatalf("the free model is not on the list:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(free, "$") {
		t.Fatalf("a row with no published price must draw no figure: %q", free)
	}
	if !strings.Contains(free, "262k") || !strings.Contains(free, "sees · hears") {
		t.Fatalf("the row lost the facts it does publish: %q", free)
	}
}

// THE COLUMNS GO FROM THE LOW-RANKED END, and they go for every row at once.
// Narrowing the frame walks back up [modelColumns] and never leaves a gap in
// the middle of what is drawn.
func TestANarrowingFrameGivesUpColumnsFromTheBottomOfTheRanking(t *testing.T) {
	seen := [][]int{}
	for width := 140; width >= 60; width-- {
		_, fit := tablePicker(width)
		if !fit.drawn() {
			continue
		}
		if len(seen) == 0 || len(seen[len(seen)-1]) != len(fit.at) {
			seen = append(seen, append([]int(nil), fit.at...))
		}
	}
	for _, at := range seen {
		for n := range at {
			if n > 0 && at[n] <= at[n-1] {
				t.Fatalf("a fit drew its columns out of rank order: %v", at)
			}
		}
	}
	for n := 1; n < len(seen); n++ {
		wide, tight := seen[n-1], seen[n]
		if len(tight) >= len(wide) {
			t.Fatalf("a narrower frame kept as many columns: %v then %v", wide, tight)
		}
		// The narrower fit is a PREFIX of the wider one: it gave up the last
		// column and promoted nothing into the space.
		for n, at := range tight {
			if at != wide[n] {
				t.Fatalf("a narrower frame reordered its columns: %v then %v", wide, tight)
			}
		}
	}
}

// THE NAMES ARE MEASURED FIRST AND THE COLUMNS TAKE WHAT IS LEFT (law 1). There
// is no width at which a model's id is shortened so that an arena score can be
// drawn — the score's column goes instead.
func TestNoWidthShortensANameToKeepAColumn(t *testing.T) {
	for width := 60; width <= 160; width++ {
		p, fit := tablePicker(width)
		if !fit.drawn() {
			continue
		}
		for _, model := range tableCatalog {
			label, _ := p.rowText(model, "", width)
			if strings.Contains(label, glyphMore) || !strings.Contains(label, model.ID) {
				t.Fatalf("at %d columns %q was cut to keep %d columns: %q",
					width, model.ID, len(fit.at), label)
			}
		}
	}
}

// A DIALLED LEVEL DOES NOT SHORTEN THE NAME IT RIDES ON. The name column
// reserves [modelLevelRoom] on a list with a model that takes a reasoning knob,
// so pressing ctrl+t cannot make a row's own id give up its author.
func TestDiallingALevelDoesNotCostTheNameItsAuthor(t *testing.T) {
	const width = 70
	p, fit := tablePicker(width)
	if !fit.drawn() {
		t.Fatalf("at %d columns the list drew no table", width)
	}
	for _, level := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		label, _ := p.rowText(tableCatalog[0], level, width)
		want := tableCatalog[0].ID
		if level != "" {
			want += ":" + level
		}
		if !strings.Contains(label, want) {
			t.Fatalf("dialled to %q the row reads %q, want %q", level, label, want)
		}
	}
}

// A PHONE DRAWS NO TABLE. Under sixty columns the tail has a line of its own and
// there is no second column to stand anything in, so the row is the ranked tail
// it has always been — units and all, since no head is there to carry them.
func TestAPhoneKeepsTheRankedTailAndDrawsNoHeading(t *testing.T) {
	const width = 44
	p, fit := tablePicker(width)
	if fit.drawn() {
		t.Fatalf("at %d columns there is no room for a table", width)
	}
	if p.headLines(width) != 0 {
		t.Fatalf("a frame with no table must spend no line on a heading")
	}
	_, note := p.rowText(tableCatalog[0], "", width)
	if !strings.Contains(note, "per M") || !strings.Contains(note, "elo ") {
		t.Fatalf("the tail has to carry its own units where no head can: %q", note)
	}
}

// ONE READING OF A MODEL DRESSES BOTH SHAPES. A figure that means dollars per
// million in the table means dollars per million in the tail, because there is
// one place that works it out (CLAUDE.md's one-source-of-truth rule).
func TestTheTailAndTheTableAreTheSameReadingOfAModel(t *testing.T) {
	facts := modelFactsOf(tableCatalog[0], "", "")
	note := modelNote(tableCatalog[0])
	for _, want := range []string{
		facts.in + "/" + facts.out + " per M",
		facts.window,
		"elo " + facts.elo,
		facts.can,
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("the tail says %q, which does not carry %q", note, want)
		}
	}
	cells := facts.cells()
	if cells[2] != facts.in || cells[3] != facts.out || cells[6] != facts.elo {
		t.Fatalf("the table's cells are not the same reading: %v", cells)
	}
}

// THE MEASUREMENT IS THE LIST'S AND DOES NOT MOVE WHILE SOMEBODY TYPES. A
// keystroke narrows the rows; a table that re-measured itself on each one would
// slide its columns sideways under a person's eye, worst on exactly the
// keystroke that removed the row they were reading.
func TestFilteringDoesNotMoveTheColumns(t *testing.T) {
	const width = 100
	p, before := tablePicker(width)
	for _, r := range "sonnet" {
		p.filter.insert(string(r))
		p.rank()
	}
	if len(p.hits) != 1 {
		t.Fatalf("the filter matched %d rows, want the one", len(p.hits))
	}
	after := p.tableFit(width)
	if after.header() != before.header() {
		t.Fatalf("a keystroke moved the columns:\n%q\n%q", before.header(), after.header())
	}
}
