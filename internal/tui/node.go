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
	"github.com/charmbracelet/x/ansi"
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
	ids := make([]string, 0, len(m.graphRows)+len(m.standingRows)+len(m.serviceRows))
	if m.graphScopeID == "" && m.charterCardID == "" && m.serviceCardID == "" {
		for _, service := range m.activeServices() {
			ids = append(ids, serviceGraphRowID(service.ID))
		}
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
	if m.serviceCardID != "" {
		if len(m.serviceCardRows) == 0 {
			m.refreshGraph()
		}
		return
	}
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
	if _, ok := serviceIDFromGraphRow(m.selectedNodeID); ok {
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
	if serviceID, ok := serviceIDFromGraphRow(m.selectedNodeID); ok {
		m.openServiceCard(serviceID)
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
	// The chips belong to the line they were attached to, and that line stays
	// in the thread: carried into a steer they are dropped on the way back out,
	// and they were never the steer's attachments to begin with.
	m.chatAttachments = append([]string(nil), m.attachments...)
	m.attachments = nil
	// A recall walk belongs to the composer that started it. Left standing, it
	// captured the thread's arrows for the rest of the session after a visit
	// here, because the steer line never types the walk to an end.
	m.inputRecall = 0
	m.nodeViewID = node.ID
	m.inspectedNode = node
	m.nodeMessages = nil
	m.nodeLastSeq = 0
	m.nodeTraceText = ""
	m.nodeTraceStamp = NodeTraceStamp{}
	m.feedExpanded = map[string]bool{}
	m.forgetFeedTrace()
	m.nodeTrace.GotoBottom()
	m.palette = paletteNone
	m.input.Reset()
	m.input.Placeholder = "steer this worker — lands before its next turn"
	m.focus = focusInput
	m.inputFocused = true
	_ = m.input.Focus()
	m.setSize(m.width, m.height)
	m.refreshNodeView(true)
	// Where the document opens is the answer to why it was opened. A running
	// worker is opened to be watched, so it opens at the live end; a settled one
	// is opened to be read, and reading starts at the brief. The pin holds the
	// settled reader in place while the first poll fills the feed in underneath
	// them — one scroll of their own releases it.
	if terminalStatus(node.Status) {
		m.nodePinTop = true
		m.nodeTrace.GotoTop()
	}
	return m.poll()
}

func (m *Model) closeNodeView() {
	m.nodeViewID = ""
	m.inspectedNode = store.Node{}
	m.nodeMessages = nil
	m.nodeLastSeq = 0
	m.nodeTraceText = ""
	m.nodeTraceStamp = NodeTraceStamp{}
	m.nodePinTop = false
	m.input.Reset()
	m.input.Placeholder = composerPlaceholder
	m.input.SetValue(m.chatDraft)
	m.inputRecall = 0
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

// nodeIndex is a snapshot read by id. A snapshot is a value the poll swaps
// whole rather than edits in place, so the slice it is holding — its length and
// where its first element lives — is the whole identity the index needs: hand
// it a different slice and it rebuilds, hand it the same one and it answers
// from the map.
type nodeIndex struct {
	nodes []store.Node
	byID  map[string]store.Node
}

func (x *nodeIndex) lookup(nodes []store.Node, nodeID string) (store.Node, bool) {
	if x.byID == nil || len(x.nodes) != len(nodes) ||
		(len(nodes) > 0 && &x.nodes[0] != &nodes[0]) {
		x.nodes = nodes
		x.byID = make(map[string]store.Node, len(nodes))
		// First writer wins, which is what a walk from the front returned.
		for _, node := range nodes {
			if _, seen := x.byID[node.ID]; !seen {
				x.byID[node.ID] = node
			}
		}
	}
	node, ok := x.byID[nodeID]
	return node, ok
}

// jobRootIndex is the same projection for "which job does this node belong
// to". Resolving it walks the parents of every node in the graph and builds
// two maps to do it, and the thread asks for it once per provenance chip on
// screen — so the answer is kept for as long as the snapshot it came from is.
type jobRootIndex struct {
	nodes []store.Node
	roots map[string]store.Node
}

func (x *jobRootIndex) lookup(nodes []store.Node, nodeID string) (store.Node, bool) {
	if x.roots == nil || len(x.nodes) != len(nodes) ||
		(len(nodes) > 0 && &x.nodes[0] != &nodes[0]) {
		x.nodes = nodes
		x.roots = nodeJobRoots(nodes)
	}
	root, ok := x.roots[nodeID]
	return root, ok
}

// nodeLabelIn is nodeLabelInSnapshot against the snapshot this window is
// showing, answered from the kept projection rather than by walking it again.
func (m *Model) nodeLabelIn(node store.Node, nodes []store.Node) string {
	if root, ok := m.jobRoots.lookup(nodes, node.ID); ok {
		return nodeLabel(node, root)
	}
	return nodeLabel(node)
}

func (m *Model) snapshotNode(nodeID string) (store.Node, bool) {
	if node, ok := m.snapshotIndex.lookup(m.snapshot.Nodes, nodeID); ok {
		return node, true
	}
	if m.graphScopeID == "" {
		return store.Node{}, false
	}
	return m.cardSnapshotIndex.lookup(m.cardSnapshot.Nodes, nodeID)
}

func (m *Model) submitSteer() tea.Cmd {
	body := strings.TrimSpace(m.input.Value())
	if body == "" || m.nodeViewID == "" {
		return nil
	}
	if strings.HasPrefix(body, "/") {
		return m.executeSlash(body)
	}
	// A steer is a sent turn like any other, so the arrows reach it: the ring is
	// the composer's undo, and the steer line is the composer.
	m.rememberSubmission(body)
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

// landOptimisticNodeMessage swaps the echoed steer for the journaled one. The
// swap is the machine catching up with what the person already sent, not an act
// of theirs — so it follows the feed only for a reader who was already at the
// bottom, and leaves a reader who scrolled up where they are.
func (m *Model) landOptimisticNodeMessage(nodeID string, posted store.Message) {
	if nodeID != m.nodeViewID {
		return
	}
	for index := range m.nodeMessages {
		message := &m.nodeMessages[index]
		if message.Seq == 0 && message.Role == posted.Role && message.Body == posted.Body {
			*message = posted
			m.refreshNodeView(false)
			return
		}
	}
	m.nodeMessages = append(m.nodeMessages, posted)
	m.refreshNodeView(false)
}

// appendNodeMessages reports whether anything the document draws actually
// arrived: a poll that carried no new turn has nothing to re-render for.
func (m *Model) appendNodeMessages(messages []store.Message) bool {
	changed := false
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
				changed = true
				break
			}
		}
		if !duplicate {
			m.nodeMessages = append(m.nodeMessages, incoming)
			changed = true
		}
		m.nodeLastSeq = max(m.nodeLastSeq, incoming.Seq)
	}
	return changed
}

// toggleNodeSteerFocus hands the keyboard between the steer line and the feed.
// The letters can only belong to one of them at a time: with the field focused
// every key is a character, so a steer that opens with "check…" survives, and
// the single-key actions wait until the field gives the keyboard back. The
// blurred caret and the footer both say which half is listening.
func (m *Model) toggleNodeSteerFocus() {
	m.inputFocused = !m.inputFocused
	if m.inputFocused {
		_ = m.input.Focus()
		return
	}
	m.input.Blur()
}

func (m *Model) cancelInspectedNode() tea.Cmd {
	// Work that has already stopped has nothing to stop. The store refuses the
	// command anyway; refusing it here keeps the key silent rather than making
	// the reader of a finished task read an error they could not have avoided.
	if terminalStatus(m.inspectedNode.Status) {
		return nil
	}
	if m.commander == nil {
		return m.showStatus("cancelling isn't available in this window")
	}
	if asked, command := m.gateNodeSurgery(store.CommandCancel); asked {
		return command
	}
	if err := m.commander.Cancel(m.nodeViewID); err != nil {
		return m.showStatus(fmt.Sprintf("could not cancel %s: %v", m.nodeViewID, err))
	}
	return m.showStatus("cancel requested → " + m.nodeViewID)
}

// restartInspectedNode is the verb that matches the state the reader is looking
// at. A node that failed or was cancelled has no resume — the work is over, and
// the only forward move is to run it again against the digest of the attempt
// that stopped. Offered nowhere else, because nowhere else is it true.
func (m *Model) restartInspectedNode() tea.Cmd {
	if !nodeRestartable(m.inspectedNode.Status) {
		return nil
	}
	restarter, ok := m.commander.(Restarter)
	if !ok || restarter == nil {
		return m.showStatus("restarting isn't available in this window")
	}
	if asked, command := m.gateNodeSurgery(store.CommandRestart); asked {
		return command
	}
	if err := restarter.Restart(m.nodeViewID); err != nil {
		return m.showStatus(fmt.Sprintf("could not restart %s: %v", m.nodeViewID, err))
	}
	return m.showStatus("restart requested → " + m.nodeViewID)
}

// gateNodeSurgery puts the conversational confirm gates in front of the key
// path. When the loss is large enough that a sentence would have been asked to
// confirm it, the key press asks the same durable question and journals
// nothing: the answer replays the command. The status line only says where to
// look, because the question itself is the reply.
func (m *Model) gateNodeSurgery(kind store.CommandKind) (bool, tea.Cmd) {
	gate, ok := m.commander.(SurgeryGate)
	if !ok || gate == nil {
		return false, nil
	}
	asked, err := gate.ConfirmSurgery(kind, m.nodeViewID)
	if err != nil || !asked {
		return false, nil
	}
	return true, m.showStatus("asked first — answer in the thread")
}

// nodeRestartable is the store's own restart law, read here so the surface
// never offers a door the journal would refuse.
func nodeRestartable(status store.Status) bool {
	return status == store.Failed || status == store.Cancelled
}

// sizeNodeViewports gives the node view one sticky line and one scrolling
// document. Everything a person came to read — the brief, what was decided,
// what came of it, and every turn — lives in the same scroll, so the reader
// never has to argue with a header that will not move. The title is the only
// chrome that stays, because it is the only line that answers "where am I".
func (m *Model) sizeNodeViewports() {
	innerWidth := max(1, m.width-2)
	innerHeight := max(1, m.chatHeight)
	// title + hairline
	m.nodeTraceHeight = max(3, innerHeight-2)
	m.nodeTrace.Width, m.nodeTrace.Height = innerWidth, m.nodeTraceHeight
}

// refreshNodeView re-renders the document without stealing the scrollback: it
// follows new output only when the reader was already at the bottom (or just
// acted), never yanking someone who scrolled up to read history. A reader who
// opened a settled worker to read it stays at its first line until they move.
func (m *Model) refreshNodeView(force bool) {
	if force {
		m.nodePinTop = false
	}
	follow := force || m.nodeTrace.AtBottom()
	anchor := m.feedAnchorAt()
	m.nodeTrace.SetContent(m.renderActivityFeed(max(1, m.nodeTrace.Width)))
	switch {
	case m.nodePinTop:
		m.nodeTrace.GotoTop()
	case follow:
		m.nodeTrace.GotoBottom()
	default:
		m.restoreFeedAnchor(anchor)
	}
}

// feedAnchor is where the reader is, said in the document's own terms: the
// block under the top visible line, and how far into that block they are. A
// line number is only true of the document that produced it — the trace window
// slides off its own head every time the log passes its byte budget, a block
// opens, a resize rewraps the lot — and restoring one across any of those is
// the yank a reader feels as being thrown around every poll. The block's
// identity survives all three, because it is derived from the block's content.
type feedAnchor struct {
	key    string
	within int
	offset int
}

func (m *Model) feedAnchorAt() feedAnchor {
	anchor := feedAnchor{offset: m.nodeTrace.YOffset}
	start := 0
	for index, row := range m.feedRows {
		if index == 0 || row.block != m.feedRows[index-1].block {
			start = row.line
		}
		if row.line != anchor.offset {
			continue
		}
		if row.block < len(m.feedKeys) {
			anchor.key, anchor.within = m.feedKeys[row.block], row.line-start
		}
		break
	}
	return anchor
}

// restoreFeedAnchor puts the reader back on the same words. A block that is
// gone — scrolled off the window's head, or never keyed, as the document's own
// descriptive head is not — falls back to the line number, which is the best
// answer left.
func (m *Model) restoreFeedAnchor(anchor feedAnchor) {
	if anchor.key != "" {
		for block, key := range m.feedKeys {
			if key != anchor.key {
				continue
			}
			for _, row := range m.feedRows {
				if row.block == block {
					m.nodeTrace.SetYOffset(row.line + anchor.within)
					return
				}
			}
			break
		}
	}
	m.nodeTrace.SetYOffset(anchor.offset)
}

// nodeDocumentMoved reports whether the parts of a node the scrolling document
// draws — its own words, how it ended, what it chose — actually changed. The
// clock in the sticky header is drawn outside the document and ticks on its
// own, so it is no reason to rebuild one.
func nodeDocumentMoved(before, after store.Node) bool {
	return before.ID != after.ID || before.Status != after.Status ||
		before.Brief != after.Brief || before.Summary != after.Summary ||
		before.Error != after.Error ||
		before.Provenance.WorkModel != after.Provenance.WorkModel ||
		before.Provenance.PlanModel != after.Provenance.PlanModel ||
		settledWorker(before) != settledWorker(after)
}

// releaseNodeTopPin hands the document back to the reader: any scroll they make
// themselves is a decision, and the pin never argues with one.
func (m *Model) releaseNodeTopPin() { m.nodePinTop = false }

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
		anchor := m.feedAnchorAt()
		m.nodeTrace.SetContent(m.renderActivityFeed(max(1, m.nodeTrace.Width)))
		m.restoreFeedAnchor(anchor)
		return true
	}
	return false
}

