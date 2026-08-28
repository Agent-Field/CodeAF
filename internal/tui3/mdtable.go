package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── TABLES THAT OPEN ────────────────────────────────────────────────────────
//
// A TABLE THAT WAS CUT IS A TABLE THE READER CANNOT FINISH READING, AND UNTIL
// NOW THE ONLY WAY TO THE REST OF IT WAS THE RAW REPLY.
//
// internal/tui2/prose fits a table by TRUNCATING its cells, and that decision
// is right and stays: a table's value is the comparison down its columns, a
// wrapped table has no columns, and prose/table.go argues both at length. What
// the decision leaves behind is the case it names and does not answer — a grid
// of ellipses, with the words still in the source and no door to them.
//
// So the surface draws the door. A table whose cells did not fit grows one dim
// row under it, and pressing that row lays the same table out with its cells
// WRAPPED instead of cut. Pressing it again tucks the table back.
//
// THE DOOR ONLY APPEARS WHERE THE PROBLEM DID. A table that fits its column
// renders exactly as it always has, with nothing underneath: an affordance for
// a problem that did not happen is noise, and a row spent on a possibility is a
// row not spent on the conversation.
//
// ── THE CLOSED RENDER IS PROSE'S, BYTE FOR BYTE ──
//
// [renderMarkdown] is a PURE FUNCTION of (text, width) and stays one — every
// tier above the phone renders exactly what prose renders, and
// markdownwrap_test.go's TestWideTiersUnchanged pins that byte for byte. So
// none of this happens inside it. The document is rendered whole, exactly as
// before, and only THEN is each table located in the finished rows: closed, it
// keeps prose's own bytes and gains a foot; open, its rows are swapped for this
// file's.
//
// The location is not guessed. A top-level table rendered ALONE at the same
// width produces exactly the rows prose gave it inside the document — a table
// lays out in the figure width, the figure width is the full width less the
// container gutter, and a table at column zero is in no container (mdSegments
// recognizes no other kind). So the table is FOUND: the isolated rendering is
// looked for as a run of rows, searching forward from where the last table
// ended, and A TABLE THIS FILE CANNOT FIND GETS NO FOOT. Every way this can be
// wrong therefore ends in the rendering the surface had yesterday, which is the
// property that lets a splice sit under a pinned test at all.
//
// ── OPENING IS A LAYOUT DECISION, NOT A SETTING ──
//
// The person asked to see the cells; they did not ask to choose a layout. So
// the surface picks the honest one for the width it has:
//
//   - WRAPPED COLUMNS, when every column can hold its widest word. The same
//     grid, the same gap, the same hairline under the header — a cell that is
//     too long becomes several rows inside its own column instead of an
//     ellipsis. A record that spans rows is separated from the next by a blank
//     one, because at that point the eye needs to be told where a record stops.
//   - STACKED RECORDS, when it cannot: `header: value` a line at a time, which
//     is what the phone tier already does with every table ([phoneTableRows])
//     and for the same reason — under some width a grid stops being a grid, and
//     a column three cells wide is a column of confetti.
//
// ── AND IT IS ALSO HOW YOU COPY A TABLE ──
//
// Copy mode yanks the RENDERED rows (copymode.go), so an opened table is the
// only way to put a table's real content on the clipboard: what a closed one
// offers is the ellipses the reader can already see.
//
// ── THE KEYBOARD ──
//
// There is no chord for this wave, and that is a stated gap rather than an
// oversight. Every other pressable thing on this surface is reachable from a
// cursor that walks blocks ([app.selectTool]), and a table is not a block that
// walk visits — prose is not selectable here. The words are not lost to a
// person with ui.mouse off either: the model's reply is still in the transcript
// whole, which is exactly where they were before this file existed.

