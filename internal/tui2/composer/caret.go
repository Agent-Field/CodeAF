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

// TextColumn is the screen column the draft's own text starts at inside a
// rectangle of this width: the prompt glyph and the space after it. It is
// exported because a caller resolving a pointer has to name the same column the
// paint used, and two spellings of one number is how a click lands on the wrong
// character.
func TextColumn(width int) int { return width - usable(width) }

// ClickCaret puts the caret on the cell a click landed on, and reports whether
// it moved. A click below the last drafted row, on the chrome, or outside the
// rectangle moves nothing — a caret that jumped to the end of the draft because
// the reader clicked empty space would be an edit position nobody chose.
func (m *Model) ClickCaret(width, height, x, y int) bool {
	if width <= 0 || height <= 0 || y < 0 {
		return false
	}
	sty := m.activeStyler()
	// The `@`/`/` chrome borrows from the bottom of the rectangle; the draft's
	// own rows are what is left. Render's arithmetic, run again.
	hints := m.hintRows(sty, width, height-1)
	drafted := height - len(hints)
	if drafted <= 0 {
		return false
	}
	text := usable(width)
	rows := layoutRows(m.value, text)
	total := len(rows)
	visible := min(drafted, total)
	if y >= visible {
		return false
	}
	scrollTop := 0
	if total > drafted {
		scrollTop = rowOf(rows, m.cursor) - (drafted - 1)
		if scrollTop < 0 {
			scrollTop = 0
		}
		if maxTop := total - drafted; scrollTop > maxTop {
			scrollTop = maxTop
		}
	}
	index := scrollTop + y
	if index < 0 || index >= total {
		return false
	}
	pos := m.caretInRow(rows[index], x-(width-text))
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
