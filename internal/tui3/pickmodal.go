package tui3

// THE MODEL PICKER, AS A SHEET THAT OWNS THE SCREEN.
//
// `/model`, a press on the status model word, and a task's model door all open
// ONE list (`a.pick`), and from this wave it is a real modal: a bounded, framed
// sheet drawn over a faded conversation, holding the keyboard and every pointer
// event until a model is chosen or the sheet is cancelled.
//
// ── WHAT THIS REPLACED, AND WHY ─────────────────────────────────────────────
//
// The list used to be BOTTOM CHROME. [app.overlayHeight] answered the picker's
// want and view.go appended its rows between the draft and the status line, the
// same slot the command list still lives in. On a wide terminal that meant a
// column of model names with a hundred cells of dead space beside it, the
// previous turn's error line still on screen above it, no framing of any kind,
// and nothing to say a chooser had opened at all — the conversation simply grew
// a list. People expect to have opened something.
//
// So the chat overlay is now what every other deliberate surface in this package
// is: a frame taken WHOLE at the top of [app.frameBody], beside the places, the
// rewind timeline, the status sheet and the context chooser. It differs from the
// places in one way, and the difference is the point — they REPLACE the
// conversation and this one COVERS it. The chat frame is composed exactly as it
// would have been and then faded to the depth ladder's faintest stop, so a
// person choosing a model can still see the message they were part way through
// writing, and cannot touch it.
//
// Settings, home and the composer still embed the same [picker] type in their
// own bodies. This file frames only `a.pick` — the chat door — and does not draw
// a second list.
//
// ── THE ARRANGEMENT ─────────────────────────────────────────────────────────
//
//	╭─ choose model ──────────────────────────── deepseek/deepseek-v4 ─╮
//	│  › filter · ↑↓ · enter · esc                                      │  the box
//	│  ────────────────────────────────────────────────────────────    │
//	│    deepseek/deepseek-v4-flash          $0.14 · 128k · arena 72    │  the list
//	│    anthropic/claude-sonnet-4.5         $3.00 · 200k · arena 81    │
//	│  …                                                                │
//	╰──────────────────────────────────────────────── esc · cancel ──╯
//
// Everything between the thin rule and the foot is palette.go's own
// [picker.rows], unchanged: this file does not draw a second list, it frames
// the one that already exists. What it adds is the title, the box in a place of
// its own, the framing, and one explicit CANCEL target on the foot rule —
// because a modal whose only way out is a key nobody was told about is a modal
// people get stuck in.
//
// ── ONE HIT MAP ─────────────────────────────────────────────────────────────
//
// [pickWin] is what the last paint put on the SCREEN, and it is the only answer
// the pointer gets. Press, hover, wheel and the terminal's own caret all resolve
// through it, so what lights, what a press acts on and where the cursor blinks
// cannot be three different opinions about where the sheet is.
//
// A press ANYWHERE while the sheet is up is the sheet's press. Outside the box
// it does nothing at all: it may not reach the conversation, the tab bar or an
// action underneath, and it does not dismiss either — a sheet somebody opened
// on purpose must not throw the filter away because somebody's aim was off. The
// two ways out are `esc` and the cancel target, and both are named on the sheet.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

// The sheet's own numbers.
//
// THE WIDTH IS BOUNDED AND THAT IS THE WHOLE POINT OF IT. The list laid out at
// the terminal's own width put a name and its price a hundred cells apart on a
// wide screen — two things a person's eye cannot associate. A hundred cells is
// about as wide as a model id and its facts stay readable together.
const (
	// pickSheetWide is the widest the sheet is ever drawn.
	pickSheetWide = 100
	// pickSheetTall is the tallest. It is generous enough for the picker's own
	// twelve-row want plus chrome, and bounded so the sheet reads as an object
	// on a very tall terminal rather than as a second screen.
	pickSheetTall = 28
	// pickSheetSide is the least clear margin left and right, so the frame
	// never sits against the terminal's own edge.
	pickSheetSide = 3
	// pickSheetFloor is the narrowest sheet that keeps its margins. Under it
	// the sheet takes the window whole: a person on a phone-width terminal needs
	// the names more than they need the margin, which is the same trade the
	// context chooser and the switcher card make.
	pickSheetFloor = 44
	// pickSheetChrome is how many rows the frame itself costs: the head rule,
	// the box, the thin separator under it, and the foot rule.
	pickSheetChrome = 4
)

