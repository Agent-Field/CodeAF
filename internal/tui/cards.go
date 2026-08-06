package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type cardState string

const (
	cardCompiling cardState = "compiling"
	cardWorking   cardState = "working"
	cardQuestion  cardState = "question"
	cardSettled   cardState = "settled"
)

// jobCard is a presentation-only rollup of one top-level subtree. Everything
// here is rebuilt from durable messages, graph nodes, commands, and usage.
type jobCard struct {
	ID          string
	RootID      string
	State       cardState
	Title       string
	Ask         string
	Reading     string
	Receipt     string
	Latest      string
	Question    string
	Outcome     string
	BirthSeq    int64
	CommandSeq  int64
	StartedAt   time.Time
	FinishedAt  time.Time
	Done        int
	Total       int
	Usage       store.JobUsage
	Messages    []store.Message
	Narration   []string
	Parts       []cardPart
	Deliverable *store.Message
	Failed      bool
}

type cardPart struct {
	NodeID string
	Title  string
	Status store.Status
	Result string
}

// cardRow and cardPartRow map rendered lines back to the disclosure ladder.
// Dock rows are relative to the dock; chat rows are relative to chat content.
type cardRow struct {
	start  int
	end    int
	cardID string
	dock   bool
}

type cardPartRow struct {
	line   int
	cardID string
	nodeID string
	dock   bool
}

type cardCloseRow struct {
	line   int
	cardID string
	dock   bool
}

