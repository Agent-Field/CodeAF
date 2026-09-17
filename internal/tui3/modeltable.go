package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── THE MODEL LIST AS A TABLE ───────────────────────────────────────────────
//
// rowfit.go lays every list on this surface out the same way: the thing's name
// on the left and a dim tail of ranked facts about it on the right. For a list
// of settings that is exactly right — each row's tail is about that row and
// nothing else, so there is nothing to line up.
//
// THE MODEL LIST IS THE ONE LIST HERE THAT IS READ DOWN. Six hundred rows, and
// the question in front of somebody scrolling it is never "what does this one
// cost". It is "which of these is cheap", "which of these holds a million
// tokens", "which of these can see" — every one of them a COMPARISON, and a
// comparison between rows needs the fact in the same place on each of them.
// What the ranked tail drew instead was this:
//
//	inference-net/schematron-v2-turbo      $0.03/$0.15 per M · 128k
//	~openai/gpt-astra-latest                    $10/$50 per M · 1M · sees
//	inclusionai/ling-3.0-flash-vl   $0.06/$0.18 per M · 131k · sees · watches
//	nex-agi/nex-n2.5-mini:free                            262k · sees
//
// Four prices, four windows, and no two of them in the same column — because
// the tail is right-aligned as ONE string and every row's string is a different
// length, so a row that publishes no price shifts its window under somebody
// else's price. Every figure is correct and the list cannot be scanned.
//
// So the facts get columns. The facts themselves do not change and their
// RANKING does not change — this file is modelFields' ordering (models.go says
// why that order and not another) laid out down the page instead of along the
// row, and it gives up its columns from the same end for the same reasons.
//
// THREE THINGS THE COLUMNS BUY, past the alignment:
//
//  1. THE UNIT IS SAID ONCE. A tail has to name every figure it carries, because
//     a fact standing alone in a sentence of facts has no other way to say what
//     it is: `$0.09/$0.18 per M · 1M · 58t/s · elo 1424`. A column's head names
//     it for the whole list, so the row carries the figure alone — which is
//     eleven cells a row back, and eleven cells is another column.
//
//  2. THE PRICE BECOMES TWO COLUMNS. `$0.09/$0.18` is two numbers in one
//     string, and the string is the reason neither of them could be compared:
//     the prompt half is a different width on every row, so the completion half
//     — the one a long answer actually spends — never landed anywhere twice.
//     Apart, each lines up under its own head.
//
//  3. A GAP IS A FACT NOW. In a tail, a row that published no price and a row
//     whose price did not fit both draw nothing, and they look identical. In a
//     column they cannot: every row shows every column the table drew, so a
//     blank cell means the catalog published nothing and can mean nothing else.
//     That is the emptiness law (design-law-v2 §16) getting stronger rather
//     than weaker — the silence stayed silent and became unambiguous.
//
// WHAT IT COSTS, stated plainly because it is real: a column is as wide as the
// widest row in it, so the table fits fewer facts on a narrow frame than the
// tail did. The tail spends each row's cells on that row alone and is therefore
// unbeatable per row; it is only beaten down the page. So the table is what a
// frame with room draws, the tail is what a frame without one falls back to
// ([modelTable.fit] decides, and tierPhone never tables at all), and both are
// built from one reading of the model ([modelFacts]).

// modelColumn is one column: what its head says, and which way its values are
// set inside it.
//
// NUMBERS ARE SET RIGHT AND WORDS ARE SET LEFT, which is the whole of the
// alignment rule. A column of prices is read by its last digit and a column of
// names is read by its first letter.
type modelColumn struct {
	head  string
	right bool
}