// pickWin is where the last paint put the sheet, in SCREEN cells. Everything
// the pointer asks is answered from here (this file's header states the law).
type pickWin struct {
	// left, top, width and height are the sheet's outer rectangle.
	left, top, width, height int
	// bodyX and bodyY are the screen cell that [picker.rows]' own origin — row
	// zero, column zero of the list block — was drawn at.
	bodyX, bodyY int
	// bodyRows is how many of those rows were drawn.
	bodyRows int
	// boxY is the filter row, and boxX where its text starts, so a press in the
	// box can put the caret under the pointer.
	boxY, boxX int
	// cancel is the cancel target on the foot rule, and cancelY the row it is on.
	cancel  hudSpan
	cancelY int
}

// holds reports whether a screen cell is inside the sheet itself.
func (w pickWin) holds(x, y int) bool {
	return w.width > 0 && w.height > 0 &&
		x >= w.left && x < w.left+w.width && y >= w.top && y < w.top+w.height
}

// pickModalShowing is whether the chat overlay's model sheet owns the frame.
// It is `a.pick` alone: settings, home and the composer keep their own embedded
// pickers and never reach this door.
func (a *app) pickModalShowing() bool { return a.pick.open }

// ── the frame ───────────────────────────────────────────────────────────────

// pickModalOver composites the sheet onto a finished chat frame and answers
// where the caret goes.
//
// THE ROWS UNDERNEATH ARE FADED AND NOT BLANKED. A modal drawn over a screenful
// of spaces would have hidden the very thing a person is choosing a model FOR —
// and covering the transcript with blanks and calling it a modal is not the
// fix. Fading is the statement this surface already makes about a layer that is
// not live (hop.go's card, the context chooser, and composerFade under them).
func (a *app) pickModalOver(under []string, width, height int) ([]string, int, int) {
	out := make([]string, len(under))
	for i, line := range under {
		out[i] = composerFade(line, a.pal)
	}
	sheet, caretX, caretY := a.pickSheet(width, height)
	win := a.pick.win
	for i, line := range sheet {
		at := win.top + i
		if at < 0 || at >= len(out) {
			continue
		}
		out[at] = contextInlay(out[at], line, win.left, win.left+win.width, width)
	}
	return out, caretX, caretY
}

// pickSheet draws the sheet and records where every part of it landed.
func (a *app) pickSheet(width, height int) ([]string, int, int) {
	p := &a.pick
	if height <= 0 {
		p.win = pickWin{}
		a.caret = false
		return nil, 0, 0
	}
	box := a.pickSheetWidth()
	inner := a.pickInner()
	// The body asks for what the list wants and is given what there is. The
	// filter sits IN the sheet chrome, so [picker.height] is still the list
	// part alone — the same number overlayHeight used to hand the frame.
	body := 0
	if height >= pickSheetChrome {
		body = min(min(p.height(inner), pickSheetTall-pickSheetChrome),
			height-pickSheetChrome)
		body = max(body, 0)
	}
	drawHead := height >= 2
	drawBox := height >= 3
	drawThin := height >= pickSheetChrome
	left := (width - box) / 2

	// The hovered SCREEN line within the list block. It is the SAME number the
	// press uses for lighting, which is what this file's one-hit-map law means
	// in practice — and it is a line index, not a list index, because
	// [overlayFill] lights either half of a two-line row from the pointer's row.
	hover := -1
	if a.hot.kind == hoverOverlay {
		hover = a.hot.index
	}
	rows := p.rows(inner, body, a.pal, hover, a.reasoningFor)

	glyph := contextGlyphs(a.pal)
	out := make([]string, 0, min(height, pickSheetTall))
	if drawHead {
		out = append(out, a.pickHeadRule(inner, glyph))
	}
	// EVERY ROW OF THE SHEET GOES THROUGH ONE PADDER, so the right edge lands in
	// one column on every one of them. A row measured its own way is a frame with
	// a notch in it, which is the first thing an eye finds and the last thing
	// anybody wants to debug.
	side := func(content string) string {
		return glyph.side + contextPadded(content, inner) + glyph.side
	}
	caretX := 0
	boxRow := -1
	boxX := left + 1
	if drawBox {
		room := max(inner, 1)
		boxRows, drawnCaretX, _ := draftBlock(&p.filter, a.pal, room, 1,
			p.hintAt(max(room-ansi.StringWidth(prompt), 0)), "")
		line := ""
		if len(boxRows) > 0 {
			line = boxRows[0]
		}
		boxRow = len(out)
		caretX = drawnCaretX
		out = append(out, side(line))
	} else {
		// With no box there is nowhere on this frame to type. Hiding the caret is
		// the same answer every other non-writing surface gives (view.go).
		a.caret = false
	}
	if drawThin {
		out = append(out, side(a.pal.dim(strings.Repeat(glyph.thin, max(inner, 1)))))
	}
	bodyRow := len(out)
	for i := 0; i < body; i++ {
		row := ""
		if i < len(rows) {
			row = rows[i]
		}
		out = append(out, side(row))
	}
	foot, cancel := a.pickFootRule(inner, glyph)
	out = append(out, foot)
	tall := len(out)
	// CENTRED AND NUDGED UP BY A THIRD, which is hop.go's own answer to the same
	// question: dead centre reads as low, because the eye's centre is above the
	// frame's. The size comes from what was actually built, and the final clamp
	// keeps every one of those rows on the terminal.
	top := max((height-tall)/3, 0)
	if top+tall > height {
		top = max(height-tall, 0)
	}
	bodyY := top + bodyRow
	boxY := -1
	if boxRow >= 0 {
		boxY = top + boxRow
	}
	screenCancel := hudSpan{}
	if cancel.pressable() {
		screenCancel = hudSpan{from: left + cancel.from, to: left + cancel.to}
	}

	p.win = pickWin{
		left: left, top: top, width: box, height: len(out),
		bodyX: left + 1, bodyY: bodyY,
		bodyRows: body,
		boxY:     boxY, boxX: boxX,
		cancel:  screenCancel,
		cancelY: top + len(out) - 1,
	}
	// The caret is in the box, which is the one thing on this sheet a person
	// types into.
	return out, p.win.boxX + caretX, p.win.boxY
}

