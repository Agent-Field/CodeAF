package tui3

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// ── SELECTING TEXT INSIDE A BOX ─────────────────────────────────────────────
//
// THE DEFECT THIS FILE EXISTS FOR. Mouse reporting takes the terminal's own
// drag-select away — dragselect.go states that law and answers it for the
// transcript — and every BOX on this surface was left out of the answer: a
// press on the message box put the caret under the pointer and returned
// (draftclick.go, placemouse.go), so the sweep that followed reached nothing at
// all. The drag machinery parks on the BODY, and a press a box has already
// taken never parks. The owner's words were "i am unable to select text with
// highlight from input bar in all places … it acts like normal text".
//
// So a box now selects the way the transcript does and the way every text field
// does: sweep and the run wears the highlight, double-click takes the word,
// triple-click takes the line, release copies what is lit, and the selection
// stays there afterwards as a live selection — type over it and it is replaced,
// backspace and it is gone.
//
// ── THE SELECTION IS A RUN OF THE DRAFT, NOT A RUN OF THE SCREEN ──
//
// It is kept as two rune offsets into the editor's value and never as rows and
// columns, because a box re-wraps: the same sentence is two rows at eighty
// columns and four at forty, and a selection anchored to the glass would move
// through the text the moment the window changed. The drawing converts it back
// through the layout the frame actually drew with (input.go's draftWindow),
// which is the same bargain draftclick.go struck for the caret.
//
// ── ANY MOTION DROPS IT, AND THAT IS WHY IT IS SAFE ──
//
// Every caret motion and every edit clears the selection at the [editor] level,
// so a box that has never heard of this file cannot be left drawing a highlight
// over text that has moved. The two gestures that MAKE a selection — the sweep
// and the shift-arrows — set it back immediately after the motion they ran.

// selection is the selected run as rune offsets [lo, hi), and false when there
// is no selection or it is empty.
func (e *editor) selection() (int, int, bool) {
	if !e.picked {
		return 0, 0, false
	}
	lo, hi := e.anchor, e.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	lo, hi = min(max(lo, 0), len(e.value)), min(max(hi, 0), len(e.value))
	if lo >= hi {
		return 0, 0, false
	}
	return lo, hi, true
}

// selectedText is what a copy of the selection would carry, and "" when there
// is none.
func (e *editor) selectedText() string {
	lo, hi, ok := e.selection()
	if !ok {
		return ""
	}
	return string(e.value[lo:hi])
}

// pickFrom anchors a selection at one offset and parks the caret there: the
// press of a sweep, before the hand has moved anywhere.
func (e *editor) pickFrom(at int) {
	at = min(max(at, 0), len(e.value))
	e.anchor, e.cursor, e.picked = at, at, true
}

// pickTo moves the far end of a selection already anchored, which is every
// moved-with-the-button-down event of a sweep and every shift-arrow after the
// first.
func (e *editor) pickTo(at int) {
	if !e.picked {
		e.pickFrom(at)
		return
	}
	e.cursor = min(max(at, 0), len(e.value))
}

// pickSpan selects a known run and leaves the caret at its end — what a
// double-click, a triple-click and select-all all end with.
func (e *editor) pickSpan(lo, hi int) {
	lo, hi = min(max(lo, 0), len(e.value)), min(max(hi, 0), len(e.value))
	e.anchor, e.cursor, e.picked = lo, hi, true
}

// dropPick clears the selection. It is called by every motion and every edit
// the editor makes, so no caller has to remember to.
func (e *editor) dropPick() { e.picked = false }

// pickAll is select-all: the whole draft, caret at its end.
func (e *editor) pickAll() { e.pickSpan(0, len(e.value)) }

// pickWordAt selects the word around one offset — the double-click.
func (e *editor) pickWordAt(at int) {
	lo, hi := wordSpanAt(e.value, at)
	e.pickSpan(lo, hi)
}

// pickLineAt selects the LOGICAL line around one offset — the triple-click.
// Logical rather than the soft-wrapped row for [editor.killToStart]'s reason:
// a row is a slice the box's width happened to make, and nobody means "the
// eighty cells that fit" by triple-clicking a sentence.
func (e *editor) pickLineAt(at int) {
	at = min(max(at, 0), len(e.value))
	e.pickSpan(lineHead(e.value, at), lineTail(e.value, at))
}

// cutPick deletes the selected run and reports whether there was one. It is a
// whole step of its own for the undo, because replacing a selection is one
// thing a person did and not a run of anything.
func (e *editor) cutPick() bool {
	lo, hi, ok := e.selection()
	if !ok {
		return false
	}
	e.remember(runWhole, true)
	e.value = append(e.value[:lo], e.value[hi:]...)
	e.cursor, e.picked = lo, false
	return true
}

