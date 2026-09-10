package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE GRID, PAINTED ───────────────────────────────────────────────────────
//
// What [homeView.buildGrid] laid out, drawn: each column's lines painted at the
// column's width, and the columns zipped side by side into the body's rows. The
// paint decides nothing about WHICH rows exist — that was settled when the lines
// were built — only what the cursor, the pointer and the one moving cell look
// like this frame.

// homeCellLine is one screen row of one column, and the line of home's list a
// pointer on it means (-1 for a row that opens nothing).
type homeCellLine struct {
	text string
	at   int
}

// homeGridGeometry is where each column starts and how wide it is. The columns
// are equal; what does not divide stays as air at the right edge.
func homeGridGeometry(width, cols int) (xs, widths []int) {
	cols = max(1, cols)
	each := max(1, (width-homeGridMargin-homeGridGutter*(cols-1))/cols)
	for c := 0; c < cols; c++ {
		xs = append(xs, homeGridMargin+c*(each+homeGridGutter))
		widths = append(widths, each)
	}
	return xs, widths
}

// homeGridRows is the resting body: the columns side by side, room rows tall,
// every row answering the pointer with the line each column holds on it.
//
// THE COLUMN EDGES ARE KEPT WHERE THE POINTER CAN FIND THEM ([homeView.gridX]),
// written by the draw for the reason every hit map on this screen is: a press
// resolves against what this frame actually drew.
func (a *app) homeGridRows(width, room int, pal palette) []placeRow {
	h := &a.home
	xs, widths := homeGridGeometry(width, h.grid.cols)
	h.gridX = xs
	columns := make([][]homeCellLine, len(xs))
	for at, line := range h.lines {
		if at >= len(h.grid.col) {
			break
		}
		c := min(h.grid.col[at], len(xs)-1)
		columns[c] = append(columns[c], a.homeLineRows(line, at, widths[c], pal, h.marksPanel(at))...)
	}
	rows := make([]placeRow, room)
	for y := range rows {
		rows[y] = homeGridZip(columns, y, xs)
	}
	return rows
}

// homeGridZip is one body row: each column's row at its x, and the mark that
// says which line of the list each column drew there.
func homeGridZip(columns [][]homeCellLine, y int, xs []int) placeRow {
	mark := homeMark{line: -1, pane: -1, grid: true}
	for c := range mark.cells {
		mark.cells[c] = -1
	}
	var b strings.Builder
	used := 0
	for c, column := range columns {
		if y >= len(column) {
			continue
		}
		mark.cells[c] = column[y].at
		if column[y].text == "" {
			continue
		}
		b.WriteString(strings.Repeat(" ", max(0, xs[c]-used)))
		b.WriteString(column[y].text)
		used = max(used, xs[c]) + ansi.StringWidth(column[y].text)
	}
	// AND THE ROW'S ONE LINE, for every reader that asks a row for one line, is
	// the first column's — the column a one-column home is entirely.
	mark.line = mark.cells[0]
	return placeRow{text: b.String(), hit: mark}
}

// homeLineRows paints one line of the list at one column's width.
func (a *app) homeLineRows(line homeLine, at, width int, pal palette, heading bool) []homeCellLine {
	h := &a.home
	hit := -1
	if line.stop() {
		hit = at
	}
	lit := at == h.cursor || at == h.hover
	if line.cell == nil {
		if line.kind == homeExchangeRow {
			return []homeCellLine{{text: a.exchangeRowLine(line, at, width, pal), at: hit}}
		}
		return []homeCellLine{{at: -1}}
	}
	cell := line.cell
	var texts []string
	switch cell.kind {
	case cellHead:
		texts = []string{homeCellHead(cell, width, pal, heading)}
	case cellWhisper, cellFold:
		texts = []string{homeCellQuiet(cell, width, pal, lit)}
	case cellBar:
		texts = []string{homeCellBand(homeCellLeadBlank+homeSpendBar(cell.share, width-homeGridLead, pal), width, pal, lit)}
	case cellSpark:
		texts = []string{homeCellBand(homeCellLeadBlank+homeSpendSpark(cell, width-homeGridLead, pal), width, pal, lit)}
	default:
		texts = a.homeCellRow(line, at, width, pal, lit)
	}
	out := make([]homeCellLine, 0, len(texts))
	for _, text := range texts {
		out = append(out, homeCellLine{text: text, at: hit})
	}
	return out
}

// marksPanel reports that the line at `at` is the heading of the panel the
// cursor is standing in — THE ONE HEADING A FRAME MARKS (docs/DESIGN-LANGUAGE.md,
// "the section holding the cursor marks its own heading"). It follows the
// keyboard only: [homeView.hover] never enters into it.
func (h *homeView) marksPanel(at int) bool {
	if at < 0 || at >= len(h.lines) {
		return false
	}
	cell := h.lines[at].cell
	panel, ok := h.cursorPanel()
	return ok && cell != nil && cell.kind == cellHead && cell.panel == panel
}

// homeCellLeadBlank is the lead of a row that wears no mark.
var homeCellLeadBlank = strings.Repeat(" ", homeGridLead)

// homeCellHead is a panel's heading: its word in the muted tier, its clause at
// the right margin dim.
//
// A HEADING IS NEVER LIT. The panel the cursor is standing in says so with the
// cursor step's ground on its heading, and the words stay where they were
// (docs/DESIGN-LANGUAGE.md, "the section holding the cursor marks its own
// heading") — one heading per frame, following the keyboard only.
func homeCellHead(cell *homeCell, width int, pal palette, marked bool) string {
	text := switcherSides(width, cell.title, cell.right, pal.muted, pal.dim)
	if marked {
		return pal.cursor(text, width)
	}
	return text
}

