package prose

import (
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Tables — the other half of 13.3's finding.
//
// The soup comes from one decision made wrong: a cell that does not fit gets
// WRAPPED, so one long cell turns a row into three rows, the columns stop
// lining up vertically, and the reader loses the only thing a table was for.
// Here a cell that does not fit gets TRUNCATED. A row is always one row.
//
// That is a real trade — an ellipsis loses text — and it is the right one for a
// grid, because a table's value is the comparison down its columns and a
// wrapped table has no columns. Where the truncation would be severe the
// content is still reachable: the source text is a scroll away in the raw reply,
// and a table too wide even for its floors keeps its one-row-per-row shape so a
// caller with a horizontal viewport can crop rather than reflow.
//
// There are no rules between columns and no box around the grid (5.13:
// "cards separated by whitespace not boxes"). Two spaces separate columns and
// one hairline separates the header from the body, because that is the least
// ink that still says "grid".

// tableGap is the whitespace between two columns. Two cells is the smallest gap
// that reads as a column boundary without a rule; one reads as a typo.
const tableGap = 2

// tableFloor is the narrowest a column may be squeezed to before the table is
// declared unfittable: one visible cell plus the ellipsis that admits the rest
// was cut, plus one for a wide rune not to be halved into nothing.
const tableFloor = 3

type tableModel struct {
	align  []extast.Alignment
	header []string
	rows   [][]string
}

func (r *renderer) table(n *extast.Table) {
	m := r.readTable(n)
	cols := len(m.align)
	if cols == 0 {
		return
	}
	widths := fitColumns(m, cols, r.figureWidth())

	headStyle := style{tok: tokens.TextSecondary}
	bodyStyle := r.base
	bodyStyle.tok = tokens.TextPrimary

	if len(m.header) > 0 {
		r.emit(r.tableRow(m.header, widths, m.align, headStyle))
		total := tableGap * (len(widths) - 1)
		for _, w := range widths {
			total += w
		}
		r.emit([]piece{{text: hairline(total), st: style{tok: tokens.TextTertiary}}})
	}
	for _, row := range m.rows {
		r.emit(r.tableRow(row, widths, m.align, bodyStyle))
	}
}

// readTable flattens the AST into plain strings. Cells lose their inline
// styling on the way: see [renderer.inlineText] for why a run of bold inside a
// cell that is about to be cut is not worth the style boundary.
func (r *renderer) readTable(n *extast.Table) tableModel {
	m := tableModel{align: n.Alignments}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		cells := r.readRow(c)
		if len(cells) == 0 {
			continue
		}
		if c.Kind() == extast.KindTableHeader {
			m.header = cells
			continue
		}
		m.rows = append(m.rows, cells)
	}
	// A malformed table can declare more cells than it declared alignments;
	// the alignment list is the column count, so widen it rather than index
	// past it.
	width := len(m.header)
	for _, row := range m.rows {
		if len(row) > width {
			width = len(row)
		}
	}
	for len(m.align) < width {
		m.align = append(m.align, extast.AlignNone)
	}
	return m
}

func (r *renderer) readRow(n ast.Node) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.inlineText(c))
	}
	return out
}

// fitColumns is max-min fair sharing: every column gets what it asks for until
// the table runs out of room, and then the WIDEST columns give first. A binary
// search finds the cap that just fits, which is one pass rather than shrinking
// the widest column a cell at a time.
//
// If even the floors do not fit, the floors are returned anyway. The row is
// then over-wide and [fit] cuts it at the ceiling — deliberately, and this is
// the "scrollable-shaped" case the package comment names: the alternative is a
// table with fewer columns than the source had, which is a table that lies.
func fitColumns(m tableModel, cols, width int) []int {
	natural := make([]int, cols)
	measure := func(row []string) {
		for i, cell := range row {
			if i < cols && cells(cell) > natural[i] {
				natural[i] = cells(cell)
			}
		}
	}
	measure(m.header)
	for _, row := range m.rows {
		measure(row)
	}
	for i := range natural {
		if natural[i] < 1 {
			natural[i] = 1
		}
	}

	avail := width - tableGap*(cols-1)
	total, widest := 0, 0
	for _, n := range natural {
		total += n
		if n > widest {
			widest = n
		}
	}
	if avail < cols*tableFloor {
		// Not even the floors fit. Hand back the floors (clamped to what each
		// column actually wants) and let the ceiling do the cropping.
		out := make([]int, cols)
		for i := range out {
			out[i] = min(natural[i], tableFloor)
		}
		return out
	}
	if total <= avail {
		return natural
	}

	// Largest cap for which sum(min(natural, cap)) <= avail.
	lo, hi := tableFloor, widest
	for lo < hi {
		mid := (lo + hi + 1) / 2
		sum := 0
		for _, n := range natural {
			sum += min(n, mid)
		}
		if sum <= avail {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	out := make([]int, cols)
	used := 0
	for i, n := range natural {
		out[i] = min(n, lo)
		used += out[i]
	}
	// Whatever the cap left over goes back, one cell at a time, to the columns
	// still pressed against it — the ones that wanted more.
	for i := 0; used < avail && i < cols; i++ {
		if out[i] < natural[i] {
			out[i]++
			used++
			i--
		}
	}
	return out
}

// tableRow lays one row out. Every cell is padded to its column so the columns
// are columns; the trailing run that leaves on the last cell is removed by
// [renderer.emit], which trims every row it draws, so no row ever carries
// whitespace past its last word.
func (r *renderer) tableRow(cells []string, widths []int, align []extast.Alignment, st style) []piece {
	row := make([]piece, 0, len(widths)*2)
	for i, w := range widths {
		if i > 0 {
			// The gap wears the row's own style so a whole row coalesces into
			// one painted run; a separator in a different style would cost an
			// escape pair per column to draw nothing.
			row = append(row, piece{text: spaces(tableGap), st: st})
		}
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		row = append(row, piece{text: alignCell(truncate(cell, w), w, alignOf(align, i)), st: st})
	}
	return row
}

func alignOf(align []extast.Alignment, i int) extast.Alignment {
	if i < len(align) {
		return align[i]
	}
	return extast.AlignNone
}

// alignCell places a cell in its column. AlignNone is left, which is what a
// column of words wants; a column of numbers asks for AlignRight in the source
// and gets a shared right edge, which is the only way figures compare.
func alignCell(cell string, width int, a extast.Alignment) string {
	switch a {
	case extast.AlignRight:
		return padLeft(cell, width)
	case extast.AlignCenter:
		slack := width - cells(cell)
		if slack <= 0 {
			return truncate(cell, width)
		}
		return spaces(slack/2) + cell + spaces(slack-slack/2)
	default:
		return pad(cell, width)
	}
}
