package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const nodeTraceLineLimit = 25

type graphRow struct {
	line   int
	nodeID string
}

type paneBounds struct {
	x      int
	y      int
	width  int
	height int
}

func (b paneBounds) contains(x, y int) bool {
	return b.width > 0 && b.height > 0 && x >= b.x && x < b.right() && y >= b.y && y < b.bottom()
}

func (b paneBounds) right() int  { return b.x + b.width }
func (b paneBounds) bottom() int { return b.y + b.height }

type nodeSection int

const (
	nodeSectionDetails nodeSection = iota
	nodeSectionTrail
	nodeSectionTrace
)

func (m *Model) ensureGraphSelection() {
	if len(m.graphRows) == 0 {
		m.refreshGraph()
	}
	if len(m.graphRows) == 0 {
		m.selectedNodeID = ""
		return
	}
	for _, row := range m.graphRows {
		if row.nodeID == m.selectedNodeID {
			m.ensureGraphSelectionVisible()
			return
		}
	}
	m.selectedNodeID = m.graphRows[0].nodeID
	m.refreshGraph()
	m.ensureGraphSelectionVisible()
}

func (m *Model) moveGraphSelection(delta int) {
	if len(m.graphRows) == 0 {
		m.refreshGraph()
	}
	if len(m.graphRows) == 0 {
		return
	}
	selected := -1
	for index, row := range m.graphRows {
		if row.nodeID == m.selectedNodeID {
			selected = index
			break
		}
	}
	if selected < 0 {
		if delta < 0 {
			selected = len(m.graphRows) - 1
		} else {
			selected = 0
		}
	} else {
		selected = max(0, min(len(m.graphRows)-1, selected+delta))
	}
	m.selectedNodeID = m.graphRows[selected].nodeID
	m.refreshGraph()
	m.ensureGraphSelectionVisible()
}

func (m *Model) ensureGraphSelectionVisible() {
	if m.selectedNodeID == "" {
		return
	}
	for _, row := range m.graphRows {
		if row.nodeID != m.selectedNodeID {
			continue
		}
		if row.line < m.graph.YOffset {
			m.graph.SetYOffset(row.line)
		} else if row.line >= m.graph.YOffset+max(1, m.graph.Height) {
			m.graph.SetYOffset(row.line - max(1, m.graph.Height) + 1)
		}
		return
	}
}

func (m *Model) graphNodeAtLine(line int) string {
	for _, row := range m.graphRows {
		if row.line == line {
			return row.nodeID
		}
	}
	return ""
}

func (m *Model) openSelectedNode() tea.Cmd {
	if m.selectedNodeID == "" {
		return nil
	}
	node, ok := m.snapshotNode(m.selectedNodeID)
	if !ok {
		return m.showStatus("selected node is no longer visible")
	}
	m.returnFocus = m.focus
	m.chatDraft = m.input.Value()
	m.nodeViewID = node.ID
	m.inspectedNode = node
	m.nodeMessages = nil
	m.nodeLastSeq = 0
	m.nodeTraceText = ""
	m.nodeScroll = nodeSectionDetails
	m.nodeDetails.GotoTop()
	m.nodeTrail.GotoTop()
	m.nodeTrace.GotoBottom()
	m.palette = paletteNone
	m.input.Reset()
	m.input.Placeholder = "steer this worker — lands before its next turn"
	m.focus = focusInput
	m.inputFocused = true
	_ = m.input.Focus()
	m.setSize(m.width, m.height)
	m.refreshNodeView(true)
	return m.poll()
}