// deriveJobCards is the living-card query. It intentionally accepts store
// values rather than a database handle: the TUI remains a replayable lens and
// tests can exercise the same derivation with a seeded store.
func deriveJobCards(
	sessionID string,
	snapshot store.Snapshot,
	messages []store.Message,
	pending []store.Command,
	usage map[string]store.JobUsage,
	commands map[int64]store.Command,
) []jobCard {
	byID := make(map[string]store.Node, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		byID[node.ID] = node
	}

	rootOf := make(map[string]string, len(snapshot.Nodes))
	var resolveRoot func(string) string
	resolveRoot = func(nodeID string) string {
		if rootID, ok := rootOf[nodeID]; ok {
			return rootID
		}
		node, ok := byID[nodeID]
		if !ok || node.ID == store.RootID || node.Parent == "" {
			return ""
		}
		if node.Parent == store.RootID {
			rootOf[nodeID] = nodeID
			return nodeID
		}
		rootID := resolveRoot(node.Parent)
		rootOf[nodeID] = rootID
		return rootID
	}
	for nodeID := range byID {
		resolveRoot(nodeID)
	}

	nodesByRoot := make(map[string][]store.Node)
	for _, node := range snapshot.Nodes {
		if rootID := rootOf[node.ID]; rootID != "" {
			nodesByRoot[rootID] = append(nodesByRoot[rootID], node)
		}
	}
	messagesByRoot := make(map[string][]store.Message)
	for _, message := range messages {
		if message.NodeID == "" || (sessionID != "" && message.SessionID != "" && message.SessionID != sessionID) {
			continue
		}
		if rootID := rootOf[message.NodeID]; rootID != "" {
			messagesByRoot[rootID] = append(messagesByRoot[rootID], message)
		}
	}

	cards := make([]jobCard, 0, len(nodesByRoot)+len(pending))
	matchedCommands := make(map[int64]bool)
	for _, root := range snapshot.Nodes {
		if root.Parent != store.RootID ||
			(sessionID != "" && root.Provenance.SessionID != "" && root.Provenance.SessionID != sessionID) {
			continue
		}
		nodes := nodesByRoot[root.ID]
		card := jobCard{
			ID:        root.ID,
			RootID:    root.ID,
			State:     cardWorking,
			Title:     nodeLabel(root),
			Ask:       strings.TrimSpace(root.Provenance.Intent),
			Reading:   strings.TrimSpace(root.Brief),
			BirthSeq:  root.CreatedSeq,
			Usage:     usage[root.ID],
			Messages:  append([]store.Message(nil), messagesByRoot[root.ID]...),
			Failed:    root.Status == store.Failed || root.Status == store.Cancelled,
			StartedAt: root.StartedAt,
		}
		if card.Ask == "" {
			card.Ask = card.Reading
		}

		for _, node := range nodes {
			if nodeSettled(node) {
				card.Done++
			}
			if !node.StartedAt.IsZero() && (card.StartedAt.IsZero() || node.StartedAt.Before(card.StartedAt)) {
				card.StartedAt = node.StartedAt
			}
			if !node.FinishedAt.IsZero() && node.FinishedAt.After(card.FinishedAt) {
				card.FinishedAt = node.FinishedAt
			}
			result := strings.TrimSpace(node.Summary)
			if node.Status == store.Failed || node.Status == store.Cancelled {
				result = strings.TrimSpace(node.Error)
			}
			card.Parts = append(card.Parts, cardPart{
				NodeID: node.ID,
				Title:  nodeLabel(node),
				Status: node.Status,
				Result: firstLine(result),
			})
		}
		card.Total = max(len(nodes), card.Usage.NodeCount)
		if card.Total == 0 {
			card.Total = 1
		}

		var fallbackLatest string
		for index := range card.Messages {
			message := card.Messages[index]
			if card.BirthSeq == 0 || (message.Seq != 0 && message.Seq < card.BirthSeq) {
				card.BirthSeq = message.Seq
			}
			if message.Role == store.RoleAgent {
				line := firstLine(message.Body)
				if line != "" {
					card.Narration = append(card.Narration, line)
					card.Latest = line
				}
				if isQuestionMessage(message) {
					card.Question = strings.TrimSpace(message.Body)
				}
			} else if message.Role != store.RoleUser {
				fallbackLatest = firstLine(message.Body)
			}
		}
		if card.Latest == "" {
			card.Latest = fallbackLatest
		}

		commandSeq := matchingCommandSeq(root, commands, matchedCommands)
		if commandSeq != 0 {
			matchedCommands[commandSeq] = true
			card.CommandSeq = commandSeq
		}
		for _, message := range messages {
			if commandSeq != 0 && message.CommandSeq == commandSeq && message.Role == store.RoleSystem {
				card.Receipt = strings.TrimSpace(message.Body)
			}
		}
		if command, ok := commands[commandSeq]; ok && card.StartedAt.IsZero() {
			card.StartedAt = command.Time
		}

		if subtreeCardSettled(nodes) {
			card.State = cardSettled
			card.Deliverable = cardDeliverable(root, card.Messages)
			switch {
			case card.Deliverable != nil:
				card.Outcome = firstLine(card.Deliverable.Body)
			case card.Failed:
				card.Outcome = firstLine(root.Error)
			default:
				card.Outcome = firstLine(root.Summary)
			}
			if card.Outcome == "" {
				card.Outcome = string(root.Status)
			}
		} else if card.Question != "" {
			card.State = cardQuestion
			card.Latest = firstLine(card.Question)
		} else if card.Latest == "" {
			card.Latest = firstLine(card.Reading)
		}
		cards = append(cards, card)
	}

	pendingCommands := make(map[int64]bool)
	for _, command := range pending {
		pendingCommands[command.Seq] = true
		if command.Kind != store.CommandSplice ||
			(sessionID != "" && command.SessionID != "" && command.SessionID != sessionID) ||
			matchedCommands[command.Seq] {
			continue
		}
		card := jobCard{
			ID:         fmt.Sprintf("command:%d", command.Seq),
			State:      cardCompiling,
			Title:      firstLine(command.Instruction),
			Ask:        strings.TrimSpace(command.Instruction),
			BirthSeq:   command.Seq,
			CommandSeq: command.Seq,
			StartedAt:  command.Time,
		}
		for _, message := range messages {
			if message.CommandSeq != command.Seq {
				continue
			}
			if message.Role == store.RoleAgent {
				card.Latest = firstLine(message.Body)
			} else if message.Role == store.RoleSystem {
				card.Receipt = strings.TrimSpace(message.Body)
			}
		}
		cards = append(cards, card)
	}

	for seq, command := range commands {
		if command.Kind != store.CommandSplice || command.Status != store.CommandRejected ||
			matchedCommands[seq] || pendingCommands[seq] ||
			(sessionID != "" && command.SessionID != "" && command.SessionID != sessionID) {
			continue
		}
		var question store.Message
		for _, message := range messages {
			if message.CommandSeq == seq && isQuestionMessage(message) {
				question = message
			}
		}
		if question.Body == "" {
			continue
		}
		answered := false
		for _, message := range messages {
			if message.Role == store.RoleUser && message.NodeID == "" && message.Seq > question.Seq {
				answered = true
				break
			}
		}
		if answered {
			continue
		}
		cards = append(cards, jobCard{
			ID:         fmt.Sprintf("command:%d", seq),
			State:      cardQuestion,
			Title:      firstLine(command.Instruction),
			Ask:        strings.TrimSpace(command.Instruction),
			Question:   strings.TrimSpace(question.Body),
			Latest:     firstLine(question.Body),
			BirthSeq:   command.Seq,
			CommandSeq: command.Seq,
			StartedAt:  command.Time,
		})
	}

	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].BirthSeq == cards[j].BirthSeq {
			return cards[i].ID < cards[j].ID
		}
		return cards[i].BirthSeq < cards[j].BirthSeq
	})
	return cards
}