// pickInner is how many cells the list itself is laid out in — the sheet's
// width less its two edges. It is the ONE answer, asked by the paint and by
// anything that needs to know what the list was drawn at.
func (a *app) pickInner() int {
	return max(a.pickSheetWidth()-2, 1)
}

// pickSheetWidth is how wide the sheet is drawn: bounded, centred, and the
// whole window on a terminal too narrow to lend it a margin.
func (a *app) pickSheetWidth() int {
	width, _ := a.size()
	if width <= pickSheetFloor {
		return max(width, 8)
	}
	return min(width-2*pickSheetSide, pickSheetWide)
}

// pickTitleWord is what the sheet calls itself when the subject is the
// conversation. It is a constant because the manual quotes it exactly as it is
// spelled here.
const pickTitleWord = "choose model"

// pickTaskTitleWord is what the same sheet calls itself when a task's model
// door opened it — the subject is that node, not the conversation
// ([picker.task]).
const pickTaskTitleWord = "choose task model"

// pickHeadRule is the sheet's top edge with its title in it: what this sheet
// IS on the left, and which model is marked on the right.
func (a *app) pickHeadRule(inner int, glyph contextGlyph) string {
	title := pickTitleWord
	if a.pick.task != 0 {
		title = pickTaskTitleWord
	}
	left := a.pal.dim(glyph.rule) + " " + a.pal.bold(a.pal.ink(title)) + " "
	used := 1 + 1 + ansi.StringWidth(title) + 1
	where := a.pick.current
	right := ""
	if where != "" && inner-used-3 >= 8 {
		where = fit(where, inner-used-3)
		right = " " + where + " "
	}
	return glyph.topLeft + left +
		a.pal.dim(strings.Repeat(glyph.rule, max(inner-used-ansi.StringWidth(right), 0))) +
		a.pal.dim(right) + glyph.topRight
}

// pickFootRule is the sheet's bottom edge with the cancel target on it, and
// the cells that target was drawn in. The word is the context chooser's own
// spelling, so one surface does not teach two ways out of a sheet.
func (a *app) pickFootRule(inner int, glyph contextGlyph) (string, hudSpan) {
	word := contextCancelWord
	if inner < ansi.StringWidth(word)+6 {
		return glyph.footLeft + a.pal.dim(strings.Repeat(glyph.rule, max(inner, 0))) + glyph.footRight, hudSpan{}
	}
	// The rule, then the word with a clear cell either side of it, then one more
	// cell of rule before the corner — so the target has air around it and the
	// foot still reads as an edge.
	gap := inner - ansi.StringWidth(word) - 3
	painted := a.pal.dim(word)
	if a.hot.kind == hoverContextCancel {
		painted = a.pal.cursor(painted, 0)
	}
	// The span is in the SHEET's own cells: one for the corner, then the rule.
	span := hudSpan{from: 1 + gap + 1}
	span.to = span.from + ansi.StringWidth(word)
	return glyph.footLeft + a.pal.dim(strings.Repeat(glyph.rule, gap)) + " " + painted + " " +
		a.pal.dim(glyph.rule) + glyph.footRight, span
}

