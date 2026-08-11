package chat

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Pipe tables (13.3.2: "tables render as soup").
//
// A markdown table is the one construct in a model's output whose meaning is
// POSITIONAL. Prose survives being re-wrapped; a table does not — re-flowing
// `| a | b |` as a sentence throws away which cell belonged to which column,
// which is the entire content. The old path had no table pass at all, so every
// table arrived as a wall of pipes.
//
// The ladder here is the responsive law applied to a grid, and it never has a
// soup rung:
//
//  1. FITS — columns at their natural widths, separated by two spaces, with the
//     header at the promoted tier and one hairline under it. 5.13 allows a rule
//     at a boundary and a header/body seam is one; there is no box drawing,
//     because 5.16's grey ramp does not have a budget for a frame.
//  2. SQUEEZES — the widest column gives back cells, one at a time, until the
//     grid fits. Cells WRAP inside their column, so a row becomes several screen
//     rows and no character is lost. This is the "wrap-in-cell" rung.
//  3. DEGRADES — below the point where every column can hold [tableMinCol]
//     cells, the grid stops being a grid: each record renders as its own
//     labelled group, `header: value` per line. That is the designed
//     degradation, and it is why a 24-column terminal still reads a table
//     correctly instead of receiving a scrollable pile it cannot scroll.
//
// Horizontal scroll is deliberately NOT the answer here. The transcript is an
// anchored viewport over finalized blocks (8.1.1); a per-block horizontal offset
// would be a second scroll axis with its own state, and 12.5.2's truncation law
// wants what is cut to be VISIBLY cut rather than parked off-screen where the
// reader cannot tell it exists.

const (
	// tableGap separates two columns. Two spaces is the smallest gap that reads
	// as a column boundary without a rule; one space reads as a wrapped word.
	tableGap = 2
	// tableMinCol is the narrowest a column may be squeezed to before rung 3
	// takes over. Three cells holds a short word's stem and is where a wrapped
	// cell stops being readable at all.
	tableMinCol = 3
	// tableMaxCol caps a natural column width so one essay-length cell cannot
	// starve every other column before the squeeze even begins.
	tableMaxCol = 40
	// tableMaxCols bounds how many columns are honoured. Past this a "table" is
	// almost always a line of pipes that was never a table.
	tableMaxCols = 12
)

// table is one parsed pipe table. Cells are stored as faced fragments so the
// inline pass runs once, at parse, rather than once per width.
type table struct {
	header [][]fragment
	rows   [][][]fragment
	// plain is the header's text, used by the degraded rendering as the label
	// of each field.
	plain []string
	cols  int
}

// tableAt recognizes a pipe table starting at lines[at] and returns it with the
// number of source lines it consumed.
//
// The delimiter row is REQUIRED. A single line containing pipes is far more
// often prose ("use a | b to pipe") than a one-row table, and a renderer that
// guessed would reformat sentences into grids.
func tableAt(lines []string, at int) (table, int, bool) {
	if at+1 >= len(lines) {
		return table{}, 0, false
	}
	head := strings.TrimSpace(lines[at])
	if !strings.Contains(head, "|") {
		return table{}, 0, false
	}
	if !isTableDelimiter(strings.TrimSpace(lines[at+1])) {
		return table{}, 0, false
	}
	names := splitTableRow(head)
	if len(names) == 0 || len(names) > tableMaxCols {
		return table{}, 0, false
	}
	if want := len(splitTableRow(strings.TrimSpace(lines[at+1]))); want != len(names) {
		// A delimiter row that does not describe this header is not this
		// table's; treating it as one would silently drop or invent a column.
		return table{}, 0, false
	}

	t := table{cols: len(names), plain: names}
	for _, name := range names {
		t.header = append(t.header, parseInline(nil, name, faceBody))
	}
	used := 2
	for i := at + 2; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, "|") || line == "" {
			break
		}
		cells := splitTableRow(line)
		row := make([][]fragment, t.cols)
		for c := 0; c < t.cols; c++ {
			text := ""
			if c < len(cells) {
				text = cells[c]
			}
			row[c] = parseInline(nil, text, faceBody)
		}
		t.rows = append(t.rows, row)
		used++
	}
	return t, used, true
}

