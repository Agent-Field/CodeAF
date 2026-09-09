package tui3

// THE CONTEXT CHOOSER, AS A SHEET THAT OWNS THE SCREEN.
//
// `/folder`, `/place`, `/dir` and a bare `/attach` all open ONE chooser, and
// from this wave it is a real modal: a bounded, framed sheet drawn over a faded
// conversation, holding the keyboard and every pointer event until it is
// answered or cancelled.
//
// ── WHAT THIS REPLACED, AND WHY ─────────────────────────────────────────────
//
// The browser used to be BOTTOM CHROME. [app.overlayHeight] answered the
// picker's want and view.go appended its rows between the draft and the status
// line, the same slot the model picker and the command list live in. On a
// two-hundred-column terminal that meant a column of folder names with a
// hundred cells of dead space beside it, the previous turn's error line still on
// screen above it, no framing of any kind, and nothing to say a chooser had
// opened at all — the conversation simply grew a list. The owner's own word for
// it was that they expected to have opened something.
//
// So the chooser is now what every other deliberate surface in this package is:
// a frame taken WHOLE at the top of [app.frameBody], beside the places, the
// rewind timeline and the status sheet. It differs from those four in one way,
// and the difference is the point — they REPLACE the conversation and this one
// COVERS it. The chat frame is composed exactly as it would have been and then
// faded to the depth ladder's faintest stop, so a person choosing a folder can
// still see the message they were part way through writing, and cannot touch it.
//
// ── THE ARRANGEMENT ─────────────────────────────────────────────────────────
//
//	╭─ add context ─────────────────────────────── ~/code/aforge-v2 ─╮
//	│  › ~/code/aforge-v2/internal/                                  │  the box
//	│  ──────────────────────────────────────────────────────────    │
//	│  ~ › code › aforge-v2 › internal                               │  breadcrumb
//	│  aforge-v2/  › session/        contextpreview.go     14.2 KB   │  the columns
//	│              tui3/             folderpick.go         41.8 KB   │  and the preview
//	│  2 chosen · ▪ session/  ▪ notes.md                             │  the tray
//	│  esc cancel · alt+o preview · alt+m choose · ←→ walk            │  the legend
//	│  add this folder · ~/code/aforge-v2/internal   repository·main  │  the action
//	╰──────────────────────────────────────────────── esc · cancel ──╯
//
// Everything between the breadcrumb and the action row is folderpick.go's own
// [folderPick.rows], unchanged: this file does not draw a second browser, it
// frames the one that already exists. What it adds is the title, the location,
// the box in a place of its own, the framing, and one explicit CANCEL target on
// the foot rule — because a modal whose only way out is a key nobody was told
// about is a modal people get stuck in.
//
// ── ONE HIT MAP ─────────────────────────────────────────────────────────────
//
// [contextWin] is what the last paint put on the SCREEN, and it is the only
// answer the pointer gets. Press, hover, wheel and the terminal's own caret all
// resolve through it, so what lights, what a press acts on and where the cursor
// blinks cannot be three different opinions about where the sheet is. Inside it
// the rows are folderpick.go's own row numbers and the columns are its own
// cells, so [folderGeom] keeps working exactly as it did — there is one layout
// and this file merely says where on the screen it landed.
//
// A press ANYWHERE while the sheet is up is the sheet's press. Outside the box
// it does nothing at all: it may not reach the conversation, the tab bar or an
// action underneath, and it does not dismiss either — a sheet holding four
// chosen things must not throw them away because somebody's aim was off. The
// two ways out are `esc` and the cancel target, and both are named on the sheet.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