// homeCellQuiet is a whisper or a fold: dim words under the rows' own lead.
func homeCellQuiet(cell *homeCell, width int, pal palette, lit bool) string {
	text := homeCellLeadBlank + pal.dim(fit(cell.title, max(0, width-homeGridLead)))
	return homeCellBand(text, width, pal, lit && cell.kind == cellFold)
}

// homeCellBand is the one ground a stop wears: the cursor's, and the pointer's
// at the same step.
func homeCellBand(text string, width int, pal palette, lit bool) string {
	if !lit {
		return text
	}
	return pal.cursor(text, width)
}

// homeCellRow paints a row and the line under it.
func (a *app) homeCellRow(line homeLine, at, width int, pal palette, lit bool) []string {
	cell := line.cell
	body := homeCellBody(a.homeCellDoor(cell, at, width-homeGridLead), width-homeGridLead, pal, lit)
	rows := []string{homeCellBand(a.homeCellLead(cell, at, pal)+body, width, pal, lit)}
	if cell.sub != "" {
		under := switcherSides(max(1, width-homeGridLead), cell.sub, a.homeRowAnswers(line), pal.dim, pal.muted)
		rows = append(rows, homeCellLeadBlank+under)
	}
	return rows
}

// homeCellLead is the row's mark and the air after it, or two blank cells.
//
// TWO MARKS AND NO OTHER (law 8). The question mark in the warn hue on a row
// waiting for a person, and the ONE moving cell on the first running row — the
// spinner where the frame animates, the still working mark where it does not —
// or on the conversation being moved here, which takes it outright
// ([homeView.spinAt]).
func (a *app) homeCellLead(cell *homeCell, at int, pal palette) string {
	if spin := a.homeSpinCell(at); spin != "" && cell.mark != cellMarkNeeds {
		return pal.accent(spin) + " "
	}
	switch cell.mark {
	case cellMarkNeeds:
		return pal.warn(pal.glyph(tokens.GNeedsHuman)) + " "
	case cellMarkSpin:
		spin := a.homeSpinCell(at)
		if spin == "" {
			spin = pal.glyph(tokens.GWorking)
		}
		return pal.accent(spin) + " "
	}
	return homeCellLeadBlank
}

// homeCellBody is a row after its lead: the title, a note in the title's
// shadow, and the facts at the right margin.
//
// THE TITLE IS WHOLE BEFORE ANY FACT GETS A CELL (rowfit.go's law 1). The note
// gives way first, then the tag, then the right-hand word, and only a title that
// will not fit alone is cut.
func homeCellBody(cell *homeCell, width int, pal palette, lit bool) string {
	if width < 1 {
		return ""
	}
	title, note, tag, right := cell.title, cell.note, cell.tag, cell.right
	facts := []*string{&note, &tag, &right}
	if cell.hold {
		facts = facts[:2]
	}
	for _, fact := range facts {
		if homeCellWidth(title, cell.pad, note, tag, right) <= width {
			break
		}
		*fact = ""
	}
	if over := homeCellWidth(title, cell.pad, note, tag, right) - width; over > 0 {
		title = fit(title, max(1, ansi.StringWidth(title)-over))
	}
	titleInk, factInk := pal.ink, pal.dim
	if cell.bold || lit {
		titleInk = func(s string) string { return pal.bold(pal.ink(s)) }
	}
	if lit {
		factInk = pal.ink
	}
	line := titleInk(title)
	used := ansi.StringWidth(title)
	if note != "" {
		gap := max(0, cell.pad-used) + len(homeCellGap)
		line += strings.Repeat(" ", gap) + factInk(note)
		used += gap + ansi.StringWidth(note)
	}
	tail := homeCellTail(tag, right)
	if tail == "" {
		return line
	}
	return line + strings.Repeat(" ", max(1, width-used-ansi.StringWidth(tail))) + factInk(tail)
}

// homeCellDoor is the row UNDER THE CURSOR growing its held word into the door
// it offers — `another window · enter brings it here` — where the whole title
// still fits beside the whole clause, and never while a question this window
// raised about the row is already on the screen (takeovervoice.go's
// [takeoverHeldDoorWord], the list's own rule before the grid).
func (a *app) homeCellDoor(cell *homeCell, at, width int) *homeCell {
	if cell.door == "" || at != a.home.cursor || a.home.ask != nil {
		return cell
	}
	grown := *cell
	grown.right = cell.door
	if homeCellWidth(grown.title, grown.pad, "", grown.tag, grown.right) > width {
		return cell
	}
	return &grown
}

// homeCellGap is the air between two clauses of one row that are not joined by
// a separator: a title and its note, a tag and its right-hand word.
const homeCellGap = "  "

// homeCellTail is the right margin's words.
func homeCellTail(tag, right string) string {
	switch {
	case tag == "":
		return right
	case right == "":
		return tag
	}
	return tag + homeCellGap + right
}

// homeCellWidth is what a row's parts take side by side.
func homeCellWidth(title string, pad int, note, tag, right string) int {
	n := ansi.StringWidth(title)
	if note != "" {
		n = max(n, pad) + len(homeCellGap) + ansi.StringWidth(note)
	}
	if tail := homeCellTail(tag, right); tail != "" {
		n += 1 + ansi.StringWidth(tail)
	}
	return n
}
