// Package viewport provides the vertically scrollable Bubble Tea view used by
// aforge's conversation pane.
package viewport

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Model is a line-oriented viewport.
type Model struct {
	Width   int
	Height  int
	YOffset int

	lines []string
}

// New creates an empty viewport with the supplied dimensions.
func New(width, height int) Model {
	return Model{Width: max(1, width), Height: max(1, height)}
}

// SetContent replaces the viewport contents while preserving a valid offset.
func (m *Model) SetContent(content string) {
	if content == "" {
		m.lines = nil
	} else {
		m.lines = strings.Split(content, "\n")
	}
	m.SetYOffset(m.YOffset)
}

// TotalLineCount returns the number of content lines.
func (m Model) TotalLineCount() int { return len(m.lines) }

// AtTop reports whether the first line is visible.
func (m Model) AtTop() bool { return m.YOffset <= 0 }

// AtBottom reports whether the last line is visible.
func (m Model) AtBottom() bool { return m.YOffset >= m.maxYOffset() }

// SetYOffset moves to an absolute, clamped line offset.
func (m *Model) SetYOffset(offset int) {
	m.YOffset = min(max(0, offset), m.maxYOffset())
}

// GotoTop moves to the first line.
func (m *Model) GotoTop() { m.YOffset = 0 }

// GotoBottom moves to the final screenful.
func (m *Model) GotoBottom() { m.YOffset = m.maxYOffset() }

// PageUp moves up by one screenful.
func (m *Model) PageUp() []string {
	m.SetYOffset(m.YOffset - max(1, m.Height))
	return m.visibleLines()
}

// PageDown moves down by one screenful.
func (m *Model) PageDown() []string {
	m.SetYOffset(m.YOffset + max(1, m.Height))
	return m.visibleLines()
}

// Update handles conventional viewport navigation keys.
func (m Model) Update(message tea.Msg) (Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		m.SetYOffset(m.YOffset - 1)
	case "down", "j":
		m.SetYOffset(m.YOffset + 1)
	case "ctrl+u":
		m.SetYOffset(m.YOffset - max(1, m.Height/2))
	case "ctrl+d":
		m.SetYOffset(m.YOffset + max(1, m.Height/2))
	case "home", "g":
		m.GotoTop()
	case "end", "G":
		m.GotoBottom()
	}
	return m, nil
}

// View returns the currently visible content lines.
func (m Model) View() string { return strings.Join(m.visibleLines(), "\n") }

func (m Model) visibleLines() []string {
	if len(m.lines) == 0 || m.Height <= 0 {
		return nil
	}
	start := min(max(0, m.YOffset), len(m.lines))
	end := min(len(m.lines), start+m.Height)
	return m.lines[start:end]
}

func (m Model) maxYOffset() int { return max(0, len(m.lines)-max(1, m.Height)) }
