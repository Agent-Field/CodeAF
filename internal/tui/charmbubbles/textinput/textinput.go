// Package textinput provides the compact Bubble Tea input used by aforge.
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

// Model is a focused, soft-wrapping input with a bounded visible height.
type Model struct {
	Prompt           string
	Placeholder      string
	CharLimit        int
	Width            int
	MaxLines         int
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
		MaxLines:         1,
		PlaceholderStyle: lipgloss.NewStyle().Faint(true),
		cursorVisible:    true,
	}
}

// LineCount reports the number of rows View currently occupies.
func (m Model) LineCount() int {
	if len(m.value) == 0 {
		return 1
	}
	return min(max(1, m.MaxLines), len(m.lineRanges()))
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

// View renders the prompt, soft-wrapped value, and cursor. Long values retain
// the rows around the insertion point rather than scrolling horizontally.
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

	ranges := m.lineRanges()
	cursorLine := len(ranges) - 1
	for index, line := range ranges {
		if m.position < line.end || m.position == line.end && line.end == len(m.value) {
			cursorLine = index
			break
		}
	}
	maxLines := max(1, m.MaxLines)
	startLine := max(0, cursorLine-maxLines+1)
	endLine := min(len(ranges), startLine+maxLines)
	continuation := strings.Repeat(" ", lipgloss.Width(m.Prompt))

	rows := make([]string, 0, endLine-startLine)
	for index := startLine; index < endLine; index++ {
		line := ranges[index]
		linePrompt := continuation
		if index == 0 {
			linePrompt = prompt
		}
		if index != cursorLine {
			rows = append(rows, linePrompt+m.TextStyle.Render(string(m.value[line.start:line.end])))
			continue
		}

		position := min(max(line.start, m.position), line.end)
		before := m.TextStyle.Render(string(m.value[line.start:position]))
		cursor := " "
		afterStart := position
		if position < line.end {
			cursor = string(m.value[position])
			afterStart++
		}
		cursorView := m.TextStyle.Render(cursor)
		if m.focused && m.cursorVisible {
			cursorView = m.Cursor.Style.Reverse(true).Render(cursor)
		}
		rows = append(rows, linePrompt+before+cursorView+m.TextStyle.Render(string(m.value[afterStart:line.end])))
	}
	return strings.Join(rows, "\n")
}

type lineRange struct{ start, end int }

func (m Model) lineRanges() []lineRange {
	width := max(1, m.Width)
	ranges := make([]lineRange, 0, 3)
	start, used := 0, 0
	for index, char := range m.value {
		charWidth := lipgloss.Width(string(char))
		if used > 0 && used+charWidth > width {
			ranges = append(ranges, lineRange{start: start, end: index})
			start, used = index, 0
		}
		used += charWidth
	}
	ranges = append(ranges, lineRange{start: start, end: len(m.value)})
	return ranges
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
