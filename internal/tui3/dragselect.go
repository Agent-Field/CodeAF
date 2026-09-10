package tui3

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// DRAG THE MOUSE OVER TEXT AND IT IS COPIED. That is the whole feature, and it
// exists because mouse reporting takes the terminal's own drag-select away: the
// app owns every mouse event while reporting is on, so the one gesture every
// terminal user owns — sweep the pointer over text, paste it somewhere — did
// nothing at all here unless they knew about ctrl+s or copy mode. Both of those
// remain (copymode.go), but a person should not need to know a chord to copy a
// sentence they can see.
//
// So the drag is answered in kind: press, sweep, and the cells under the sweep
// wear the selection highlight; release, and their text is on the clipboard —
// stripped of paint and rails exactly as copy mode strips it, written over
// OSC 52 so it works over ssh and through tmux, with a word on the status line
// saying what landed.
//
// ── CELLS, NOT ROWS ──
//
// The first build selected whole rows, on the reasoning that a transcript is
// made of lines and a row has no seams around wide glyphs. Both true, and
// neither is what a person's hand is asking for: a press on the third word and
// a release on the fifth means those three words, on every screen they have
// ever used, and a sweep that answered with the whole line read as broken
// rather than as opinionated (#222). So the selection is a STREAM, the way a
// terminal's is — from the anchor cell to the end of its row, every row between
// in full, and the head's row up to the pointer — and a sweep that stays on one
// row is the span between its two columns. The wide-glyph seam is answered by
// snapping: a column is converted through the row's plain text so a glyph is
// wholly in or wholly out, and a selection never emits half of one.
//
// The gestures a hand already knows come with it: a double-click takes the word
// under the pointer, a triple-click the row, and a plain click is still a click.
// The whole-row copy the old sweep did is what triple-click does now.
//
// ── THE GESTURE IS ANCHORED TO CONTENT, NOT TO THE GLASS ──
//
// Every row coordinate this file keeps is an index into the body's OWN row
// list, converted from the screen the moment the event arrives. The first build
// kept screen rows, and a streaming room broke both gestures at once: a task's
// page follows its live edge and repaints four times a second, so between a
// press and its release the text slid up under the pointer — the parked click
// fired on whatever had scrolled into the cell (usually nothing), and a sweep
// copied rows the person never highlighted. Anchored to content, the selection
// rides the scroll, the click opens the row that was actually pressed, and a
// row that has scrolled clean off the screen resolves to no click at all.
// Columns need no such conversion: the body is drawn from column zero and only
// ever scrolls vertically.
//
// ── WHAT THIS COSTS THE CLICK, AND WHY IT IS SAFE TO PAY ──
//
// A drag and a click begin identically: a left press. The press therefore
// cannot act on the body any more — a drag that begins on a thinking block
// would collapse the very text somebody is trying to copy, and the collapse
// would shuffle every row under the selection mid-sweep. So a press on the BODY
// is parked, and the body acts on RELEASE: release in place is the click,
// exactly as every button in every GUI has fired on mouse-up for forty years,
// and release after a sweep is a copy and no click at all. The chrome — offer
// rows, panels, chips, the rail, the strip — keeps firing on press, because
// nothing anybody drags starts on a button.
type dragSelect struct {
	// parked is a left press the body has not answered yet: the click fires on
	// release at the CONTENT row prow (px carries the column and py the slop
	// baseline), unless the pointer moves first and turns it into a selection.
	parked bool
	px, py int
	prow   int
	// on is a selection: a sweep past the slop threshold, or a word or row a
	// multi-click took. anchorRow/anchorCol is the cell the press landed on and
	// row/col the one under the pointer now; the stream between them, both
	// ends inclusive, is the selection.
	on             bool
	anchorRow, row int
	anchorCol, col int
	// unit is what the gesture took, for the status line's word: cells for a
	// sweep, a word for a double-click, a row for a triple.
	unit dragUnit
}

// dragUnit names what a selection was made of.
type dragUnit int

const (
	cellsUnit dragUnit = iota
	wordUnit
	rowUnit
)