// renderNodeDetailsContent is the job's own description — what was asked, what
// was decided, and what came of it — nothing more. It is no longer a
// fixed-height précis: it opens the scrolling document, so it says the whole
// thing and lets the scroll do the hiding.
func (m *Model) renderNodeDetailsContent(width int) string {
	brief := strings.TrimSpace(m.inspectedNode.Brief)
	if brief == "" {
		brief = m.nodeLabelIn(m.inspectedNode, m.snapshot.Nodes)
	}
	content := inputTextStyle.Render(wrapText(brief, width))
	// One rung down from the card, the same receipt in full: this is where a
	// person comes to check what actually ran their work, so the model id keeps
	// its vendor path and nothing is shortened. The title line carries the short
	// spelling for the glance; this is the copy you can check a build against.
	// Silent, as ever, on a job that chose nothing.
	if receipt := nodeChoiceReceipt(m.inspectedNode); receipt != "" {
		content += "\n" + mutedStyle.Render(truncate(receipt, width))
	}
	if terminalStatus(m.inspectedNode.Status) {
		switch {
		case m.inspectedNode.Status == store.Cancelled:
			// A cancellation is not a failure and must not wear failure's ink. The
			// user did this on purpose, so the reason reads as the quiet fact it
			// is — and whatever the worker had written before it stopped is the
			// most useful thing on the page, said under a label that admits what
			// it is rather than pretending to be a summary.
			reason := strings.TrimSpace(m.inspectedNode.Error)
			if reason == "" {
				reason = "stopped"
			}
			content += "\n" + mutedStyle.Render(wrapText(reason, width))
			if partial := strings.TrimSpace(m.inspectedNode.Summary); partial != "" {
				content += "\n\n" + mutedStyle.Faint(true).Render("LEFT OFF") +
					"\n" + inputTextStyle.Render(wrapText(partial, width))
			}
		case m.inspectedNode.Status == store.Failed:
			failure := strings.TrimSpace(m.inspectedNode.Error)
			if failure == "" {
				failure = string(m.inspectedNode.Status)
			}
			content += "\n" + roseStyle.Render(wrapText(failure, width))
		default:
			if summary := strings.TrimSpace(m.inspectedNode.Summary); summary != "" {
				content += "\n" + inputTextStyle.Render(wrapText(summary, width))
			}
		}
	}
	return content
}