// matchingCommandSeq pairs a job root with the command that asked for it.
// Roots are visited in creation order and each command matches at most one
// root (used tracks consumption), so two identical asks in flight keep their
// own receipts instead of both attaching to the later command.
func matchingCommandSeq(root store.Node, commands map[int64]store.Command, used map[int64]bool) int64 {
	var matched int64
	for seq, command := range commands {
		if used[seq] || command.Kind != store.CommandSplice ||
			command.SessionID != root.Provenance.SessionID ||
			command.Instruction != root.Provenance.Intent ||
			(root.CreatedSeq != 0 && seq > root.CreatedSeq) {
			continue
		}
		if matched == 0 || seq < matched {
			matched = seq
		}
	}
	return matched
}

func subtreeCardSettled(nodes []store.Node) bool {
	if len(nodes) == 0 {
		return false
	}
	for _, node := range nodes {
		if !nodeSettled(node) {
			return false
		}
	}
	return true
}

func cardDeliverable(root store.Node, messages []store.Message) *store.Message {
	if !root.FinishedAt.IsZero() {
		// The deliverable is the FIRST root-anchored system message at or after
		// the finish — the landing itself. Later system posts (a detached
		// recalibration report, follow-up notes) must not replace the answer.
		for index := range messages {
			message := &messages[index]
			if message.NodeID == root.ID && message.Role == store.RoleSystem &&
				(message.Time.IsZero() || !message.Time.Before(root.FinishedAt)) {
				return message
			}
		}
	}
	if root.FinishedAt.IsZero() {
		for index := len(messages) - 1; index >= 0; index-- {
			message := &messages[index]
			if message.NodeID == root.ID && message.Role == store.RoleSystem {
				return message
			}
		}
	}
	if strings.TrimSpace(root.Summary) == "" && strings.TrimSpace(root.Error) == "" {
		return nil
	}
	body := root.Summary
	if root.Status == store.Failed || root.Status == store.Cancelled {
		body = root.Error
	}
	return &store.Message{
		SessionID: root.Provenance.SessionID,
		Role:      store.RoleSystem,
		Body:      body,
		NodeID:    root.ID,
	}
}

