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
			if strings.TrimSpace(cellSlice(line, want, width)) != "" {
				t.Fatalf("at %d columns line %d runs past the heading's %d cells:\n%s",
					width, at, want, strings.Join(lines, "\n"))
			}
		}
		// And the figures land UNDER their heads rather than near them, which is
		// the whole claim: the cells of the window column all start in the cell
		// the word `window` starts in.
		at, wide := columnCells(fit, lines[0], "window")
		for _, cell := range []string{"1M", "128k", "262k"} {
			if at < 0 {
				t.Fatalf("at %d columns there is no window column:\n%s", width, lines[0])
			}
			if !columnHolds(lines, at, wide, cell) {
				t.Fatalf("at %d columns %q is not under the window head:\n%s",
					width, cell, strings.Join(lines, "\n"))
			}
		}
	}
}

// columnCells is where one head's column starts and how wide it is drawn — the
// head's offset in the heading line IN CELLS, and the width the fit gave it,
// which is NOT the head's own length: a column is as wide as its widest cell,
// and `speech` is one cell wider than `makes`.
func columnCells(fit modelTableFit, heading, head string) (int, int) {
	for n, at := range fit.at {
		if modelColumns[at].head == head {
			return ansi.StringWidth(heading[:strings.Index(heading, head)]), fit.wide[n]
		}
	}
	return -1, 0
}

// cellSlice is the text standing in the cells [at, at+wide) of one drawn line.
//
// IT COUNTS CELLS AND NOT BYTES, which is the whole reason it exists: the
// cursor's own mark is `›`, one cell and three bytes, so a byte offset reads
// the selected row two cells to the right of every other row — and a test
// written that way would swear the table was misaligned on exactly the row a
// person is looking at, or, worse, miss a real misalignment everywhere else.
func cellSlice(line string, at, wide int) string {
	out, cell := strings.Builder{}, 0
	for _, r := range line {
		if cell >= at+wide {
			break
		}
		if cell >= at {
			out.WriteRune(r)
		}
		cell += ansi.StringWidth(string(r))
	}
	return out.String()
}

