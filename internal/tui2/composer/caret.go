package composer

// The pointer's other half.
//
// [Model.ClickHint] answers "did the reader click a candidate row"; this answers
// "where in the draft did the reader click". They are the same shape for the
// same reason: the geometry is DERIVED, from the very calls Render makes at the
// very same size, so nothing is recorded during a paint (Part 2's anti-pattern
// 14) and there is no draft, width or completion state under which the answer
// can disagree with the picture.
//
// Unlike ClickHint, x IS consulted here, and that is not an inconsistency. A
// candidate is a whole row of a list and the reader is pointing at the CHOICE;
// a character in a sentence is a position, and a caret that ignored the column
// would be a click that said "somewhere on this line" to a reader who pointed
// at a word.

// TextColumn is §20's CONTENT EDGE for this surface: the screen column the
// draft's own text starts at inside a rectangle of this width — the field's
// edge, §19's inner pad, then the prompt glyph and the space after it. It is
// exported because a caller resolving a pointer has to name the same column the
// paint used, and two spellings of one number is how a click lands on the wrong
// character.
//
// It is NOT `width - usable(width)` any more, and the difference is the whole
// right-hand half of the padding law: the draft is now measured short at BOTH
// edges, so the cells missing from `usable` are no longer all in front of the
// text. Deriving the caret's column by subtraction would have put the terminal's
// real cursor one cell right of the painted one at every width.
func TextColumn(width int) int { return padAt(width) + gutterAt(innerWidth(width)) }

// ClickCaret puts the caret on the cell a click landed on, and reports whether
// it moved. A click below the last drafted row, on the chrome, or outside the
// rectangle moves nothing — a caret that jumped to the end of the draft because
// the reader clicked empty space would be an edit position nobody chose.
func (m *Model) ClickCaret(width, height, x, y int) bool {
	if width <= 0 || height <= 0 || y < 0 {
		return false
	}
	// The `@`/`/` chrome takes the TOP of the rectangle and the draft's own rows
	// sit under it. Render's plan, read again.
	p := m.plan(m.activeStyler(), width, height)
	row := y - p.draftTop()
	if row < 0 || row >= p.visible {
		return false
	}
	index := p.first + row
	if index < 0 || index >= len(p.rows) {
		return false
	}
	pos := m.caretInRow(p.rows[index], x-TextColumn(width))
	if pos == m.cursor {
		return false
	}
	m.cursor = pos
	// Walking out of an open `@` needle ends the session, exactly as the arrow
	// keys do (edit.go). A pointer is a second hand on one cursor, not a second
	// set of rules for it.
	m.syncFilter()
	return true
}

// caretInRow turns a column offset inside one display row into a rune index,
// snapping to the nearer edge of whatever character the click landed on — which
// is what every editor does and what a reader clicking the right half of a
// letter means.
func (m *Model) caretInRow(sp span, col int) int {
	if col <= 0 {
		return sp.Start
	}
	cells := 0
	for i := sp.Start; i < sp.End; i++ {
		w := runeCells(m.value[i])
		if col < cells+w {
			// Inside this character: the nearer edge wins.
			if col-cells >= (w+1)/2 {
				return i + 1
			}
			return i
		}
		cells += w
	}
	return sp.End
}