const (
	// mdTableGap is the whitespace between two columns of an OPENED table.
	//
	// It is two cells because prose spends two (prose/table.go's tableGap), and
	// it is restated here because that constant is unexported. THE TWO HAVE TO
	// AGREE: a table that changed the distance between its columns when it
	// opened would read as a second table rather than as the same one, and the
	// gesture claims to be showing the reader what they were already looking at.
	mdTableGap = 2
	// mdOpenFloor is the narrowest column a WRAPPED layout is worth having.
	//
	// Under it a column takes nearly every word onto a row of its own, which
	// costs a record ten rows to say what a stacked record says in three — so
	// below this floor the honest answer is the stacked one. A column that never
	// wanted this much (a column of "opus" and "sonnet") is not held to it: the
	// floor is a minimum ASKED FOR, not a minimum granted.
	mdOpenFloor = 8
)

// The two words the foot says, and they are the whole vocabulary of this
// control.
//
// THEY NAME WHAT THE PRESS DOES, NOT WHAT THE SURFACE IS. "expand", "collapse"
// and "wrap mode" are machinery talking about itself; a person looking at a row
// of ellipses wants the table opened, and says so in those words. The ellipsis
// leading each one is the same glyph that cut the cells (prose's own overflow
// mark, and the transcript's "… N more lines" foot) — the mark of the thing
// that was held back, standing in front of the offer to release it.
const (
	mdOpenWord = glyphMore + " open the table"
	mdTuckWord = glyphMore + " tuck the table back"
)

// tableFoot is one drawn affordance: the columns it occupies on its row, and
// which of the entry's tables it opens.
//
// It is carried on a [row] for the reason a [taskLink] is — it is resolved by
// COLUMN and not by row, and the geometry is written where it is decided,
// because a hit-test that recomputed it would be measuring a row the frame has
// not drawn.
type tableFoot struct {
	span  hudSpan
	table int
}

// mdAlign is a column's alignment, read off the table's own delimiter row. The
// three GFM writes down, and nothing else.
type mdAlign uint8

const (
	mdAlignLeft mdAlign = iota
	mdAlignRight
	mdAlignCenter
)

// settledMarkdown is a finished answer's rows: prose's rendering of the whole
// document, with every table that was cut given a foot to open it.
//
// The tier check is the phone's own exemption. A phone-width table is already
// stacked by [phoneMarkdown] — every cell of it is on screen whole — so there
// is nothing to open, and a foot under it would be an offer to do what has been
// done.
func (a *app) settledMarkdown(at int, e *entry, width int) []string {
	rows := trimBlanks(a.renderMarkdown(e.text, width))
	e.feet = nil
	if layoutTier(width) == tierPhone || !strings.Contains(e.text, "|") {
		return rows
	}
	out, feet := a.openableTables(a.styler(), at, e, rows, width)
	e.feet = feet
	return out
}