// dragSlop is how many columns a pressed pointer may wander sideways and still
// be a click, and dragSlopRows is how many rows. A hand is not a vice, and a
// press that slid two cells is a press.
//
// THE SLOP IS THE SAME PHYSICAL DISTANCE ON BOTH AXES, and that is why the two
// numbers differ. A terminal cell is roughly twice as tall as it is wide, so
// three columns and one row are about the same tremor of the wrist — and the
// vertical figure used to be ZERO, on the reasoning that "rows are what a sweep
// selects". That reasoning was about what a SWEEP means and got applied to what
// a CLICK survives: a press that landed a couple of pixels from a row boundary
// and drifted across it was silently spent as a two-line copy — the tool call
// under the pointer did not open, and the status line said `copied · 2 lines`
// instead. It reads as "clicking does not work", and no synthetic click ever
// reproduced it, because bytes fed to the surface never wobble.
const (
	dragSlop     = 3
	dragSlopRows = 1
)

// multiClickWithin is how soon a second press on the same spot is the same
// gesture — a double-click, then a triple. Four hundred milliseconds is what
// every desktop has settled on; a press after that, or three cells away, is a
// new click.
const multiClickWithin = 400 * time.Millisecond

// countClick folds a left press into the click count and answers it: one for a
// press on its own, two for a press soon and near the last, three for the one
// after that. A fourth starts again at one, as terminals do.
func (a *app) countClick(x, y int) int {
	now := time.Now()
	if now.Sub(a.clickAt) <= multiClickWithin && abs(x-a.clickX) < dragSlop && abs(y-a.clickY) <= dragSlopRows && a.clicks < 3 {
		a.clicks++
	} else {
		a.clicks = 1
	}
	a.clickAt, a.clickX, a.clickY = now, x, y
	return a.clicks
}

// bodyContentRow converts one screen row into an index into the body's own row
// list, through whichever body is up — the conversation's window or a room's
// ([app.bodyRows] is the windowing this reverses). Negative means the screen
// row is above the body region.
func (a *app) bodyContentRow(y int) int {
	top := a.bodyTop()
	if top < 0 || y < top {
		return -1
	}
	return a.bodyScroll() + (y - top)
}

// bodyScroll is the index of the first body row on screen: the same offset
// [app.bodyRows] windows by, answered without slicing anything.
func (a *app) bodyScroll() int {
	width, height := a.bodyWidth(), a.viewHeight()
	if a.roomOpen() {
		return a.roomOffsetFor(len(a.roomRows(width)), height)
	}
	return a.offsetFor(len(a.visible(width)), height)
}

// allBodyRows is the body's whole row list — what the window is a window onto.
func (a *app) allBodyRows() []row {
	if a.roomOpen() {
		return a.roomRows(a.bodyWidth())
	}
	return a.visible(a.bodyWidth())
}

// dragSel is the selection that should be drawn: the live one while the button
// is down, and then — for as long as the status line still says "copied" —
// the one the release just copied, kept lit so a person can see exactly what
// landed on the clipboard instead of watching their selection vanish the
// moment they let go.
func (a *app) dragSel() (dragSelect, bool) {
	if a.drag.on {
		return a.drag, true
	}
	// A COPY MADE IN A TEXT BOX LIGHTS NOTHING HERE. dragLit is a span of body
	// rows and a box's copy has none, so the transcript would light row zero of
	// whatever happened to be on screen; the box draws its own selection from
	// its own text (boxselect.go, editselect.go).
	if a.dragCopied > 0 && !a.dragInBox && time.Now().Before(a.dragUntil) {
		return a.dragLit, true
	}
	return dragSelect{}, false
}

// dragSpan is the selection as CONTENT rows, low first.
func (a *app) dragSpan() (int, int, bool) {
	sel, on := a.dragSel()
	if !on {
		return 0, 0, false
	}
	sr, _, er, _ := sel.ordered()
	return sr, er, true
}

// ordered is the selection's two ends in reading order: start first.
func (d dragSelect) ordered() (sr, sc, er, ec int) {
	if d.anchorRow < d.row || (d.anchorRow == d.row && d.anchorCol <= d.col) {
		return d.anchorRow, d.anchorCol, d.row, d.col
	}
	return d.row, d.col, d.anchorRow, d.anchorCol
}