func isQuestionMessage(message store.Message) bool {
	return message.Role == store.RoleAgent && strings.HasSuffix(strings.TrimSpace(message.Body), "?")
}

func placeJobCards(cards []jobCard) (active, settled []jobCard) {
	for _, card := range cards {
		if card.State == cardSettled {
			settled = append(settled, card)
		} else {
			active = append(active, card)
		}
	}
	return active, settled
}

func (m *Model) rebuildCards() {
	previous := m.cards
	next := deriveJobCards(m.sessionID, m.cardSnapshot, m.messages, m.pending, m.jobUsage, m.commands)
	for _, old := range previous {
		if old.CommandSeq == 0 {
			continue
		}
		for _, card := range next {
			if card.CommandSeq != old.CommandSeq || card.ID == old.ID {
				continue
			}
			if m.cardExpanded[old.ID] {
				m.cardExpanded[card.ID] = true
				delete(m.cardExpanded, old.ID)
			}
			if m.selectedCardID == old.ID {
				m.selectedCardID = card.ID
			}
			break
		}
	}
	m.cards = next
	if m.selectedCardID != "" && m.cardByID(m.selectedCardID) == nil {
		m.selectedCardID = ""
	}
	if m.focus != focusCards {
		return
	}
	if selected := m.cardByID(m.selectedCardID); selected != nil && selected.State == cardSettled {
		m.focus = focusChat
		return
	}
	if m.activeCardCount() == 0 {
		m.focus = focusInput
		m.inputFocused = true
		_ = m.input.Focus()
		return
	}
	m.ensureCardSelection()
}

func (m *Model) cardByID(cardID string) *jobCard {
	for index := range m.cards {
		if m.cards[index].ID == cardID {
			return &m.cards[index]
		}
	}
	return nil
}

func (m *Model) cardForNodeID(nodeID string) *jobCard {
	for index := range m.cards {
		card := &m.cards[index]
		for _, part := range card.Parts {
			if part.NodeID == nodeID {
				return card
			}
		}
	}
	return nil
}

func (m *Model) streamMessage(message store.Message) bool {
	if message.Role == store.RoleUser && message.NodeID != "" {
		return false
	}
	if message.NodeID == "" || m.cardForNodeID(message.NodeID) == nil {
		return true
	}
	if isQuestionMessage(message) {
		return true
	}
	if node, ok := m.cardNode(message.NodeID); ok && node.Status == store.Failed {
		return true
	}
	return false
}

func (m *Model) cardNode(nodeID string) (store.Node, bool) {
	for _, node := range m.cardSnapshot.Nodes {
		if node.ID == nodeID {
			return node, true
		}
	}
	return store.Node{}, false
}

func (m *Model) attentionMessage(message store.Message) bool {
	if message.Role == store.RoleUser && message.NodeID != "" {
		return false
	}
	if message.NodeID == "" {
		return true
	}
	card := m.cardForNodeID(message.NodeID)
	return card == nil || card.State == cardSettled || isQuestionMessage(message) ||
		func() bool {
			node, ok := m.cardNode(message.NodeID)
			return ok && node.Status == store.Failed
		}()
}

func (m *Model) cardDockHeight() int {
	return lipgloss.Height(m.renderCardDock(false))
}

func (m *Model) limitCardDock(content string) string {
	lines := strings.Split(content, "\n")
	// Keep the minimum three-line conversation viewport and the fixed frame,
	// input, and hint rows. A very detailed card yields with an honest tail.
	limit := max(1, m.height-7-m.input.LineCount())
	if len(lines) <= limit {
		return content
	}
	hidden := len(lines) - limit + 1
	lines = lines[:limit]
	lines[limit-1] = mutedStyle.Faint(true).Render(fmt.Sprintf("… %d more card lines", hidden))
	return strings.Join(lines, "\n")
}

