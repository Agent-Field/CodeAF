// Package textinput provides the single-line Bubble Tea input used by aforge.
package textinput

import (
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const blinkSpeed = 530 * time.Millisecond

type blinkMsg struct{}

// Cursor contains the style applied to the character at the insertion point.
type Cursor struct {
	Style lipgloss.Style
}

// Model is a focused, horizontally scrolling, single-line input.
type Model struct {
	Prompt           string
	Placeholder      string
	CharLimit        int
	Width            int
	PromptStyle      lipgloss.Style
	TextStyle        lipgloss.Style
	PlaceholderStyle lipgloss.Style
	Cursor           Cursor

	value         []rune
	position      int
	focused       bool
	cursorVisible bool
}

// New creates an unfocused input with conventional defaults.
func New() Model {
	return Model{
		Prompt:           "> ",
		PlaceholderStyle: lipgloss.NewStyle().Faint(true),
		cursorVisible:    true,
	}
}

// Blink initializes or advances cursor blinking.
func Blink() tea.Msg { return blinkMsg{} }

// Focus allows the input to consume key events.
func (m *Model) Focus() tea.Cmd {
	m.focused = true
	m.cursorVisible = true
	return Blink
}

// Blur prevents the input from consuming key events.
func (m *Model) Blur() {
	m.focused = false
	m.cursorVisible = false
}

// Focused reports whether the input consumes key events.
func (m Model) Focused() bool { return m.focused }

// Value returns the complete input value.
func (m Model) Value() string { return string(m.value) }

// SetValue replaces the input value and moves the cursor to the end.
func (m *Model) SetValue(value string) {
	m.value = sanitize(value)
	if m.CharLimit > 0 && len(m.value) > m.CharLimit {
		m.value = m.value[:m.CharLimit]
	}
	m.position = len(m.value)
}

// Reset clears the input.
func (m *Model) Reset() {
	m.value = nil
	m.position = 0
}

// Update handles standard line-editing keys and printable runes.
func (m Model) Update(message tea.Msg) (Model, tea.Cmd) {
	if blink, ok := message.(blinkMsg); ok {
		_ = blink
		if !m.focused {
			return m, nil
		}
		m.cursorVisible = !m.cursorVisible
		return m, tea.Tick(blinkSpeed, func(time.Time) tea.Msg { return blinkMsg{} })
	}
	if !m.focused {
		return m, nil
	}

	key, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	m.cursorVisible = true
	switch key.String() {
	case "left", "ctrl+b":
		m.position = max(0, m.position-1)
	case "right", "ctrl+f":
		m.position = min(len(m.value), m.position+1)
	case "home", "ctrl+a":
		m.position = 0
	case "end", "ctrl+e":
		m.position = len(m.value)
	case "backspace", "ctrl+h":
		if m.position > 0 {
			m.value = append(m.value[:m.position-1], m.value[m.position:]...)
			m.position--
		}
	case "delete", "ctrl+d":
		if m.position < len(m.value) {
			m.value = append(m.value[:m.position], m.value[m.position+1:]...)
		}
	case "ctrl+u":
		m.value = append([]rune(nil), m.value[m.position:]...)
		m.position = 0
	case "ctrl+k":
		m.value = m.value[:m.position]
	case "ctrl+w", "alt+backspace":
		m.deleteWordBackward()
	case "enter", "tab", "esc", "ctrl+c", "pgup", "pgdown", "ctrl+v":
		// The parent model owns application keys. Clipboard commands are left
		// to Bubble Tea's terminal paste events, which arrive as runes.
	default:
		m.insert(key.Runes)
	}
	return m, nil
}

// View renders the prompt, visible value, and cursor on one line.
func (m Model) View() string {
	prompt := m.PromptStyle.Render(m.Prompt)
	available := max(1, m.Width)
	if len(m.value) == 0 {
		placeholder := clipRunes([]rune(m.Placeholder), available)
		if m.focused && m.cursorVisible {
			cursor := " "
			if len(placeholder) > 0 {
				cursor = string(placeholder[0])
				placeholder = placeholder[1:]
			}
			return prompt + m.Cursor.Style.Reverse(true).Render(cursor) + m.PlaceholderStyle.Render(string(placeholder))
		}
		return prompt + m.PlaceholderStyle.Render(string(placeholder))
	}

	start := visibleStart(m.value, m.position, available)
	visible := clipRunes(m.value[start:], available)
	position := min(len(visible), m.position-start)
	before := m.TextStyle.Render(string(visible[:position]))
	after := ""
	cursor := " "
	if position < len(visible) {
		cursor = string(visible[position])
		after = m.TextStyle.Render(string(visible[position+1:]))
	}
	if !m.focused || !m.cursorVisible {
		return prompt + before + m.TextStyle.Render(cursor) + after
	}
	return prompt + before + m.Cursor.Style.Reverse(true).Render(cursor) + after
}

func (m *Model) insert(input []rune) {
	input = sanitize(string(input))
	if len(input) == 0 {
		return
	}
	if m.CharLimit > 0 {
		remaining := m.CharLimit - len(m.value)
		if remaining <= 0 {
			return
		}
		if len(input) > remaining {
			input = input[:remaining]
		}
	}
	tail := append([]rune(nil), m.value[m.position:]...)
	m.value = append(m.value[:m.position], input...)
	m.value = append(m.value, tail...)
	m.position += len(input)
}

func (m *Model) deleteWordBackward() {
	end := m.position
	for m.position > 0 && unicode.IsSpace(m.value[m.position-1]) {
		m.position--
	}
	for m.position > 0 && !unicode.IsSpace(m.value[m.position-1]) {
		m.position--
	}
	m.value = append(m.value[:m.position], m.value[end:]...)
}

func sanitize(value string) []rune {
	value = strings.Map(func(char rune) rune {
		if char == '\n' || char == '\r' || char == '\t' {
			return ' '
		}
		if unicode.IsControl(char) {
			return -1
		}
		return char
	}, value)
	return []rune(value)
}

func visibleStart(value []rune, position, width int) int {
	start := 0
	for start < position && lipgloss.Width(string(value[start:position])) >= width {
		start++
	}
	return start
}

func clipRunes(value []rune, width int) []rune {
	used := 0
	for index, char := range value {
		used += lipgloss.Width(string(char))
		if used > width {
			return value[:index]
		}
	}
	return value
}
