package prose

import (
	"unicode/utf8"
)

// Wrapping styled text.
//
// blocks.Wrap wraps a string; prose has to wrap a SEQUENCE OF STYLED RUNS,
// because a sentence can change style in the middle of a word — `**re**factor`
// is one word and two styles, and breaking it at the style boundary would put a
// line ending in "re" on screen. So the unit here is the word, a word is a
// []piece, and the wrapper only ever looks at style to decide what the
// separating space should be painted as.
//
// The algorithm is greedy, like blocks', for the same reason: greedy wrapping
// is the only rule under which appending text cannot rewrite an earlier row,
// which is what makes a streaming reply cheap to redraw. This package does not
// stream today — it renders a whole document — but a wrapper that could not
// stream would be the thing standing in the way when it does.

// wrapper accumulates pieces and emits finished rows through emit.
type wrapper struct {
	width int
	emit  func([]piece)

	line  []piece
	lineW int
	word  []piece
	wordW int
}

func newWrapper(width int, emit func([]piece)) *wrapper {
	if width < 1 {
		width = 1
	}
	return &wrapper{width: width, emit: emit}
}

// push adds a run of text in one style. Spaces inside the run are word
// separators and are never carried onto a row: a row that opened on a space
// would be a row whose left edge moved, and the separator a wrap actually needs
// is re-minted by [wrapper.commit].
func (w *wrapper) push(text string, st style) {
	start := 0
	for i, r := range text {
		if r != ' ' {
			continue
		}
		if i > start {
			w.word = append(w.word, piece{text: text[start:i], st: st})
			w.wordW += cells(text[start:i])
		}
		w.commit()
		start = i + utf8.RuneLen(r)
	}
	if start < len(text) {
		w.word = append(w.word, piece{text: text[start:], st: st})
		w.wordW += cells(text[start:])
	}
}

// space forces a separator without ending a word — the join between a link's
// text and its dim URL, where the two must not be allowed to fuse but may be
// broken apart by a wrap.
func (w *wrapper) space() { w.commit() }

// commit places the pending word on the current row, opening a new row when it
// does not fit and splitting it when it does not fit on a row of its own.
func (w *wrapper) commit() {
	if len(w.word) == 0 {
		return
	}
	word, ww := w.word, w.wordW
	w.word, w.wordW = nil, 0

	sep := 0
	if w.lineW > 0 {
		sep = 1
	}
	if w.lineW+sep+ww <= w.width {
		if sep == 1 {
			w.line = append(w.line, piece{text: " ", st: w.separator(word[0].st)})
			w.lineW++
		}
		w.line = append(w.line, word...)
		w.lineW += ww
		return
	}
	if w.lineW > 0 {
		w.flushLine()
	}
	// A word longer than the whole measure — a URL, a hash, a path — is broken
	// where the row ends rather than allowed to overflow. Nothing else in this
	// package may exceed the width, and a word is not an exception to a ceiling.
	for ww > w.width {
		head, tail := cut(word, w.width)
		w.emit(head)
		word = tail
		ww = 0
		for _, pc := range word {
			ww += cells(pc.text)
		}
	}
	w.line, w.lineW = word, ww
}

// separator picks the style of the space a wrap inserts. It matches the run on
// either side when they agree, so an inline-code span that spans a space keeps
// one unbroken ground; when they disagree it falls back to plain body, because
// a space that borrowed one neighbour's raised ground would draw a step where
// there is no step.
func (w *wrapper) separator(next style) style {
	if n := len(w.line); n > 0 && w.line[n-1].st == next {
		return next
	}
	return body
}

// hardBreak ends the current row where the source said to end it (a markdown
// hard break, or the boundary between two lines of a code block).
func (w *wrapper) hardBreak() {
	w.commit()
	w.flushLine()
}

// flush ends the paragraph, emitting whatever is left. An empty wrapper emits
// nothing: a paragraph that rendered to no cells must not become a blank row,
// or a reply full of stripped HTML would become a reply full of holes.
func (w *wrapper) flush() {
	w.commit()
	if len(w.line) > 0 {
		w.flushLine()
	}
}

func (w *wrapper) flushLine() {
	w.emit(w.line)
	w.line, w.lineW = nil, 0
}