// modelColumns are the table's columns IN RANK ORDER — the order they are drawn
// in, and the order they are given up in when the frame cannot hold them all.
//
// IT IS modelFields' RANKING, FACT FOR FACT, with the price split in two. That
// ordering is argued at length where it lives (models.go, "THE PICKER ROW'S
// DATA HIERARCHY") and it is not re-argued here: a table whose columns were
// ordered differently from the tail it degrades into would be two lists to
// learn, and the one thing a person carries between the two shapes is where to
// look.
//
// THE HEADS CARRY THE UNITS, and that is what the heads are for. `$0.09` under
// `in/M` says what `$0.09/$0.18 per M` had to spell out on every row; `1424`
// under `elo` says what `elo 1424` did; `58` under `t/s` says what `58t/s` did.
//
// AND A HEAD SAYS ONLY WHAT ITS COLUMN DOES NOT. The price heads do not say `$`
// — every figure under them already does, and a head is charged to the column
// (a column is as wide as its widest line, head included), so two cells spent
// re-saying the dollar are two cells taken off the model names on a frame that
// has none to give. `in/M` is the half the figures cannot say: per million, and
// which million.
var modelColumns = [...]modelColumn{
	{head: "via"},
	{head: "first", right: true},
	{head: "in/M", right: true},
	{head: "out/M", right: true},
	{head: "window", right: true},
	{head: "t/s", right: true},
	{head: "elo", right: true},
	{head: "can"},
}

// modelHead is what stands over the name column. It is the only head that names
// a thing rather than a unit, and it is here so the header line is built from
// one list rather than from a literal plus a loop.
const modelHead = "model"

// modelTableSep is what stands between two columns, and it is SPACE and not the
// tail's ` · `. A dot between two figures is a join — it says these belong to
// one row and are read together — which is exactly what a tail means by it and
// exactly what a table does not: down a column the dot would be a mark that
// appears in every cell of a separator that is not a cell. Two spaces are a
// column boundary, and a blank cell between two blank neighbours stays blank.
const modelTableSep = "  "

// modelNameWide is the most the name column may demand from the columns,
// however long the ids on this list run.
//
// THE NAME COLUMN IS DEMAND-DRIVEN AND THIS IS ITS ONLY LIMIT — it asks for
// exactly what the longest row needs and not one cell more, so the columns
// start where the names end rather than at the far side of a field of blanks.
// Law 1 of rowfit.go is what makes that ask come FIRST: the identity is whole
// or the row is pointless, so the columns are budgeted out of what the names
// leave, and a column goes rather than a name.
//
// The ceiling is here because one absurd id is not an argument. A catalog with
// a forty-eight-character slug in it would otherwise buy that one row a name
// column at the price of every other row's price and window. Past the ceiling
// the id is trimmed the way rowfit.go trims one — author first, which sheds the
// part that identifies nothing.
const modelNameWide = 44

// modelLevelRoom is what a reasoning level costs the name it rides on: the
// longest rung the ladder can put there, `:medium`.
//
// IT IS RESERVED BY THE COLUMN AND ONLY WHERE A ROW COULD CARRY ONE
// ([Model.Reasoning]). The level is the one thing on the row that is not a fact
// about the model — it is what THIS person asked for — and a name column
// measured without it would make the act of dialling a model in shorten that
// model's own name, which is a surface answering a keypress by taking something
// away.
const modelLevelRoom = len(":medium")

// modelTableLeast is how many columns make a table worth drawing. One column is
// not a comparison, it is a fact with a heading over it, and the tail draws
// that fact on more rows than a single column would (the tail spends each row's
// own cells; the column spends the widest row's). So below two, the picker
// falls back to the ranked tail.
const modelTableLeast = 2

// cells is one model's facts in column order, each in the spelling its column's
// head has already accounted for. The array is indexed by [modelColumns], so a
// column added there and a cell added here is one change in two halves and the
// compiler names the half that was forgotten.
func (f modelFacts) cells() [len(modelColumns)]string {
	return [len(modelColumns)]string{f.via, f.first, f.in, f.out, f.window, f.rate, f.elo, f.can}
}