func (m *Model) renderCardDock(track bool) string {
	active, _ := placeJobCards(m.cards)
	if track {
		m.cardDockRows = m.cardDockRows[:0]
		kept := m.cardPartRows[:0]
		for _, row := range m.cardPartRows {
			if !row.dock {
				kept = append(kept, row)
			}
		}
		m.cardPartRows = kept
	}
	keptClose := m.cardCloseRows[:0]
	for _, row := range m.cardCloseRows {
		if !row.dock {
			keptClose = append(keptClose, row)
		}
	}
	m.cardCloseRows = keptClose
	if len(active) == 0 {
		return m.renderLegacyActivityBar()
	}
	if len(active) > 3 && m.focus != focusCards {
		questions := 0
		for _, card := range active {
			if card.State == cardQuestion {
				questions++
			}
		}
		line := fmt.Sprintf("%d running", len(active))
		if questions > 0 {
			line += fmt.Sprintf(" · %d question ⚑", questions)
		}
		line += " — tab or click to expand"
		return m.limitCardDock(truncate(lipgloss.NewStyle().Foreground(peach).Render(line), m.width))
	}

	lines := make([]string, 0, len(active))
	atLine := 0
	for _, card := range active {
		expanded := m.cardExpanded[card.ID]
		rendered := m.renderJobCard(card, m.width, expanded, atLine, true, track)
		if track {
			m.cardDockRows = append(m.cardDockRows, cardRow{
				start: atLine, end: atLine + lipgloss.Height(rendered) - 1, cardID: card.ID, dock: true,
			})
		}
		lines = append(lines, rendered)
		atLine += lipgloss.Height(rendered)
	}
	return m.limitCardDock(strings.Join(lines, "\n"))
}

