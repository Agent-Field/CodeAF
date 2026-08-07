package tui

import (
	"fmt"
	"hash/fnv"
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

const historyGraphRowID = "\x00history"

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

func (m *Model) railSelectionIDs() []string {
	ids := make([]string, 0, len(m.graphRows)+len(m.standingRows))
	if m.graphScopeID == "" && m.charterCardID == "" {
		for _, charter := range m.standingCharters() {
			ids = append(ids, standingGraphRowID(charter.ID))
		}
	}
	for _, row := range m.graphRows {
		ids = append(ids, row.nodeID)
	}
	return ids
}

func (m *Model) ensureGraphSelection() {
	if m.charterCardID != "" && m.graphScopeID == "" {
		m.ensureCharterSelection()
		return
	}
	if len(m.graphRows) == 0 {
		m.refreshGraph()
	}
	ids := m.railSelectionIDs()
	if len(ids) == 0 {
		m.selectedNodeID = ""
		return
	}
	for _, id := range ids {
		if id == m.selectedNodeID {
			m.ensureGraphSelectionVisible()
			return
		}
	}
	m.selectedNodeID = ids[0]
	m.refreshGraph()
	m.ensureGraphSelectionVisible()
}

func (m *Model) moveGraphSelection(delta int) {
	if len(m.graphRows) == 0 {
		m.refreshGraph()
	}
	ids := m.railSelectionIDs()
	if len(ids) == 0 {
		return
	}
	selected := -1
	for index, id := range ids {
		if id == m.selectedNodeID {
			selected = index
			break
		}
	}
	if selected < 0 {
		if delta < 0 {
			selected = len(ids) - 1
		} else {
			selected = 0
		}
	} else {
		selected = max(0, min(len(ids)-1, selected+delta))
	}
	m.selectedNodeID = ids[selected]
	m.refreshGraph()
	m.ensureGraphSelectionVisible()
}

func (m *Model) ensureGraphSelectionVisible() {
	if m.selectedNodeID == "" {
		return
	}
	if _, ok := charterIDFromGraphRow(m.selectedNodeID); ok {
		m.graph.SetYOffset(0)
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
	if charterID, ok := charterIDFromGraphRow(m.selectedNodeID); ok {
		m.openStandingCharter(charterID)
		return nil
	}
	if m.selectedNodeID == historyGraphRowID {
		m.toggleHistory()
		return nil
	}
	if _, ok := m.snapshotNode(m.selectedNodeID); !ok {
		return m.showStatus("selected node is no longer visible")
	}
	return m.openNodeByID(m.selectedNodeID)
}

func (m *Model) toggleHistory() {
	m.historyExpanded = !m.historyExpanded
	m.refreshGraph()
	m.ensureGraphSelectionVisible()
}

// openNodeByID opens the activity view for any node the store knows about —
// from the rail selection or from a provenance chip in chat. A node missing
// from the live snapshot (folded away, old session) opens as a placeholder
// that the next poll fills in.
func (m *Model) openNodeByID(nodeID string) tea.Cmd {
	node, ok := m.snapshotNode(nodeID)
	if !ok {
		node = store.Node{ID: nodeID}
	}
	m.returnFocus = m.focus
	m.chatDraft = m.input.Value()
	m.chatAttachments = append([]string(nil), m.attachments...)
	m.nodeViewID = node.ID
	m.inspectedNode = node
	m.nodeMessages = nil
	m.nodeLastSeq = 0
	m.nodeTraceText = ""
	m.feedExpanded = map[string]bool{}
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
	m.attachments = append([]string(nil), m.chatAttachments...)
	m.chatDraft = ""
	m.chatAttachments = nil
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
	if m.graphScopeID == "" {
		return store.Node{}, false
	}
	for _, node := range m.cardSnapshot.Nodes {
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
	if strings.HasPrefix(body, "/") {
		return m.executeSlash(body)
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
	innerWidth := max(1, m.width-2)
	innerHeight := max(1, m.chatHeight)
	m.nodeDetailsText = m.renderNodeDetailsContent(innerWidth, max(2, innerHeight/3))
	detailLines := strings.Count(m.nodeDetailsText, "\n") + 1
	// header + hairline + BRIEF label + details + blank + ACTIVITY label
	m.nodeTraceHeight = max(3, innerHeight-5-detailLines)
	m.nodeTrace.Width, m.nodeTrace.Height = innerWidth, m.nodeTraceHeight
}

// refreshNodeView re-renders the feed without stealing the scrollback: it
// follows new output only when the reader was already at the bottom (or just
// acted), never yanking someone who scrolled up to read history.
func (m *Model) refreshNodeView(force bool) {
	follow := force || m.nodeTrace.AtBottom()
	offset := m.nodeTrace.YOffset
	m.nodeTrace.SetContent(m.renderActivityFeed(max(1, m.nodeTrace.Width)))
	if follow {
		m.nodeTrace.GotoBottom()
	} else {
		m.nodeTrace.SetYOffset(offset)
	}
}

// toggleFeedBlockAt opens or closes the expandable block under a click in
// the feed, keeping the scroll where the reader left it.
func (m *Model) toggleFeedBlockAt(x, y int) bool {
	if !m.nodeTraceBounds.contains(x, y) {
		return false
	}
	line := y - m.nodeTraceBounds.y + m.nodeTrace.YOffset
	for _, row := range m.feedRows {
		if row.line != line {
			continue
		}
		if row.block >= len(m.feedBlocks) || !m.feedBlocks[row.block].expandable() {
			return false
		}
		key := m.feedKeys[row.block]
		m.feedExpanded[key] = !m.feedExpanded[key]
		offset := m.nodeTrace.YOffset
		m.nodeTrace.SetContent(m.renderActivityFeed(max(1, m.nodeTrace.Width)))
		m.nodeTrace.SetYOffset(offset)
		return true
	}
	return false
}

func (m *Model) renderNodeDetailsContent(width, maxLines int) string {
	brief := strings.TrimSpace(m.inspectedNode.Brief)
	if brief == "" {
		brief = nodeLabelInSnapshot(m.inspectedNode, m.snapshot)
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
// learns it once (block anatomy documented with the design system in view.go):
// a muted rule per turn; the model's own words behind ✳, markdown-rendered; a
// tool call as kind glyph + tool name in the working accent followed by its
// command in primary ink; output dim behind a faint "│" gutter with failure as
// a rose ✗ on the status position only; and you in powder behind ›.
var (
	feedTurnRule   = regexp.MustCompile(`^── turn (\d+)\s+finish=(\S*)\s+in=(\d+) out=(\d+)\s*(?:\[([^\]]*)\])?\s*──$`)
	feedThought    = lipgloss.NewStyle().Foreground(powder)
	feedToolName   = lipgloss.NewStyle().Foreground(peach).Bold(true)
	feedResult     = lipgloss.NewStyle().Foreground(muted).Faint(true)
	feedGutter     = lipgloss.NewStyle().Foreground(muted).Faint(true)
	feedError      = lipgloss.NewStyle().Foreground(rose)
	feedYou        = lipgloss.NewStyle().Foreground(powder)
	feedThoughtCap = 6
	feedOutputCap  = 3
)

// feedBlock is one visual unit of the activity feed. When full is non-nil the
// block collapses to brief and a click (or x) trades between the two views.
type feedBlock struct {
	brief []string
	full  []string
}

func (b feedBlock) expandable() bool { return b.full != nil }

// feedRow maps one rendered feed line back to the block it belongs to, so a
// click anywhere on a block can toggle it.
type feedRow struct {
	line  int
	block int
}

// renderActivityFeed parses the worker's flight-recorder log into a readable
// timeline: what the model said to itself, what it ran, what came back, and
// any steering, followed by the node's thread messages. The raw log stays on
// disk; this is the human view of it. Collapsed blocks end in a muted ⋯ and
// open on click.
func (m *Model) renderActivityFeed(width int) string {
	blocks := parseFeedBlocks(m.nodeTraceText, m.nodeMessages, width)
	if artifacts := m.renderMediaArtifacts(store.Message{
		NodeID: m.nodeViewID, Body: strings.ReplaceAll(m.nodeTraceText, "⏎", " "),
	}, width); artifacts != "" {
		blocks = append(blocks, feedBlock{brief: strings.Split(artifacts, "\n")})
	}
	m.feedRows = m.feedRows[:0]
	m.feedBlocks = blocks
	m.feedKeys = feedBlockKeys(blocks)
	var out []string
	for index, block := range blocks {
		lines := block.brief
		if block.expandable() && m.feedExpanded[m.feedKeys[index]] {
			// The affordance flips with state: an opened block ends in the
			// collapse glyph, itself part of the block's click target.
			lines = append(append([]string(nil), block.full...), mutedStyle.Faint(true).Render("  ▾"))
		}
		for _, line := range lines {
			m.feedRows = append(m.feedRows, feedRow{line: len(out), block: index})
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return mutedStyle.Faint(true).Render("waiting for the worker's first turn…")
	}
	return strings.Join(out, "\n")
}

// feedBlockKeys derives a stable identity per block from its own content plus
// an occurrence counter for identical blocks. Identity survives the trace's
// head truncation and new blocks appending, which block indices do not.
func feedBlockKeys(blocks []feedBlock) []string {
	keys := make([]string, len(blocks))
	occurrences := make(map[uint64]int, len(blocks))
	for index, block := range blocks {
		lines := block.full
		if lines == nil {
			lines = block.brief
		}
		digest := fnv.New64a()
		for _, line := range lines {
			_, _ = digest.Write([]byte(line))
			_, _ = digest.Write([]byte{'\n'})
		}
		sum := digest.Sum64()
		keys[index] = fmt.Sprintf("%016x#%d", sum, occurrences[sum])
		occurrences[sum]++
	}
	return keys
}

func parseFeedBlocks(trace string, messages []store.Message, width int) []feedBlock {
	trace = strings.TrimRight(strings.ReplaceAll(trace, "\r\n", "\n"), "\n")
	var blocks []feedBlock
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
			blocks = append(blocks, feedBlock{brief: []string{"", mutedStyle.Faint(true).Render(truncate(rule, width))}})
		case strings.HasPrefix(line, "text: "):
			blocks = append(blocks, thoughtBlock(strings.TrimPrefix(line, "text: "), width))
		case strings.HasPrefix(line, "call "):
			blocks = append(blocks, toolCallBlock(strings.TrimPrefix(line, "call "), width))
		case strings.HasPrefix(line, "  → "):
			blocks = append(blocks, toolResultBlock(strings.TrimPrefix(line, "  → "), width))
		case strings.HasPrefix(line, "steered: "):
			blocks = append(blocks, feedBlock{brief: []string{feedYou.Render("› you  ") +
				inputTextStyle.Render(truncate(strings.TrimPrefix(line, "steered: "), max(1, width-7)))}})
		default:
			blocks = append(blocks, feedBlock{brief: []string{
				mutedStyle.Faint(true).Render(truncate(strings.ReplaceAll(line, "⏎", " "), width))}})
		}
	}
	if len(messages) > 0 {
		rule := "── thread "
		rule += strings.Repeat("─", max(0, width-lipgloss.Width(rule)-1))
		blocks = append(blocks, feedBlock{brief: []string{"", mutedStyle.Faint(true).Render(rule)}})
		now := time.Now()
		for _, message := range messages {
			header := speakerHeader(message, now)
			body := strings.Split(renderMarkdown(message.Body, width), "\n")
			blocks = append(blocks, feedBlock{brief: append([]string{header}, body...)})
		}
	}
	return blocks
}

// thoughtBlock renders the model's own words with markdown, capped when long;
// the full version is a click away. A leading blank line keeps thoughts
// visually apart from the tool blocks around them.
func thoughtBlock(raw string, width int) feedBlock {
	thought := strings.ReplaceAll(raw, "⏎", "\n")
	rendered := strings.Split(renderMarkdown(thought, max(1, width-2)), "\n")
	full := make([]string, 0, len(rendered)+1)
	full = append(full, "")
	for index, line := range rendered {
		prefix := "  "
		if index == 0 {
			prefix = feedThought.Render("✳ ")
		}
		full = append(full, prefix+line)
	}
	if len(full) <= feedThoughtCap+1 {
		return feedBlock{brief: full}
	}
	brief := append(append([]string{}, full[:feedThoughtCap+1]...), mutedStyle.Faint(true).Render("  ⋯"))
	return feedBlock{brief: brief, full: full}
}

// toolCallBlock turns `sh {"cmd":"ls"}` into a distinct call line — the kind
// glyph and tool name carry the working accent, the salient argument reads in
// primary ink, and the JSON plumbing disappears. A leading blank line lets
// each call breathe. Multi-line commands collapse to their first line with a
// ⋯; the full command opens on click. Extraction tolerates truncated JSON.
func toolCallBlock(rest string, width int) feedBlock {
	name, args, _ := strings.Cut(rest, " ")
	glyph, detail := "⚙", ""
	salient := map[string]string{"sh": "cmd", "write": "path", "edit": "path", "web": "q", "generate_image": "prompt", "generate_music": "prompt", "generate_video": "prompt", "speak": "text", "view_image": "path"}[name]
	if salient != "" {
		switch name {
		case "sh":
			glyph = "$"
		case "write", "edit":
			glyph = "✎"
		case "web":
			glyph = "⌕"
		case "generate_image", "view_image":
			glyph = "⌾"
		case "generate_music", "speak":
			glyph = "♪"
		case "generate_video":
			glyph = "▶"
		}
		if value, ok := extractStringField(args, salient); ok {
			detail = value
		} else if name == "web" {
			detail = "fetch pages"
		}
	}
	if detail == "" {
		detail = strings.ReplaceAll(args, "⏎", " ")
	}
	detail = strings.TrimSpace(detail)
	head := feedToolName.Render(glyph+" "+name) + "  "
	headWidth := lipgloss.Width(glyph+" "+name) + 2
	lines := strings.Split(detail, "\n")
	room := max(1, width-headWidth-2)
	first := head + renderToolDetail(name, lines[0], room)
	if len(lines) == 1 && lipgloss.Width(lines[0]) <= room {
		return feedBlock{brief: []string{"", first}}
	}
	full := []string{"", head + renderToolDetail(name, lines[0], room)}
	for _, line := range lines[1:] {
		full = append(full, "  "+inputTextStyle.Render(truncate(strings.TrimRight(line, " "), max(1, width-2))))
	}
	return feedBlock{brief: []string{"", first + mutedStyle.Faint(true).Render(" ⋯")}, full: full}
}

func renderToolDetail(name, detail string, width int) string {
	if (name == "write" || name == "edit" || name == "view_image") && strings.HasPrefix(detail, "/") {
		return pathLink(detail, width)
	}
	return inputTextStyle.Render(truncate(detail, width))
}

// toolResultBlock renders what came back under the call it answers: dim mono
// behind a faint "│" gutter, collapsed to a few lines with the rest a click
// away. Failure is a rose ✗ on the status position only — the output itself
// never turns red.
func toolResultBlock(rest string, width int) feedBlock {
	size, content, _ := strings.Cut(rest, ": ")
	failed := strings.HasSuffix(size, " ERROR")
	size = strings.TrimSuffix(strings.TrimSuffix(size, " ERROR"), "B")
	if bytes, err := strconv.Atoi(size); err == nil {
		size = humanBytes(bytes)
	}
	gutter := feedGutter.Render("  │ ")
	status := ""
	if failed {
		status = feedError.Render("✗ ")
	}
	wrapped := strings.Split(wrapText(strings.ReplaceAll(content, "⏎", "\n"), max(1, width-10)), "\n")
	render := func(count int) []string {
		out := make([]string, 0, count)
		for index, line := range wrapped[:count] {
			if index == 0 {
				out = append(out, truncate(gutter+status+feedResult.Render(size+"  "+line), width))
			} else {
				out = append(out, truncate(gutter+feedResult.Render(line), width))
			}
		}
		return out
	}
	if len(wrapped) <= feedOutputCap {
		return feedBlock{brief: render(len(wrapped))}
	}
	brief := append(render(feedOutputCap), gutter+mutedStyle.Faint(true).Render("⋯"))
	return feedBlock{brief: brief, full: render(len(wrapped))}
}

// extractStringField pulls one string value out of raw JSON text by scanning,
// tolerating the truncation the trace applies: a value whose closing quote
// never arrives still yields everything up to the cut. Unescapes the common
// sequences so commands read as typed.
func extractStringField(raw, key string) (string, bool) {
	marker := `"` + key + `"`
	at := strings.Index(raw, marker)
	if at < 0 {
		return "", false
	}
	rest := raw[at+len(marker):]
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, ":") {
		return "", false
	}
	rest = strings.TrimLeft(rest[1:], " \t")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	rest = rest[1:]
	var value strings.Builder
	escaped := false
	for _, r := range rest {
		if escaped {
			switch r {
			case 'n':
				value.WriteByte('\n')
			case 't':
				value.WriteByte('\t')
			case '"', '\\', '/':
				value.WriteRune(r)
			default:
				value.WriteByte('\\')
				value.WriteRune(r)
			}
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			break
		}
		value.WriteRune(r)
	}
	return value.String(), true
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

func (m *Model) updateMouseClick(x, y int) (tea.Cmd, bool) {
	if m.boostBounds.contains(x, y) {
		m.toggleBoost()
		return nil, true
	}
	if m.headerQuestionBounds.contains(x, y) {
		m.focusPendingQuestion()
		return nil, true
	}
	if m.headerModelsBounds.contains(x, y) {
		return m.openModelsPalette(), true
	}
	if m.headerTasksBounds.contains(x, y) {
		if m.nodeViewID != "" {
			m.closeNodeView()
		}
		m.toggleGraph()
		return nil, true
	}
	if m.activityBarBounds.contains(x, y) {
		return m.clickActivityDock(x-m.activityBarBounds.x, y-m.activityBarBounds.y)
	}
	if m.palette == paletteModels {
		if m.paletteCloseBounds.contains(x, y) {
			m.closePalette()
			m.focus = focusHeader
			m.headerFocusIndex = 0
			return nil, true
		}
		for _, row := range m.modelSlotRows {
			if row.bounds.contains(x, y) {
				m.modelSlotIndex = row.index
				return m.openModelPicker(modelSlots[row.index]), true
			}
		}
		if m.modelPickerBounds.contains(x, y) {
			m.focus = focusHeader
			m.inputFocused = false
			m.input.Blur()
			return nil, true
		}
	}
	if m.palette == paletteModel {
		if m.paletteCloseBounds.contains(x, y) {
			m.returnToModelsPalette()
			return nil, true
		}
		for _, row := range m.modelPickerRows {
			if !row.bounds.contains(x, y) {
				continue
			}
			choices := m.filteredModelChoices()
			if row.index >= 0 && row.index < len(choices) {
				m.paletteSelected = row.index
				return m.applySelectedModel(choices), true
			}
		}
		if m.modelPickerBounds.contains(x, y) {
			m.focus = focusInput
			m.inputFocused = true
			_ = m.input.Focus()
			return nil, true
		}
	}
	if m.paletteCloseBounds.contains(x, y) {
		m.closePalette()
		return nil, true
	}
	if m.voiceCancelBounds.contains(x, y) {
		return m.cancelVoice(), true
	}
	if m.micBounds.contains(x, y) {
		return m.toggleVoice(), true
	}
	for index, bounds := range m.attachmentBounds {
		if bounds.contains(x, y) {
			m.removeAttachment(index)
			return nil, true
		}
	}
	if m.inputBounds.contains(x, y) {
		if m.textQuestionDismissBounds.contains(x, y) {
			m.dismissTextQuestion()
			return nil, true
		}
		m.focus = focusInput
		m.inputFocused = true
		_ = m.input.Focus()
		return nil, true
	}
	if m.nodeViewID != "" {
		if m.nodeBackBounds.contains(x, y) {
			m.closeNodeView()
			return nil, true
		}
		if m.toggleFeedBlockAt(x, y) {
			return nil, true
		}
		if !m.nodeBounds.contains(x, y) {
			return nil, false
		}
		m.inputFocused = false
		m.input.Blur()
		return nil, true
	}
	if m.graphToggleHit(x, y) {
		if !m.closeScopedGraph() && !m.closeCharterCard() {
			m.toggleGraph()
		}
		return nil, true
	}
	if m.graphBounds.contains(x, y) {
		m.focus = focusGraph
		m.inputFocused = false
		m.input.Blur()
		if m.standingRowsBounds.contains(x, y) {
			line := y - m.graphBounds.y
			for _, row := range m.standingRows {
				if row.line == line {
					m.selectedNodeID = standingGraphRowID(row.charterID)
					m.openStandingCharter(row.charterID)
					return nil, true
				}
			}
		}
		if m.graphRowsBounds.contains(x, y) {
			line := y - m.graphRowsBounds.y + m.graph.YOffset
			if m.charterCardID != "" && m.graphScopeID == "" {
				if command, ok := m.activateCharterLine(line); ok {
					return command, true
				}
				return nil, true
			}
			nodeID := m.graphNodeAtLine(line)
			if nodeID != "" {
				if nodeID == historyGraphRowID {
					m.selectedNodeID = nodeID
					m.toggleHistory()
					return nil, true
				}
				alreadySelected := nodeID == m.selectedNodeID
				m.selectedNodeID = nodeID
				m.refreshGraph()
				if alreadySelected {
					return m.openSelectedNode(), true
				}
			}
		}
		return nil, true
	}
	if m.chatBounds.contains(x, y) {
		if m.toggleChatMessageAt(x, y) {
			return nil, true
		}
		m.focus = focusChat
		m.inputFocused = false
		m.input.Blur()
		return nil, true
	}
	return nil, false
}

func (m *Model) clickCardDock(x, line int) (tea.Cmd, bool) {
	active := dockJobCards(m.cards, m.standingTime())
	if m.dockSummaryLine >= 0 && line == m.dockSummaryLine {
		if m.dockOverflowOpen() {
			m.dockExpanded = false
			if m.focus == focusCards {
				m.focus = focusInput
				m.inputFocused = true
				_ = m.input.Focus()
			}
			m.setSize(m.width, m.height)
		} else {
			m.dockExpanded = true
			m.focusCardDock()
		}
		return nil, true
	}
	if len(active) > dockOverflowLimit && !m.dockOverflowOpen() {
		for _, row := range m.cardDockRows {
			if row.dock && line >= row.start && line <= row.end {
				m.selectedCardID = row.cardID
				m.dockExpanded = true
				m.focusCardDock()
				return nil, true
			}
		}
	}
	if len(active) > dockOverflowLimit && !m.dockOverflowOpen() {
		m.dockExpanded = true
		m.focusCardDock()
		return nil, true
	}
	for _, option := range m.cardOptionRows {
		if option.dock && option.line == line && x >= option.startX && x < option.endX {
			m.questionSelection[option.cardID] = option.optionIndex
			return m.submitQuestionOption(option.cardID, option.optionIndex), true
		}
	}
	for _, row := range m.cardCloseRows {
		if row.dock && row.line == line {
			m.selectedCardID = row.cardID
			m.cardExpanded[row.cardID] = false
			m.setSize(m.width, m.height)
			return nil, true
		}
	}
	for _, part := range m.cardPartRows {
		if part.dock && part.line == line {
			m.selectedCardID = part.cardID
			return m.openNodeByID(part.nodeID), true
		}
	}
	for _, row := range m.cardDockRows {
		if line < row.start || line > row.end {
			continue
		}
		return m.advanceCard(row.cardID, focusCards), true
	}
	if len(active) > 0 {
		m.focusCardDock()
		return nil, true
	}
	m.toggleGraph()
	return nil, true
}

// toggleChatMessageAt handles a click inside the chat column: a provenance
// chip jumps to its task, a collapsed answer opens or closes in place, and
// anything else is a harmless no-op.
func (m *Model) toggleChatMessageAt(x, y int) bool {
	line := y - m.chatBounds.y + m.chat.YOffset
	if line < 0 {
		return false
	}
	contentX := x - m.chatBounds.x
	for _, option := range m.notebookOptionRows {
		if option.line != line || contentX < option.startX || contentX >= option.endX {
			continue
		}
		m.notebookOption = option.optionIndex
		m.activateSelectedNotebookOption()
		m.refreshChat()
		return true
	}
	return m.activateChatLine(line)
}

// activateChatLine is the one activation path for a thread content line —
// clicks and keyboard traversal both land here, so enter always equals click.
func (m *Model) activateChatLine(line int) bool {
	for _, option := range m.notebookOptionRows {
		if option.line == line {
			m.activateSelectedNotebookOption()
			m.refreshChat()
			return true
		}
	}
	for _, row := range m.cardCloseRows {
		if !row.dock && row.line == line {
			m.selectedCardID = row.cardID
			m.cardExpanded[row.cardID] = false
			m.setSize(m.width, m.height)
			return true
		}
	}
	for _, chip := range m.chatChipRows {
		if line == chip.line {
			_ = m.openNodeByID(chip.nodeID)
			return true
		}
	}
	for _, part := range m.cardPartRows {
		if !part.dock && part.line == line {
			m.selectedCardID = part.cardID
			_ = m.openNodeByID(part.nodeID)
			return true
		}
	}
	for _, row := range m.chatExpandRows {
		if row.line != line {
			continue
		}
		switch row.action {
		case chatExpandReceipts:
			m.receiptsExpanded = !m.receiptsExpanded
		case chatExpandMessage:
			m.expandedMessages[row.seq] = !m.expandedMessages[row.seq]
		case chatExpandBrief:
			m.selectedCardID = ""
			m.selectedBriefSeq = row.seq
			m.briefExpanded[row.seq] = !m.briefExpanded[row.seq]
		case chatExpandLearning:
			m.learningExpanded[row.seq] = !m.learningExpanded[row.seq]
		case chatExpandNotebookFact:
			m.expandNotebookFact(row.seq)
		case chatNotebookClose:
			m.closeNotebook()
		}
		offset := m.chat.YOffset
		m.refreshChat()
		m.chat.SetYOffset(offset)
		return true
	}
	for _, row := range m.chatMessageRows {
		if line < row.start || line > row.end {
			continue
		}
		m.expandedMessages[row.seq] = !m.expandedMessages[row.seq]
		offset := m.chat.YOffset
		m.refreshChat()
		m.chat.SetYOffset(offset)
		return true
	}
	for _, row := range m.chatCardRows {
		if line < row.start || line > row.end {
			continue
		}
		_ = m.advanceCard(row.cardID, focusChat)
		return true
	}
	return false
}
