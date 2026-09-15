package tui3

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE GRID, PAINTED ───────────────────────────────────────────────────────
//
// What [homeView.buildGrid] laid out, drawn: each column's lines painted at the
// column's width, and the columns zipped side by side into the body's rows. The
// paint decides nothing about WHICH rows exist — that was settled when the lines
// were built — only what the cursor, the pointer and the one moving cell look
// like this frame.

// homeCellLine is one screen row of one column, the line of home's list a
// pointer on it means (-1 for a row that opens nothing), and the heading line
// it is (-1 for every row that is not a panel's heading).
type homeCellLine struct {
	text string
	at   int
	head int
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
	if at := homeDescCol(h.grid.cols); at != homeNoLine && at < len(columns) {
		columns[at] = a.homeDescLines(widths[at], room, pal, columns[0])
	}
	rows := make([]placeRow, room)
	for y := range rows {
		rows[y] = homeGridZip(columns, y, xs)
	}
	return rows
}

// homeDescLines is the middle column: what the rows have to say about
// themselves, each sentence standing ON THE LINE OF THE ROW IT BELONGS TO.
//
// TWO KINDS SHARE IT. A `needs you` question is drawn WHETHER OR NOT its row is
// selected ([homeCell.alwaysSaid]) — the panel exists so a person reads what is
// waiting on them at a glance. Every other row's sentence is drawn only while it
// is the row under the cursor, because a gloss on forty rows at once is a wall.
//
// ALIGNMENT IS WHAT MAKES THE TWO LEGIBLE TOGETHER. Each note starts on its own
// row's line, so which row a sentence is about is a fact about where it is
// rather than something the reader works out, and a note runs down only as far
// as the next note's row so two of them never overlap.
//
// IT IS BUILT AT PAINT TIME AND NOT AT BUILD TIME, which is the whole reason it
// can follow the cursor: the grid's lines are settled when the room or the width
// moves ([homeView.buildGrid]) and an arrow key moves neither, so a column
// assembled up there would answer about whichever row the cursor happened to be
// on when the frame was last rebuilt.
//
// IT FOLLOWS THE POINTER TOO, through the same [homeView.previewLine] the card
// beside the search has always used.
//
// NOTHING IN IT IS A STOP. The column holds no row a cursor may stand on, which
// is what makes `→` step over it to the rail ([homeView.gridCrossTarget]) rather
// than parking the cursor on a sentence about the row it just left.
func (a *app) homeDescLines(width, room int, pal palette, field []homeCellLine) []homeCellLine {
	h := &a.home
	if room <= 0 || width <= homeGridLead {
		return nil
	}
	room = min(room, len(field))
	preview := h.previewAt()
	type note struct {
		y     int
		words []string
	}
	var notes []note
	for y := 0; y < room; y++ {
		at := field[y].at
		if at == homeNoLine || at < 0 || at >= len(h.lines) {
			continue
		}
		line := h.lines[at]
		if line.cell == nil {
			continue
		}
		said := strings.TrimSpace(line.cell.sub)
		selected := at == preview
		if said == "" || (!line.cell.alwaysSaid() && !selected) {
			continue
		}
		notes = append(notes, note{y: y, words: a.homeDescNote(line, at, said, width, selected, pal)})
	}
	if len(notes) == 0 {
		return nil
	}
	out := make([]homeCellLine, room)
	for i := range out {
		out[i] = homeCellLine{at: homeNoLine, head: -1}
	}
	for i, n := range notes {
		// A NOTE RUNS DOWN ONLY AS FAR AS THE NEXT ONE'S ROW. The rest of a
		// sentence that does not fit is dropped rather than drawn over somebody
		// else's row: the column's whole promise is that a line belongs to the
		// row beside it.
		stop := room
		if i+1 < len(notes) {
			stop = notes[i+1].y
		}
		for j, words := range n.words {
			if n.y+j >= stop {
				break
			}
			out[n.y+j] = homeCellLine{at: homeNoLine, head: -1, text: words}
		}
	}
	return out
}

