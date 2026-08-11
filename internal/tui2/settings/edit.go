package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// Editing, one grammar per row kind (8.2.19). The words are deliberately the
// same as the v1 sheet's — a bool toggles, a choice cycles, everything else
// opens an inline editor on the number without its unit — because the grammar
// is the part a user already knows and 7.1's line is "familiar in grammar,
// ours in taste".

// activate is enter (and space, for the kinds where space is the faster verb).
func (m *Model) activate(space bool) tea.Cmd {
	r, ok := m.current()
	if !ok {
		return nil
	}
	if name, pinned := r.setting.PinnedBy(); pinned {
		// 5.20 honesty: the row does not silently do nothing, and it does not
		// pretend it could have. It says who has it and how to take it back.
		m.failed[r.setting.Key] = "unset " + name + " in your shell to change this here"
		m.touch()
		return nil
	}

	switch r.setting.Kind {
	case config.SettingModel:
		return m.openModels(r.setting.Slot)

	case config.SettingBool:
		next := "on"
		if strings.EqualFold(m.value(r), "on") {
			next = "off"
		}
		m.stage(r.setting, next)
		return nil

	case config.SettingChoice:
		if space {
			m.stage(r.setting, nextChoice(r.setting, m.value(r)))
			return nil
		}
		m.picking = true
		m.pick = max(0, choiceIndex(r.setting, m.value(r)))
		return nil

	default:
		if space {
			// Space on a typed row would insert a leading space into a search
			// nobody started. Enter is the door; the hint line says so.
			return nil
		}
		m.editing = true
		m.editor.set(editableValue(r.setting, m.value(r)))
		delete(m.failed, r.setting.Key)
		return nil
	}
}

func (m *Model) openModels(slot string) tea.Cmd {
	if m.onModel != nil {
		return m.onModel(slot)
	}
	return func() tea.Msg { return ModelMsg{Slot: slot} }
}

// commit takes the inline editor's text, or the picker's choice, and stages it.
func (m *Model) commit() {
	r, ok := m.current()
	if !ok {
		m.editing, m.picking = false, false
		return
	}
	switch {
	case m.editing:
		m.editing = false
		m.stage(r.setting, m.editor.text())
	case m.picking:
		m.picking = false
		if m.pick >= 0 && m.pick < len(r.setting.Choices) {
			m.stage(r.setting, r.setting.Choices[m.pick])
		}
	}
}

// cancel drops the editor or the picker without staging anything. It is the
// first rung of the esc ladder here, and it never destroys a stored value —
// only the unsubmitted text, which is the one thing 8.2.21 does allow esc to
// take back.
func (m *Model) cancel() {
	m.editing, m.picking = false, false
	m.editor.set("")
}

// editableValue is the reading with its unit removed, so the editor opens on
// the number a person would type rather than on its decoration.
func editableValue(setting config.Setting, value string) string {
	switch setting.Kind {
	case config.SettingDollars:
		return strings.TrimPrefix(value, "$")
	case config.SettingPercent:
		return strings.TrimSuffix(value, "%")
	case config.SettingText:
		if value == setting.EmptyLabel {
			return ""
		}
	}
	return value
}

func choiceIndex(setting config.Setting, value string) int {
	for index, choice := range setting.Choices {
		if choice == value {
			return index
		}
	}
	return 0
}

func nextChoice(setting config.Setting, value string) string {
	if len(setting.Choices) == 0 {
		return value
	}
	return setting.Choices[(choiceIndex(setting, value)+1)%len(setting.Choices)]
}

// field is the inline editor: one line of runes and a cursor. It is not the
// composer — the composer is a multi-line draft with history, paste bracketing
// and a send key, and a settings row wants none of that. Runes rather than
// bytes because the cursor is a position a person moves, and a vision-model
// name pasted with an accent in it must not be half-deleted by one backspace.
type field struct {
	runes  []rune
	cursor int
}

func (f *field) set(text string) {
	f.runes = []rune(text)
	f.cursor = len(f.runes)
}

func (f *field) text() string { return string(f.runes) }

func (f *field) insert(text string) {
	if text == "" {
		return
	}
	added := []rune(text)
	f.runes = append(f.runes, added...)
	copy(f.runes[f.cursor+len(added):], f.runes[f.cursor:])
	copy(f.runes[f.cursor:], added)
	f.cursor += len(added)
}

func (f *field) backspace() {
	if f.cursor == 0 {
		return
	}
	f.runes = append(f.runes[:f.cursor-1], f.runes[f.cursor:]...)
	f.cursor--
}

func (f *field) deleteForward() {
	if f.cursor >= len(f.runes) {
		return
	}
	f.runes = append(f.runes[:f.cursor], f.runes[f.cursor+1:]...)
}

func (f *field) left() {
	if f.cursor > 0 {
		f.cursor--
	}
}

func (f *field) right() {
	if f.cursor < len(f.runes) {
		f.cursor++
	}
}

func (f *field) home() { f.cursor = 0 }
func (f *field) end()  { f.cursor = len(f.runes) }
