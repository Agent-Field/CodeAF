package tui3

import (
	"strings"
	"time"

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
// So the drag is answered in kind: press, sweep, and the rows under the sweep
// wear the selection highlight; release, and their text is on the clipboard —
// stripped of paint and rails exactly as copy mode strips it, written over
// OSC 52 so it works over ssh and through tmux, with a word on the status line
// saying how many lines landed. Rows rather than characters, deliberately: a
// transcript is made of lines, the highlight can then be exactly what is
// copied, and a row-based sweep has no seams around wide glyphs.
//
// ── THE GESTURE IS ANCHORED TO CONTENT, NOT TO THE GLASS ──
//
// Every coordinate this file keeps is an index into the body's OWN row list,
// converted from the screen the moment the event arrives. The first build kept
// screen rows, and a streaming room broke both gestures at once: a task's page
// follows its live edge and repaints four times a second, so between a press
// and its release the text slid up under the pointer — the parked click fired
// on whatever had scrolled into the cell (usually nothing), and a sweep copied
// rows the person never highlighted. Anchored to content, the selection rides
// the scroll, the click opens the row that was actually pressed, and a row
// that has scrolled clean off the screen resolves to no click at all.
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
	// on is a sweep past the slop threshold. anchorRow is the content row the
	// press landed on and row is the one under the pointer now; the rows
	// between them, inclusive, are the selection.
	on             bool
	anchorRow, row int
}

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
//
// WHAT IT COSTS IS ONE GESTURE: selecting exactly two adjacent rows in a single
// straight drag. The sweep still starts the moment the pointer passes the slop,
// and once it has started it stays started — so those two rows are had by
// sweeping past them and coming back. Selecting ONE row is unaffected: that is
// done by sweeping sideways within it, which the column slop above already
// admits as a sweep once it is wider than a wobble.
const (
	dragSlop     = 3
	dragSlopRows = 1
)

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

// dragSpan is the selection as CONTENT rows, low first: the live sweep while
// the button is down, and then — for as long as the status line still says
// "copied" — the rows the release just copied, kept lit so a person can see
// exactly what landed on the clipboard instead of watching their selection
// vanish the moment they let go.
func (a *app) dragSpan() (int, int, bool) {
	if a.drag.on {
		if a.drag.anchorRow <= a.drag.row {
			return a.drag.anchorRow, a.drag.row, true
		}
		return a.drag.row, a.drag.anchorRow, true
	}
	if a.dragCopied > 0 && time.Now().Before(a.dragUntil) {
		return a.dragFrom, a.dragTo, true
	}
	return 0, 0, false
}

// dragMotion folds one moved-with-the-button-down event in, and reports whether
// it was taken. The first move past the slop is what turns a parked click into
// a selection; every move after that just grows it.
func (a *app) dragMotion(x, y int) bool {
	if !a.drag.parked && !a.drag.on {
		return false
	}
	if !a.drag.on {
		if abs(y-a.drag.py) <= dragSlopRows && abs(x-a.drag.px) < dragSlop {
			return true
		}
		a.drag.on, a.drag.anchorRow = true, a.drag.prow
	}
	if at := a.bodyContentRow(y); at >= 0 && a.drag.row != at {
		a.drag.row = at
		a.touch()
	}
	return true
}

// dragRelease ends the gesture, whichever it turned out to be: a sweep is
// copied, a parked click is spent on the body at the row it pressed, and a
// release nothing owns is nothing.
func (a *app) dragRelease() tea.Cmd {
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

// dragYank copies the swept rows: the drawn text under the sweep, stripped of
// escapes and rails the way copy mode strips its yank, onto the clipboard the
// same OSC 52 way. What was highlighted is exactly what lands.
func (a *app) dragYank(drag dragSelect) tea.Cmd {
	body := a.allBodyRows()
	from, to := drag.anchorRow, drag.row
	if from > to {
		from, to = to, from
	}
	from, to = max(from, 0), min(to, len(body)-1)
	if from > to || len(body) == 0 {
		return nil
	}
	lines := make([]string, 0, to-from+1)
	for _, r := range body[from : to+1] {
		lines = append(lines, copyClean(ansi.Strip(r.text)))
	}
	a.dragCopied, a.dragUntil = len(lines), time.Now().Add(dragFlashFor)
	a.dragFrom, a.dragTo = from, to
	a.touch()
	// The flash needs one more frame when it expires, or "copied" would sit on
	// an idle status line forever.
	return tea.Batch(
		tea.Raw(osc52(strings.Join(lines, "\n"), a.tmux)),
		tea.Tick(dragFlashFor, func(time.Time) tea.Msg { return dragFlashMsg{} }),
	)
}

// dragFlashFor is how long the status line says what a sweep copied — and how
// long the copied rows stay lit after the release (dragSpan).
const dragFlashFor = 3 * time.Second

// dragFlashMsg is the flash expiring: one repaint, so the word comes down.
type dragFlashMsg struct{}

// dragWord is the status line's account of the last sweep, and "" once it has
// expired.
func (a *app) dragWord() string {
	if a.dragCopied <= 0 || !time.Now().Before(a.dragUntil) {
		return ""
	}
	if a.dragCopied == 1 {
		return "copied · 1 line"
	}
	return "copied · " + itoa(a.dragCopied) + " lines"
}