// homeDescNote is one row's note as the lines it takes.
//
// THE SELECTED ROW'S IS WRAPPED AND A PERMANENT ONE IS NOT. The row a person is
// on is the one they are reading, and it gets the room; a `needs you` question
// standing over other rows keeps the one line it had under its row, cut the way
// the row cut it, with its key at the right where the row put it.
func (a *app) homeDescNote(line homeLine, at int, said string, width int, selected bool, pal palette) []string {
	room := max(1, width-homeGridLead)
	answers := strings.TrimSpace(a.homeRowAnswers(line, at))
	if !selected {
		return []string{homeCellLeadBlank + switcherSides(room, said, answers, pal.dim, pal.muted)}
	}
	var out []string
	for _, words := range wrap(said, room) {
		out = append(out, homeCellLeadBlank+pal.dim(words))
	}
	if answers != "" {
		out = append(out, "", homeCellLeadBlank+paintHint(answers, pal, pal.dim))
	}
	return out
}

// homeGridZip is one body row: each column's row at its x, and the mark that
// says which line of the list each column drew there.
func homeGridZip(columns [][]homeCellLine, y int, xs []int) placeRow {
	mark := homeMark{line: -1, pane: -1, grid: true}
	for c := range mark.cells {
		mark.cells[c], mark.heads[c] = -1, -1
	}
	var b strings.Builder
	used := 0
	for c, column := range columns {
		if y >= len(column) {
			continue
		}
		mark.cells[c], mark.heads[c] = column[y].at, column[y].head
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
			return []homeCellLine{{text: a.exchangeRowLine(line, at, width, pal), at: hit, head: -1}}
		}
		return []homeCellLine{{at: -1, head: -1}}
	}
	cell := line.cell
	head := -1
	var texts []string
	switch cell.kind {
	case cellHead:
		texts, head = []string{homeCellHead(cell, width, pal, heading)}, at
	case cellWhisper, cellFold:
		texts = []string{homeCellQuiet(cell, width, pal, lit)}
	case cellGroup:
		texts = []string{homeCellGroup(cell, width, pal)}
	case cellBar:
		texts = []string{homeCellBand(homeCellLeadBlank+homeSpendMeter(cell.share, width-homeGridLead, pal), width, pal, lit)}
	case cellSpark:
		texts = []string{homeCellBand(homeCellLeadBlank+homeSpendSpark(cell, width-homeGridLead, pal), width, pal, lit)}
	case cellFacts:
		texts = []string{homeCellBand(homeCellLeadBlank+homeSpendFacts(cell, width-homeGridLead, pal), width, pal, lit)}
	default:
		texts = a.homeCellRow(line, at, width, pal, lit)
	}
	out := make([]homeCellLine, 0, len(texts))
	for _, text := range texts {
		out = append(out, homeCellLine{text: text, at: hit, head: head})
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

// homeCellHead is a panel's heading: its word in the places' one heading ink
// (placeprose.go's [placeHeadingInk]), its explainer beside it dim, its clause at
// the right margin dim.
// HOME AND THE PLACES READ THEIR HEADINGS FROM ONE LINE, because `tab` from home
// into a place crosses no seam only while a section word is the same furniture
// on both sides of it.
//
// A HEADING IS NEVER LIT. The panel the cursor is standing in says so with the
// cursor step's ground on its heading, and the words stay where they were
// (docs/DESIGN-LANGUAGE.md, "the section holding the cursor marks its own
// heading") — one heading per frame, following the keyboard only.
func homeCellHead(cell *homeCell, width int, pal palette, marked bool) string {
	left := homeCellHeadLeft(cell, width)
	text := switcherSides(width, left, cell.right, homeCellHeadInk(left, cell.note, pal), homeCellMoneyInk(cell.money, pal))
	if marked {
		return pal.cursor(text, width)
	}
	return text
}

// homeCellHeadLeft is a heading's left side: its word, and its explainer beside
// it where the two fit.
//
// THE EXPLAINER GIVES WAY WHOLE, AND IT NEVER CUTS THE HEADING. The heading is
// the one word a person navigates by, so a gloss that pushed it into an ellipsis
// would trade the name for the note. So the explainer is drawn only while the
// heading, the separator and the explainer TOGETHER fit the room the heading's
// right-hand clause leaves — and where they do not, the heading is exactly what
// it read before explainers existed.
func homeCellHeadLeft(cell *homeCell, width int) string {
	room := width
	if cell.right != "" {
		if ansi.StringWidth(cell.right) >= width {
			return cell.title
		}
		room -= ansi.StringWidth(cell.right) + 1
	}
	if cell.note != "" &&
		ansi.StringWidth(cell.title)+ansi.StringWidth(rowSep)+ansi.StringWidth(cell.note) <= room {
		return cell.title + rowSep + cell.note
	}
	return cell.title
}

// homeCellHeadInk paints a heading's left side: the heading ink for the word,
// one shade lower for the explainer after it (docs/DESIGN-LANGUAGE.md — the
// section word is one shade under the title, and its gloss one under that).
// `left` is what [homeCellHeadLeft] returned, so its tail is the explainer only
// where the explainer was kept; a heading that had to be cut keeps the one ink.
func homeCellHeadInk(left, note string, pal palette) func(string) string {
	heading := placeHeadingInk(pal)
	tail := rowSep + note
	if note == "" || !strings.HasSuffix(left, tail) {
		return heading
	}
	return func(s string) string {
		if !strings.HasSuffix(s, tail) {
			return heading(s)
		}
		return heading(strings.TrimSuffix(s, tail)) + pal.dim(tail)
	}
}

// homeCellMoneyInk is the heading clause's ink: dim, with the one figure in
// it that is money in the money ink (docs/DESIGN-LANGUAGE.md — money is a
// number, and it is findable because it is the one mint thing on the line).
func homeCellMoneyInk(money string, pal palette) func(string) string {
	return func(text string) string {
		at := strings.Index(text, money)
		if money == "" || at < 0 {
			return pal.dim(text)
		}
		return pal.dim(text[:at]) + placeMoneyInk(pal)(money) + pal.dim(text[at+len(money):])
	}
}

// homeSparkSteps is the eight-step block ramp home's fortnight is drawn in, a
// cell a value. It is not [sparkline]'s braille: at a column's width the braille
// ramp's low steps are dots a person cannot tell apart, and the spend place,
// which draws a whole frame's width, keeps it.
var homeSparkSteps = [...]string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// homeSparkCells is a series as one step a value, scaled to its own peak; a
// zero draws the lowest step, so the line keeps its length and a quiet day is
// the floor rather than a hole. A series with no spend in it is no cells.
func homeSparkCells(values []float64) []string {
	peak := 0.0
	for _, value := range values {
		peak = max(peak, value)
	}
	if peak <= 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	top := len(homeSparkSteps) - 1
	for _, value := range values {
		out = append(out, homeSparkSteps[min(top, max(0, int(value/peak*float64(top)+0.5)))])
	}
	return out
}

// homeCellGroup is a group's own line inside a panel: its word and count at the
// left and its clause at the right, both dim, under the rows' own lead
// ([homePanelGroup]).
//
// IT IS DIMMER THAN A HEADING ON PURPOSE. A panel's heading is the places' one
// heading ink and marks itself when the cursor is in it ([homeCellHead]); a
// group is a sorting of rows INSIDE one panel, and a second thing on the column
// wearing heading ink would read as a second panel.
func homeCellGroup(cell *homeCell, width int, pal palette) string {
	return switcherSides(max(1, width), cell.title, cell.right, pal.dim, pal.dim)
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
// A ROW THAT GROWS DRAWS ITS SECOND LINE ONLY UNDER THE CURSOR, and the band
// covers both of them: the two lines are one row, and a ground that stopped
// half way would read as two ([homeCell.grows]).
func (a *app) homeCellRow(line homeLine, at, width int, pal palette, lit bool) []string {
	cell := line.cell
	body := homeCellBody(a.homeCellDoor(cell, at, width-homeGridLead), width-homeGridLead, pal, lit)
	rows := []string{homeCellBand(a.homeCellLead(cell, at, pal)+body, width, pal, lit)}
	// THE DESCRIPTION COLUMN HAS THIS LINE WHERE THERE IS ONE, so the row is one
	// line and the panel above it is that much shorter ([homeDescCol]).
	if cell.sub == "" || homeDescOn(a.home.grid.cols) || (cell.grows && at != a.home.cursor) {
		return rows
	}
	under := switcherSides(max(1, width-homeGridLead), cell.sub, a.homeRowAnswers(line, at), pal.dim, pal.muted)
	return append(rows, homeCellBand(homeCellLeadBlank+under, width, pal, lit && cell.grows))
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
// THE AGE OUTRANKS THE TAIL OF A TITLE (owner, 2026-09-10: `Generate and Display
// First 200 Primes, Sleep, Then Prin…` drew with no age beside rows that had
// one). The note gives way first, then the tag, then the title is cut — and the
// right-hand word, the age or the door word, is the last thing to go: only where
// the title would keep fewer than [homeCellTitleFloor] cells beside it, and a
// held word not even then.
func homeCellBody(cell *homeCell, width int, pal palette, lit bool) string {
	if width < 1 {
		return ""
	}
	title, note, tag, right := cell.title, cell.note, cell.tag, cell.right
	pad := cell.pad
	if cell.path {
		title, pad = homeCellPathTitle(cell, width)
	}
	for _, fact := range []*string{&note, &tag} {
		if homeCellWidth(title, pad, note, tag, right) <= width {
			break
		}
		*fact = ""
	}
	if !cell.hold && homeCellWidth(title, pad, note, tag, right) > width &&
		width < homeCellTitleFloor+homeCellWidth("", 0, "", "", right) {
		right = ""
	}
	if over := homeCellWidth(title, pad, note, tag, right) - width; over > 0 {
		keep := max(1, ansi.StringWidth(title)-over)
		if cell.path {
			title = homeFitPathLeft(title, keep)
		} else {
			title = fit(title, keep)
		}
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
		gap := max(0, pad-used) + len(homeCellGap)
		line += strings.Repeat(" ", gap) + factInk(note)
		used += gap + ansi.StringWidth(note)
	}
	tail := homeCellTail(tag, right)
	if tail == "" {
		return line
	}
	return line + strings.Repeat(" ", max(1, width-used-ansi.StringWidth(tail))) + factInk(tail)
}

// homeCellPathTitle is a path title fitted into what its row's facts leave,
// and the pad cut to match.
//
// A PATH GIVES WAY BEFORE ITS FACTS, from the left. `~/Documents/agentfield/
// code/codeaf` pushed `61 chats` off its row in the owner's first binary,
// and the part of a path that tells two folders apart is its end — so the path
// is cut to `…/code/codeaf` while the count and the repository keep their
// cells. It is cut to the panel's pad as well, so every path on the panel ends
// at or before one column and the counts beside them stand in one line. Only
// where even the folder's own name would not fit do the facts give way, in the
// row's ordinary order.
func homeCellPathTitle(cell *homeCell, width int) (string, int) {
	room := width - homeCellWidth("", 0, cell.note, cell.tag, cell.right)
	if cell.pad > 0 {
		room = min(room, cell.pad)
	}
	if room < ansi.StringWidth(glyphMore+"/"+filepath.Base(cell.title)) {
		return cell.title, cell.pad
	}
	return homeFitPathLeft(cell.title, room), min(cell.pad, room)
}

// homeFitPathLeft is [fitLeft] for a path: cut from the left, and then on to
// the next separator, so what is left starts at a folder — `…/code/codeaf`
// and never `…ield/code/codeaf`.
func homeFitPathLeft(path string, width int) string {
	cut := fitLeft(path, width)
	rest := strings.TrimPrefix(cut, glyphMore)
	if rest == cut {
		return cut
	}
	if at := strings.Index(rest, "/"); at > 0 && at < len(rest)-1 {
		return glyphMore + rest[at:]
	}
	return cut
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

// homeCellTitleFloor is the fewest cells a cut title keeps before the row's
// right-hand word gives way to it ([homeCellBody]).
const homeCellTitleFloor = 12

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
