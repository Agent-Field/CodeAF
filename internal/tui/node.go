package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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

// sizeNodeViewports lays the node view out as a short static header (the
// brief, plus outcome once terminal) over one scrolling activity feed. One
// scrollable region means one obvious scroll — the wheel, PgUp, and arrows
// all move the same thing.
func (m *Model) sizeNodeViewports() {
	innerWidth := max(1, m.width-4)
	innerHeight := max(1, m.chatHeight-2)
	m.nodeDetailsText = m.renderNodeDetailsContent(innerWidth, max(2, innerHeight/3))
	detailLines := strings.Count(m.nodeDetailsText, "\n") + 1
	// header + blank + BRIEF label + details + blank + ACTIVITY label
	m.nodeTraceHeight = max(3, innerHeight-5-detailLines)
	m.nodeTrace.Width, m.nodeTrace.Height = innerWidth, m.nodeTraceHeight
}

// refreshNodeView re-renders the feed without stealing the scrollback: it
// follows new output only when the reader was already at the bottom (or just
// acted), never yanking someone who scrolled up to read history.
func (m *Model) refreshNodeView(force bool) {
	follow := force || m.nodeTrace.AtBottom()
	offset := m.nodeTrace.YOffset
	m.nodeTrace.SetContent(renderActivityFeed(m.nodeTraceText, m.nodeMessages, max(1, m.nodeTrace.Width)))
	if follow {
		m.nodeTrace.GotoBottom()
	} else {
		m.nodeTrace.SetYOffset(offset)
	}
}

func (m *Model) renderNodeDetailsContent(width, maxLines int) string {
	brief := strings.TrimSpace(m.inspectedNode.Brief)
	if brief == "" {
		brief = m.inspectedNode.ID
	}
	content := inputTextStyle.Render(wrapText(brief, width))
	if terminalStatus(m.inspectedNode.Status) {
		if m.inspectedNode.Status == store.Failed || m.inspectedNode.Status == store.Cancelled {
			failure := strings.TrimSpace(m.inspectedNode.Error)
			if failure == "" {
				failure = string(m.inspectedNode.Status)
			}
			content += "\n" + lipgloss.NewStyle().Foreground(rose).Render(wrapText(failure, width))
		} else if summary := strings.TrimSpace(m.inspectedNode.Summary); summary != "" {
			content += "\n" + inputTextStyle.Render(wrapText(summary, width))
		}
	}
	lines := strings.Split(content, "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], mutedStyle.Faint(true).Render("… (full result lands in chat)"))
	}
	return strings.Join(lines, "\n")
}

// The activity feed's visual grammar, kept to five distinct voices so the eye
// learns it once: a muted rule per turn, the model's own words behind ✳, tool
// calls behind their glyph, results as faint arrows, and you in lavender.
var (
	feedTurnRule   = regexp.MustCompile(`^── turn (\d+)\s+finish=(\S*)\s+in=(\d+) out=(\d+)\s*(?:\[([^\]]*)\])?\s*──$`)
	feedThought    = lipgloss.NewStyle().Foreground(powder)
	feedToolGlyph  = lipgloss.NewStyle().Foreground(peach)
	feedResult     = lipgloss.NewStyle().Foreground(muted).Faint(true)
	feedError      = lipgloss.NewStyle().Foreground(rose)
	feedYou        = lipgloss.NewStyle().Foreground(lavender).Bold(true)
	feedThoughtCap = 6
)

