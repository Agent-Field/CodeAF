package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) renderNotebookSurface(width, atLine int, track bool) string {
	if !m.notebookOpen {
		return ""
	}
	if track {
		m.notebookOptionRows = m.notebookOptionRows[:0]
	}
	width = max(12, width)
	if len(m.notebookFacts) == 0 {
		line := "notebook is empty"
		if m.notebookQuery != "" {
			line = "nothing learned about “" + m.notebookQuery + "”"
		}
		lines := []string{mutedStyle.Faint(true).Render(truncate(line, width))}
		lines = append(lines, mutedStyle.Faint(true).Render("⟨×⟩ close"))
		if track {
			m.chatExpandRows = append(m.chatExpandRows, chatExpandRow{
				line: atLine + 1, action: chatNotebookClose,
			})
		}
		return strings.Join(lines, "\n")
	}

	lines := make([]string, 0, len(m.notebookFacts)*2+1)
	for index, fact := range m.notebookFacts {
		expanded := fact.Seq == m.notebookExpandedSeq
		disclosure := "▸"
		if expanded {
			disclosure = "▾"
		}
		prefix := mutedStyle.Faint(true).Render(disclosure + " " + strconv.Itoa(index+1) + " ")
		glyph := notebookKindGlyph(fact.Kind)
		age := store.AgeLabel(fact.Time, time.Now())
		status := notebookStatusMark(fact.Status)
		meta := age
		if status != "" {
			if meta != "" {
				meta += " · "
			}
			meta += status
		}
		available := max(1, width-lipgloss.Width(prefix)-lipgloss.Width(glyph)-1)
		if meta != "" {
			available = max(1, available-lipgloss.Width(meta)-3)
		}
		body := notebookFactStyle(fact).Render(truncate(firstLine(fact.Body), available))
		row := prefix + glyph + " " + body
		if meta != "" {
			row += mutedStyle.Faint(true).Render(" · " + meta)
		}
		lines = append(lines, truncate(row, width))
		if track {
			m.chatExpandRows = append(m.chatExpandRows, chatExpandRow{
				line: atLine + len(lines) - 1, action: chatExpandNotebookFact, seq: fact.Seq,
			})
		}
		if !expanded {
			continue
		}

		bodyWidth := max(1, width-2)
		for _, line := range strings.Split(wrapText(strings.TrimSpace(fact.Body), bodyWidth), "\n") {
			lines = append(lines, mutedStyle.Faint(true).Render("  ")+notebookFactStyle(fact).Render(line))
		}
		refs := m.notebookEvidence(fact)
		evidence := fmt.Sprintf("evidence · %d", len(refs))
		if len(refs) > 0 {
			evidence += " · " + strings.Join(refs, " · ")
		}
		lines = append(lines, mutedStyle.Faint(true).Render(truncate("  "+evidence, width)))
		scope := "scope · " + strings.TrimSpace(fact.Scope)
		if age != "" {
			scope += " · " + age
		}
		lines = append(lines, mutedStyle.Faint(true).Render(truncate("  "+scope, width)))

		if fact.Status == store.FactActive {
			component := notebookRetractQuestion()
			lines = append(lines, mutedStyle.Faint(true).Render("  "+component.Prompt))
			optionLine, spans := renderConfirmOptions(component, m.notebookOption, width,
				mutedStyle.Faint(true).Render("  "))
			lines = append(lines, optionLine)
			if track {
				line := atLine + len(lines) - 1
				for optionIndex, span := range spans {
					m.notebookOptionRows = append(m.notebookOptionRows, cardOptionRow{
						line: line, startX: span.startX, endX: span.endX,
						cardID: fmt.Sprint(fact.Seq), optionIndex: optionIndex,
					})
				}
			}
		}
	}
	lines = append(lines, mutedStyle.Faint(true).Render("⟨×⟩ close"))
	if track {
		m.chatExpandRows = append(m.chatExpandRows, chatExpandRow{
			line: atLine + len(lines) - 1, action: chatNotebookClose,
		})
	}
	return strings.Join(lines, "\n")
}

func notebookRetractQuestion() questionComponent {
	return questionComponent{
		Kind: questionConfirm, Prompt: "Retract this belief?", Default: "keep",
		Options: []questionOption{
			{Number: 1, Key: "keep", Label: "Keep"},
			{Number: 2, Key: "retract", Label: "Retract"},
		},
	}
}

func notebookKindGlyph(kind store.FactKind) string {
	glyph := "~"
	switch kind {
	case store.FactPlain:
		glyph = "·"
	case store.FactSkill:
		glyph = "⚒"
	case store.FactUnsettled:
		glyph = "⚖"
	}
	return mutedStyle.Faint(true).Render(glyph)
}

