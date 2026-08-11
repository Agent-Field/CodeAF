package consentui

import (
	"strings"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// A one-line editor, deliberately not the composer.
//
// internal/tui2/composer is the draft editor: multi-line, history ring, paste
// promotion, capability-bound send key. None of that belongs in a dialog. A
// steering line is one sentence typed in a modal that owns the keyboard for as
// long as it is open, and inheriting a history ring here would mean a redirect
// typed at one question could be recalled into another — which is the one thing
// a consent surface must not do casually.
//
// So this is the smallest thing that upholds the law that DOES apply (8.2.21:
// typed text is never destroyed) and nothing else. Text is stored as runes
// because a cursor that indexes bytes lands inside a multi-byte character on
// the first non-ASCII redirect anyone types.

type editor struct {
	value  []rune
	cursor int
	// scroll is the leftmost rune column drawn, kept between renders so a
	// cursor at the end of a long line does not jump the window every frame.
	scroll int
}

func (e *editor) String() string { return string(e.value) }

func (e *editor) set(text string) {
	e.value = []rune(text)
	e.cursor = len(e.value)
	e.scroll = 0
}

func (e *editor) reset() {
	e.value = e.value[:0]
	e.cursor = 0
	e.scroll = 0
}

// insert takes the text a key press carries. Control characters are dropped
// rather than stored: a stray one would be invisible in the editor and a
// surprise in the journal, and the sanitizer upstream is not this line's
// last defence.
func (e *editor) insert(text string) {
	runes := make([]rune, 0, len(text))
	for _, r := range text {
		if r == '\n' || r == '\t' {
			runes = append(runes, ' ')
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		runes = append(runes, r)
	}
	if len(runes) == 0 {
		return
	}
	e.cursor = clamp(e.cursor, 0, len(e.value))
	e.value = append(e.value[:e.cursor], append(runes, e.value[e.cursor:]...)...)
	e.cursor += len(runes)
}

func (e *editor) backspace() {
	e.cursor = clamp(e.cursor, 0, len(e.value))
	if e.cursor == 0 {
		return
	}
	e.value = append(e.value[:e.cursor-1], e.value[e.cursor:]...)
	e.cursor--
}

func (e *editor) deleteForward() {
	e.cursor = clamp(e.cursor, 0, len(e.value))
	if e.cursor >= len(e.value) {
		return
	}
	e.value = append(e.value[:e.cursor], e.value[e.cursor+1:]...)
}

func (e *editor) left()  { e.cursor = clamp(e.cursor-1, 0, len(e.value)) }
func (e *editor) right() { e.cursor = clamp(e.cursor+1, 0, len(e.value)) }
func (e *editor) home()  { e.cursor = 0 }
func (e *editor) end()   { e.cursor = len(e.value) }

// key folds one key press into the line and reports whether it was consumed.
// esc and enter are NOT consumed: they are the caller's decisions, and an
// editor that swallowed esc would be an editor that could destroy a draft.
func (e *editor) key(name, text string) bool {
	switch name {
	case "backspace":
		e.backspace()
	case "delete":
		e.deleteForward()
	case "left":
		e.left()
	case "right":
		e.right()
	case "home", "ctrl+a":
		e.home()
	case "end", "ctrl+e":
		e.end()
	case "ctrl+u":
		e.value = append(e.value[:0], e.value[e.cursor:]...)
		e.cursor = 0
	default:
		if text == "" {
			return false
		}
		e.insert(text)
	}
	return true
}

// window returns the visible slice of the line for a field of width cells, and
// the cell column the cursor sits at within it. The last cell is left for the
// cursor itself so a cursor at end-of-line is never drawn off the edge.
func (e *editor) window(width int) (string, int) {
	if width < 1 {
		return "", 0
	}
	// The caret is a printed cell, so the text gets width-1. At width 1 that
	// leaves nothing but the caret, which is the honest rendering of a
	// one-column field rather than an off-by-one that overruns it.
	room := width - 1
	if room < 0 {
		room = 0
	}
	e.cursor = clamp(e.cursor, 0, len(e.value))
	if e.cursor < e.scroll {
		e.scroll = e.cursor
	}
	// Walk back from the cursor until the visible run fits, which is correct
	// for wide runes as well as narrow ones because it measures cells.
	for e.scroll < e.cursor && blocks.Width(string(e.value[e.scroll:e.cursor])) > room {
		e.scroll++
	}
	if e.scroll > len(e.value) {
		e.scroll = len(e.value)
	}
	tail := e.value[e.scroll:]
	text := blocks.Truncate(string(tail), room)
	col := blocks.Width(string(e.value[e.scroll:e.cursor]))
	return text, clamp(col, 0, room)
}

// caretRow draws the line with a visible caret. The caret is a block on the
// character under it rather than a real terminal cursor: the dialog is one pane
// among several and moving the terminal's cursor into an overlay is the
// shell's decision to make, not a pane's.
func (e *editor) caretRow(width int) string {
	if width < 1 {
		return ""
	}
	text, col := e.window(width)
	runes := []rune(text)
	var b strings.Builder
	cells := 0
	placed := false
	for i := 0; i < len(runes); i++ {
		w := blocks.Width(string(runes[i]))
		if !placed && cells == col {
			b.WriteString(caret)
			placed = true
		}
		b.WriteString(string(runes[i]))
		cells += w
	}
	if !placed {
		b.WriteString(caret)
	}
	return b.String()
}

// caret is the composer's own stream caret glyph rather than a block: one cell,
// metric-safe, and already the vocabulary for "the text goes here".
const caret = "▏"

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