// renderActivityFeed parses the worker's flight-recorder log into a readable
// timeline: what the model said to itself, what it ran, what came back, and
// any steering, followed by the node's thread messages. The raw log stays on
// disk; this is the human view of it.
func renderActivityFeed(trace string, messages []store.Message, width int) string {
	trace = strings.TrimRight(strings.ReplaceAll(trace, "\r\n", "\n"), "\n")
	var out []string
	for _, line := range strings.Split(trace, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch {
		case feedTurnRule.MatchString(line):
			parts := feedTurnRule.FindStringSubmatch(line)
			rule := "── turn " + parts[1] + " · " + parts[4] + " tok"
			if parts[5] != "" {
				rule += " · " + parts[5]
			}
			rule += " " + strings.Repeat("─", max(0, width-lipgloss.Width(rule)-1))
			out = append(out, "", mutedStyle.Faint(true).Render(truncate(rule, width)))
		case strings.HasPrefix(line, "text: "):
			thought := strings.ReplaceAll(strings.TrimPrefix(line, "text: "), "⏎", " ")
			wrapped := strings.Split(wrapText(thought, max(1, width-2)), "\n")
			if len(wrapped) > feedThoughtCap {
				wrapped = append(wrapped[:feedThoughtCap], "…")
			}
			out = append(out, feedThought.Render("✳ ")+inputTextStyle.Render(wrapped[0]))
			for _, extra := range wrapped[1:] {
				out = append(out, "  "+inputTextStyle.Render(extra))
			}
		case strings.HasPrefix(line, "call "):
			out = append(out, renderToolCall(strings.TrimPrefix(line, "call "), width))
		case strings.HasPrefix(line, "  → "):
			out = append(out, renderToolResult(strings.TrimPrefix(line, "  → "), width))
		case strings.HasPrefix(line, "steered: "):
			out = append(out, feedYou.Render("▸ you  ")+
				inputTextStyle.Render(truncate(strings.TrimPrefix(line, "steered: "), max(1, width-7))))
		default:
			out = append(out, mutedStyle.Faint(true).Render(truncate(strings.ReplaceAll(line, "⏎", " "), width)))
		}
	}
	if len(messages) > 0 {
		rule := "── thread "
		rule += strings.Repeat("─", max(0, width-lipgloss.Width(rule)-1))
		out = append(out, "", mutedStyle.Faint(true).Render(rule))
		now := time.Now()
		for _, message := range messages {
			accent, label := messagePresentation(message)
			header := lipgloss.NewStyle().Foreground(accent).Faint(true).Render(label) +
				mutedStyle.Faint(true).Render("  "+relativeTime(message.Time, now))
			out = append(out, header, inputTextStyle.Render(wrapText(message.Body, width)))
		}
	}
	if len(out) == 0 {
		return mutedStyle.Faint(true).Render("waiting for the worker's first turn…")
	}
	return strings.Join(out, "\n")
}

// renderToolCall turns `sh {"cmd":"ls"}` into `$ ls` — the glyph names the
// tool, the salient argument names the act, and the JSON plumbing disappears.
func renderToolCall(rest string, width int) string {
	name, args, _ := strings.Cut(rest, " ")
	glyph, detail := "⚙ "+name+" ", ""
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err == nil {
		field := func(key string) string { value, _ := parsed[key].(string); return value }
		switch name {
		case "sh":
			glyph, detail = "$ ", field("cmd")
		case "write":
			glyph, detail = "✎ ", field("path")
		case "edit":
			glyph, detail = "✎ ", field("path")
		case "web":
			if query := field("q"); query != "" {
				glyph, detail = "⌕ ", query
			} else {
				glyph, detail = "⌕ ", "fetch pages"
			}
		}
	}
	if detail == "" {
		detail = args
	}
	detail = strings.ReplaceAll(detail, "⏎", " ")
	return feedToolGlyph.Render(glyph) + inputTextStyle.Render(truncate(detail, max(1, width-lipgloss.Width(glyph))))
}

// renderToolResult compresses `1438B: total 3984…` to a faint one-liner —
// enough to see that something came back and roughly what, without the feed
// becoming the raw output it summarizes.
func renderToolResult(rest string, width int) string {
	size, content, _ := strings.Cut(rest, ": ")
	failed := strings.HasSuffix(size, " ERROR")
	size = strings.TrimSuffix(strings.TrimSuffix(size, " ERROR"), "B")
	if bytes, err := strconv.Atoi(size); err == nil {
		size = humanBytes(bytes)
	}
	head := "  → " + size + "  "
	if failed {
		head = "  → " + size + " " + feedError.Render("ERROR") + "  "
	}
	content = strings.ReplaceAll(content, "⏎", " ")
	return feedResult.Render(head + truncate(content, max(1, width-lipgloss.Width(head))))
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
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
	updated, _ := m.nodeTrace.Update(message)
	m.nodeTrace = updated
}

func (m *Model) pageNodeViewport(down bool) {
	if down {
		m.nodeTrace.PageDown()
	} else {
		m.nodeTrace.PageUp()
	}
}

func (m *Model) scrollNodeFeed(down bool) {
	if down {
		m.nodeTrace.SetYOffset(m.nodeTrace.YOffset + 3)
	} else {
		m.nodeTrace.SetYOffset(m.nodeTrace.YOffset - 3)
	}
}

// scrollNodeAt scrolls the feed for a wheel event anywhere inside the node
// pane. One pane, one scroll — no per-section focus to guess at.
func (m *Model) scrollNodeAt(x, y int, down bool) bool {
	if !m.nodeBounds.contains(x, y) {
		return false
	}
	m.scrollNodeFeed(down)
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
		if !m.nodeBounds.contains(x, y) {
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