func (m *Model) renderJobCard(card jobCard, width int, expanded bool, atLine int, dock, track bool) string {
	width = max(12, width)
	if dock && !expanded {
		return m.renderCompactCard(card, width)
	}

	glyph := m.cardGlyph(card)
	meta := m.cardMeta(card, time.Now())
	titleWidth := max(1, width-lipgloss.Width(glyph)-lipgloss.Width(meta)-7)
	title := lipgloss.NewStyle().Foreground(ink).Bold(true).Render(truncate(card.Title, titleWidth))
	header := mutedStyle.Faint(true).Render("╭─ ") + glyph + " " + title
	if meta != "" {
		header += mutedStyle.Render(" · " + meta)
	}
	lines := []string{truncate(header, width)}
	addText := func(text string, style lipgloss.Style) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		for _, line := range strings.Split(wrapText(text, max(1, width-4)), "\n") {
			lines = append(lines, mutedStyle.Faint(true).Render("│ ")+style.Render(line))
		}
	}

	if expanded {
		if card.Ask != "" {
			addText("asked · "+card.Ask, mutedStyle)
		}
		if card.Reading != "" && strings.TrimSpace(card.Reading) != strings.TrimSpace(card.Ask) {
			addText("reading this as · "+firstLine(card.Reading), mutedStyle)
		}
		if card.State == cardCompiling && card.Latest != "" {
			addText("reading this as · "+card.Latest, mutedStyle)
		}
		if card.State == cardQuestion {
			addText(card.Question, questionStyle)
		}
		for _, assumption := range cardAssumptions(card.Receipt) {
			addText(assumption, mutedStyle)
		}
		if len(card.Narration) > 0 {
			lines = append(lines, mutedStyle.Faint(true).Render("│   running summary"))
			for _, narration := range card.Narration {
				addText("· "+narration, inputTextStyle)
			}
		}
		if len(card.Parts) > 0 {
			lines = append(lines, mutedStyle.Faint(true).Render("│   parts"))
			parts := make([]cardPart, 0, len(card.Parts))
			for _, part := range card.Parts {
				if part.NodeID != card.RootID {
					parts = append(parts, part)
				}
			}
			if len(parts) == 0 {
				parts = card.Parts
			}
			for _, part := range parts {
				glyph := cardPartGlyph(part.Status)
				result := part.Result
				if result == "" {
					result = string(part.Status)
				}
				line := "│   " + glyph + " " + lipgloss.NewStyle().Foreground(ink).Bold(true).Render(part.Title) +
					mutedStyle.Render(" — "+result)
				lines = append(lines, truncate(line, width))
				if track {
					m.cardPartRows = append(m.cardPartRows, cardPartRow{
						line: atLine + len(lines) - 1, cardID: card.ID, nodeID: part.NodeID, dock: dock,
					})
				}
			}
		}
		if card.Usage.PromptTokens+card.Usage.CompletionTokens > 0 {
			addText(fmt.Sprintf("cost · %s · %s tokens", formatCardCost(card.Usage.Cost),
				humanizeTokens(card.Usage.PromptTokens+card.Usage.CompletionTokens)), mutedStyle)
		}
	} else if card.State == cardQuestion {
		addText(card.Question, questionStyle)
	}

	if card.State == cardSettled && card.Deliverable != nil {
		if expanded && card.Outcome != "" {
			addText("outcome · "+card.Outcome, mutedStyle)
		}
		rendered := m.renderAnswer(*card.Deliverable, max(1, width-4))
		bodyStart := atLine + len(lines)
		for _, line := range strings.Split(rendered, "\n") {
			lines = append(lines, mutedStyle.Faint(true).Render("│ ")+line)
		}
		if track && !dock && card.Deliverable.Seq != 0 {
			m.chatMessageRows = append(m.chatMessageRows, chatMessageRow{
				start: bodyStart, end: bodyStart + lipgloss.Height(rendered) - 1, seq: card.Deliverable.Seq,
			})
		}
	}

	hint := "click for details"
	if expanded {
		hint = "⟨×⟩ close · compiling the job graph…"
		if card.RootID != "" {
			hint = "⟨×⟩ close · enter or click for job graph"
		}
	}
	lines = append(lines, mutedStyle.Faint(true).Render("╰─ "+hint))
	if track && expanded {
		m.cardCloseRows = append(m.cardCloseRows, cardCloseRow{
			line: atLine + len(lines) - 1, cardID: card.ID, dock: dock,
		})
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderCompactCard(card jobCard, width int) string {
	glyph := m.cardGlyph(card)
	title := card.Title
	if title == "" {
		title = card.Ask
	}
	meta := m.cardMeta(card, time.Now())
	if card.State == cardQuestion && card.Latest != "" {
		meta = card.Latest
	}
	marker := ""
	if m.focus == focusCards && m.selectedCardID == card.ID {
		marker = lipgloss.NewStyle().Foreground(powder).Render("▸ ")
	}
	line := marker + glyph + " " + lipgloss.NewStyle().Foreground(ink).Bold(true).Render(title)
	if meta != "" {
		line += mutedStyle.Render(" · " + meta)
	}
	return truncate(line, width)
}

func (m *Model) cardGlyph(card jobCard) string {
	switch card.State {
	case cardCompiling:
		return lipgloss.NewStyle().Foreground(butter).Render("◌")
	case cardQuestion:
		return questionStyle.Render("⚑")
	case cardSettled:
		if card.Failed {
			return lipgloss.NewStyle().Foreground(rose).Render("✗")
		}
		return lipgloss.NewStyle().Foreground(mint).Render("✓")
	default:
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		return lipgloss.NewStyle().Foreground(peach).Render(frame)
	}
}

func (m *Model) cardMeta(card jobCard, now time.Time) string {
	parts := make([]string, 0, 4)
	if card.State == cardCompiling {
		parts = append(parts, "compiling")
		if card.Latest != "" {
			parts = append(parts, card.Latest)
		}
	}
	if card.State != cardCompiling && card.Total > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d", card.Done, card.Total))
	}
	if !card.StartedAt.IsZero() {
		end := now
		if card.State == cardSettled && !card.FinishedAt.IsZero() {
			end = card.FinishedAt
		}
		if end.After(card.StartedAt) {
			parts = append(parts, formatElapsed(end.Sub(card.StartedAt)))
		}
	}
	if card.State != cardCompiling {
		parts = append(parts, formatCardCost(card.Usage.Cost))
	}
	if card.State == cardWorking && card.Latest != "" {
		parts = append(parts, card.Latest)
	}
	if card.State == cardSettled && card.Outcome != "" {
		parts = append(parts, card.Outcome)
	}
	return strings.Join(parts, " · ")
}