// isTableDelimiter reports the `|---|:--:|` row. It must carry at least one
// dash and nothing that is not part of the grammar.
func isTableDelimiter(line string) bool {
	if !strings.Contains(line, "-") || !strings.Contains(line, "|") {
		return false
	}
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '|', '-', ':', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

// splitTableRow splits one row into its cells, dropping the optional outer
// pipes and honouring the `\|` escape a cell uses to carry a literal one.
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	if line == "" {
		return nil
	}
	cells := make([]string, 0, 4)
	var cell strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '|' {
			cell.WriteByte('|')
			i++
			continue
		}
		if line[i] == '|' {
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
			continue
		}
		cell.WriteByte(line[i])
	}
	cells = append(cells, strings.TrimSpace(cell.String()))
	return cells
}

// tableRows renders the table into dst at width, indented by indent cells.
func (p prose) tableRows(dst []string, t table, width, indent int) []string {
	avail := width - indent
	if avail < 1 || t.cols == 0 {
		return dst
	}
	widths, ok := t.fit(avail)
	if !ok {
		return p.tableAsRecords(dst, t, width, indent)
	}

	pad := strings.Repeat(" ", indent)
	dst = p.tableLine(dst, t.header, widths, pad, faceStrong)
	dst = append(dst, pad+p.paint(tableRule(widths), faceChrome))
	for _, row := range t.rows {
		dst = p.tableLine(dst, row, widths, pad, faceBody)
	}
	return dst
}

// fit resolves the column widths, or reports that the grid cannot be one.
func (t table) fit(avail int) ([]int, bool) {
	gaps := tableGap * (t.cols - 1)
	if avail-gaps < t.cols*tableMinCol {
		return nil, false
	}
	widths := make([]int, t.cols)
	for c := 0; c < t.cols; c++ {
		widths[c] = min(tableMaxCol, max(tableMinCol, fragWidth(t.header[c])))
		for _, row := range t.rows {
			if w := fragWidth(row[c]); w > widths[c] {
				widths[c] = min(tableMaxCol, w)
			}
		}
	}
	total := gaps
	for _, w := range widths {
		total += w
	}
	// Rung 2: the widest column gives a cell back at a time, so a table with one
	// long prose column keeps its short columns intact instead of everything
	// shrinking together into unreadable stubs.
	for total > avail {
		widest, at := 0, -1
		for c, w := range widths {
			if w > widest && w > tableMinCol {
				widest, at = w, c
			}
		}
		if at < 0 {
			return nil, false
		}
		widths[at]--
		total--
	}
	return widths, true
}

// tableLine draws one record as however many screen rows its tallest wrapped
// cell needs. Cells are padded by their PLAIN width, which is tracked through
// the wrap, so a painted cell can never push its neighbour out of column.
func (p prose) tableLine(dst []string, row [][]fragment, widths []int, pad string, head face) []string {
	cells := make([][]wrapped, len(widths))
	tall := 0
	for c := range widths {
		var frags []fragment
		if c < len(row) {
			frags = row[c]
		}
		cells[c] = wrapFragments(frags, widths[c])
		if len(cells[c]) > tall {
			tall = len(cells[c])
		}
	}
	if tall == 0 {
		tall = 1
	}
	for line := 0; line < tall; line++ {
		var out strings.Builder
		out.WriteString(pad)
		for c := range widths {
			if c > 0 {
				out.WriteString(strings.Repeat(" ", tableGap))
			}
			text, plain := "", 0
			if line < len(cells[c]) {
				text, plain = p.paintFragments(cells[c][line], head), cells[c][line].width
			}
			out.WriteString(text)
			if fill := widths[c] - plain; fill > 0 && c < len(widths)-1 {
				out.WriteString(strings.Repeat(" ", fill))
			}
		}
		dst = append(dst, strings.TrimRight(out.String(), " "))
	}
	return dst
}

// tableRule is the hairline under the header, drawn per column so the seam
// shows where the columns are without a frame around them.
func tableRule(widths []int) string {
	var out strings.Builder
	for c, w := range widths {
		if c > 0 {
			out.WriteString(strings.Repeat(" ", tableGap))
		}
		out.WriteString(strings.Repeat(tokens.GlyphTreeDash, w))
	}
	return out.String()
}