// columnHolds reports whether some line carries text inside the cells [at,
// at+wide) and nothing of it outside them.
func columnHolds(lines []string, at, wide int, text string) bool {
	if at < 0 {
		return false
	}
	for _, line := range lines {
		if strings.TrimSpace(cellSlice(line, at, wide)) == text {
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
	for _, head := range []string{"via", "first", "t/s", modalityOutputsLead} {
		if strings.Contains(fit.header(), head) {
			t.Fatalf("nothing was measured, so %q has no column:\n%s", head, fit.header())
		}
	}
	for _, head := range []string{"in/M", "out/M", "window", "elo", modalityInputsLead} {
		if !strings.Contains(fit.header(), head) {
			t.Fatalf("the catalog publishes %q, so the table has to draw it:\n%s", head, fit.header())
		}
	}
}

// THE TWO SIDES ARE TWO COLUMNS, and which of them a list draws is decided by
// the list. `/model` offers only models that answer in text and nothing else,
// so `makes` has nothing to say there and is not drawn; a media slot's list is
// exactly the rows that make something, and there it is the column the slot
// exists for.
func TestTheMakesColumnAppearsOnAListOfModelsThatMakeSomething(t *testing.T) {
	const width = 100
	drawing := []Model{
		{ID: "vendor/painter", Input: []string{"text", "image"}, Output: []string{"image"}},
		{ID: "vendor/tts", Input: []string{"text"}, Output: []string{"speech"}},
		{ID: "vendor/song", Input: []string{"text"}, Output: []string{"music"}},
		{ID: "vendor/film", Input: []string{"text"}, Output: []string{"video"}},
	}
	p := &picker{}
	p.start(drawing, "vendor/painter")
	fit := p.tableFit(width)
	if !strings.Contains(fit.header(), modalityOutputsLead) || !strings.Contains(fit.header(), modalityInputsLead) {
		t.Fatalf("a list of makers has to draw both sides:\n%s", fit.header())
	}
	lines := tableRows(p, width)
	// AND THE THREE KINDS OF SOUND STAY THREE KINDS. The old vocabulary folded
	// speech, audio and music into one word, so a model that writes songs and
	// one that reads a paragraph aloud drew the same row.
	at, wide := columnCells(fit, lines[0], modalityOutputsLead)
	for _, want := range []string{"speech", "music", "video", "image"} {
		if !columnHolds(lines, at, wide, want) {
			t.Fatalf("%q is not under the makes head:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

// A COLUMN THE LIST WAS CHOSEN BY IS NOT DRAWN. Every row of a drawing slot
// makes an image — that is what the slot means — so a `makes` column there is
// the slot's own name written fifty-four times under a head somebody had to
// read first.
func TestAColumnEveryRowAgreesOnIsNotDrawnWhenTheListWasChosenByIt(t *testing.T) {
	const width = 100
	drawing := []Model{
		{ID: "vendor/painter", ContextLength: 65_000, Input: []string{"text", "image"}, Output: []string{"image"}},
		{ID: "vendor/dreamer", ContextLength: 4_000, Input: []string{"text"}, Output: []string{"image"}},
		{ID: "vendor/sketcher", ContextLength: 65_000, Input: []string{"text"}, Output: []string{"image"}},
	}
	p := &picker{}
	p.start(drawing, "vendor/painter")
	head := p.tableFit(width).header()
	if strings.Contains(head, modalityOutputsLead) {
		t.Fatalf("every row makes an image, so the column says nothing:\n%s", head)
	}
	// AND THE SIDE THAT DOES VARY IS STILL DRAWN — one of the three reads an
	// image and two do not, which is exactly the thing somebody is choosing on.
	if !strings.Contains(head, modalityInputsLead) {
		t.Fatalf("the side that varies has to be drawn:\n%s", head)
	}
}

// AND A COLUMN NOBODY FILTERED ON KEEPS ITS CELLS EVEN WHEN EVERY ROW AGREES.
// Two models that happen to hold the same number of tokens are a coincidence,
// not a definition, and a person who came to read that number would find the
// column gone. The emptiness law is about facts nobody published; it may not
// grow into hiding facts that were.
func TestAConstantColumnTheListWasNotChosenByIsStillDrawn(t *testing.T) {
	const width = 100
	same := []Model{
		{ID: "vendor/one", ContextLength: 1_000_000, PromptPrice: 3e-6, CompletionPrice: 9e-6},
		{ID: "vendor/two", ContextLength: 1_000_000, PromptPrice: 1e-6, CompletionPrice: 9e-6},
	}
	p := &picker{}
	p.start(same, "vendor/one")
	head := p.tableFit(width).header()
	for _, want := range []string{"window", "in/M", "out/M"} {
		if !strings.Contains(head, want) {
			t.Fatalf("%q agrees across the list but nothing filtered on it:\n%s", want, head)
		}
	}
	lines := tableRows(p, width)
	at, wide := columnCells(p.tableFit(width), lines[0], "window")
	if !columnHolds(lines, at, wide, "1M") {
		t.Fatalf("the window both rows share still has to be readable:\n%s", strings.Join(lines, "\n"))
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
	if !strings.Contains(free, "262k") || !strings.Contains(free, "text image audio") {
		t.Fatalf("the row lost the facts it does publish: %q", free)
	}
	// AND THE CELL SAYS `text` WHERE THE TAIL LEAVES IT OUT: under a head
	// reading `inputs`, a blank means "nothing", and every model on this list
	// takes text.
	if plain := modelNote(tableCatalog[3]); strings.Contains(plain, "text") {
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
		modalityInputsLead + " " + modalitySay(tableCatalog[0].Input, modalityOrderIn, false),
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("the tail says %q, which does not carry %q", note, want)
		}
	}
	cells := facts.cells()
	if cells[2] != facts.in || cells[3] != facts.out || cells[6] != facts.elo || cells[7] != facts.inputs {
		t.Fatalf("the table's cells are not the same reading: %v", cells)
	}
	// AND THE ONE PLACE THE TWO SHAPES DELIBERATELY DIFFER IS `text`, which the
	// cell carries and the tail does not — same reading, asked with withText
	// either way ([modalitySay]).
	if cells[7] != "text "+modalitySay(tableCatalog[0].Input, modalityOrderIn, false) {
		t.Fatalf("the cell has to be the tail's words with text in front: %q", cells[7])
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