func cardAssumptions(receipt string) []string {
	var assumptions []string
	for _, line := range strings.Split(strings.ReplaceAll(receipt, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Assumed:") {
			assumptions = append(assumptions, line)
		}
	}
	return assumptions
}

func cardPartGlyph(status store.Status) string {
	switch status {
	case store.Done:
		return lipgloss.NewStyle().Foreground(mint).Render("✓")
	case store.Failed, store.Cancelled:
		return lipgloss.NewStyle().Foreground(rose).Render("✗")
	case store.Running, store.Claimed:
		return lipgloss.NewStyle().Foreground(peach).Render("◐")
	default:
		return mutedStyle.Render("○")
	}
}

func formatCardCost(cost float64) string {
	if cost < 1 {
		return fmt.Sprintf("%.0f¢", cost*100)
	}
	return fmt.Sprintf("$%.2f", cost)
}

func (m *Model) advanceCard(cardID string, returnFocus paneFocus) tea.Cmd {
	card := m.cardByID(cardID)
	if card == nil {
		return nil
	}
	m.selectedCardID = cardID
	if !m.cardExpanded[cardID] {
		m.cardExpanded[cardID] = true
		m.focus = returnFocus
		m.inputFocused = false
		m.input.Blur()
		m.setSize(m.width, m.height)
		return nil
	}
	if card.RootID == "" {
		return nil
	}
	m.graphScopeID = card.RootID
	m.cardReturnFocus = returnFocus
	m.graphOpen = true
	m.selectedNodeID = card.RootID
	m.focus = focusGraph
	m.inputFocused = false
	m.input.Blur()
	m.setSize(m.width, m.height)
	return nil
}

func (m *Model) ensureCardSelection() {
	active, _ := placeJobCards(m.cards)
	for _, card := range active {
		if card.ID == m.selectedCardID {
			return
		}
	}
	if len(active) > 0 {
		m.selectedCardID = active[0].ID
	}
}

func (m *Model) moveCardSelection(delta int) {
	active, _ := placeJobCards(m.cards)
	if len(active) == 0 {
		return
	}
	m.ensureCardSelection()
	selected := 0
	for index, card := range active {
		if card.ID == m.selectedCardID {
			selected = index
			break
		}
	}
	selected = max(0, min(len(active)-1, selected+delta))
	m.selectedCardID = active[selected].ID
}

func (m *Model) focusCardDock() {
	m.ensureCardSelection()
	m.focus = focusCards
	m.inputFocused = false
	m.input.Blur()
	m.setSize(m.width, m.height)
}

func (m *Model) activeCardCount() int {
	active, _ := placeJobCards(m.cards)
	return len(active)
}

func (m *Model) collapseSelectedCard() bool {
	if m.selectedCardID == "" || !m.cardExpanded[m.selectedCardID] {
		return false
	}
	m.cardExpanded[m.selectedCardID] = false
	m.setSize(m.width, m.height)
	return true
}

func (m *Model) closeScopedGraph() bool {
	if m.graphScopeID == "" {
		return false
	}
	m.graphScopeID = ""
	m.graphOpen = false
	m.focus = m.cardReturnFocus
	m.inputFocused = m.focus == focusInput
	if m.inputFocused {
		_ = m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.setSize(m.width, m.height)
	return true
}