// modelTable is how wide each column wants to be, measured over a whole list
// ([modelTable.add], once per row). It holds no rows: a row renders itself from
// its own cells through [modelTableFit], so the table is a measurement and not a
// copy of the list.
//
// IT IS MEASURED OVER EVERY ROW ON OFFER AND NOT OVER THE FILTER'S HITS, which
// is [picker.shared]'s rule said about furniture instead of about identity: a
// table that re-measured itself on every keystroke would slide sideways under
// somebody who is typing, and it would do it worst on the narrowing keystrokes,
// where a column vanishing and the ones behind it jumping left is indis-
// tinguishable from the list having scrolled. A column's width is not worth
// that. Measured once, it is furniture — and furniture stays where it was put.
type modelTable struct {
	// wide is the widest CELL in each column, ignoring the head — which is what
	// makes zero mean "not one row on this list said anything here" and makes
	// that column absent rather than empty (law 4: unknown is not narrow).
	wide [len(modelColumns)]int
	// name is what the name column ASKS FOR: the widest label this list will
	// draw — an id, plus the room a reasoning level takes on a row that can
	// carry one — capped at [modelNameWide].
	//
	// IT IS THE ONE THING THAT BUYS A COLUMN BACK OFF THE TABLE, and that is
	// law 1 of rowfit.go: the columns are budgeted out of what the names leave.
	name int
}

// add takes one row into the measurement: its cells, and what its label will
// ask for in cells ([modelTable.name]).
//
// THE CELLS COME FROM THE CALLER AND ARE NOT READ HERE, and that is the point:
// the picker freezes a model's cells the first time it draws them
// ([picker.rowCells]) and hands the SAME strings to this, so a column can never
// be measured against one reading of the ledger and drawn against another. A
// cell wider than its own column is the one thing that would put a row out of
// line with its neighbours, and this is what makes it impossible.
func (t *modelTable) add(cells [len(modelColumns)]string, name int) {
	for at, cell := range cells {
		if wide := ansi.StringWidth(cell); wide > t.wide[at] {
			t.wide[at] = wide
		}
	}
	if name > modelNameWide {
		name = modelNameWide
	}
	if name > t.name {
		t.name = name
	}
}

// nameAsk is what one row's label will ask the name column for: the id, and on
// a model whose endpoint takes a reasoning knob the rung that may be dialled
// onto it ([modelLevelRoom]).
func nameAsk(model Model) int {
	ask := ansi.StringWidth(model.ID)
	if model.Reasoning {
		ask += modelLevelRoom
	}
	return ask
}

// modelTableFit is the table at ONE width: which columns are drawn, how wide
// each of them is, and where the whole block sits.
type modelTableFit struct {
	// at are the indexes into [modelColumns] that survived, in rank order, and
	// wide is each one's drawn width — the widest cell or the head, whichever
	// asks for more, since a head narrower than its column would be a label
	// that did not reach its own numbers.
	at   []int
	wide []int
	// facts is the columns' own total width, separators included. Every row's
	// tail is exactly facts plus pad cells wide, and THAT is the whole
	// mechanism: [overlayRowTinted] right-aligns a tail inside the measure, so
	// tails of one width align on both edges and therefore column by column.
	facts int
	// pad is the blank the block carries on its RIGHT, which is what holds the
	// columns beside the names instead of against the far edge of the measure.
	// The tail of every list on this surface is right-aligned and this one is a
	// tail; the padding is how a table hugs its names without becoming the one
	// list whose facts are somewhere else.
	pad int
	// name is what the id half is drawn in, and room is the measure the pair is
	// laid out in ([overlayMeasure]). The header line uses both to stand its
	// heads over the columns they belong to.
	name int
	room int
}

// drawn reports whether this fit has a table in it at all.
func (f modelTableFit) drawn() bool { return len(f.at) >= modelTableLeast }

