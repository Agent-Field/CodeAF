package settings

import (
	"image"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// The keyboard.
//
// One rule shapes all of it: ANY printable character starts a global fuzzy
// search across every tab (8.2.19). That is a real cost — j and k cannot be
// navigation here, and space cannot open a text editor — and the honest
// response is to pay it visibly rather than to carve out exceptions nobody can
// predict. Navigation is arrows and the keys that have never been letters
// (tab, page, home, end); the hint line at the foot of the sheet says "type to
// search" before the first keystroke, so the trade is stated on screen and not
// in a manual (5.20 rule 3, 5.22).
//
// The esc ladder (8.2.21) has three rungs and every one of them is advertised
// on that same line as it becomes live:
//
//	editing or picking   esc drops the unsubmitted text; the stored value stands
//	searching            esc clears the query and the tabs come back
//	otherwise            esc closes the sheet — after flushing (see write.go)
//
// Esc is always consumed. The shell routes it to the focused pane and this
// pane always answers it, so esc in the settings sheet never leaks past it
// into whatever is underneath.

// flushMsg asks the surface to write its pending edits. It carries the epoch it
// was armed for, so a tick that outlived its reason — a later edit moved the
// deadline — is ignored rather than writing a value the user has already
// changed again. Same idiom, same reason, as the shell's own resize debounce.
type flushMsg struct{ epoch uint64 }

// Update folds a host-routed message into the surface. Routing is OPTIONAL and
// worth doing: with it, a debounced write lands at the deadline even on an
// idle terminal; without it, the write still lands on the next keystroke, the
// next click, or [Model.Close], because those paths check the deadline too. A
// host that ignores this method loses punctuality, never durability.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if flush, ok := msg.(flushMsg); ok && flush.epoch == m.epoch && m.armed {
		m.Flush()
	}
	return nil
}

// Key implements tui2.PaneKeys.
func (m *Model) Key(msg tea.KeyPressMsg) tea.Cmd {
	m.settle()
	return m.withTick(m.key(msg))
}

// withTick attaches the debounce tick to whatever command the input produced,
// so every path that can stage an edit — a key, a click — arms the deadline
// exactly once and none of them has to remember to.
func (m *Model) withTick(cmd tea.Cmd) tea.Cmd {
	if !m.needTick {
		return cmd
	}
	m.needTick = false
	epoch := m.epoch
	return tea.Batch(cmd, tea.Tick(m.debounce, func(_ time.Time) tea.Msg {
		return flushMsg{epoch: epoch}
	}))
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	name := msg.String()

	if name == "esc" {
		return m.escape()
	}
	if m.editing {
		return m.editingKey(msg, name)
	}
	if m.picking {
		return m.pickingKey(name)
	}
	return m.browsingKey(msg, name)
}

// escape walks the ladder. Every rung consumes the key.
func (m *Model) escape() tea.Cmd {
	if m.editing || m.picking {
		m.cancel()
		return nil
	}
	if m.query != "" {
		m.setQuery("")
		return nil
	}
	return m.Close()
}

func (m *Model) editingKey(msg tea.KeyPressMsg, name string) tea.Cmd {
	switch name {
	case "enter":
		m.commit()
	case "backspace":
		m.editor.backspace()
	case "delete":
		m.editor.deleteForward()
	case "left":
		m.editor.left()
	case "right":
		m.editor.right()
	case "home", "ctrl+a":
		m.editor.home()
	case "end", "ctrl+e":
		m.editor.end()
	case "ctrl+u":
		m.editor.set("")
	default:
		if text := printable(msg); text != "" {
			m.editor.insert(text)
		}
	}
	return nil
}

func (m *Model) pickingKey(name string) tea.Cmd {
	r, ok := m.current()
	if !ok {
		m.picking = false
		return nil
	}
	switch name {
	case "enter", " ", "space":
		m.commit()
	case "left", "up":
		if len(r.setting.Choices) > 0 {
			m.pick = (m.pick - 1 + len(r.setting.Choices)) % len(r.setting.Choices)
		}
	case "right", "down", "tab":
		if len(r.setting.Choices) > 0 {
			m.pick = (m.pick + 1) % len(r.setting.Choices)
		}
	}
	return nil
}