// The sheet's own numbers.
//
// THE WIDTH IS BOUNDED AND THAT IS THE WHOLE POINT OF IT. The browser laid out
// at the terminal's own width put a name and its size a hundred cells apart on a
// wide screen — two things a person's eye cannot associate, which is the defect
// the screenshot shows. A hundred and twenty-eight cells is about as wide as
// three columns of paths stay readable, and it is close to the proportion the
// owner's reference (../reference-yazi.png) keeps on a large display.
const (
	// contextSheetWide is the widest the sheet is ever drawn.
	contextSheetWide = 128
	// contextSheetTall is the tallest. It is generous — the preview is what the
	// rows are for — and bounded, so the sheet reads as an object on a very tall
	// terminal rather than as a second screen.
	contextSheetTall = 34
	// contextSheetSide is the least clear margin left and right, so the frame
	// never sits against the terminal's own edge.
	contextSheetSide = 3
	// contextSheetFloor is the narrowest sheet that keeps its margins. Under it
	// the sheet takes the window whole: a person on a phone-width terminal needs
	// the names more than they need the margin, which is the same trade the
	// switcher card makes (hop.go).
	contextSheetFloor = 44
	// contextSheetChrome is how many rows the frame itself costs: the head rule,
	// the box, the thin separator under it, and the foot rule.
	contextSheetChrome = 4
)

// contextWin is where the last paint put the sheet, in SCREEN cells. Everything
// the pointer asks is answered from here (this file's header states the law).
type contextWin struct {
	// left, top, width and height are the sheet's outer rectangle.
	left, top, width, height int
	// bodyX and bodyY are the screen cell that [folderGeom]'s own origin — row
	// zero, column zero of [folderPick.rows] — was drawn at.
	bodyX, bodyY int
	// bodyRows is how many of those rows were drawn.
	bodyRows int
	// boxY is the search-and-path row, and boxX where its text starts, so a press
	// in the box can put the caret under the pointer.
	boxY, boxX int
	// cancel is the cancel target on the foot rule, and cancelY the row it is on.
	cancel  hudSpan
	cancelY int
}

// holds reports whether a screen cell is inside the sheet itself.
func (w contextWin) holds(x, y int) bool {
	return w.width > 0 && w.height > 0 &&
		x >= w.left && x < w.left+w.width && y >= w.top && y < w.top+w.height
}

// contextModalShowing is whether the chooser owns the frame. It is the browser
// being open and nothing else: there is one chooser and it is always modal.
func (a *app) contextModalShowing() bool { return a.folder.open }

// closeContextSheet is the one way out of the chooser, and it asks for the
// whole screen back on the way.
//
// THE SHEET IS THE ONLY SURFACE HERE THAT COVERS RATHER THAN REPLACES. The
// ordinary frame revealed after it closes often has rows shorter than the
// sheet, and the incremental renderer does not always overwrite the cells the
// sheet lit beyond those rows. Padding cannot repair that: the renderer clears
// its cell buffer before every frame, so explicit trailing spaces and absent
// cells produce the same diff. This is one full repaint at the one moment this
// surface has a layer to undraw, routed through one door so none of the ways out
// can leave the layer behind.
func (a *app) closeContextSheet() tea.Cmd {
	a.folder.close()
	a.touch()
	return tea.ClearScreen
}

// ── the frame ───────────────────────────────────────────────────────────────

// contextModalOver composites the sheet onto a finished chat frame and answers
// where the caret goes.
//
// THE ROWS UNDERNEATH ARE FADED AND NOT BLANKED. A modal drawn over a screenful
// of spaces would have hidden the very thing a person is choosing context FOR —
// and the brief is explicit that covering the transcript with blanks and calling
// it a modal is not the fix. Fading is the statement this surface already makes
// about a layer that is not live (hop.go's card, and composerFade under it).
func (a *app) contextModalOver(under []string, width, height int) ([]string, int, int) {
	out := make([]string, len(under))
	for i, line := range under {
		out[i] = composerFade(line, a.pal)
	}
	sheet, caretX, caretY := a.contextSheet(width, height)
	win := a.folder.win
	for i, line := range sheet {
		at := win.top + i
		if at < 0 || at >= len(out) {
			continue
		}
		out[at] = contextInlay(out[at], line, win.left, win.left+win.width, width)
	}
	return out, caretX, caretY
}