// cellsOn is the selection's cell range [from, to) on one content row, before
// any snapping to glyphs, and whether the row is in the selection at all. width
// is the body's, and a row that is not the start or the end is selected whole.
func (d dragSelect) cellsOn(at, width int) (int, int, bool) {
	sr, sc, er, ec := d.ordered()
	if at < sr || at > er {
		return 0, 0, false
	}
	if d.unit == rowUnit {
		return 0, width, true
	}
	from, to := 0, width
	if at == sr {
		from = sc
	}
	if at == er {
		to = ec + 1
	}
	if to > width {
		to = width
	}
	if from >= to {
		return 0, 0, false
	}
	return from, to, true
}

// dragCells is the selection's cells on one content row, snapped so a wide
// glyph is wholly in or wholly out: the start moves back to the glyph it is
// inside and the end moves forward past it.
func (a *app) dragCells(at int, plain string) (int, int, bool) {
	sel, on := a.dragSel()
	if !on {
		return 0, 0, false
	}
	from, to, ok := sel.cellsOn(at, a.bodyWidth())
	if !ok {
		return 0, 0, false
	}
	return snapCells(plain, from, to)
}

// glyphStarts is the column each glyph of plain text starts on, and the total
// width after them. A zero-width rune — a combining mark, a variation selector
// — belongs to the glyph before it and starts nothing of its own.
func glyphStarts(plain string) ([]int, int) {
	starts := make([]int, 0, len(plain))
	col := 0
	for _, r := range plain {
		w := ansi.StringWidth(string(r))
		if w == 0 && len(starts) > 0 {
			continue
		}
		starts = append(starts, col)
		col += w
	}
	return starts, col
}

// snapCells widens [from, to) so it never cuts a glyph: from moves back to the
// start of the glyph it falls inside, and to moves forward to the end of the
// glyph it falls inside. Cells past the text's end are their own columns.
func snapCells(plain string, from, to int) (int, int, bool) {
	starts, total := glyphStarts(plain)
	for i, s := range starts {
		end := total
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		if s < from && from < end {
			from = s
		}
		if s < to && to < end {
			to = end
		}
	}
	if from >= to {
		return 0, 0, false
	}
	return from, to, true
}

// wordSeparators is what a double-click stops at. A path, a hash, a flag, a
// dotted name and a URL are one word each, because those are the things a
// person double-clicks in a transcript; brackets and quotes are not, because a
// word inside them is what was meant.
const wordSeparators = " \t()[]{}<>\"'`,;"

// wordAt is the cell span [from, to) of the word under column col of plain
// text, or false when the column is past the text. On whitespace it is the run
// of whitespace, as a terminal's is. A sentence's closing punctuation is left
// behind when it ends the word: "Println." is the word `Println`, but `go.mod`
// keeps its dot because a letter follows it.
func wordAt(plain string, col int) (int, int, bool) {
	starts, total := glyphStarts(plain)
	if col < 0 || col >= total {
		return 0, 0, false
	}
	runes := []rune(strings.Map(func(r rune) rune {
		if ansi.StringWidth(string(r)) == 0 {
			return -1
		}
		return r
	}, plain))
	if len(runes) != len(starts) {
		// A row this cannot map is selected by its cell alone.
		return col, col + 1, true
	}
	at := 0
	for i, s := range starts {
		if s <= col {
			at = i
		}
	}
	space := func(r rune) bool { return r == ' ' || r == '\t' }
	sep := func(r rune) bool { return strings.ContainsRune(wordSeparators, r) }
	lo, hi := at, at
	if space(runes[at]) {
		for lo > 0 && space(runes[lo-1]) {
			lo--
		}
		for hi+1 < len(runes) && space(runes[hi+1]) {
			hi++
		}
	} else {
		for lo > 0 && !sep(runes[lo-1]) {
			lo--
		}
		for hi+1 < len(runes) && !sep(runes[hi+1]) {
			hi++
		}
		for hi > lo && strings.ContainsRune(".,:;!?", runes[hi]) && (hi+1 == len(runes) || sep(runes[hi+1])) {
			hi--
		}
	}
	end := total
	if hi+1 < len(starts) {
		end = starts[hi+1]
	}
	return starts[lo], end, true
}