// fit lays the table out in a frame of the given width. The answer is a table
// with no columns in it ([modelTableFit.drawn] is false) where this frame has
// no room for one, and the picker draws the ranked tail instead.
//
// IT GIVES UP COLUMNS FROM THE LOW-RANKED END, which is rowfit.go's law 3 — the
// tail is a prefix of itself — holding down the page instead of along the row.
// Every row shows the same columns from the top, so a blank never has to be
// read as "it did not fit" and the ranking is the only thing that decides what
// a narrow frame loses.
func (t modelTable) fit(width int) modelTableFit {
	room := width - 2
	// THE TABLE STOPS TRAVELLING RIGHT AT THE MEASURE, exactly as every other
	// label/tail pair on this surface does and for [overlayMeasure]'s reason:
	// past about a hundred cells the eye stops landing on the right row.
	if room > overlayMeasure {
		room = overlayMeasure
	}
	fit := modelTableFit{room: room}
	for at := range modelColumns {
		// LAW 4: a column nobody published is not a narrow column, it is no
		// column. A list where nothing has been measured draws no `via`, no
		// `first` and no `t/s` rather than three heads over three hundred
		// blanks.
		if t.wide[at] == 0 {
			continue
		}
		wide := t.wide[at]
		if head := ansi.StringWidth(modelColumns[at].head); head > wide {
			wide = head
		}
		fit.at = append(fit.at, at)
		fit.wide = append(fit.wide, wide)
	}
	for len(fit.at) > 0 {
		fit.facts = 0
		for _, wide := range fit.wide {
			fit.facts += wide + len(modelTableSep)
		}
		fit.facts -= len(modelTableSep)
		// THE NAMES ARE ASKED FIRST (law 1) AND THE COLUMNS TAKE WHAT IS LEFT.
		// Where the frame has more than both want, the surplus is spent as
		// padding on the right of the block, so the columns sit beside the names
		// and the block still ends where every other tail on this surface does.
		if left := room - rowGutter - fit.facts; left >= t.name {
			fit.name, fit.pad = t.name, left-t.name
			return fit
		}
		fit.at = fit.at[:len(fit.at)-1]
		fit.wide = fit.wide[:len(fit.wide)-1]
	}
	return modelTableFit{room: room}
}

// row is one model's facts as the tail the list draws — every drawn column, in
// order, padded to exactly [modelTableFit.facts] cells.
//
// THE PADDING IS THE POINT AND THE TRAILING BLANKS STAY. A tail trimmed on the
// right would be a different width on every row, and a row's width is what the
// alignment is made of: the cells after the last thing a model published are
// what hold its neighbours' columns up.
func (f modelTableFit) row(cells [len(modelColumns)]string) string {
	if !f.drawn() {
		return ""
	}
	var out strings.Builder
	out.Grow(f.facts + f.pad)
	for n, at := range f.at {
		if n > 0 {
			out.WriteString(modelTableSep)
		}
		out.WriteString(padCell(cells[at], f.wide[n], modelColumns[at].right))
	}
	out.WriteString(strings.Repeat(" ", f.pad))
	return out.String()
}

// header is the one line that stands over the list: `model` above the names and
// each column's head above its own cells.
//
// IT IS BUILT TO THE SAME WIDTHS AS A ROW and ends at the same cell, so the
// heads sit over their columns rather than near them. The lead is the two cells
// every row on this surface pays for its cursor mark ([overlayLead]), padded
// here because the header answers to no cursor.
func (f modelTableFit) header() string {
	if !f.drawn() {
		return ""
	}
	gap := f.room - ansi.StringWidth(modelHead) - f.facts - f.pad
	if gap < rowGutter {
		gap = rowGutter
	}
	heads := [len(modelColumns)]string{}
	for _, at := range f.at {
		heads[at] = modelColumns[at].head
	}
	return "  " + modelHead + strings.Repeat(" ", gap) + f.row(heads)
}

// padCell sets one value inside its column: to the right for a figure, to the
// left for a word, and blank for a fact nobody published.
//
// A VALUE WIDER THAN ITS COLUMN CANNOT HAPPEN — the column was measured over
// every row that will be drawn in it ([modelTable.add]) — so this pads and never
// cuts. Were it ever to happen, a cut figure is a wrong figure (rowfit.go's
// opening argument), so the value is left whole and the row is one cell wide of
// its neighbours, which is a thing a person can see and report.
func padCell(text string, wide int, right bool) string {
	pad := wide - ansi.StringWidth(text)
	if pad <= 0 {
		return text
	}
	if right {
		return strings.Repeat(" ", pad) + text
	}
	return text + strings.Repeat(" ", pad)
}