// contextInlay writes one row of the sheet INTO a faded row, keeping whatever
// the frame drew on either side of it.
//
// IT IS NOT A PAD AND A LINE, and the difference was a real defect: pasting
// `spaces + sheet` over the row dropped everything to the RIGHT of the sheet, so
// a window with a task rail or a wide status line went blank down one side while
// the same rows on the left stayed faded. A backdrop that is dim on one side and
// absent on the other is not a backdrop — and it is the layer this whole file
// claims to be drawing.
func contextInlay(under, sheet string, from, to, width int) string {
	left := ansi.Truncate(under, from, "")
	if gap := from - ansi.StringWidth(left); gap > 0 {
		left += strings.Repeat(" ", gap)
	}
	// The tail is what the row held past the sheet's right edge, cut at the same
	// cell the sheet ends on. ansi.Cut keeps the paint of the span it takes, so
	// the faded ink on that side survives.
	right := ""
	if to < width {
		right = ansi.Cut(under, to, width)
	}
	return left + sheet + right
}

// contextSheet draws the sheet and records where every part of it landed.
func (a *app) contextSheet(width, height int) ([]string, int, int) {
	f := &a.folder
	if height <= 0 {
		f.win = contextWin{}
		a.caret = false
		return nil, 0, 0
	}
	box := contextSheetWidth(width)
	inner := a.contextInner()
	// The body asks for what it wants and is given what there is. One row is
	// added to the browser's own want for the legend it draws above the action
	// row, which [folderPick.height] does not count. It may be zero:
	// [folderPick.rows] already answers nil for a nonpositive count, so the frame
	// does not need a second guard or a made-up browser row.
	want := f.height(inner) + 1 + contextSheetChrome
	body := 0
	if height >= contextSheetChrome {
		body = min(min(want-contextSheetChrome, contextSheetTall-contextSheetChrome),
			height-contextSheetChrome)
		body = max(body, 0)
	}
	drawHead := height >= 2
	drawBox := height >= 3
	drawThin := height >= contextSheetChrome
	left := (width - box) / 2

	// The hovered row and column, translated out of the pointer's own answer.
	// They are the SAME numbers the press uses, which is what this file's one-hit
	// -map law means in practice.
	hover, col := -1, ""
	if a.hot.kind == hoverOverlay {
		hover, col = a.hot.index, a.hot.key
	}
	rows := f.rows(inner, body, a.pal, a.styler(), hover, col)

	glyph := contextGlyphs(a.pal)
	out := make([]string, 0, min(height, contextSheetTall))
	if drawHead {
		out = append(out, a.contextHeadRule(inner, glyph))
	}
	// THE BOX SITS IN THE SAME LEFT MARGIN EVERY OTHER ROW OF THE SHEET SITS IN
	// ([folderPad]), so the `›` of the box and the names under it stand in one
	// column. It is the surface's own one-line box and not a widget of its own —
	// the same [draftBlock] the message composer and every other picker's filter
	// is drawn with.
	// EVERY ROW OF THE SHEET GOES THROUGH ONE PADDER, so the right edge lands in
	// one column on every one of them. A row measured its own way is a frame with
	// a notch in it, which is the first thing an eye finds and the last thing
	// anybody wants to debug.
	side := func(content string) string {
		return glyph.side + contextPadded(content, inner) + glyph.side
	}
	caretX := 0
	boxRow := -1
	boxX := left + 1 + len(folderPad)
	if drawBox {
		room := max(inner-len(folderPad), 1)
		boxRows, drawnCaretX, _ := draftBlock(&f.filter, a.pal, room, 1,
			f.folderHintAt(room-ansi.StringWidth(prompt)), "")
		line := ""
		if len(boxRows) > 0 {
			line = boxRows[0]
		}
		boxRow = len(out)
		caretX = drawnCaretX
		out = append(out, side(folderPad+line))
	} else {
		// With no box there is nowhere on this frame to type. Hiding the caret is
		// the same answer every other non-writing surface gives (view.go).
		a.caret = false
	}
	if drawThin {
		out = append(out, side(folderPad+a.pal.dim(strings.Repeat(glyph.thin, max(inner-2*len(folderPad), 1)))))
	}
	bodyRow := len(out)
	for i := 0; i < body; i++ {
		row := ""
		if i < len(rows) {
			row = rows[i]
		}
		out = append(out, side(row))
	}
	foot, cancel := a.contextFootRule(inner, glyph)
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

	f.win = contextWin{
		left: left, top: top, width: box, height: len(out),
		bodyX: left + 1, bodyY: bodyY,
		bodyRows: body,
		boxY:     boxY, boxX: boxX,
		cancel:  screenCancel,
		cancelY: top + len(out) - 1,
	}
	// The caret is in the box, which is the one thing on this sheet a person
	// types into.
	return out, f.win.boxX + caretX, f.win.boxY
}