func notebookFactStyle(fact store.Fact) lipgloss.Style {
	switch fact.Status {
	case store.FactQuarantined:
		return lipgloss.NewStyle().Foreground(rose).Strikethrough(true)
	case store.FactCandidate, store.FactSuperseded:
		return mutedStyle.Faint(true)
	default:
		return inputTextStyle
	}
}

func notebookStatusMark(status string) string {
	switch status {
	case store.FactCandidate:
		return "candidate"
	case store.FactSuperseded:
		return "superseded"
	case store.FactQuarantined:
		return "✗"
	default:
		return ""
	}
}

func (m *Model) notebookEvidence(fact store.Fact) []string {
	if provider, ok := m.commander.(interface {
		NotebookEvidence(int64) []string
	}); ok {
		return provider.NotebookEvidence(fact.Seq)
	}
	if strings.TrimSpace(fact.NodeID) != "" && fact.NodeID != store.RootID {
		return []string{fact.NodeID}
	}
	return nil
}

func (m *Model) expandNotebookFact(seq int64) {
	if seq <= 0 {
		return
	}
	if m.notebookExpandedSeq == seq {
		m.notebookExpandedSeq = 0
	} else {
		m.notebookExpandedSeq = seq
		m.notebookOption = 0
	}
}

func (m *Model) keepNotebookFact(seq int64) {
	if seq == m.notebookExpandedSeq {
		m.notebookOption = 0
	}
}

func (m *Model) activateSelectedNotebookOption() {
	if m.notebookExpandedSeq == 0 {
		return
	}
	if m.notebookOption == 1 {
		m.retractNotebookFact(m.notebookExpandedSeq)
		return
	}
	m.keepNotebookFact(m.notebookExpandedSeq)
}

func (m *Model) retractNotebookFact(seq int64) {
	if seq <= 0 {
		return
	}
	handler, ok := m.commander.(interface {
		RetractNotebook(int64) error
	})
	if !ok {
		m.status = "retract unavailable — no Commander"
		m.statusUntil = time.Now().Add(statusTTL)
		return
	}
	if err := handler.RetractNotebook(seq); err != nil {
		m.status = "could not retract belief: " + err.Error()
		m.statusUntil = time.Now().Add(statusTTL)
		return
	}
	for index := range m.notebookFacts {
		if m.notebookFacts[index].Seq == seq {
			m.notebookFacts[index].Status = store.FactQuarantined
			break
		}
	}
	m.notebookOption = 0
}

func (m *Model) closeNotebook() {
	m.notebookOpen = false
	m.notebookQuery = ""
	m.notebookFacts = nil
	m.notebookExpandedSeq = 0
	m.notebookOption = 0
	if m.focus == focusChat {
		m.focus = focusInput
		m.inputFocused = true
		_ = m.input.Focus()
	}
	m.setSize(m.width, m.height)
}

func (m *Model) collapseNotebook() bool {
	if !m.notebookOpen || m.notebookExpandedSeq == 0 {
		return false
	}
	m.notebookExpandedSeq = 0
	m.notebookOption = 0
	m.refreshChat()
	return true
}

func (m *Model) activateNotebookNumber(number int) {
	if !m.notebookOpen || number <= 0 {
		return
	}
	if m.notebookExpandedSeq != 0 && number <= 2 {
		if number == 1 {
			m.keepNotebookFact(m.notebookExpandedSeq)
		} else {
			m.retractNotebookFact(m.notebookExpandedSeq)
		}
		m.refreshChat()
		return
	}
	if number > len(m.notebookFacts) {
		return
	}
	m.expandNotebookFact(m.notebookFacts[number-1].Seq)
	m.refreshChat()
}

func (m *Model) focusFirstNotebookRow() {
	if !m.notebookOpen {
		return
	}
	targets := m.chatFocusLines()
	for targetIndex, line := range targets {
		for _, row := range m.chatExpandRows {
			if row.line == line && row.action == chatExpandNotebookFact {
				m.chatFocusIndex = targetIndex
				m.refreshChat()
				m.chat.SetYOffset(max(0, line-m.chat.Height+2))
				return
			}
		}
	}
}

func (m *Model) focusNotebookOptionRow() {
	if len(m.notebookOptionRows) == 0 {
		return
	}
	want := m.notebookOptionRows[0].line
	targets := m.chatFocusLines()
	for index, line := range targets {
		if line == want {
			m.chatFocusIndex = index
			m.refreshChat()
			return
		}
	}
}