// openableTables is the splice: prose's finished rows in, the same rows with
// each cut table's foot (and, where the person asked, its opened body) out.
//
// It returns the rows UNTOUCHED whenever it has nothing to say, and everything
// it does not splice is copied verbatim — which is the whole of the byte
// identity claim. A closed table is prose's own bytes, moved.
//
// The ordinal is counted over EVERY table in the document, including the ones
// this pass cannot find. It has to be: the ordinal is what a person's choice is
// remembered against ([entry.tables]), and a numbering that skipped a table at
// one width and not at another would hand a resize somebody else's table.
func (a *app) openableTables(st *tokens.Styler, at int, e *entry, rows []string, width int) ([]string, map[int]tableFoot) {
	var (
		out    []string
		feet   map[int]tableFoot
		copied int // how much of rows is already in out
		seek   int // where the search for the next table starts
	)
	ordinal := -1
	for _, seg := range mdSegments(e.text) {
		if seg.kind != mdSegTable {
			continue
		}
		ordinal++
		drawn := proseRowsWithCode(st, seg.src, width, a.plainCodePath)
		from := mdRunAt(rows, drawn, seek)
		if from < 0 {
			continue
		}
		end := from + len(drawn)
		seek = end
		open := e.tables[ordinal]
		// A TABLE THAT FITS IS A TABLE WITH NOTHING BEHIND IT. The question is
		// asked of prose rather than answered here: the same source at a width
		// nothing can overflow renders the same rows if and only if no column was
		// squeezed. An OPEN table keeps its foot whatever the width, because the
		// foot is the only way back from a choice the person made.
		if !open && mdTableFits(st, seg.src, width, drawn, a.plainCodePath) {
			continue
		}
		if open {
			out = append(out, rows[copied:from]...)
			out = append(out, mdOpenTable(st, seg, width, a.plainCodePath)...)
		} else {
			out = append(out, rows[copied:end]...)
		}
		copied = end

		word, paint := mdOpenWord, a.pal.dim
		if open {
			word = mdTuckWord
		}
		if a.hoveringTable(at, ordinal) {
			// ACCENT IS THE HOVER, not a background band — the jump chip's law
			// (jumpchip.go), and for its reason: this is three words at the left
			// edge of a row that is otherwise empty, and a highlighted rectangle
			// around them would be the one boxed thing on a surface with no boxes.
			paint = a.pal.accent
		}
		if feet == nil {
			feet = map[int]tableFoot{}
		}
		feet[len(out)] = tableFoot{span: hudSpan{from: 0, to: ansi.StringWidth(word)}, table: ordinal}
		// THE FOOT IS THE SURFACE'S OWN CHROME and wears the surface's own ramp,
		// while the table above it is prose's and wears the token layer's. That is
		// not a slip: everything in the block the model wrote is painted by the one
		// renderer that owns its colour, and the row this file ADDED is a control,
		// which is the same dim the "… N more lines" foot spends (toolview.go).
		out = append(out, paint(word))
	}
	if out == nil {
		return rows, nil
	}
	return append(out, rows[copied:]...), feet
}