// nodeDocumentHead is the descriptive half of the scrolling document: a quiet
// BRIEF label over the job's own words, then one seam. The seam is the feed's
// own rule idiom — the same `──` that separates turn from turn — because the
// grammar the eye already learned three lines down is the cheapest way to say
// "the description ends here and the work begins".
func (m *Model) nodeDocumentHead(width int) []string {
	lines := []string{mutedStyle.Faint(true).Render("BRIEF")}
	lines = append(lines, strings.Split(m.renderNodeDetailsContent(width), "\n")...)
	lines = append(lines, "", mutedStyle.Faint(true).Render(truncate(feedRule("execution", width), width)))
	lines = append(lines, mutedStyle.Faint(true).Render(truncate(activityLegend, width)))
	return lines
}

// activityLegend teaches the feed's five voices once, in the scroll rather than
// pinned above it: a legend is read on the first visit and never again.
const activityLegend = "✳ model · $ shell · ✎ file · ⌕ web · › you · ⋯ expands"

// feedRule is the one divider this surface draws: a named seam that runs to the
// right edge. Turn rules, the thread rule, and the execution seam are all the
// same line so none of them reads as a new kind of thing.
func feedRule(label string, width int) string {
	rule := "── " + label + " "
	return rule + strings.Repeat("─", max(0, width-lipgloss.Width(rule)-1))
}

