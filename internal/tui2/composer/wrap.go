package composer

import "github.com/charmbracelet/x/ansi"

// Hard-wrap layout for the draft, plus the arithmetic that maps the cursor's
// rune index onto a display row. This is deliberately NOT word wrap: a
// composer's job is to show exactly what will be sent, and greedy word
// wrapping (blocks.Wrap, built for settled prose) would decouple "the Nth
// character" from "column N of some row," which is what the cursor position
// needs to stay simple and correct. A hard wrap costs a mid-word break that
// prose wrapping would have avoided; it buys a cursor that is never wrong.

// span is a half-open rune-index range [Start, End) into the draft, one
// display row's worth of text.
type span struct{ Start, End int }

// layoutRows hard-wraps value into rows of at most width cells each,
// breaking at '\n' first and then at the width budget. width <= 0 is the
// degenerate case (a pane rectangle of zero columns): every logical line
// still gets exactly one row, but it carries no characters, because there is
// no cell to put one in — the caller (render.go) is what actually guarantees
// nothing overflows, by truncating the assembled string regardless, but this
// function does not manufacture an infinite loop trying to fit a positive
// number of cells into a non-positive budget.
func layoutRows(value []rune, width int) []span {
	if width <= 0 {
		return zeroWidthRows(value)
	}
	rows := make([]span, 0, 1)
	rowStart, cells := 0, 0
	for i := 0; i <= len(value); i++ {
		if i == len(value) || value[i] == '\n' {
			rows = append(rows, span{rowStart, i})
			rowStart, cells = i+1, 0
			continue
		}
		w := runeCells(value[i])
		if cells+w > width && cells > 0 {
			rows = append(rows, span{rowStart, i})
			rowStart, cells = i, 0
		}
		cells += w
	}
	if len(rows) == 0 {
		rows = append(rows, span{0, 0})
	}
	return rows
}

// zeroWidthRows gives every logical line exactly one empty row, so a caller
// walking rows to find the cursor's row still finds one even when there is
// no room to draw a single cell of it.
func zeroWidthRows(value []rune) []span {
	rows := make([]span, 0, 1)
	rowStart := 0
	for i := 0; i <= len(value); i++ {
		if i == len(value) || value[i] == '\n' {
			rows = append(rows, span{rowStart, rowStart})
			rowStart = i + 1
		}
	}
	if len(rows) == 0 {
		rows = append(rows, span{0, 0})
	}
	return rows
}

// runeCells is the width, in terminal cells, of one rune of typed draft
// text. Draft text is never styled input (see doc.go: Key.Text is only
// populated for printable characters), so this only has to answer "how wide
// is this character," which ansi.StringWidth already knows including
// East-Asian width and zero-width marks.
func runeCells(r rune) int {
	return ansi.StringWidth(string(r))
}

// rowOf returns the index of the row in rows that contains rune-index pos —
// "contains" meaning pos falls in [Start, End), or pos == End for the last
// row (the cursor may sit one past the last character, which is any row's
// End when nothing follows it).
func rowOf(rows []span, pos int) int {
	for i, r := range rows {
		if pos < r.End || i == len(rows)-1 {
			return i
		}
	}
	return len(rows) - 1
}