// tableAsRecords is rung 3: the grid stops being a grid.
//
// Each record becomes its own labelled group — the header as a dim label, the
// value beside it, wrapped — with a blank row between records. Nothing is lost
// and nothing is positional, which is exactly what a terminal too narrow to
// hold columns can honestly show.
func (p prose) tableAsRecords(dst []string, t table, width, indent int) []string {
	label := 0
	for _, name := range t.plain {
		if w := blocks.Width(name); w > label {
			label = w
		}
	}
	if label > (width-indent)/2 {
		label = max(1, (width-indent)/2)
	}
	for r, row := range t.rows {
		if r > 0 {
			dst = append(dst, "")
		}
		for c := range t.header {
			name := ""
			if c < len(t.plain) {
				name = t.plain[c]
			}
			lead := blocks.Truncate(name, label)
			lead += strings.Repeat(" ", max(0, label-blocks.Width(lead))) + "  "
			contd := strings.Repeat(" ", blocks.Width(lead))
			var frags []fragment
			if c < len(row) {
				frags = row[c]
			}
			dst = p.wrapLabelled(dst, frags, lead, contd, width, indent)
		}
	}
	if len(t.rows) == 0 {
		// A header with no records still says what the columns were.
		for c := range t.plain {
			dst = append(dst, p.flat(t.plain[c], faceChrome, width, indent))
		}
	}
	return dst
}

// wrapLabelled draws one `label  value` line group. It reuses the ordinary wrap
// so a long value hangs under its label exactly as a bullet's text does.
func (p prose) wrapLabelled(dst []string, frags []fragment, lead, contd string, width, indent int) []string {
	return p.wrap(dst, frags, lead, contd, width, indent)
}

// -- fragment wrapping -------------------------------------------------------

// wrapped is one screen row's worth of fragments plus the plain width they
// occupy. The width is tracked rather than measured after painting, so padding
// is exact under every profile including NoColor.
type wrapped struct {
	frags []fragment
	width int
}

// fragWidth is the plain display width of a fragment run.
func fragWidth(frags []fragment) int {
	total := 0
	for _, f := range frags {
		total += blocks.Width(f.text)
	}
	return total
}

// wrapFragments greedily packs faced fragments into rows of at most width
// cells, breaking words longer than a whole row rather than dropping them.
func wrapFragments(frags []fragment, width int) []wrapped {
	if width < 1 {
		return nil
	}
	out := make([]wrapped, 0, 2)
	cur := wrapped{}
	pending := false
	push := func() {
		if len(cur.frags) > 0 {
			out = append(out, cur)
		}
		cur = wrapped{}
		pending = false
	}
	add := func(text string, f face, w int) {
		cur.frags = append(cur.frags, fragment{text, f})
		cur.width += w
	}
	for _, frag := range frags {
		for _, word := range splitWords(frag.text) {
			if word == " " {
				if cur.width > 0 {
					pending = true
				}
				continue
			}
			wordW := blocks.Width(word)
			need := wordW
			if pending {
				need++
			}
			if cur.width+need > width && cur.width > 0 {
				push()
				need = wordW
			}
			for cur.width+need > width && wordW > width {
				head, tail := cutCells(word, width-cur.width)
				if head == "" {
					break
				}
				add(head, frag.face, blocks.Width(head))
				push()
				word, wordW = tail, blocks.Width(tail)
				need = wordW
			}
			if word == "" {
				continue
			}
			if pending {
				add(" ", frag.face, 1)
				pending = false
			}
			add(word, frag.face, blocks.Width(word))
		}
	}
	push()
	return out
}

// paintFragments paints one wrapped row. base replaces [faceBody] so a header
// row is drawn at the promoted tier without the parse having to know which row
// it was building.
func (p prose) paintFragments(row wrapped, base face) string {
	var out strings.Builder
	for _, frag := range row.frags {
		f := frag.face
		if f == faceBody {
			f = base
		}
		out.WriteString(p.paint(frag.text, f))
	}
	return out.String()
}
