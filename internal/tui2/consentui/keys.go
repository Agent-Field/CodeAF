package consentui

import (
	tea "charm.land/bubbletea/v2"
)

// The keyboard. Four modes, one rule each about esc, and no chord anywhere:
// 10.1.2 forbids requiring shift+enter or a key-release, and a dialog that
// needed a modifier to say "no" would be a dialog people say "yes" to.
//
// Esc, by mode (8.2.21 — esc never destroys a draft, and what it does is what
// the surface currently says it does):
//
//	answer     -> close the dialog, stashing any typed steering
//	scope      -> back to the answers, granting nothing
//	scope-edit -> discard this one edit, back to the pattern list
//	steer      -> back to the answers, KEEPING what was typed
//
// Note what esc never does here: answer the question. A modal whose escape
// hatch picked an option would be a modal that decides consent by impatience.

// Key implements tui2.PaneKeys.
func (m *Model) Key(msg tea.KeyPressMsg) tea.Cmd {
	if !m.Open() {
		return nil
	}
	name := msg.String()
	text := msg.Key().Text
	switch m.mode {
	case modeSteer:
		return m.keySteer(name, text)
	case modeScope:
		return m.keyScope(name)
	case modeScopeEdit:
		return m.keyScopeEdit(name, text)
	default:
		return m.keyAnswer(name, text)
	}
}

func (m *Model) keyAnswer(name, text string) tea.Cmd {
	q, ok := m.Current()
	if !ok {
		return nil
	}
	switch name {
	case "esc":
		return m.dismiss()
	case "enter":
		return m.choose(m.sel)
	case "up", "shift+tab", "ctrl+p":
		m.move(-1)
		return nil
	case "down", "tab", "ctrl+n":
		m.move(1)
		return nil
	case "t":
		// An affordance that does nothing is worse than an absent one (5.20
		// rule 3), so `t` is inert — not merely unhelpful — on a question that
		// carries no detail, and the key strip does not offer it there either.
		if q.HasDetail() {
			m.detail = !m.detail
			m.touch()
		}
		return nil
	case "f":
		m.full = !m.full
		m.touch()
		return nil
	}
	if index, ok := digitIndex(name); ok {
		return m.choose(index)
	}
	if index, ok := mnemonicIndex(q.Options, text); ok {
		return m.choose(index)
	}
	return nil
}

func (m *Model) keyScope(name string) tea.Cmd {
	q, ok := m.Current()
	if !ok {
		return nil
	}
	switch name {
	case "esc":
		m.hasPending = false
		m.pending = Option{}
		m.scope = nil
		m.mode = modeAnswer
		m.touch()
		return nil
	case "enter":
		if !m.hasPending {
			return nil
		}
		return m.take(q, m.pending, m.scope)
	case "e":
		// The escalation of 10.4.18: editing a grant is never on the fast path,
		// so asking for it is also asking for the full frame.
		if len(m.scope) == 0 {
			return nil
		}
		m.full = true
		m.mode = modeScopeEdit
		m.scopeEdit.set(m.scope[clamp(m.scopeSel, 0, len(m.scope)-1)])
		m.touch()
		return nil
	case "up", "shift+tab", "ctrl+p":
		m.moveScope(-1)
		return nil
	case "down", "tab", "ctrl+n":
		m.moveScope(1)
		return nil
	case "f":
		m.full = !m.full
		m.touch()
		return nil
	}
	return nil
}

func (m *Model) keyScopeEdit(name, text string) tea.Cmd {
	switch name {
	case "esc":
		m.mode = modeScope
		m.scopeEdit.reset()
		m.touch()
		return nil
	case "enter":
		m.commitScopeEdit()
		m.mode = modeScope
		m.touch()
		return nil
	}
	if m.scopeEdit.key(name, text) {
		m.touch()
	}
	return nil
}

func (m *Model) keySteer(name, text string) tea.Cmd {
	q, ok := m.Current()
	if !ok {
		return nil
	}
	switch name {
	case "esc":
		// The draft stays in the editor, which is the whole of 8.2.21 here:
		// backing out of the redirect must not cost the sentence.
		m.mode = modeAnswer
		m.touch()
		return nil
	case "enter":
		if !m.hasPending {
			m.mode = modeAnswer
			m.touch()
			return nil
		}
		// An empty redirect is still a rejection. Refusing to accept one would
		// make "no" conditional on having something to say, which is a worse
		// failure than a bare no.
		return m.advance(m.result(q, m.pending, m.scope, m.steer.String()))
	}
	if m.steer.key(name, text) {
		m.touch()
	}
	return nil
}

// choose acts on one option by position.
func (m *Model) choose(index int) tea.Cmd {
	q, ok := m.Current()
	if !ok || index < 0 || index >= len(q.Options) {
		return nil
	}
	m.sel = index
	option := q.Options[index]
	if len(option.Scope) > 0 {
		// 10.4.18: what "always" would whitelist is shown before it is granted,
		// never after. The working copy is what gets confirmed, so an edit
		// cannot reach back into the durable option.
		m.pending, m.hasPending = option, true
		m.scope = append([]string(nil), option.Scope...)
		m.scopeSel = 0
		m.mode = modeScope
		m.touch()
		return nil
	}
	return m.take(q, option, nil)
}

// take is the last fork: a refusal opens steering (10.4.16), anything else
// settles now.
func (m *Model) take(q Question, option Option, scope []string) tea.Cmd {
	if option.Rejecting {
		m.pending, m.hasPending = option, true
		m.scope = scope
		m.mode = modeSteer
		m.touch()
		return nil
	}
	return m.advance(m.result(q, option, scope, ""))
}

func (m *Model) move(delta int) {
	q, ok := m.Current()
	if !ok || len(q.Options) == 0 {
		return
	}
	n := len(q.Options)
	m.sel = ((m.sel+delta)%n + n) % n
	m.touch()
}

func (m *Model) moveScope(delta int) {
	if len(m.scope) == 0 {
		return
	}
	n := len(m.scope)
	m.scopeSel = ((m.scopeSel+delta)%n + n) % n
	m.touch()
}

// commitScopeEdit writes the edited line back. A line edited to nothing REMOVES
// the pattern: narrowing a grant is always safe, and the alternative — keeping
// a pattern the user just cleared — is the one outcome a scope editor must
// never produce.
func (m *Model) commitScopeEdit() {
	if len(m.scope) == 0 {
		return
	}
	index := clamp(m.scopeSel, 0, len(m.scope)-1)
	edited := trimPattern(m.scopeEdit.String())
	m.scopeEdit.reset()
	if edited == "" {
		m.scope = append(m.scope[:index], m.scope[index+1:]...)
		m.scopeSel = clamp(index, 0, max(0, len(m.scope)-1))
		return
	}
	m.scope[index] = edited
}

// digitIndex reads the durable wire form. Every surface numbers a question's
// options the same way and internal/head resolves "2" against the same list, so
// a digit is a legal answer even where a letter is ambiguous or absent.
func digitIndex(name string) (int, bool) {
	if len(name) != 1 || name[0] < '1' || name[0] > '9' {
		return 0, false
	}
	return int(name[0] - '1'), true
}

// mnemonicIndex matches one typed letter against the letters the options wear.
// It reads Key rather than re-deriving anything, so what the strip shows and
// what the keyboard does cannot drift apart.
func mnemonicIndex(options []Option, text string) (int, bool) {
	if len(text) != 1 {
		return 0, false
	}
	typed := lowerASCII(text[0])
	for i := range options {
		if options[i].Key != "" && options[i].Key[0] == typed {
			return i, true
		}
	}
	return 0, false
}

func trimPattern(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
