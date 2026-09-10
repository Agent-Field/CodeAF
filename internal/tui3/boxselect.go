package tui3

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// ── THE SWEEP, IN A BOX ─────────────────────────────────────────────────────
//
// dragselect.go answers the sweep over the TRANSCRIPT and states the law it is
// answering: mouse reporting takes the terminal's own drag-select away, so the
// one gesture every terminal user owns has to be answered in kind. This file is
// the same bargain kept in the boxes — the message box at the foot of a
// conversation and the composer every place draws at the foot of its own screen
// — where the gesture reached nothing at all, because a press the box has
// already taken never parks a drag.
//
// Press, sweep, and the run under the pointer wears the selection; release, and
// its text is on the clipboard over OSC 52, with the same word on the status
// line the transcript's own copy puts there. A double-click takes the word, a
// triple-click takes the line, and a plain click is still the caret placement
// draftclick.go put there.
//
// ── AND THE SELECTION STAYS AFTERWARDS, WHICH THE TRANSCRIPT'S DOES NOT ──
//
// A copied run in the transcript is lit for three seconds and then forgotten,
// because there is nothing else to do with text you cannot edit. A box is
// different: the selection it is left holding is LIVE — type and it is
// replaced, backspace and it is gone (editselect.go) — so it stays until the
// caret moves or the text changes, exactly as it does in every text field.
//
// ── ONE BOX AT A TIME, HELD BY POINTER ──
//
// The sweep remembers WHICH box it began in and extends that one for as long as
// the button is down, rather than re-asking the frame on every motion. A hand
// sweeping down the last line of a draft leaves the box's rows entirely, and a
// sweep that re-resolved would stop dead at the boundary instead of running to
// the end of the text, which is what a sweep does everywhere else.

// boxDrag is a sweep over a text box.
type boxDrag struct {
	// box is the editor the press landed in, held for the life of the gesture.
	// The editors it points at live in the app itself — the message box, a
	// place's own box, the shared composer — so the pointer outlives every
	// frame the sweep crosses.
	box *editor
	on  bool
	// place says which arithmetic converts a pointer into an offset: the
	// composer a place draws at its foot, or the message box in a conversation.
	place bool
	// unit is what the gesture took, for the status line's word — the same
	// three the transcript's sweep answers in (dragselect.go).
	unit dragUnit
	// px, py is where the button went down, so a press that has not moved past
	// the slop is still a click and not a one-character selection.
	px, py int
	// swept is whether the pointer has moved past that slop yet.
	swept bool
}

// boxPressed is what a box's own press handler calls once it has put the caret
// under the pointer: it arms the sweep, and answers the double- and
// triple-click on the spot.
//
// THE CLICK COUNT IS THE SURFACE'S ONE COUNT (dragselect.go's [app.countClick]).
// A box with a counter of its own would let two clicks — one on a transcript
// row, one in the box — read as a double-click on neither.
func (a *app) boxPressed(box *editor, place bool, x, y int) {
	// A new press retires the lit remnant of the last copy, on the transcript's
	// own terms: one selection on screen at a time.
	a.dragCopied, a.dragInBox = 0, false
	a.boxSel = boxDrag{box: box, on: true, place: place, px: x, py: y}
	switch a.countClick(x, y) {
	case 2:
		box.pickWordAt(box.cursor)
		a.boxSel.unit, a.boxSel.swept = wordUnit, true
	case 3:
		box.pickLineAt(box.cursor)
		a.boxSel.unit, a.boxSel.swept = rowUnit, true
	default:
		box.dropPick()
	}
	a.touch()
}

