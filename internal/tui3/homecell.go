package tui3

import (
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
		columns[c] = append(columns[c], a.homeLineRows(line, at, widths[c], pal, h.marksPanel(at), at == h.headHover)...)
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
// NOTHING IN IT IS A STOP. The column holds no row a cursor may stand on, and
// the arrows never leave the field in any case (homegrid.go, "the arrows stay
// in their column").
func (a *app) homeDescLines(width, room int, pal palette, field []homeCellLine) []homeCellLine {
	h := &a.home
	if room <= 0 || width <= homeGridLead {
		return nil
	}
	// A QUESTION HOME HAS RAISED TAKES THE WHOLE COLUMN, beside the row it is
	// about. One decision is drawn once and nothing is drawn beside it: the
	// column is otherwise a set of notes about rows, and notes stacked around a
	// question a person has to answer are the screen talking over it.
	//
	// IT IS ASKED BEFORE THE ROOM IS CLAMPED TO THE FIELD'S OWN HEIGHT. A card is
	// not a note about a row and is not bounded by how many rows there are: a
	// frame with two conversations on it has the whole column for the question,
	// and clamping it to the field's length refused to draw one on every quiet
	// machine.
	if rows := a.homeAskNote(field, width, room); rows != nil {
		return rows
	}
	// The selected description may outgrow a short field: its thread title
	// and answer buttons still have the rest of the body available to them.
	preview := h.previewAt()
	withVerbs := a.homeStripInDescription(h.gridWidth, room)
	type note struct {
		y     int
		words []string
	}
	var notes []note
	for y := 0; y < min(room, len(field)); y++ {
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
		if !selected || (said == "" && !withVerbs) {
			continue
		}
		words := a.homeDescNote(line, at, said, width, selected, pal)
		top := y
		if withVerbs {
			options := a.homeDescriptionVerbs(width, pal)
			// Keep every option visible even when a long description or a row
			// near the bottom would otherwise push the shortcuts off screen.
			if keep := max(0, room-len(options)-1); len(words) > keep {
				words = words[:keep]
			}
			if len(words) > 0 {
				words = append(words, "")
			}
			words = append(words, options...)
			top = min(top, max(0, room-len(words)))
		}
		notes = append(notes, note{y: top, words: words})
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

// homeStripInDescription is shared by the frame and the description painter:
// exactly one of them draws the options, including during a resize.
func (a *app) homeStripInDescription(width, room int) bool {
	if !a.at(pageHome) || !a.strip.open || !a.home.gridOn() || !homeDescOn(homeGridCols(width)) || a.composer.open || a.hopShowing() {
		return false
	}
	if _, stacked := a.homeStacked(); stacked {
		return false
	}
	if _, asking := a.homeAsking(); asking {
		return false
	}
	_, widths := homeGridGeometry(width, homeGridCols(width))
	options := a.homeDescriptionVerbs(widths[homeDescCol(len(widths))], a.pal)
	return len(options) > 0 && len(options) <= room
}

// homeDescriptionVerbs wraps whole choices within the description column.
// It reads the same captured verbs as the inline strip, preserving their keys.
func (a *app) homeDescriptionVerbs(width int, pal palette) []string {
	return verbChoiceLines(a.strip.verbs, width, homeDescLeadBlank, pal)
}

// homeDescNote is one row's note as the lines it takes: the thread's title
// line and a blank where the row names one ([homeCell.thread]), the sentence,
// wrapped, and the row's answers under it. It is only ever asked for the row
// being read ([homeView.previewAt]) — no note stands permanently in the column
// since the `needs you` exception went (2026-09-17, [homeCell.grows]) — so the
// note has the column to itself and may wrap.
func (a *app) homeDescNote(line homeLine, at int, said string, width int, selected bool, pal palette) []string {
	room := max(1, width-homeDescLeadCells)
	answers := ""
	if selected {
		answers = strings.TrimSpace(a.homeRowAnswers(line, at))
	}
	var out []string
	if thread := strings.TrimSpace(line.cell.thread); thread != "" {
		out = append(out, homeDescLeadBlank+homeThreadLine(thread, room, pal), "")
	}
	if said != "" {
		for _, words := range wrap(said, room) {
			out = append(out, homeDescLeadBlank+pal.dim(words))
		}
	}
	if answers != "" {
		out = append(out, "", homeDescLeadBlank+paintHint(answers, pal, pal.dim))
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
func (a *app) homeLineRows(line homeLine, at, width int, pal palette, heading, hovered bool) []homeCellLine {
	h := &a.home
	hit := -1
	if line.stop() || line.kind == homeProjectRow {
		hit = at
	}
	lit := at == h.cursor
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
		texts, head = []string{homeCellHead(cell, width, pal, heading, hovered)}, at
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

// homeDescLeadCells is what a note in the description column stands in, and it
// is ONE CELL WIDER THAN A ROW'S LEAD so the mark and the first letter of the
// sentence it leads are not touching (owner, 2026-09-15). Every note takes it,
// marked or not, because notes are read down the column against each other
// rather than against the rows in the column beside them.
const homeDescLeadCells = homeGridLead + 1

var homeDescLeadBlank = strings.Repeat(" ", homeDescLeadCells)

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
//
// A HEADING THAT IS A DOOR UNDERLINES UNDER THE POINTER, and that is the whole
// of what the pointer does to a heading: the word keeps its ink and takes no
// ground, so it cannot be mistaken for the cursor's mark, and the underline is
// the one attribute every terminal has used to say "this opens somewhere"
// ([palette.underline]). Only the word underlines — the explainer and the
// clause at the right are not the door — and hovered is true only for a
// heading that names a place ([app.homeHeadDoor]), so `projects` and
// `threads` never wear it.
//
// A HEADING THAT OPENS NOTHING IS DIM, word and explainer alike — `projects`
// and `threads` name their own panel and no place, and a heading painted like
// the five that are doors read as a door that did not work (owner,
// 2026-09-17: "make the non-clickable projects heading grey"). The ground the
// cursor's panel wears still lands on it, because that fact is about where the
// cursor is and not about what the heading opens.
func homeCellHead(cell *homeCell, width int, pal palette, marked, hovered bool) string {
	left := homeCellHeadLeft(cell, width)
	ink := homeCellHeadInk(left, cell.note, pal)
	if !homeHeadOpens(cell.panel) {
		ink = pal.dim
	} else if hovered {
		ink = homeCellHeadDoorInk(ink, cell.title, pal)
	}
	text := switcherSides(width, left, cell.right, ink, homeCellMoneyInk(cell.money, pal))
	if marked {
		return pal.cursor(text, width)
	}
	return text
}

// homeHeadOpens reports whether a panel's heading names a place — the order
// table's head column, answered by the registry ([app.homeHeadDoor] asks the
// same of a line).
func homeHeadOpens(id homePanelID) bool {
	return placeFor(homeSlotOf(id).head) != nil
}

// homeCellHeadDoorInk is a heading's left-side ink with the word underlined:
// the panel's word at the front of the text, and nothing after it — not the
// count a heading carries after its separator (`tasks · 3`), and not the
// explainer. A left side that does not start with the word (which
// [homeCellHeadLeft] never hands out) is painted as it was.
func homeCellHeadDoorInk(ink func(string) string, title string, pal palette) func(string) string {
	word, _, _ := strings.Cut(title, rowSep)
	return func(s string) string {
		if word == "" || !strings.HasPrefix(s, word) {
			return ink(s)
		}
		return ink(pal.underline(word) + s[len(word):])
	}
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

// homeCellGroup is a group's own line inside a panel: its word, dim, under the
// rows' own lead ([homePanelGroup]).
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
// A ROW THAT GROWS DRAWS ITS SECOND LINE ONLY WHILE IT IS THE ROW BEING READ —
// under the pointer, or under the cursor when nothing is pointed at
// ([homeView.previewAt]) — and the band covers both of them: the two lines are
// one row, and a ground that stopped half way would read as two
// ([homeCell.grows]). ONE ROW OF A FRAME GROWS, which is the one line each
// panel reserves for it ([homeGridPanel.height]).
func (a *app) homeCellRow(line homeLine, at, width int, pal palette, lit bool) []string {
	cell := line.cell
	if line.kind == homeProjectRow && at == a.home.projectHover {
		hovered := *cell
		hovered.underline = true
		cell = &hovered
	}
	lead := a.homeCellLead(cell, at, pal)
	leadWidth := homeGridLead
	if (cell.panel == panelRecent || cell.panel == panelSessions) && line.kind == homeSession {
		lead = a.homeConversationBullet(cell, pal) + " "
	}
	body := homeCellBody(a.homeCellDoor(cell, at, width-leadWidth), width-leadWidth, pal, lit)
	rows := []string{homeCellBand(lead+body, width, pal, lit)}
	// THE DESCRIPTION COLUMN HAS THIS LINE WHERE THERE IS ONE, so the row is one
	// line and the panel above it is that much shorter ([homeDescCol]).
	if cell.sub == "" || homeDescOn(a.home.grid.cols) || (cell.grows && at != a.home.previewAt()) {
		return rows
	}
	// THE THREAD'S TITLE LINE AND A BLANK COME FIRST where the row names one
	// ([homeCell.thread]), and the band covers them with the rest.
	if thread := strings.TrimSpace(cell.thread); thread != "" {
		rows = append(rows,
			homeCellBand(homeCellLeadBlank+homeThreadLine(thread, max(1, width-homeGridLead), pal), width, pal, lit && cell.grows),
			homeCellBand("", width, pal, lit && cell.grows))
	}
	under := switcherSides(max(1, width-homeGridLead), cell.sub, a.homeRowAnswers(line, at), pal.dim, pal.muted)
	return append(rows, homeCellBand(homeCellLeadBlank+under, width, pal, lit && cell.grows))
}

// homeThreadLine is a description's title line: the word dim and the thread's
// name one shade up, cut whole to the room — `thread: Prime Sieve`.
func homeThreadLine(thread string, room int, pal palette) string {
	name := fit(thread, max(1, room-ansi.StringWidth(homeThreadWord)))
	return pal.dim(homeThreadWord) + pal.muted(name)
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
	// THE MARK LEADS THE ROW, ALWAYS. It used to move beside the question on a
	// frame whose description column drew the question permanently (owner,
	// 2026-09-15); the question is drawn only under the pointer or the cursor
	// now, and the owner asked (2026-09-17) that the `?` always show — so it
	// stands beside the title, where it is on every frame.
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
		if cell.panel == panelRecent || cell.panel == panelSessions {
			title = fitConversationTitle(title, keep)
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
	if cell.closed {
		titleInk, factInk = pal.dim, pal.dim
	}
	line := titleInk(title)
	if cell.underline {
		line = pal.underline(line)
	}
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

// homeCellPathTitle preserves the start of a project path: an absolute root
// or the home-directory tilde. Facts keep their columns while the right end
// of the path is cut; if even a useful prefix cannot fit, facts give way first.
func homeCellPathTitle(cell *homeCell, width int) (string, int) {
	room := width - homeCellWidth("", 0, cell.note, cell.tag, cell.right)
	if cell.pad > 0 {
		room = min(room, cell.pad)
	}
	if room < homeCellTitleFloor {
		return cell.title, cell.pad
	}
	return fit(cell.title, room), min(cell.pad, room)
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

// homeAskNote is the question home is holding, drawn in the description column
// on the line of the row whose `enter` raised it, and nil where there is no
// question or its row is not in the field.
//
// IT IS THE QUESTION BLOCK'S OWN CARD ([app.homeAskRows]) and not a second
// drawing of the same facts, so a person who has learnt one question on this
// surface has learnt this one. Until this column existed the grid had nowhere to
// put it: the card belongs to the detail column beside the typed search, which
// is not up at rest, so a raised question left only its one-line foot version —
// appended to the resting sentence, where it read as a run-on and was missed
// (owner, 2026-09-15).
//
// IT IS PULLED UP RATHER THAN CUT where the card is taller than the room under
// its row. A decision with its last answer off the bottom of the screen is worse
// than one drawn a few lines above the row it belongs to.
func (a *app) homeAskNote(field []homeCellLine, width, room int) []homeCellLine {
	h := &a.home
	if _, ok := a.homeAsking(); !ok {
		return nil
	}
	// THE CARD DRAWS WHEREVER THE QUESTION CAME FROM, and it must: the foot stops
	// saying a question once this column can draw one ([app.sayHomeAsk]), so a
	// question with no row to stand beside would be a decision on the screen with
	// nothing on the screen about it. A question raised by `enter` has the row
	// its key was pressed on ([homeView.armed]); one raised by the LAUNCH — a
	// window that met a lock on the conversation it was opening — has no row at
	// all, and stands at the top of the column instead.
	at := 0
	if armed := strings.TrimSpace(h.armed); armed != "" {
		for y := 0; y < min(room, len(field)); y++ {
			line := field[y].at
			if line == homeNoLine || line < 0 || line >= len(h.lines) {
				continue
			}
			if h.lines[line].kind == homeSession && strings.TrimSpace(h.lines[line].row.Transcript) == armed {
				at = y
				break
			}
		}
	}
	said := a.homeAskRows(max(1, width-homeGridLead))
	// A CARD TALLER THAN THE ROOM IS NOT DRAWN AT ALL. Cutting it takes the
	// closing edge, the keys and usually an answer off the bottom of the screen,
	// and a decision whose answers a person cannot see is worse than the same
	// decision said in one line on the foot — which is exactly what the foot
	// version is for, and what [app.sayHomeAsk] falls back to when this refuses
	// ([app.homeAskFitsColumn] is the one predicate both of them ask).
	if len(said) == 0 || len(said) > room {
		return nil
	}
	top := min(at, max(0, room-len(said)))
	out := make([]homeCellLine, room)
	for i := range out {
		out[i] = homeCellLine{at: homeNoLine, head: -1}
	}
	for i, words := range said {
		if top+i >= room {
			break
		}
		out[top+i] = homeCellLine{at: homeNoLine, head: -1, text: homeCellLeadBlank + words}
	}
	return out
}

// homeAskFitsColumn reports that a question home is holding can be drawn WHOLE
// in the description column of the frame as it last stood.
//
// IT IS THE ONE PREDICATE, asked by the column that draws the card and by the
// foot that would otherwise say it. Two answers to "is the card on this frame"
// is a question drawn twice on a tall frame and drawn nowhere at all on a short
// one, and both of those have happened.
func (a *app) homeAskFitsColumn() bool {
	if _, ok := a.homeAsking(); !ok || !a.home.gridOn() || !homeDescOn(a.home.cols) {
		return false
	}
	col := homeDescCol(a.home.cols)
	_, widths := homeGridGeometry(a.home.gridWidth, a.home.cols)
	if col < 0 || col >= len(widths) {
		return false
	}
	said := a.homeAskRows(max(1, widths[col]-homeGridLead))
	return len(said) > 0 && len(said) <= a.home.room
}