// mdRunAt is the first index at or after `from` where `want` sits in `rows` as
// a contiguous run, or -1.
func mdRunAt(rows, want []string, from int) int {
	if len(want) == 0 || from < 0 {
		return -1
	}
	for i := from; i+len(want) <= len(rows); i++ {
		match := true
		for j := range want {
			if rows[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// mdTableFits reports whether prose drew this table at its natural size —
// whether, that is, no cell was cut.
//
// It asks by RE-RENDERING at a width nothing can overflow and comparing. The
// alternative is measuring the cells here and re-deriving prose's fitter from
// the outside, which would be a second answer to "does this table fit" and
// would disagree with the first the week the fitter was tuned.
func mdTableFits(st *tokens.Styler, src string, width int, drawn []string, plainCodeSpan func(string) bool) bool {
	probe := proseRowsWithCode(st, src, mdProbeWidth(src, width), plainCodeSpan)
	if len(probe) != len(drawn) {
		return false
	}
	for i := range probe {
		if probe[i] != drawn[i] {
			return false
		}
	}
	return true
}

// mdProbeWidth is a width no rendering of this source can fill.
//
// The bound is the SOURCE's own length rather than a large constant, because a
// constant large enough for the widest table anybody sends is a constant that
// allocates that many cells for every table nobody does. Two cells per byte is
// slack enough for anything markdown can turn one byte into.
func mdProbeWidth(src string, width int) int {
	probe := 2*len(src) + 8
	if probe < width {
		return width
	}
	return probe
}

// mdOpenTable is an opened table's rows: the same grid with its cells wrapped,
// or stacked records where a grid can no longer be one.
func mdOpenTable(st *tokens.Styler, seg mdSegment, width int, plainCodeSpan func(string) bool) []string {
	cells := mdFlatCells(st, seg.table, plainCodeSpan)
	if len(cells) == 0 {
		// A table this file could not take apart opens as records, which is the
		// layout that needs no measurements at all.
		return phoneTableRows(st, seg, width, plainCodeSpan)
	}
	cols := len(cells[0])
	avail := width - mdTableGap*(cols-1)
	if avail < cols {
		return phoneTableRows(st, seg, width, plainCodeSpan)
	}

	natural, need := make([]int, cols), make([]int, cols)
	for _, record := range cells {
		for j, cell := range record {
			if w := ansi.StringWidth(cell); w > natural[j] {
				natural[j] = w
			}
			if w := mdLongestWord(cell); w > need[j] {
				need[j] = w
			}
		}
	}
	total := 0
	for j := range need {
		if natural[j] < 1 {
			natural[j] = 1
		}
		// What a column ASKS FOR is its widest word, or the floor, whichever is
		// larger — and never more than it actually wants.
		if need[j] < mdOpenFloor {
			need[j] = mdOpenFloor
		}
		if need[j] > natural[j] {
			need[j] = natural[j]
		}
		total += need[j]
	}
	if total > avail {
		// Even at its widest word every column would not fit. Records, then: the
		// grid is over, and pretending otherwise costs the reader the words.
		return phoneTableRows(st, seg, width, plainCodeSpan)
	}

	widths := mdOpenWidths(natural, need, avail)
	align := mdAligns(seg.src, cols)
	head, records := cells[0], cells[1:]

	// A GAP IS FOR A RECORD THAT SPANS ROWS. Where every record is still one row
	// the grid reads as a grid and needs no help; a blank line between single
	// rows would be spacing a problem nobody has.
	body := make([][]string, 0, len(records))
	spaced := false
	for _, record := range records {
		rows := mdGridRows(record, widths, align)
		if len(rows) > 1 {
			spaced = true
		}
		body = append(body, rows)
	}

	out := make([]string, 0, len(records)+4)
	for _, line := range mdGridRows(head, widths, align) {
		out = append(out, mdPaint(st, line, tokens.TextSecondary))
	}
	rule := mdTableGap * (cols - 1)
	for _, w := range widths {
		rule += w
	}
	out = append(out, mdPaint(st, strings.Repeat(tokens.GlyphTreeDash, rule), tokens.TextTertiary))
	for i, rows := range body {
		if spaced && i > 0 {
			out = append(out, "")
		}
		for _, line := range rows {
			out = append(out, mdPaint(st, line, tokens.TextPrimary))
		}
	}
	return out
}

// mdOpenWidths shares the room out: every column gets what it asks for, and
// what is left over goes to the columns that wanted more.
//
// It is [fitColumns]'s max-min fair sharing (prose/table.go) with one thing
// changed — the floor is the column's WIDEST WORD rather than three cells,
// because a column that cannot hold its longest word cannot wrap, it can only
// break words in half. The binary search is that file's too, and the caller has
// already established that the floors fit.
func mdOpenWidths(natural, need []int, avail int) []int {
	widest := 0
	for _, n := range natural {
		if n > widest {
			widest = n
		}
	}
	spend := func(cap int) int {
		sum := 0
		for j, n := range natural {
			sum += mdColWidth(n, need[j], cap)
		}
		return sum
	}
	lo, hi := 1, widest
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if spend(mid) <= avail {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	out := make([]int, len(natural))
	used := 0
	for j, n := range natural {
		out[j] = mdColWidth(n, need[j], lo)
		used += out[j]
	}
	// Whatever the cap left over goes back, one cell at a time, to the columns
	// still pressed against it.
	for j := 0; used < avail && j < len(out); j++ {
		if out[j] < natural[j] {
			out[j]++
			used++
			j--
		}
	}
	return out
}

// mdColWidth is one column under a cap: never less than it needs, never more
// than it wants.
func mdColWidth(natural, need, cap int) int {
	w := natural
	if w > cap {
		w = cap
	}
	if w < need {
		w = need
	}
	return w
}

// mdGridRows lays one record out across as many rows as its tallest cell takes.
//
// Every cell is wrapped inside its own column and padded to it, so the columns
// stay columns down the whole record — which is the one thing prose refused to
// give up, and the one thing this layout must not give up either. The trailing
// run is trimmed for the reason prose trims its own: no row is ever handed to a
// terminal with whitespace past its last word.
func mdGridRows(record []string, widths []int, align []mdAlign) []string {
	lines := make([][]string, len(widths))
	tall := 1
	for j := range widths {
		lines[j] = mdWrapCell(mdCellAt(record, j), widths[j])
		if len(lines[j]) > tall {
			tall = len(lines[j])
		}
	}
	out := make([]string, 0, tall)
	for r := 0; r < tall; r++ {
		var b strings.Builder
		for j, w := range widths {
			if j > 0 {
				b.WriteString(strings.Repeat(" ", mdTableGap))
			}
			part := ""
			if r < len(lines[j]) {
				part = lines[j][r]
			}
			b.WriteString(mdAlignCell(part, w, align[j]))
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

// mdWrapCell breaks one cell into the rows of its column. It is the surface's
// own wrapper ([wrap]'s, one measure down) — words where there are words, and
// mid-token where a token is wider than the column, because a forty-cell URL in
// a twelve-cell column has no break in it and the choice there is between a
// broken token and a lost one.
func mdWrapCell(cell string, width int) []string {
	if width < 1 {
		width = 1
	}
	if cell == "" {
		return []string{""}
	}
	out := strings.Split(ansi.Wrap(cell, width, ""), "\n")
	for i, line := range out {
		// THE COLUMN IS A CEILING. The wrapper is trusted and this is the guard
		// that says so out loud: a row over its width breaks the frame around it,
		// which is the one failure the whole markdown slice exists to prevent.
		if ansi.StringWidth(line) > width {
			out[i] = ansi.Truncate(line, width, "")
		}
	}
	return out
}

// mdAlignCell places one wrapped line in its column, honouring what the table's
// delimiter row asked for. A column of figures that came back left-aligned
// because it had been opened would be a different table.
func mdAlignCell(line string, width int, a mdAlign) string {
	slack := width - ansi.StringWidth(line)
	if slack < 0 {
		slack = 0
	}
	switch a {
	case mdAlignRight:
		return strings.Repeat(" ", slack) + line
	case mdAlignCenter:
		return strings.Repeat(" ", slack/2) + line + strings.Repeat(" ", slack-slack/2)
	default:
		return line + strings.Repeat(" ", slack)
	}
}

// mdAligns reads the alignments out of a table's delimiter row.
func mdAligns(src string, cols int) []mdAlign {
	out := make([]mdAlign, cols)
	lines := strings.Split(src, "\n")
	if len(lines) < 2 {
		return out
	}
	for j, cell := range mdCells(lines[1]) {
		if j >= cols {
			break
		}
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			out[j] = mdAlignCenter
		case right:
			out[j] = mdAlignRight
		}
	}
	return out
}

// mdFlatCells is the table's cells as PROSE would print them, plain.
//
// A cell in the source is markdown — `**opus**`, a link, a code span — and the
// closed table shows what that markdown SAYS, because prose flattens every cell
// on its way into the grid (prose/table.go's readTable). An opened table has to
// show the same words, and the only honest way to know them is to ask the same
// renderer rather than to grow a second inline parser here — which is exactly
// what markdown.go's opening comment refuses to do, for exactly this reason.
//
// So each column is handed back to prose ALONE, as a one-column table at a
// width nothing can overflow, and what comes back is that column's cells, one
// per row, already flattened. The paint is stripped: an opened table wears one
// style per row, as prose's own table does, and the tint is put on afterwards.
//
// It returns nil when the answer does not have the shape it asked for — a table
// this file split differently than goldmark did — and the caller falls back to
// the layout that needs no cells measured.
func mdFlatCells(st *tokens.Styler, table [][]string, plainCodeSpan func(string) bool) [][]string {
	if len(table) == 0 || len(table[0]) == 0 {
		return nil
	}
	cols := len(table[0])
	out := make([][]string, len(table))
	for i := range out {
		out[i] = make([]string, cols)
	}
	for j := 0; j < cols; j++ {
		var src strings.Builder
		for i, record := range table {
			src.WriteString("| " + strings.ReplaceAll(mdCellAt(record, j), "|", "\\|") + " |\n")
			if i == 0 {
				// The delimiter row is written WITHOUT alignment, so nothing comes
				// back padded: the alignment is this file's to apply, once, at the
				// width the column ends up with.
				src.WriteString("| --- |\n")
			}
		}
		text := src.String()
		rows := proseRowsWithCode(st, text, mdProbeWidth(text, 1), plainCodeSpan)
		// A header, a hairline, and one row per record. Anything else means the
		// source this file wrote was not read back as the table it meant.
		if len(rows) != len(table)+1 {
			return nil
		}
		for i := range table {
			at := i
			if i > 0 {
				at = i + 1
			}
			out[i][j] = ansi.Strip(rows[at])
		}
	}
	return out
}

// mdCellAt is a record's cell in a column, or "" where the row was short. GFM
// pads a short row and drops the cells past the header's width, and both halves
// of that are what a reader of the closed table already saw.
func mdCellAt(record []string, j int) string {
	if j < len(record) {
		return record[j]
	}
	return ""
}

// mdLongestWord is the widest run of non-space cells in a string: the narrowest
// column this text can be wrapped into without breaking a word in half.
func mdLongestWord(s string) int {
	longest := 0
	for _, word := range strings.Fields(s) {
		if w := ansi.StringWidth(word); w > longest {
			longest = w
		}
	}
	return longest
}

// mdPaint is [mdInk] for a stated token: an empty row is left empty, and a nil
// Styler — the headless default prose itself accepts — draws the row unpainted.
func mdPaint(st *tokens.Styler, s string, tok tokens.Token) string {
	if st == nil || s == "" {
		return s
	}
	return st.PaintToken(s, tok)
}

// toggleTable opens or tucks back one table of one answer, in whichever list is
// on screen.
//
// The state is a map on the entry and nil means every table is closed, which is
// what almost every answer is: a reply with no table in it never allocates one.
// The rows are marked stale by hand because they are CACHED (render.go's
// [app.entryRows]) — the same exchange [app.toggleThought] makes.
func (a *app) toggleTable(i, ordinal int) bool {
	es := a.bodyDeck().entries
	if i < 0 || i >= len(es) || es[i].kind != entryAssistant {
		return false
	}
	e := &es[i]
	if e.tables == nil {
		e.tables = map[int]bool{}
	}
	e.tables[ordinal] = !e.tables[ordinal]
	e.stale = true
	// A page's rows are cached as a LIST rather than per entry (room.go), so the
	// staleness above cannot reach them.
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
	return true
}

// footPress resolves a click on a table's foot, and reports whether it took
// one.
//
// It is read beside [app.linkPress] because both are targets INSIDE a row of
// the model's prose, and it is read BEFORE the row's own answer for that
// reason: on a node's page a paragraph row means "leave", and a foot that fell
// through would close the room instead of opening the table under the pointer.
//
// A press in the empty cells beside the words is not a press on the foot. The
// affordance is the phrase and nothing else, exactly as a task reference is —
// the rest of the row is the blank margin the table ended in.
func (a *app) footPress(x int, r row) bool {
	if !r.foot.span.holds(x) || a.welcome.open {
		return false
	}
	return a.toggleTable(r.entry, r.foot.table)
}

// hoveringTable reports whether the pointer is on this table's foot (hover.go).
func (a *app) hoveringTable(i, ordinal int) bool {
	return a.hot.kind == hoverTable && a.hot.entry == i && a.hot.index == ordinal
}
