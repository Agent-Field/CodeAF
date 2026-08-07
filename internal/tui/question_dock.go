package tui

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type questionDockRow struct {
	line        int
	questionSeq int64
}

func (m *Model) renderActivityDock(track bool) string {
	questions := m.renderAgentQuestionDock(track)
	cards := m.renderCardDock(track)
	var combined string
	switch {
	case questions == "":
		combined = cards
	case cards == "":
		combined = questions
	default:
		combined = questions + "\n" + cards
	}
	if m.questionDockExpanded {
		combined = m.clampCardDock(combined, max(4, m.height*2/5))
	}
	return m.withDockPresence(combined)
}

func (m *Model) withDockPresence(content string) string {
	if m.graphVisible() || m.nodeViewID != "" {
		return content
	}
	text := m.residentPresenceText()
	if text == "" {
		return content
	}
	presence := mutedStyle.Faint(true).Render(text)
	if content == "" {
		return truncate(presence, m.width)
	}
	lines := strings.Split(content, "\n")
	last := len(lines) - 1
	lines[last] = truncate(lines[last]+mutedStyle.Faint(true).Render(" · ")+presence, m.width)
	return strings.Join(lines, "\n")
}

func (m *Model) questionDockHeight() int {
	content := m.renderAgentQuestionDock(false)
	if content == "" {
		return 0
	}
	return lipgloss.Height(content)
}

func (m *Model) renderAgentQuestionDock(track bool) string {
	if track {
		m.questionDockRows = m.questionDockRows[:0]
	}
	count := len(m.agentQuestions)
	if count == 0 {
		return ""
	}
	noun := "question waiting"
	if count != 1 {
		noun = "questions waiting"
	}
	marker := "▸"
	if m.questionDockExpanded {
		marker = "▾"
	}
	header := questionStyle.Bold(true).Render("?") + " " + mutedStyle.Render(marker+" ") +
		questionStyle.Render(fmt.Sprintf("%d %s", count, noun))
	if count == 1 {
		header = questionStyle.Bold(true).Render("?") + " " + mutedStyle.Render(marker+" ") +
			questionStyle.Render(noun)
	}
	lines := []string{truncate(header, m.width)}
	if !m.questionDockExpanded {
		return strings.Join(lines, "\n")
	}
	for index, question := range m.agentQuestions {
		prefix := mutedStyle.Render("  ▸ ")
		line := prefix + questionStyle.Render(truncate(firstLine(question.Text), max(1, m.width-lipgloss.Width(prefix))))
		if m.focus == focusQuestions && index == m.questionDockSelection {
			line = selectedStyle.Width(m.width).Render(line)
		}
		lines = append(lines, truncate(line, m.width))
		if track {
			m.questionDockRows = append(m.questionDockRows, questionDockRow{
				line: len(lines) - 1, questionSeq: question.Seq,
			})
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) clickActivityDock(x, line int) (tea.Cmd, bool) {
	questionHeight := m.questionDockHeight()
	if questionHeight > 0 && line < questionHeight {
		return m.clickAgentQuestionDock(line)
	}
	if questionHeight > 0 {
		line -= questionHeight
	}
	return m.clickCardDock(x, line)
}

func (m *Model) clickAgentQuestionDock(line int) (tea.Cmd, bool) {
	if len(m.agentQuestions) == 0 {
		return nil, false
	}
	m.focus = focusQuestions
	m.inputFocused = false
	m.input.Blur()
	if line == 0 {
		m.questionDockExpanded = !m.questionDockExpanded
		m.setSize(m.width, m.height)
		return nil, true
	}
	if !m.questionDockExpanded {
		m.questionDockExpanded = true
		m.setSize(m.width, m.height)
		return nil, true
	}
	for index, row := range m.questionDockRows {
		if row.line != line {
			continue
		}
		m.questionDockSelection = index
		return m.surfaceSelectedAgentQuestion(), true
	}
	return nil, true
}

func (m *Model) moveAgentQuestionSelection(delta int) {
	if len(m.agentQuestions) == 0 {
		return
	}
	if !m.questionDockExpanded {
		m.questionDockExpanded = true
	}
	m.questionDockSelection = (m.questionDockSelection + delta + len(m.agentQuestions)) % len(m.agentQuestions)
	m.setSize(m.width, m.height)
}

func (m *Model) surfaceSelectedAgentQuestion() tea.Cmd {
	if len(m.agentQuestions) == 0 {
		return nil
	}
	index := max(0, min(m.questionDockSelection, len(m.agentQuestions)-1))
	question := m.agentQuestions[index]
	backend, ok := m.backend.(interface {
		SurfaceQuestion(int64) (store.Message, error)
	})
	if !ok {
		return func() tea.Msg {
			return questionSurfaceResultMsg{questionSeq: question.Seq,
				err: fmt.Errorf("question surface is unavailable")}
		}
	}
	return func() tea.Msg {
		message, err := backend.SurfaceQuestion(question.Seq)
		return questionSurfaceResultMsg{questionSeq: question.Seq, message: message, err: err}
	}
}

func removeAgentQuestion(questions []store.AgentQuestion, seq int64) []store.AgentQuestion {
	kept := questions[:0]
	for _, question := range questions {
		if question.Seq != seq {
			kept = append(kept, question)
		}
	}
	return kept
}