// contextInner is how many cells the browser itself is laid out in — the
// sheet's width less its two edges. It is the ONE answer, asked by the paint and
// by anything that needs to know what the browser was drawn at.
func (a *app) contextInner() int {
	width, _ := a.size()
	return max(contextSheetWidth(width)-2, 1)
}

// contextSheetWidth is how wide the sheet is drawn: bounded, centred, and the
// whole window on a terminal too narrow to lend it a margin.
func contextSheetWidth(width int) int {
	if width <= contextSheetFloor {
		return max(width, 8)
	}
	return min(width-2*contextSheetSide, contextSheetWide)
}

// contextGlyphs is the framing, at whichever floor this terminal stands on.
// A terminal that cannot be trusted with box drawing is given the three
// characters every terminal since 1978 can draw, rather than something close
// (styles.go states that rule for the rail).
type contextGlyph struct{ topLeft, topRight, footLeft, footRight, rule, thin, side string }

func contextGlyphs(pal palette) contextGlyph {
	if pal.ascii {
		return contextGlyph{"+", "+", "+", "+", "-", "-", "|"}
	}
	return contextGlyph{"╭", "╮", "╰", "╯", "─", "─", "│"}
}

// contextHeadRule is the sheet's top edge with its title in it: what this sheet
// IS on the left, and WHERE it is standing on the right.
//
// The title is the sheet's own name for itself and is spelled the same way the
// manual spells it, because a person asking "what is this window" is asking the
// manual the same question.
func (a *app) contextHeadRule(inner int, glyph contextGlyph) string {
	title := contextTitleWord
	left := a.pal.dim(glyph.rule) + " " + a.pal.bold(a.pal.ink(title)) + " "
	used := 1 + 1 + ansi.StringWidth(title) + 1
	where := ""
	if a.folder.browsing {
		where = tildePath(a.folder.cols.dir, a.folder.tilde)
	}
	right := ""
	if where != "" && inner-used-3 >= folderNameFloor {
		where = fit(where, inner-used-3)
		right = " " + where + " "
	}
	return glyph.topLeft + left +
		a.pal.dim(strings.Repeat(glyph.rule, max(inner-used-ansi.StringWidth(right), 0))) +
		a.pal.dim(right) + glyph.topRight
}

// contextTitleWord is what the sheet calls itself. It is `add context` and not
// `folder` because the same sheet chooses files, and it is a constant because
// the manual quotes it exactly as it is spelled here.
const contextTitleWord = "add context"

// contextCancelWord is the explicit way out, drawn on the foot rule and
// pressable. It names the key AND the act, because the key is the fast way and
// the words are the discoverable one (docs/DESIGN-LANGUAGE.md: every chord keeps
// a visible, clickable door beside it).
const contextCancelWord = "esc · cancel"

