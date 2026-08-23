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
	// parked is a left press the body has not answered yet: the click fires at
	// (px, py) on release, unless the pointer moves first and turns it into a
	// selection.
	parked bool
	px, py int
	// on is a sweep past the slop threshold. anchorY is where the press landed
	// and y is where the pointer is, both in screen rows; the rows between them,
	// inclusive, are the selection.
	on         bool
	anchorY, y int
}

// dragSlop is how many columns a pressed pointer may wander sideways and still
// be a click. Any row change is a sweep — rows are what a sweep selects — but a
// hand is not a vice, and a press that slid two cells is a press.
const dragSlop = 3

// dragSpan is the selection in screen rows, low first.
func (a *app) dragSpan() (int, int, bool) {
	if !a.drag.on {
		return 0, 0, false
	}
	if a.drag.anchorY <= a.drag.y {
		return a.drag.anchorY, a.drag.y, true
	}
	return a.drag.y, a.drag.anchorY, true
}

// dragMotion folds one moved-with-the-button-down event in, and reports whether
// it was taken. The first move past the slop is what turns a parked click into
// a selection; every move after that just grows it.
func (a *app) dragMotion(x, y int) bool {
	if !a.drag.parked && !a.drag.on {
		return false
	}
	if !a.drag.on {
		if y == a.drag.py && abs(x-a.drag.px) < dragSlop {
			return true
		}
		a.drag.on, a.drag.anchorY = true, a.drag.py
	}
	if a.drag.y != y {
		a.drag.y = y
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
	if drag.parked {
		// The body's click, exactly as it ran on press before this file existed
		// — including the room pump the spawn-card door needs (app.go states
		// why the batch matters).
		return tea.Batch(a.press(drag.px, drag.py), a.takeRoomPump())
	}
	return nil
}

// dragYank copies the swept rows: the drawn text under the sweep, stripped of
// escapes and rails the way copy mode strips its yank, onto the clipboard the
// same OSC 52 way. What was highlighted is exactly what lands.
func (a *app) dragYank(drag dragSelect) tea.Cmd {
	top := a.bodyTop()
	if top < 0 {
		return nil
	}
	body, _ := a.bodyRows(a.bodyWidth(), a.viewHeight())
	from, to := drag.anchorY, drag.y
	if from > to {
		from, to = to, from
	}
	from, to = max(from-top, 0), min(to-top, len(body)-1)
	if from > to {
		return nil
	}
	lines := make([]string, 0, to-from+1)
	for _, r := range body[from : to+1] {
		lines = append(lines, copyClean(ansi.Strip(r.text)))
	}
	a.dragCopied, a.dragUntil = len(lines), time.Now().Add(dragFlashFor)
	a.touch()
	// The flash needs one more frame when it expires, or "copied" would sit on
	// an idle status line forever.
	return tea.Batch(
		tea.Raw(osc52(strings.Join(lines, "\n"), a.tmux)),
		tea.Tick(dragFlashFor, func(time.Time) tea.Msg { return dragFlashMsg{} }),
	)
}

// dragFlashFor is how long the status line says what a sweep copied.
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

