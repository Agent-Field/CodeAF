package composer

// The draft buffer: a []rune and a rune-index cursor, edited in place. Every
// edit here is O(len(draft)) at worst — a splice on a slice — which is fine
// for a composer-sized draft (a chat message, not a file) and is what keeps
// this package free of any allocation that grows with keystrokes rather than
// with draft size.

// insert splices s into the draft at the cursor and advances the cursor past
// it. Any leaving-recall edit resets the recall walk: typing after an ↑ means
// the recalled line is now being edited, not replayed.
func (m *Model) insert(s string) {
	if s == "" {
		return
	}
	runes := []rune(s)
	next := make([]rune, 0, len(m.value)+len(runes))
	next = append(next, m.value[:m.cursor]...)
	next = append(next, runes...)
	next = append(next, m.value[m.cursor:]...)
	m.value = next
	m.cursor += len(runes)
	m.historyStep = 0
	m.afterEdit()
}

// deleteBackward removes the rune before the cursor (backspace) — or the whole
// mention token the cursor sits at the end of. A completed mention is one
// object on the screen (5.18: a hue-tinted token), so it is one object to the
// key that deletes objects; shredding it into runes would make the draft say
// "@wisp-parit", which addresses nobody and looks like it still does.
func (m *Model) deleteBackward() {
	if m.cursor == 0 {
		return
	}
	if mn, ok := m.mentionEndingAt(m.cursor); ok {
		m.value = append(m.value[:mn.start], m.value[m.cursor:]...)
		m.cursor = mn.start
		m.historyStep = 0
		m.afterEdit()
		return
	}
	m.value = append(m.value[:m.cursor-1], m.value[m.cursor:]...)
	m.cursor--
	m.historyStep = 0
	m.afterEdit()
}

// deleteForward removes the rune at the cursor (delete/fn+delete), or the whole
// mention token the cursor sits at the start of — [Model.deleteBackward]'s
// atomicity, in the other direction, for the same reason.
func (m *Model) deleteForward() {
	if m.cursor >= len(m.value) {
		return
	}
	if mn, ok := m.mentionStartingAt(m.cursor); ok {
		m.value = append(m.value[:m.cursor], m.value[mn.end():]...)
		m.historyStep = 0
		m.afterEdit()
		return
	}
	m.value = append(m.value[:m.cursor], m.value[m.cursor+1:]...)
	m.historyStep = 0
	m.afterEdit()
}

// moveLeft/moveRight step the cursor by one rune, clamped to the buffer. Both
// re-validate an open `@` filter: walking out of the needle ends the session,
// which is filter.go's rule and not a second opinion here.
func (m *Model) moveLeft() {
	if m.cursor > 0 {
		m.cursor--
	}
	m.syncFilter()
}

func (m *Model) moveRight() {
	if m.cursor < len(m.value) {
		m.cursor++
	}
	m.syncFilter()
}

// lineStart returns the rune index one past the nearest '\n' at or before
// pos, or 0 if pos's line is the first.
func (m *Model) lineStart(pos int) int {
	for i := pos - 1; i >= 0; i-- {
		if m.value[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

// lineEnd returns the rune index of the nearest '\n' at or after pos, or
// len(m.value) if pos's line is the last.
func (m *Model) lineEnd(pos int) int {
	for i := pos; i < len(m.value); i++ {
		if m.value[i] == '\n' {
			return i
		}
	}
	return len(m.value)
}

// moveHome/moveEnd move within the current logical line (the line the
// cursor's own newlines delimit, not a wrapped display row — see doc.go).
func (m *Model) moveHome() {
	m.cursor = m.lineStart(m.cursor)
	m.syncFilter()
}

func (m *Model) moveEnd() {
	m.cursor = m.lineEnd(m.cursor)
	m.syncFilter()
}

// moveUp/moveDown walk logical lines, preserving column where the target
// line is long enough to have one. They report whether they moved, which is
// how [Model.Key] decides between cursor movement and history recall: a
// single-line draft has nowhere to move to, and that is exactly when ↑/↓
// falls through to the recall ring.
func (m *Model) moveUp() bool {
	ls := m.lineStart(m.cursor)
	if ls == 0 {
		return false
	}
	col := m.cursor - ls
	prevEnd := ls - 1 // the '\n' just before this line
	prevStart := m.lineStart(prevEnd)
	if width := prevEnd - prevStart; col > width {
		col = width
	}
	m.cursor = prevStart + col
	return true
}

func (m *Model) moveDown() bool {
	le := m.lineEnd(m.cursor)
	if le == len(m.value) {
		return false
	}
	ls := m.lineStart(m.cursor)
	col := m.cursor - ls
	nextStart := le + 1
	nextEnd := m.lineEnd(nextStart)
	if width := nextEnd - nextStart; col > width {
		col = width
	}
	m.cursor = nextStart + col
	return true
}

// reset clears the draft and cursor without touching history or the stash.
// Used after a successful submit.
func (m *Model) reset() {
	m.value = m.value[:0]
	m.cursor = 0
	m.historyStep = 0
	m.mentions = m.mentions[:0]
	m.closeMentionFilter()
}

// setValue replaces the draft outright, placing the cursor at the end —
// where a person carries on typing after a recall. It also leaves the
// recall walk exactly as the caller set it, so [recallOlder]/[recallNewer]
// can call this without fighting their own bookkeeping.
//
// Mentions come back with the text and are re-derived against the CURRENT
// targets, which is the property that makes a recalled or un-stashed draft a
// live addressed draft again rather than a string that merely looks like one —
// and, in the other direction, makes a mention of a task that has since
// vanished stop pretending it can be addressed.
func (m *Model) setValue(s string) {
	m.value = []rune(s)
	m.cursor = len(m.value)
	m.afterEdit()
}
