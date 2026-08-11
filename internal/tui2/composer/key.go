package composer

import (
	tea "charm.land/bubbletea/v2"
)

// Key implements tui2.PaneKeys. Order matters here: NewlineKeys is checked
// before anything else because it is configuration (it may coincide with a
// string this package would otherwise treat as a binding — nothing in caps.go
// does today, but the composer must not assume that stays true), then an open
// `@` filter, then SendKey, then esc, then editing, then plain insertion.
//
// The filter sits between newline and send because while it is open, enter
// completes the highlighted mention rather than sending — and esc closes it
// rather than stashing the draft. Neither is a special case bolted onto the
// existing laws: both follow the one rule 8.2.21 settles for esc and 5.20 rule
// 6 states in general, that a key acts on the thing the user is looking at. The
// filter is that thing while it is open, and it claims nothing once it is not
// (see filter.go's [Model.filterKey], which takes as little as it can).
func (m *Model) Key(msg tea.KeyPressMsg) tea.Cmd {
	s := msg.String()

	if m.isNewlineKey(s) {
		m.insert("\n")
		return nil
	}
	if m.filter.open {
		if cmd, handled := m.filterKey(s); handled {
			return cmd
		}
	}
	if s == m.sendKey {
		return m.submit(false)
	}
	// The follow chord (5.18) is live only while the draft addresses someone;
	// with no mention it stays the unbound no-op it always was.
	if s == followKey && len(m.mentions) > 0 {
		return m.submit(true)
	}
	if s == "esc" {
		return m.handleEsc()
	}

	switch s {
	case "backspace":
		m.deleteBackward()
	case "delete":
		m.deleteForward()
	case "ctrl+u":
		// The clear-the-draft chord (13.5 finding 3). It sits with the other
		// editing keys and not with esc on purpose: esc is the ladder key —
		// interrupt, pop scope, jump to the live edge — and 8.2.21's own
		// non-negotiable is that it never destroys a draft. A draft a person
		// wants gone needs a key that means only that, and readline named it
		// forty years ago. See [Model.KillToStart] for why the words are stashed.
		m.KillToStart()
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
			// One typed '@' at a word boundary opens the filter. It is checked
			// here rather than inside insert() because a PASTE that happens to
			// carry an '@' is not a person reaching for the mention grammar,
			// and a filter that opened on paste would be one the user has to
			// dismiss to keep typing.
			if len(text) == 1 && text == "@" && !m.filter.open && mentionBoundary(m.value, m.cursor-1) {
				m.openMentionFilter(m.cursor - 1)
			}
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