func (m *Model) browsingKey(msg tea.KeyPressMsg, name string) tea.Cmd {
	switch name {
	case "up", "ctrl+p":
		m.move(-1)
		return nil
	case "down", "ctrl+n":
		m.move(1)
		return nil
	case "pgup":
		m.move(-8)
		return nil
	case "pgdown":
		m.move(8)
		return nil
	case "home":
		m.move(-len(m.visible))
		return nil
	case "end":
		m.move(len(m.visible))
		return nil
	case "left", "shift+tab":
		m.moveGroup(-1)
		return nil
	case "right", "tab":
		m.moveGroup(1)
		return nil
	case "enter":
		return m.activate(false)
	case "backspace":
		if m.query != "" {
			m.setQuery(trimLastRune(m.query))
			return nil
		}
		// THE BACK KEY, and only on an empty query. Empty-then-back is the rule
		// a shell path prompt and every file picker already teach, it costs no
		// chord nobody knows, and it is the same key the palette and the model
		// picker answer at their own depth zero (trail.go). With nothing above
		// this sheet it returns no command and the key does nothing, exactly as
		// it did before the trail existed.
		return m.back()
	case "ctrl+u":
		m.setQuery("")
		return nil
	}

	if text := printable(msg); text != "" {
		if text == " " {
			// Space is the toggle verb on the rows that have one, and a query
			// that opens on a space is a query nobody typed.
			if m.query == "" {
				return m.activate(true)
			}
			m.setQuery(m.query + text)
			return nil
		}
		m.setQuery(m.query + text)
	}
	return nil
}

// printable reports the text a keypress inserts, and nothing else: a chord
// (ctrl+w, alt+f) carries modifiers and is not typing, and a control rune is
// not printable however it arrived.
func printable(msg tea.KeyPressMsg) string {
	key := msg.Key()
	if key.Mod != 0 {
		// Shift is the one modifier that still produces text — Key.Text
		// already carries the shifted rune, so a capital letter searches.
		if key.Mod&^tea.ModShift != 0 {
			return ""
		}
	}
	text := key.Text
	if text == "" {
		return ""
	}
	for _, r := range text {
		if !unicode.IsPrint(r) {
			return ""
		}
	}
	return text
}

func trimLastRune(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

// setQuery is the one door into and out of search mode, so the tab bar, the
// row list and the selection can never disagree about which of the two the
// sheet is in.
func (m *Model) setQuery(query string) {
	if strings.TrimSpace(query) == "" {
		query = ""
	}
	if query == m.query {
		return
	}
	m.query = query
	m.selected = 0
	m.cancel()
	m.reselectFresh()
}

func (m *Model) move(delta int) {
	if len(m.visible) == 0 {
		return
	}
	m.selected = max(0, min(m.selected+delta, len(m.visible)-1))
	m.cancel()
}

// moveGroup jumps the band to the head of the next or previous group. It is
// what ←→ used to do to tabs, made honest now that the groups are all on one
// page: a page long enough to scroll still wants a way past a run of rows
// nobody came here for, and the same two keys already meant "sideways".
//
// Backwards from the head of a group lands on the head of the one before, not
// on the row above — the arrow that goes back to the top of THIS group when you
// are already on it would be a key that sometimes does nothing.
func (m *Model) moveGroup(delta int) {
	if len(m.visible) == 0 || m.query != "" || delta == 0 {
		return
	}
	heads := m.groupHeads()
	if len(heads) == 0 {
		return
	}
	current := 0
	for index, head := range heads {
		if head <= m.selected {
			current = index
		}
	}
	if delta < 0 && heads[current] < m.selected {
		// Standing inside a group, back means the top of this one.
		m.selected = heads[current]
		m.cancel()
		return
	}
	m.selected = heads[(current+delta+len(heads))%len(heads)]
	m.cancel()
}

// groupHeads is the position in [Model.visible] of the first row of each group.
func (m *Model) groupHeads() []int {
	heads := make([]int, 0, len(m.groups))
	group := ""
	for position, index := range m.visible {
		if m.rows[index].group == group {
			continue
		}
		group = m.rows[index].group
		heads = append(heads, position)
	}
	return heads
}

// Mouse implements tui2.PaneMouse: chips are buttons (5.22 rule 5). A click
// lands on the row the pointer is actually over — the line-to-row map the last
// frame built — and a click on the row already under the band activates it, so
// the pointer reaches every verb the keyboard does. The wheel moves the band
// rather than a separate scroll offset, because this surface has one cursor
// and a second one would be a second thing to keep track of.
func (m *Model) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	switch event := msg.(type) {
	case tea.MouseWheelMsg:
		switch event.Button {
		case tea.MouseWheelUp:
			m.move(-3)
		case tea.MouseWheelDown:
			m.move(3)
		}
		return nil

	case tea.MouseClickMsg:
		if event.Button != tea.MouseLeft {
			return nil
		}
		m.settle()
		if local.Y == headerLine {
			// The header is a target now, and only because the trail is on it: a
			// click on an ancestor word leaves for that rung, which is the
			// pointer's whole way out of a drilled sheet (5.22 rule 5 — the row
			// IS the button, and a trail step is a row of the path). Every other
			// cell of the header is still chrome and still does nothing, rather
			// than something arbitrary.
			if depth, hit := stepAt(m.steps, local.X); hit {
				return m.backTo(depth)
			}
			return nil
		}
		if local.Y < 0 || local.Y >= len(m.rowAtLine) {
			return nil
		}
		position := m.rowAtLine[local.Y]
		if position < 0 || position >= len(m.visible) {
			return nil
		}
		if position == m.selected {
			return m.withTick(m.activate(false))
		}
		m.selected = position
		m.cancel()
		return nil
	}
	return nil
}