// wordSpanAt is the run of one word around rune offset at, over the SAME
// boundaries the transcript's own double-click stops at (dragselect.go's
// wordSeparators and the sentence-punctuation rule with it). One gesture may
// not take one word in the transcript and a different word in the box directly
// under it — a path, a hash, a flag, a dotted name and a URL are each one word
// in both.
//
// It is NOT the boundary [editor.wordLeft] and `ctrl+w` walk by, and the
// difference is deliberate: those two are a caret's stride and a kill's reach,
// where "back over the whitespace, then back over the run" is what a hand
// means; a double-click is an aim at a thing on the screen.
func wordSpanAt(value []rune, at int) (int, int) {
	if len(value) == 0 {
		return 0, 0
	}
	at = min(max(at, 0), len(value)-1)
	// A caret parked past the end of a word takes that word rather than the
	// space after it, which is where a click on the last letter lands.
	if unicode.IsSpace(value[at]) && at > 0 && !unicode.IsSpace(value[at-1]) {
		at--
	}
	space := func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }
	sep := func(r rune) bool { return space(r) || strings.ContainsRune(wordSeparators, r) }
	lo, hi := at, at
	if space(value[at]) {
		for lo > 0 && space(value[lo-1]) {
			lo--
		}
		for hi+1 < len(value) && space(value[hi+1]) {
			hi++
		}
		return lo, hi + 1
	}
	for lo > 0 && !sep(value[lo-1]) {
		lo--
	}
	for hi+1 < len(value) && !sep(value[hi+1]) {
		hi++
	}
	// A sentence's closing punctuation is left behind when it ends the word:
	// "Println." is `Println`, and `go.mod` keeps its dot because a letter
	// follows it.
	for hi > lo && strings.ContainsRune(".,:;!?", value[hi]) && (hi+1 == len(value) || sep(value[hi+1])) {
		hi--
	}
	return lo, hi + 1
}

// editorPick is the keyboard's half of the gesture: shift held with a motion
// key extends the selection instead of dropping it, which is what shift means
// in every text field there is.
//
// IT IS OFFERED RATHER THAN CLAIMED. This function reports whether it took the
// key and no box calls it unless the chord is free there: `shift+←→↑↓` are the
// time window on three of the places and the pane walk on the folder picker
// (placekeys.go), and a shared map that seized them would take a working key
// off those screens — the exact trade editkeys.go refuses for `home` and `end`.
// The message box, where the chords mean nothing else, is where they are read.
func editorPick(e *editor, key string) bool {
	var move func()
	switch key {
	case "shift+left":
		move = e.left
	case "shift+right":
		move = e.right
	case "shift+up":
		move = e.up
	case "shift+down":
		move = e.down
	case "shift+home", "super+shift+left", "meta+shift+left":
		move = e.home
	case "shift+end", "super+shift+right", "meta+shift+right":
		move = e.end
	case "alt+shift+left", "ctrl+shift+left":
		move = e.wordLeft
	case "alt+shift+right", "ctrl+shift+right":
		move = e.wordRight
	default:
		return false
	}
	// The motions drop the selection on their way through, which is right for
	// every OTHER caller of them, so the anchor is carried across by hand: a
	// selection already running keeps the end it started from, and a bare
	// caret anchors where it stands.
	anchor, running := e.anchor, e.picked
	if !running {
		anchor = e.cursor
	}
	move()
	e.anchor, e.picked = anchor, true
	return true
}

// markDraftRow paints the selection over one laid-out row of a draft. The row
// arrives already painted — slash chips and all — and the mark goes over the
// top of it, exactly as the transcript's sweep marks a body row
// (dragselect.go's [app.markCells]).
//
// A SELECTION THAT RUNS THROUGH A LINE BREAK LIGHTS ONE CELL PAST THE TEXT, so
// a sweep down several lines reads as one continuous run rather than as a
// ladder of disconnected words — and an empty line inside the run shows that it
// is in the run at all.
func markDraftRow(painted string, value []rune, seg segment, lo, hi int, pal palette) string {
	from, to := max(lo, seg.from), min(hi, seg.to)
	if from > to {
		return painted
	}
	x0 := ansi.StringWidth(string(value[seg.from:from]))
	x1 := x0 + ansi.StringWidth(string(value[from:to]))
	if seg.to < len(value) && hi > seg.to {
		x1++
	}
	if x1 <= x0 {
		return painted
	}
	return markCells(pal, painted, x0, x1)
}

// dropDraftPick removes the selected run of the MESSAGE box and keeps the
// slash-tag bookkeeping in step, reporting whether there was one.
//
// EVERY DOOR THAT WRITES INTO THAT BOX CALLS IT BEFORE MEASURING THE CARET.
// [editor.insert] and the deletes replace a selection on their own — that is
// what makes the gesture work in the twenty boxes that keep no tags — but the
// message box also holds a list of demoted slash words as offsets into the
// text (input.go's editTags), and a run removed under the caret's feet would
// leave those offsets describing letters that are no longer there.
func (a *app) dropDraftPick() bool {
	lo, hi, ok := a.input.selection()
	if !ok {
		return false
	}
	a.input.cutPick()
	a.editTags(lo, hi, 0)
	return true
}