// contextFootRule is the sheet's bottom edge with the cancel target on it, and
// the cells that target was drawn in.
func (a *app) contextFootRule(inner int, glyph contextGlyph) (string, hudSpan) {
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

// contextPadded pads one painted row out to the sheet's inner width, measuring
// through the escape sequences rather than around them, and cutting a row that
// somehow came back too long — the frame's right edge has to land in one column
// on every row or the sheet is not a rectangle.
func contextPadded(painted string, room int) string {
	if room < 1 {
		return ""
	}
	width := ansi.StringWidth(painted)
	if width > room {
		return ansi.Truncate(painted, room, "")
	}
	return painted + strings.Repeat(" ", room-width)
}

// ── the pointer ─────────────────────────────────────────────────────────────

// contextModalPress resolves a press while the sheet is up, and it ALWAYS takes
// it: a press outside the sheet may not reach the conversation, a tab or an
// action underneath (this file's header states why it does not dismiss either).
func (a *app) contextModalPress(x, y int) (tea.Cmd, bool) {
	if !a.contextModalShowing() {
		return nil, false
	}
	win := a.folder.win
	// A row number is meaningful only inside the sheet's rectangle. Lateral
	// backdrop cells must not inherit that row's navigation or confirm action.
	if !win.holds(x, y) {
		return nil, true
	}
	if y == win.cancelY && win.cancel.holds(x) {
		return a.closeContextSheet(), true
	}
	if y == win.boxY && x >= win.boxX {
		// THE BOX TAKES A PRESS THE WAY THE DRAFT DOES: the caret lands under the
		// pointer rather than at the end (draftclick.go's own reason — the one
		// place a person types is the one place their pointer has to work).
		a.folder.filter.cursor = min(max(x-win.boxX, 0), len(a.folder.filter.value))
		a.touch()
		return nil, true
	}
	if row := y - win.bodyY; row >= 0 && row < win.bodyRows {
		return a.folderRowPress(x-win.bodyX, row)
	}
	// Inside the frame but on the chrome, or outside it altogether. Swallowed,
	// and nothing happens.
	return nil, true
}

// contextModalWheel is the wheel while the sheet is up, and it takes that too.
//
// THE WHEEL FOLLOWS THE POINTER AND NOT A FOCUS: turned over the preview it
// scrolls the preview, turned over the names it walks the names, and turned
// anywhere else on the screen it does nothing — the conversation underneath is
// not live and must not move (folderplace.go's [app.folderWheel] states the
// first half of this rule; the modal is what makes the second half true).
func (a *app) contextModalWheel(x, y, delta int) (tea.Cmd, bool) {
	if !a.contextModalShowing() {
		return nil, false
	}
	win := a.folder.win
	if !win.holds(x, y) || delta == 0 {
		return nil, true
	}
	row := y - win.bodyY
	if row < 0 || row >= win.bodyRows {
		return nil, true
	}
	if col, ok := a.folderHoverColumn(x-win.bodyX, row); ok && col == folderColPane &&
		a.folder.pane != folderPaneOff {
		a.folder.paneStep(delta)
		a.touch()
		return nil, true
	}
	a.folder.move(delta)
	a.touch()
	return a.folderWork(), true
}

// contextModalHover is what the pointer is over, in the sheet's own alphabet.
// It answers for EVERY cell while the sheet is up, because nothing underneath
// may light while it is (hover.go's law, applied to a layer rather than a row).
func (a *app) contextModalHover(x, y int) (hoverAt, bool) {
	if !a.contextModalShowing() {
		return hoverAt{}, false
	}
	win := a.folder.win
	if !win.holds(x, y) {
		a.folder.trayHot, a.folder.paneHot = -1, -1
		return hoverAt{}, true
	}
	if y == win.cancelY && win.cancel.holds(x) {
		return hoverAt{kind: hoverContextCancel}, true
	}
	if row := y - win.bodyY; row >= 0 && row < win.bodyRows {
		if key, ok := a.folderHoverColumn(x-win.bodyX, row); ok {
			return hoverAt{kind: hoverOverlay, index: row, key: key}, true
		}
		return hoverAt{kind: hoverOverlay, index: row}, true
	}
	a.folder.trayHot = -1
	a.folder.paneHot = -1
	return hoverAt{}, true
}