// boxMotion folds one moved-with-the-button-down event into a box's sweep and
// reports whether it was taken. The first move past the slop is what turns a
// parked caret placement into a selection; every move after that grows it,
// including one that began as a double-click's word.
func (a *app) boxMotion(x, y int) bool {
	if !a.boxSel.on || a.boxSel.box == nil {
		return false
	}
	if !a.boxSel.swept {
		if abs(y-a.boxSel.py) <= dragSlopRows && abs(x-a.boxSel.px) < dragSlop {
			return true
		}
		a.boxSel.swept = true
		a.boxSel.unit = cellsUnit
		a.boxSel.box.pickFrom(a.boxSel.box.cursor)
	}
	at, ok := a.boxOffsetAt(x, y)
	if !ok {
		return true
	}
	if a.boxSel.box.cursor != at {
		// A sweep that started as a word or a line grows by characters from
		// there, the way the transcript's does.
		a.boxSel.unit = cellsUnit
		a.boxSel.box.pickTo(at)
		a.touch()
	}
	return true
}

// boxRelease ends a box's sweep: a selection is copied, and a press that never
// moved is nothing at all — the caret it placed was placed on the press.
func (a *app) boxRelease() (tea.Cmd, bool) {
	drag := a.boxSel
	if !drag.on {
		return nil, false
	}
	a.boxSel = boxDrag{}
	if drag.box == nil || !drag.swept {
		return nil, true
	}
	return a.boxYank(drag.box, drag.unit), true
}

// boxYank puts the selected run on the clipboard and says so.
//
// IT WRITES THE SAME STATUS WORD THE TRANSCRIPT'S SWEEP WRITES, through the
// same fields and the same three-second clock (dragselect.go's [app.dragWord]),
// because "copied · 12 chars" is one sentence this surface says about one thing
// it did. What it must NOT share is the lit CELLS: dragLit is a span of body
// rows, so [app.dragSel] is told the copy came from a box and paints nothing
// over the transcript.
func (a *app) boxYank(box *editor, unit dragUnit) tea.Cmd {
	text := box.selectedText()
	if text == "" {
		return nil
	}
	a.dragCopied = strings.Count(text, "\n") + 1
	a.dragChars = utf8.RuneCountInString(text)
	a.dragUntil = time.Now().Add(dragFlashFor)
	a.dragLit = dragSelect{unit: unit}
	a.dragInBox = true
	a.touch()
	// The flash needs one more frame when it expires, or "copied" would sit on
	// an idle status line forever.
	return tea.Batch(
		tea.Raw(osc52(text, a.tmux)),
		tea.Tick(dragFlashFor, func(time.Time) tea.Msg { return dragFlashMsg{} }),
	)
}

// boxOffsetAt is the rune offset a pointer at (x, y) names in the box the sweep
// is running in, CLAMPED to that box: above its first row is the start of the
// text and below its last row is the end, so a hand that sweeps out of the box
// selects to the end of what is in it rather than stopping at the boundary.
func (a *app) boxOffsetAt(x, y int) (int, bool) {
	if a.boxSel.place {
		if a.boxRows < 1 {
			return 0, false
		}
		return a.placeBoxOffsetIn(clampRow(y-a.boxRow, a.boxRows), x), true
	}
	top, bottom, ok := a.draftRowSpan()
	if !ok {
		return 0, false
	}
	at := clampRow(y-top, bottom-top+1)
	// The tray is the block's first row when there is one (input.go's
	// [app.inputBlock]); it holds chips rather than text, so a sweep across it
	// is a sweep at the top of the draft.
	if a.chipStrip(a.bodyWidth()) != "" {
		at = max(at-1, 0)
	}
	return a.draftOffsetIn(at, x), true
}

// clampRow folds a row index into [0, rows).
func clampRow(at, rows int) int {
	if rows < 1 {
		return 0
	}
	return min(max(at, 0), rows-1)
}

// draftRowSpan is the screen rows the message box's block occupies, read off
// the chrome the last frame actually recorded (view.go's chromeDraft) rather
// than recomputed — which is draftclick.go's law about resolving a pointer
// against what was PAINTED.
func (a *app) draftRowSpan() (int, int, bool) {
	_, height := a.size()
	top, bottom := -1, -1
	for y := range height {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeDraft {
			if top < 0 {
				top = y
			}
			bottom = y
		}
	}
	return top, bottom, top >= 0
}