// ── the pointer ─────────────────────────────────────────────────────────────

// pickModalPress resolves a press while the sheet is up, and it ALWAYS takes
// it: a press outside the sheet may not reach the conversation, a tab or an
// action underneath (this file's header states why it does not dismiss either).
func (a *app) pickModalPress(x, y int) (tea.Cmd, bool) {
	if !a.pickModalShowing() {
		return nil, false
	}
	win := a.pick.win
	// A row number is meaningful only inside the sheet's rectangle. Lateral
	// backdrop cells must not inherit that row's navigation or confirm action.
	if !win.holds(x, y) {
		return nil, true
	}
	if y == win.cancelY && win.cancel.holds(x) {
		a.pick.close()
		a.touch()
		return nil, true
	}
	if y == win.boxY && x >= win.boxX {
		// THE BOX TAKES A PRESS THE WAY THE DRAFT DOES: the caret lands under the
		// pointer rather than at the end (draftclick.go's own reason — the one
		// place a person types is the one place their pointer has to work).
		a.pick.filter.cursor = min(max(x-win.boxX, 0), len(a.pick.filter.value))
		a.touch()
		return nil, true
	}
	if row := y - win.bodyY; row >= 0 && row < win.bodyRows {
		return a.pickBodyPress(row)
	}
	// Inside the frame but on the chrome, or outside it altogether. Swallowed,
	// and nothing happens.
	return nil, true
}

// pickBodyPress turns a screen line of the list block into a cursor move or a
// confirm. The owner of each LINE comes from [picker.rowsOwned], because a
// phone-width row is two lines and "line i is list index top+i" stopped being
// true the day a row could wrap (settings.go's [sheet.selectLines] states the
// same law for the panel).
//
// THE FIRST PRESS MOVES THE CURSOR; THE SECOND ON THE SAME ROW CONFIRMS. That
// is the settings panel's own answer to a click on a model row, and a sheet that
// chose on the first press would switch a model somebody was only pointing at.
func (a *app) pickBodyPress(row int) (tea.Cmd, bool) {
	_, owners := a.pick.rowsOwned(a.pickInner(), a.pick.win.bodyRows, a.pal, -1, a.reasoningFor)
	if row < 0 || row >= len(owners) {
		return nil, true
	}
	at := owners[row]
	if at < 0 {
		return nil, true
	}
	if a.pick.cursor != at {
		a.pick.cursor = at
		a.touch()
		return nil, true
	}
	return a.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}), true
}

// pickModalWheel is the wheel while the sheet is up, and it takes that too.
//
// THE WHEEL FOLLOWS THE POINTER AND NOT A FOCUS: turned over the list it walks
// the list, and turned anywhere else on the screen it does nothing — the
// conversation underneath is not live and must not move.
func (a *app) pickModalWheel(x, y, delta int) (tea.Cmd, bool) {
	if !a.pickModalShowing() {
		return nil, false
	}
	win := a.pick.win
	if !win.holds(x, y) || delta == 0 {
		return nil, true
	}
	row := y - win.bodyY
	if row < 0 || row >= win.bodyRows {
		return nil, true
	}
	a.pick.move(delta)
	a.touch()
	return nil, true
}

// pickModalHover is what the pointer is over, in the sheet's own alphabet.
// It answers for EVERY cell while the sheet is up, because nothing underneath
// may light while it is (hover.go's law, applied to a layer rather than a row).
//
// The cancel target reuses [hoverContextCancel]: one kind for every sheet's
// way out, so the foot lights the same way on both. Body rows use
// [hoverOverlay] with index equal to the screen line inside the list block —
// the same number [pickSheet] hands [picker.rows] for lighting.
func (a *app) pickModalHover(x, y int) (hoverAt, bool) {
	if !a.pickModalShowing() {
		return hoverAt{}, false
	}
	win := a.pick.win
	if !win.holds(x, y) {
		return hoverAt{}, true
	}
	if y == win.cancelY && win.cancel.holds(x) {
		return hoverAt{kind: hoverContextCancel}, true
	}
	if row := y - win.bodyY; row >= 0 && row < win.bodyRows {
		return hoverAt{kind: hoverOverlay, index: row}, true
	}
	return hoverAt{}, true
}