// dragTake turns a multi-click into a selection on the spot: the word under the
// pointer for two clicks, the row for three. The release then copies it, the
// way a sweep's release does; a fourth click is a click again.
func (a *app) dragTake(clicks int) {
	if clicks < 2 || a.drag.prow < 0 {
		return
	}
	rows := a.allBodyRows()
	if a.drag.prow >= len(rows) {
		return
	}
	// A BUTTON ACTS ON EVERY CLICK. A tool call's header, a fold line, a
	// brief's door: two quick clicks on one of those is open-then-shut, which
	// is what the person did, and not a word selection. Text has no hit, and
	// text is what a double-click selects.
	if a.rowIsButton(rows[a.drag.prow]) {
		return
	}
	plain := ansi.Strip(rows[a.drag.prow].text)
	if clicks == 2 {
		from, to, ok := wordAt(plain, a.drag.px)
		if !ok {
			return
		}
		a.drag.on, a.drag.unit = true, wordUnit
		a.drag.anchorRow, a.drag.row = a.drag.prow, a.drag.prow
		a.drag.anchorCol, a.drag.col = from, to-1
	} else {
		a.drag.on, a.drag.unit = true, rowUnit
		a.drag.anchorRow, a.drag.row = a.drag.prow, a.drag.prow
		a.drag.anchorCol, a.drag.col = 0, max(a.bodyWidth()-1, 0)
	}
	a.touch()
}

// rowIsButton reports whether a click on this row does something — the rows
// [app.press] answers with an action rather than nothing: every hit kind, and
// a thinking block's rows, which toggle it. It is the transcript's own rule
// read from the other side, and it must stay in step with press.
func (a *app) rowIsButton(r row) bool {
	if r.hit != hitNone {
		return true
	}
	es := a.bodyDeck().entries
	return r.entry >= 0 && r.entry < len(es) && es[r.entry].kind == entryThinking
}

// dragMotion folds one moved-with-the-button-down event in, and reports whether
// it was taken. The first move past the slop is what turns a parked click into
// a selection; every move after that just grows it — including one that began
// as a double-click's word, which grows by cells from there.
func (a *app) dragMotion(x, y int) bool {
	if !a.drag.parked && !a.drag.on {
		return false
	}
	if !a.drag.on {
		if abs(y-a.drag.py) <= dragSlopRows && abs(x-a.drag.px) < dragSlop {
			return true
		}
		a.drag.on, a.drag.unit = true, cellsUnit
		a.drag.anchorRow, a.drag.anchorCol = a.drag.prow, a.drag.px
		a.drag.row, a.drag.col = a.drag.prow, a.drag.px
	}
	at := a.bodyContentRow(y)
	if at < 0 {
		at = 0
	}
	if col := max(x, 0); a.drag.row != at || a.drag.col != col {
		a.drag.row, a.drag.col = at, col
		if a.drag.unit != cellsUnit {
			a.drag.unit = cellsUnit
		}
		a.touch()
	}
	return true
}

// dragRelease ends the gesture, whichever it turned out to be: a selection is
// copied, a parked click is spent on the body at the row it pressed, and a
// release nothing owns is nothing.
func (a *app) dragRelease() tea.Cmd {
	// A SWEEP INSIDE A TEXT BOX ENDS HERE FIRST. It is the same button coming
	// up, and the two gestures can never both be live — a press the box took
	// never parked a body drag (boxselect.go).
	if cmd, took := a.boxRelease(); took {
		return cmd
	}
	drag := a.drag
	a.drag = dragSelect{}
	if drag.on {
		a.touch()
		return a.dragYank(drag)
	}
	if !drag.parked {
		return nil
	}
	// The body's click, exactly as it ran on press before this file existed —
	// including the room pump the spawn-card door needs (app.go states why the
	// batch matters). The pressed CONTENT row is converted back to wherever it
	// is on screen now, so a streaming body that scrolled between press and
	// release still opens the row the person's finger was on; one that carried
	// it clean off the screen answers no click, which is the honest reading of
	// pressing something that is no longer there.
	y := drag.prow - a.bodyScroll() + a.bodyTop()
	if drag.prow < 0 || y < a.bodyTop() || y >= a.bodyTop()+a.viewHeight() {
		return nil
	}
	return tea.Batch(a.press(drag.px, y), a.takeRoomPump())
}