func (m *Model) closeNodeView() {
	m.nodeViewID = ""
	m.inspectedNode = store.Node{}
	m.nodeMessages = nil
	m.nodeLastSeq = 0
	m.nodeTraceText = ""
	m.input.Reset()
	m.input.Placeholder = "Ask the graph…"
	m.input.SetValue(m.chatDraft)
	m.chatDraft = ""
	m.focus = m.returnFocus
	m.inputFocused = m.focus == focusInput
	if m.inputFocused {
		_ = m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.setSize(m.width, m.height)
}

func (m *Model) snapshotNode(nodeID string) (store.Node, bool) {
	for _, node := range m.snapshot.Nodes {
		if node.ID == nodeID {
			return node, true
		}
	}
	return store.Node{}, false
}

func (m *Model) submitSteer() tea.Cmd {
	body := strings.TrimSpace(m.input.Value())
	if body == "" || m.nodeViewID == "" {
		return nil
	}
	m.input.Reset()
	m.err = nil
	message := store.Message{
		Time:      time.Now(),
		SessionID: m.sessionID,
		Role:      store.RoleUser,
		Body:      body,
		NodeID:    m.nodeViewID,
	}
	m.nodeMessages = append(m.nodeMessages, message)
	m.refreshNodeView(true)

	backend := m.backend
	return func() tea.Msg {
		posted, err := backend.PostMessage(message)
		return postResultMsg{message: posted, nodeID: message.NodeID, err: err}
	}
}

func (m *Model) landOptimisticNodeMessage(nodeID string, posted store.Message) {
	if nodeID != m.nodeViewID {
		return
	}
	for index := range m.nodeMessages {
		message := &m.nodeMessages[index]
		if message.Seq == 0 && message.Role == posted.Role && message.Body == posted.Body {
			*message = posted
			m.refreshNodeView(true)
			return
		}
	}
	m.nodeMessages = append(m.nodeMessages, posted)
	m.refreshNodeView(true)
}

func (m *Model) appendNodeMessages(messages []store.Message) {
	for _, incoming := range messages {
		duplicate := false
		for index := range m.nodeMessages {
			existing := &m.nodeMessages[index]
			if incoming.Seq != 0 && existing.Seq == incoming.Seq {
				duplicate = true
				break
			}
			if existing.Seq == 0 && existing.Role == incoming.Role && existing.Body == incoming.Body {
				*existing = incoming
				duplicate = true
				break
			}
		}
		if !duplicate {
			m.nodeMessages = append(m.nodeMessages, incoming)
		}
		m.nodeLastSeq = max(m.nodeLastSeq, incoming.Seq)
	}
}

func (m *Model) cancelInspectedNode() tea.Cmd {
	if m.commander == nil {
		return m.showStatus("cancel unavailable — no Commander")
	}
	if err := m.commander.Cancel(m.nodeViewID); err != nil {
		return m.showStatus(fmt.Sprintf("could not cancel %s: %v", m.nodeViewID, err))
	}
	return m.showStatus("cancel requested → " + m.nodeViewID)
}

func (m *Model) sizeNodeViewports() {
	innerWidth := max(1, m.width-4)
	innerHeight := max(1, m.chatHeight-2)
	sections := 2
	if m.commander != nil {
		sections++
	}
	available := max(0, innerHeight-2-sections)
	m.nodeDetailsHeight, m.nodeTrailHeight, m.nodeTraceHeight = 0, 0, 0
	if sections == 2 {
		m.nodeDetailsHeight = available / 2
		m.nodeTrailHeight = available - m.nodeDetailsHeight
	} else {
		m.nodeDetailsHeight = available / 3
		m.nodeTrailHeight = available / 3
		m.nodeTraceHeight = available - m.nodeDetailsHeight - m.nodeTrailHeight
	}
	m.nodeDetails.Width, m.nodeDetails.Height = innerWidth, max(1, m.nodeDetailsHeight)
	m.nodeTrail.Width, m.nodeTrail.Height = innerWidth, max(1, m.nodeTrailHeight)
	m.nodeTrace.Width, m.nodeTrace.Height = innerWidth, max(1, m.nodeTraceHeight)
}

func (m *Model) refreshNodeView(followTrail bool) {
	detailsOffset := m.nodeDetails.YOffset
	m.nodeDetails.SetContent(m.renderNodeDetailsContent())
	m.nodeDetails.SetYOffset(detailsOffset)
	m.nodeTrail.SetContent(m.renderNodeTrailContent())
	if followTrail {
		m.nodeTrail.GotoBottom()
	}
	m.nodeTrace.SetContent(renderTraceTail(m.nodeTraceText, nodeTraceLineLimit))
	m.nodeTrace.GotoBottom()
}

func (m *Model) renderNodeDetailsContent() string {
	brief := strings.TrimSpace(m.inspectedNode.Brief)
	if brief == "" {
		brief = m.inspectedNode.ID
	}
	content := inputTextStyle.Render(wrapText(brief, max(1, m.nodeDetails.Width)))
	if !terminalStatus(m.inspectedNode.Status) {
		return content
	}
	if m.inspectedNode.Status == store.Failed || m.inspectedNode.Status == store.Cancelled {
		failure := strings.TrimSpace(m.inspectedNode.Error)
		if failure == "" {
			failure = string(m.inspectedNode.Status)
		}
		return content + "\n\n" + mutedStyle.Render("ERROR") + "\n" +
			lipgloss.NewStyle().Foreground(rose).Render(wrapText(failure, max(1, m.nodeDetails.Width)))
	}
	if summary := strings.TrimSpace(m.inspectedNode.Summary); summary != "" {
		content += "\n\n" + mutedStyle.Render("SUMMARY") + "\n" +
			inputTextStyle.Render(wrapText(summary, max(1, m.nodeDetails.Width)))
	}
	return content
}

func (m *Model) renderNodeTrailContent() string {
	if len(m.nodeMessages) == 0 {
		return mutedStyle.Render("no steering or announcements yet")
	}
	blocks := make([]string, 0, len(m.nodeMessages))
	for _, message := range m.nodeMessages {
		accent, label := messagePresentation(message)
		header := lipgloss.NewStyle().Foreground(accent).Faint(true).Render(label)
		header += mutedStyle.Faint(true).Render("  " + relativeTime(message.Time, time.Now()))
		bodyStyle := inputTextStyle
		if message.Role == store.RoleSystem {
			bodyStyle = lipgloss.NewStyle().Foreground(lavender).Bold(true)
		}
		body := bodyStyle.Render(wrapText(message.Body, max(1, m.nodeTrail.Width)))
		blocks = append(blocks, header+"\n"+body)
	}
	return strings.Join(blocks, "\n\n")
}

func renderTraceTail(trace string, limit int) string {
	trace = strings.TrimRight(strings.ReplaceAll(trace, "\r\n", "\n"), "\n")
	if trace == "" || limit <= 0 {
		return mutedStyle.Faint(true).Render("waiting for worker trace…")
	}
	lines := strings.Split(trace, "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return mutedStyle.Faint(true).Render(strings.Join(lines, "\n"))
}

func terminalStatus(status store.Status) bool {
	return status == store.Done || status == store.Failed || status == store.Cancelled
}

func (m *Model) nodeTiming(now time.Time) string {
	node := m.inspectedNode
	if terminalStatus(node.Status) {
		parts := make([]string, 0, 2)
		if !node.StartedAt.IsZero() && !node.FinishedAt.IsZero() {
			parts = append(parts, formatElapsed(node.FinishedAt.Sub(node.StartedAt)))
		}
		if !node.FinishedAt.IsZero() {
			parts = append(parts, "finished "+relativeTime(node.FinishedAt, now))
		} else {
			parts = append(parts, string(node.Status))
		}
		return strings.Join(parts, " · ")
	}
	if !node.StartedAt.IsZero() {
		return formatElapsed(now.Sub(node.StartedAt)) + " elapsed"
	}
	return string(node.Status)
}

func (m *Model) updateNodeViewport(message tea.Msg) {
	viewport := m.activeNodeViewport()
	updated, _ := viewport.Update(message)
	*viewport = updated
}

func (m *Model) pageNodeViewport(down bool) {
	viewport := m.activeNodeViewport()
	if down {
		viewport.PageDown()
	} else {
		viewport.PageUp()
	}
}

func (m *Model) activeNodeViewport() *viewport.Model {
	switch m.nodeScroll {
	case nodeSectionTrail:
		return &m.nodeTrail
	case nodeSectionTrace:
		return &m.nodeTrace
	default:
		return &m.nodeDetails
	}
}

func (m *Model) scrollNodeAt(x, y int, down bool) bool {
	var target *viewport.Model
	switch {
	case m.nodeDetailsBounds.contains(x, y):
		m.nodeScroll, target = nodeSectionDetails, &m.nodeDetails
	case m.nodeTrailBounds.contains(x, y):
		m.nodeScroll, target = nodeSectionTrail, &m.nodeTrail
	case m.commander != nil && m.nodeTraceBounds.contains(x, y):
		m.nodeScroll, target = nodeSectionTrace, &m.nodeTrace
	default:
		return false
	}
	if down {
		target.SetYOffset(target.YOffset + 3)
	} else {
		target.SetYOffset(target.YOffset - 3)
	}
	return true
}

func (m *Model) updateMouseClick(x, y int) bool {
	if m.inputBounds.contains(x, y) {
		m.focus = focusInput
		m.inputFocused = true
		_ = m.input.Focus()
		return true
	}
	if m.nodeViewID != "" {
		switch {
		case m.nodeDetailsBounds.contains(x, y):
			m.nodeScroll = nodeSectionDetails
		case m.nodeTrailBounds.contains(x, y):
			m.nodeScroll = nodeSectionTrail
		case m.commander != nil && m.nodeTraceBounds.contains(x, y):
			m.nodeScroll = nodeSectionTrace
		default:
			return false
		}
		m.inputFocused = false
		m.input.Blur()
		return true
	}
	if m.graphBounds.contains(x, y) {
		m.focus = focusGraph
		m.inputFocused = false
		m.input.Blur()
		if m.graphRowsBounds.contains(x, y) {
			nodeID := m.graphNodeAtLine(y - m.graphRowsBounds.y + m.graph.YOffset)
			if nodeID != "" {
				alreadySelected := nodeID == m.selectedNodeID
				m.selectedNodeID = nodeID
				m.refreshGraph()
				if alreadySelected {
					_ = m.openSelectedNode()
				}
			}
		}
		return true
	}
	if m.chatBounds.contains(x, y) {
		m.focus = focusChat
		m.inputFocused = false
		m.input.Blur()
		return true
	}
	return false
}