// The activity feed's visual grammar, kept to five distinct voices so the eye
// learns it once (block anatomy documented with the design system in view.go):
// a muted rule per turn; the model's own words behind ✳, markdown-rendered; a
// tool call as kind glyph + tool name in the working accent followed by its
// command in primary ink; output dim behind a faint "│" gutter with failure as
// a rose ✗ on the status position only; and you in powder behind ›.
var (
	feedTurnRule   = regexp.MustCompile(`^── turn (\d+)\s+finish=(\S*)\s+in=(\d+) out=(\d+)\s*(?:\[([^\]]*)\])?\s*──$`)
	feedThought    = powderStyle
	feedToolName   = peachStyle.Bold(true)
	feedResult     = lipgloss.NewStyle().Foreground(muted).Faint(true)
	feedGutter     = lipgloss.NewStyle().Foreground(muted).Faint(true)
	feedError      = roseStyle
	feedYou        = powderStyle
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

// renderActivityFeed is the whole scrolling document, not just the turns: the
// descriptive head first, the execution seam, then the worker's flight-recorder
// log parsed into a readable timeline — what the model said to itself, what it
// ran, what came back, and any steering — followed by the node's thread
// messages. The raw log stays on disk; this is the human view of it. Collapsed
// blocks end in a muted ⋯ and open on click.
func (m *Model) renderActivityFeed(width int) string {
	settled, keys, media := m.settledTrace(width)
	blocks := append([]feedBlock(nil), settled...)
	// The tail is whatever the worker has written since the last complete
	// line, plus the thread — neither is settled, so neither is kept.
	tail := m.feedTail
	blocks = appendTraceBlocks(blocks, tail, width)
	blocks = appendMessageBlocks(blocks, m.nodeMessages, width)
	references := append(append([]string(nil), media...), traceMediaReferences(tail)...)
	if artifacts := m.renderMediaPaths(m.nodeViewID, references, width); artifacts != "" {
		blocks = append(blocks, feedBlock{brief: strings.Split(artifacts, "\n")})
	}
	m.feedRows = m.feedRows[:0]
	m.feedBlocks = blocks
	m.feedKeys = appendFeedBlockKeys(keys, m.feedOccurrences, blocks[len(settled):])
	releaseFeedBlockKeys(m.feedOccurrences, blocks[len(settled):])
	// The head opens the document and claims no click target of its own, so the
	// rows that follow keep counting from where it ends — a click still lands on
	// the block under the pointer.
	out := m.nodeDocumentHead(width)
	head := len(out)
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
	if len(out) == head {
		out = append(out, mutedStyle.Faint(true).Render("waiting for the worker's first turn…"))
	}
	return strings.Join(out, "\n")
}

// settledTrace is everything in the worker's log up to its last complete line,
// parsed once. The log is a file the executor only appends to, and the pane
// tails it every cycle the file grew — so re-matching every line of sixty-four
// kilobytes against the turn rule, and re-hashing every one of them for its
// identity, was work that had already been done for all but the last few
// hundred bytes of it.
//
// The kept prefix has to still be a prefix: the trace is clipped to its last
// sixty-four kilobytes, and when the head falls off, everything is read again.
func (m *Model) settledTrace(width int) ([]feedBlock, []string, []string) {
	trace := strings.ReplaceAll(m.nodeTraceText, "\r\n", "\n")
	if m.feedTraceWidth != width || !strings.HasPrefix(trace, m.feedTraceParsed) {
		m.forgetFeedTrace()
		m.feedTraceWidth = width
	}
	grown := trace[len(m.feedTraceParsed):]
	// Only whole lines settle. A line the worker is still writing is parsed
	// fresh every cycle until the newline that ends it arrives.
	complete := strings.LastIndexByte(grown, '\n') + 1
	if complete > 0 {
		chunk := grown[:complete]
		before := len(m.feedBlocksKept)
		m.feedBlocksKept = appendTraceBlocks(m.feedBlocksKept, chunk, width)
		m.feedKeysKept = appendFeedBlockKeys(m.feedKeysKept, m.feedOccurrences, m.feedBlocksKept[before:])
		m.feedMediaKept = append(m.feedMediaKept, traceMediaReferences(chunk)...)
		m.feedTraceParsed = trace[:len(m.feedTraceParsed)+complete]
	}
	m.feedTail = grown[complete:]
	return m.feedBlocksKept, m.feedKeysKept, m.feedMediaKept
}

// forgetFeedTrace drops the parsed prefix. Opening another node is the obvious
// caller; the other is a log whose head has fallen off its byte budget, which
// is no longer the log the prefix was read from.
func (m *Model) forgetFeedTrace() {
	m.feedTraceParsed = ""
	m.feedTraceWidth = 0
	m.feedTail = ""
	m.feedBlocksKept = nil
	m.feedKeysKept = nil
	m.feedMediaKept = nil
	m.feedOccurrences = make(map[uint64]int, 256)
}

func traceMediaReferences(chunk string) []string {
	if chunk == "" {
		return nil
	}
	return mediaReferences(strings.ReplaceAll(sanitizeTraceBytes(chunk), "⏎", " "))
}

// sanitizeTraceBytes is what stands between a subprocess's stdout and our
// frame. What the executor recorded is a copy of somebody else's terminal
// session — SGR colors, carriage returns, erase and cursor-motion sequences, an
// OSC that renames the window — and none of it is ours to replay: an escape run
// that survives into a row makes the row measure one width and print another,
// which is the soft wrap that scrolls the alt-screen and leaves a ghost header
// standing above. Newlines are the one control the feed keeps, because the feed
// reads a line at a time; a tab becomes the run of spaces the panes expand it
// to anyway.
func sanitizeTraceBytes(chunk string) string {
	if strings.IndexFunc(chunk, traceControl) < 0 {
		return chunk
	}
	var out strings.Builder
	out.Grow(len(chunk))
	for _, r := range ansi.Strip(chunk) {
		switch {
		case r == '\n':
			out.WriteByte('\n')
		case r == '\t':
			out.WriteString(styleTabStop)
		case traceControl(r):
			// Dropped: nothing a worker prints may move our cursor.
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// traceControl reports a rune the feed must not print: the C0 controls but the
// newline, delete, and the C1 range an escape-stripped stream can still carry.
func traceControl(r rune) bool {
	return (r < 0x20 && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// appendFeedBlockKeys derives a stable identity per block from its own content
// plus an occurrence counter for identical blocks. Identity survives the
// trace's head truncation and new blocks appending, which block indices do
// not. The counter carries across calls so a block appended later gets the
// number it would have got had the whole log been keyed at once; whatever the
// unsettled tail contributes is taken back off before returning, because the
// tail is keyed again from scratch next cycle.
func appendFeedBlockKeys(keys []string, occurrences map[uint64]int, blocks []feedBlock) []string {
	if len(blocks) == 0 {
		return keys
	}
	out := append(append([]string(nil), keys...), make([]string, len(blocks))...)
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
		out[len(keys)+index] = fmt.Sprintf("%016x#%d", sum, occurrences[sum])
		occurrences[sum]++
	}
	return out
}

// releaseFeedBlockKeys hands back the occurrence numbers a transient run took.
func releaseFeedBlockKeys(occurrences map[uint64]int, blocks []feedBlock) {
	for _, block := range blocks {
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
		if occurrences[sum] <= 1 {
			delete(occurrences, sum)
			continue
		}
		occurrences[sum]--
	}
}

// appendTraceBlocks turns a run of whole log lines into blocks. It appends
// rather than returning its own slice, because the feed hands it the blocks it
// already has and the chunk that arrived since.
func appendTraceBlocks(blocks []feedBlock, chunk string, width int) []feedBlock {
	if chunk == "" {
		return blocks
	}
	for _, line := range strings.Split(sanitizeTraceBytes(chunk), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch {
		case feedTurnRule.MatchString(line):
			parts := feedTurnRule.FindStringSubmatch(line)
			label := "turn " + parts[1] + " · " + parts[4] + " tok"
			if parts[5] != "" {
				label += " · " + parts[5]
			}
			rule := feedRule(label, width)
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
	return blocks
}

func appendMessageBlocks(blocks []feedBlock, messages []store.Message, width int) []feedBlock {
	if len(messages) == 0 {
		return blocks
	}
	rule := feedRule("thread", width)
	blocks = append(blocks, feedBlock{brief: []string{"", mutedStyle.Faint(true).Render(rule)}})
	now := time.Now()
	for _, message := range messages {
		header := speakerHeader(message, now)
		body := strings.Split(renderMarkdown(message.Body, width), "\n")
		blocks = append(blocks, feedBlock{brief: append([]string{header}, body...)})
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
	salient := map[string]string{"sh": "cmd", "write": "path", "edit": "path", "web": "q", "generate_image": "prompt", "generate_music": "prompt", "generate_video": "prompt", "speak": "text", "view_image": "path", "read_document": "path"}[name]
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
		case "read_document":
			glyph = "▤"
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
	if (name == "write" || name == "edit" || name == "view_image" || name == "read_document") && strings.HasPrefix(detail, "/") {
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
			// The verb is the state. "finished" over work the user stopped
			// themselves reads as a claim that it completed, and the one line
			// they cannot scroll away from is the wrong place to be vague.
			parts = append(parts, nodeEndingWord(node.Status)+" "+relativeTime(node.FinishedAt, now))
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

// nodeEndingWord says how a task ended in the word a person would use for it.
func nodeEndingWord(status store.Status) string {
	switch status {
	case store.Failed:
		return "failed"
	case store.Cancelled:
		return "cancelled"
	default:
		return "finished"
	}
}

// updateNodeViewport hands a key the document did not claim to the document
// itself. Pointer motion is not one: with cell motion reporting on, the
// terminal names every cell the pointer crosses, and a hand that brushed the
// trackpad has decided nothing — it used to release the reading pin and drop a
// settled worker's reader out of the brief they had just opened. Only a wheel
// or a click is a decision.
func (m *Model) updateNodeViewport(message tea.Msg) {
	if mouse, ok := message.(tea.MouseMsg); ok {
		if tea.MouseEvent(mouse).Action == tea.MouseActionMotion {
			return
		}
	}
	m.releaseNodeTopPin()
	updated, _ := m.nodeTrace.Update(message)
	m.nodeTrace = updated
}

func (m *Model) pageNodeViewport(down bool) {
	m.releaseNodeTopPin()
	if down {
		m.nodeTrace.PageDown()
	} else {
		m.nodeTrace.PageUp()
	}
}

func (m *Model) scrollNodeFeed(down bool) {
	m.releaseNodeTopPin()
	if down {
		m.nodeTrace.SetYOffset(m.nodeTrace.YOffset + 3)
	} else {
		m.nodeTrace.SetYOffset(m.nodeTrace.YOffset - 3)
	}
}

func (m *Model) updateMouseClick(x, y int) (tea.Cmd, bool) {
	if m.palette == paletteHelp {
		switch {
		case m.paletteCloseBounds.contains(x, y), m.headerHelpBounds.contains(x, y):
			m.closeHelp()
		case m.helpBounds.contains(x, y):
			// The help body is inert reading space; the wheel scrolls it.
		default:
			m.closeHelp()
		}
		return nil, true
	}
	if m.palette == paletteSettings {
		if m.headerSettingsBounds.contains(x, y) {
			m.closeSettings()
			return nil, true
		}
		return m.clickSettings(x, y)
	}
	if m.headerSettingsBounds.contains(x, y) {
		return m.openSettings(), true
	}
	if m.boostBounds.contains(x, y) {
		m.toggleBoost()
		return nil, true
	}
	if m.headerThreadBounds.contains(x, y) {
		return m.selectPlace(placeThread), true
	}
	if m.headerBoardBounds.contains(x, y) {
		return m.selectPlace(placeBoard), true
	}
	if m.headerSelfBounds.contains(x, y) {
		return m.selectPlace(placeSelf), true
	}
	if m.headerQuestionBounds.contains(x, y) {
		m.focusPendingQuestion()
		return nil, true
	}
	if m.headerModelsBounds.contains(x, y) {
		return m.openModelsPalette(), true
	}
	if m.headerHelpBounds.contains(x, y) {
		m.openHelp()
		return nil, true
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
	if m.selfVisible() && m.selfBounds.contains(x, y) {
		return m.activateSelfAt(x, y)
	}
	if m.graphToggleHit(x, y) {
		if !m.closeScopedGraph() && !m.closeCharterCard() && !m.closeServiceCard() {
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
		if m.serviceRowsBounds.contains(x, y) {
			line := y - m.graphBounds.y
			for _, row := range m.serviceRows {
				if row.line+m.standingSectionHeight() == line {
					m.selectedNodeID = serviceGraphRowID(row.serviceID)
					m.openServiceCard(row.serviceID)
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
			if m.serviceCardID != "" {
				if command, ok := m.activateServiceLine(line); ok {
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
		if command, ok := m.toggleChatMessageAt(x, y); ok {
			return command, true
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

// toggleChatMessageAt handles a click inside the chat column: an option row
// answers the question that offered it, a provenance chip jumps to its task, a
// collapsed answer opens or closes in place, and anything else is a harmless
// no-op.
func (m *Model) toggleChatMessageAt(x, y int) (tea.Cmd, bool) {
	line := y - m.chatBounds.y + m.chat.YOffset
	if line < 0 {
		return nil, false
	}
	contentX := x - m.chatBounds.x
	for _, option := range m.notebookOptionRows {
		if option.line != line || contentX < option.startX || contentX >= option.endX {
			continue
		}
		m.notebookOption = option.optionIndex
		m.activateSelectedNotebookOption()
		m.refreshChat()
		return nil, true
	}
	// A confirm question packs every choice onto one row, so the column decides
	// which one was clicked before the line-level path guesses.
	for _, option := range m.cardOptionRows {
		if option.dock || option.line != line || contentX < option.startX || contentX >= option.endX {
			continue
		}
		m.questionSelection[option.cardID] = option.optionIndex
		return m.submitQuestionOption(option.cardID, option.optionIndex), true
	}
	return m.activateChatLine(line)
}

// activateChatLine is the one activation path for a thread content line —
// clicks and keyboard traversal both land here, so enter always equals click.
func (m *Model) activateChatLine(line int) (tea.Cmd, bool) {
	for _, option := range m.notebookOptionRows {
		if option.line == line {
			m.activateSelectedNotebookOption()
			m.refreshChat()
			return nil, true
		}
	}
	if command, ok := m.activateChatOptionLine(line); ok {
		return command, true
	}
	for _, row := range m.historyRows {
		if row.line == line {
			return nil, m.activateHistory(row.index)
		}
	}
	for _, row := range m.cardCloseRows {
		if !row.dock && row.line == line {
			m.selectedCardID = row.cardID
			m.cardExpanded[row.cardID] = false
			m.setSize(m.width, m.height)
			return nil, true
		}
	}
	for _, chip := range m.chatChipRows {
		if line == chip.line {
			_ = m.openNodeByID(chip.nodeID)
			return nil, true
		}
	}
	for _, part := range m.cardPartRows {
		if !part.dock && part.line == line {
			m.selectedCardID = part.cardID
			_ = m.openNodeByID(part.nodeID)
			return nil, true
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
		return nil, true
	}
	for _, row := range m.chatMessageRows {
		if line < row.start || line > row.end {
			continue
		}
		m.expandedMessages[row.seq] = !m.expandedMessages[row.seq]
		offset := m.chat.YOffset
		m.refreshChat()
		m.chat.SetYOffset(offset)
		return nil, true
	}
	for _, row := range m.chatCardRows {
		if line < row.start || line > row.end {
			continue
		}
		_ = m.advanceCard(row.cardID, focusChat)
		return nil, true
	}
	return nil, false
}

// activateChatOptionLine answers the question whose option rows cover this
// thread line. One choice per line takes that choice; a confirm row carries
// every choice on one line, so keyboard activation takes the banded one — a
// click has already resolved by column before reaching here.
func (m *Model) activateChatOptionLine(line int) (tea.Cmd, bool) {
	target := -1
	shared := 0
	for index, option := range m.cardOptionRows {
		if option.dock || option.line != line {
			continue
		}
		shared++
		if target < 0 {
			target = index
		}
	}
	if target < 0 {
		return nil, false
	}
	option := m.cardOptionRows[target]
	if shared > 1 {
		return m.submitSelectedQuestionOption(option.cardID), true
	}
	m.questionSelection[option.cardID] = option.optionIndex
	return m.submitQuestionOption(option.cardID, option.optionIndex), true
}