// dragYank copies the selected cells: the drawn text under them, stripped of
// escapes and — where a row is taken from its first column — of the rails the
// renderer draws down a block's left, the way copy mode strips its yank, onto
// the clipboard the same OSC 52 way. What was highlighted is exactly what
// lands; a row taken whole is joined to the next with a newline, and a row
// taken in part carries no trailing space.
func (a *app) dragYank(drag dragSelect) tea.Cmd {
	body := a.allBodyRows()
	sr, _, er, _ := drag.ordered()
	sr, er = max(sr, 0), min(er, len(body)-1)
	if sr > er || len(body) == 0 {
		return nil
	}
	width := a.bodyWidth()
	pieces := make([]string, 0, er-sr+1)
	chars := 0
	for at := sr; at <= er; at++ {
		plain := ansi.Strip(body[at].text)
		from, to, ok := drag.cellsOn(at, width)
		if !ok {
			continue
		}
		var piece string
		if from == 0 && to >= width {
			piece = copyClean(plain, textGutterCols(width))
		} else if from, to, ok = snapCells(plain, from, to); ok {
			piece = ansi.Cut(plain, from, to)
			if from == 0 {
				piece = copyClean(piece, textGutterCols(width))
			} else {
				piece = strings.TrimRight(piece, " ")
			}
		}
		chars += utf8.RuneCountInString(piece)
		pieces = append(pieces, piece)
	}
	a.dragCopied, a.dragUntil = len(pieces), time.Now().Add(dragFlashFor)
	a.dragLit = drag
	a.dragChars = chars
	a.touch()
	// The flash needs one more frame when it expires, or "copied" would sit on
	// an idle status line forever.
	return tea.Batch(
		tea.Raw(osc52(strings.Join(pieces, "\n"), a.tmux)),
		tea.Tick(dragFlashFor, func(time.Time) tea.Msg { return dragFlashMsg{} }),
	)
}

// dragFlashFor is how long the status line says what a sweep copied — and how
// long the copied cells stay lit after the release (dragSel).
const dragFlashFor = 3 * time.Second

// dragFlashMsg is the flash expiring: one repaint, so the word comes down.
type dragFlashMsg struct{}

// dragWord is the status line's account of the last copy in the unit it was
// taken in — a word, a line, a count of lines, or a count of characters for a
// span inside one row — and "" once it has expired.
func (a *app) dragWord() string {
	if a.dragCopied <= 0 || !time.Now().Before(a.dragUntil) {
		return ""
	}
	switch {
	case a.dragLit.unit == wordUnit:
		return "copied · 1 word"
	case a.dragLit.unit == rowUnit || a.dragCopied > 1:
		if a.dragCopied == 1 {
			return "copied · 1 line"
		}
		return "copied · " + itoa(a.dragCopied) + " lines"
	case a.dragChars == 1:
		return "copied · 1 char"
	default:
		return "copied · " + itoa(a.dragChars) + " chars"
	}
}

// markCells paints the selection over cells [from, to) of one drawn row: the
// text before and after keeps its own paint, and the span between wears the
// mark step — padded to its full width, so a selection that runs past the end
// of a short row still shows how far it goes.
func (a *app) markCells(text string, from, to int) string {
	return markCells(a.pal, text, from, to)
}

// markCells is that same paint with the palette handed in, so a box can light
// its own selection with it (editselect.go's [markDraftRow]). ONE STEP LIGHTS
// EVERY SELECTION ON THIS SURFACE: a highlight in the message box that did not
// look like a highlight in the transcript six rows above it would read as two
// unrelated things.
func markCells(pal palette, text string, from, to int) string {
	if from >= to {
		return text
	}
	head := ansi.Cut(text, 0, from)
	if pad := from - ansi.StringWidth(head); pad > 0 {
		head += strings.Repeat(" ", pad)
	}
	mid := ansi.Cut(text, from, to)
	if pad := (to - from) - ansi.StringWidth(mid); pad > 0 {
		mid += strings.Repeat(" ", pad)
	}
	return head + pal.mark(mid, to-from) + ansi.Cut(text, to, ansi.StringWidth(text))
}
