package composer

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Key implements tui2.PaneKeys. Order matters here: NewlineKeys and SendKey
// are checked before anything else because they are configuration (they may
// coincide with a string this package would otherwise treat as a binding —
// nothing in caps.go does today, but the composer must not assume that stays
// true), esc is the law (8.2.21), then editing, then plain insertion.
func (m *Model) Key(msg tea.KeyPressMsg) tea.Cmd {
	s := msg.String()

	if m.isNewlineKey(s) {
		m.insert("\n")
		return nil
	}
	if s == m.sendKey {
		return m.submit()
	}
	if s == "esc" {
		return m.handleEsc()
	}

	switch s {
	case "backspace":
		m.deleteBackward()
	case "delete":
		m.deleteForward()
	case "left":
		m.moveLeft()
	case "right":
		m.moveRight()
	case "home":
		m.moveHome()
	case "end":
		m.moveEnd()
	case "up":
		m.handleUp()
	case "down":
		m.handleDown()
	default:
		if text := msg.Key().Text; text != "" {
			m.insert(text)
		}
	}
	return nil
}

func (m *Model) isNewlineKey(s string) bool {
	for _, k := range m.newlineKeys {
		if k == s {
			return true
		}
	}
	return false
}

// submit is SendKey: OnSubmit fires only for a non-empty trimmed draft
// (bracketed-paste-never-sends and blank-Enter-never-sends share this one
// gate), the sent line joins the recall ring, and the draft clears.
func (m *Model) submit() tea.Cmd {
	trimmed := strings.TrimSpace(string(m.value))
	if trimmed == "" {
		return nil
	}
	if m.onSubmit != nil {
		m.onSubmit(trimmed)
	}
	m.remember(trimmed)
	m.reset()
	return nil
}

// handleEsc is the esc law (8.2.21), stated in code: a non-empty draft is
// stashed and the key is fully consumed; an empty draft is not the
// composer's decision, so it hands EscMsg to whatever drives the shell.
func (m *Model) handleEsc() tea.Cmd {
	if len(m.value) == 0 {
		return func() tea.Msg { return EscMsg{} }
	}
	m.stash(string(m.value))
	m.reset()
	return nil
}

// handleUp/handleDown choose between history recall and cursor movement.
// Recall owns the key while the ring is already mid-walk OR the draft is
// empty (7.2: "↑/↓ on empty draft = history recall"); typing at any point
// resets historyStep to 0 (edit.go, history.go) and hands ↑/↓ back to plain
// cursor movement.
func (m *Model) handleUp() {
	if m.historyStep > 0 || len(m.value) == 0 {
		m.recallOlder()
		return
	}
	m.moveUp()
}

func (m *Model) handleDown() {
	if m.historyStep > 0 {
		m.recallNewer()
		return
	}
	m.moveDown()
}
