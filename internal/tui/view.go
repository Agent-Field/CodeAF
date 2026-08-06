package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
)

var (
	lavender = lipgloss.AdaptiveColor{Light: "#6D4BC3", Dark: "#C6B4F5"}
	powder   = lipgloss.AdaptiveColor{Light: "#256B8C", Dark: "#AEDFF7"}
	mint     = lipgloss.AdaptiveColor{Light: "#277A62", Dark: "#A8E6CF"}
	peach    = lipgloss.AdaptiveColor{Light: "#A65B2A", Dark: "#FFD3B6"}
	butter   = lipgloss.AdaptiveColor{Light: "#826614", Dark: "#FBE7A1"}
	rose     = lipgloss.AdaptiveColor{Light: "#B23A57", Dark: "#F5A9B8"}
	muted    = lipgloss.AdaptiveColor{Light: "#686A78", Dark: "#6C7086"}
	ink      = lipgloss.AdaptiveColor{Light: "#2E3038", Dark: "#E8E7EE"}

	promptStyle      = lipgloss.NewStyle().Foreground(lavender).Bold(true)
	inputTextStyle   = lipgloss.NewStyle().Foreground(ink)
	placeholderStyle = lipgloss.NewStyle().Foreground(muted)
	cursorStyle      = lipgloss.NewStyle().Foreground(lavender)
	mutedStyle       = lipgloss.NewStyle().Foreground(muted)
	selectedInk      = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#24202E"}
	selectedStyle    = lipgloss.NewStyle().Foreground(selectedInk).Background(lavender)
	pillStyle        = lipgloss.NewStyle().Foreground(selectedInk).Background(peach).Padding(0, 1)

	spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// View composes the complete frame once, avoiding terminal-clearing redraws.
func (m *Model) View() string {
	top := m.renderTopBar()

	var main string
	if m.horizontal {
		main = lipgloss.JoinHorizontal(lipgloss.Top, m.renderChatPane(), "  ", m.renderGraphPane())
	} else if m.focus == focusGraph {
		main = m.renderGraphPane()
	} else {
		main = m.renderChatPane()
	}

	parts := []string{top, "", main, ""}
	if m.paletteOpen() {
		parts = append(parts, m.renderPalette())
	}
	if !m.horizontal {
		parts = append(parts, m.renderGraphStrip())
	}
	parts = append(parts, m.renderInput())
	if !m.paletteOpen() {
		hint := "/ commands · tab focus/view · v receipts · pgup/pgdn scroll · ctrl+c quit"
		parts = append(parts, mutedStyle.Render(truncate(hint, m.width)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) renderTopBar() string {
	wordmark := lipgloss.NewStyle().Foreground(lavender).Bold(true).Render("aforge")
	models := "talk " + truncate(modelShort(m.currentModel("talk")), 18) +
		" · work " + truncate(modelShort(m.currentModel("work")), 18)
	left := wordmark + mutedStyle.Render(" · "+m.sessionID+" · "+models)

	right := m.renderTally()
	if m.status != "" && time.Now().Before(m.statusUntil) {
		right = lipgloss.NewStyle().Foreground(powder).Render(truncate(m.status, max(8, m.width/2)))
	}
	if m.err != nil {
		right = lipgloss.NewStyle().Foreground(rose).Render(truncate(m.err.Error(), max(8, m.width/2)))
	}

	space := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 1 {
		left = wordmark
		space = m.width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if space < 1 {
		return truncate(left+" "+right, m.width)
	}
	return left + strings.Repeat(" ", space) + right
}

func (m *Model) renderTally() string {
	planning, running, done, failed := m.planningCount(), 0, 0, 0
	for _, node := range m.snapshot.Nodes {
		if node.ID == store.RootID {
			continue
		}
		switch node.Status {
		case store.Claimed, store.Running:
			running++
		case store.Done:
			done++
		case store.Failed:
			failed++
		}
	}
	segments := make([]string, 0, 4)
	if planning > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d planning", planning)))
	}
	if running > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d running", running)))
	}
	segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d done", done)))
	if failed > 0 {
		segments = append(segments, lipgloss.NewStyle().Foreground(rose).Render(fmt.Sprintf("%d failed", failed)))
	}
	tally := strings.Join(segments, mutedStyle.Render(" · "))
	if m.usage.Nodes > 0 {
		spend := fmt.Sprintf("%s tok · $%.2f", humanizeTokens(m.usage.PromptTokens+m.usage.CompletionTokens), m.usage.Cost)
		tally += mutedStyle.Render("  ·  " + spend)
	}
	return tally
}

func (m *Model) renderGraphStrip() string {
	running, waiting := 0, 0
	for _, node := range m.snapshot.Nodes {
		if node.ID == store.RootID {
			continue
		}
		switch node.Status {
		case store.Claimed, store.Running:
			running++
		case store.Pending:
			waiting++
		}
	}
	destination := "view"
	if m.focus == focusGraph {
		destination = "chat"
	}
	strip := fmt.Sprintf("● %d running · ○ %d waiting — tab to %s", running, waiting, destination)
	style := mutedStyle
	if m.focus == focusGraph {
		style = lipgloss.NewStyle().Foreground(lavender)
	}
	return style.Render(truncate(strip, m.width))
}

func (m *Model) renderChatPane() string {
	border := muted
	if m.focus == focusChat {
		border = lavender
	}

	title := mutedStyle.Render("CHAT")
	content := title + "\n\n" + m.chat.View()
	lines := strings.Split(content, "\n")
	innerHeight := max(1, m.chatHeight-2)
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}
	if len(lines) > innerHeight {
		lines = lines[:innerHeight]
	}
	if m.newMessages > 0 {
		pill := pillStyle.Render(m.newMessageLabel())
		index := len(lines) - 1
		available := max(1, m.chatWidth-4)
		lines[index] = overlayRight(lines[index], pill, available)
	}
	return paneStyle(border, m.chatWidth, m.chatHeight).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderGraphPane() string {
	border := muted
	if m.focus == focusGraph {
		border = lavender
	}
	title := mutedStyle.Render("GRAPH")
	content := title + "\n\n" + m.graph.View()
	lines := strings.Split(content, "\n")
	innerHeight := max(1, m.graphHeight-2)
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}
	if len(lines) > innerHeight {
		lines = lines[:innerHeight]
	}
	return paneStyle(border, m.graphWidth, m.graphHeight).Render(strings.Join(lines, "\n"))
}

func paneStyle(border lipgloss.AdaptiveColor, width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(max(1, width-2)).
		Height(max(1, height-2))
}

func (m *Model) renderInput() string {
	border := muted
	if m.inputFocused {
		border = lavender
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(max(1, m.width-2)).
		Render(m.input.View())
}

func (m *Model) renderPalette() string {
	innerWidth := max(1, m.width-4)
	lines := m.paletteLines(innerWidth)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lavender).
		Padding(0, 1).
		Width(max(1, m.width-2)).
		Render(strings.Join(lines, "\n"))
}

func (m *Model) paletteHeight() int {
	if !m.paletteOpen() {
		return 0
	}
	return lipgloss.Height(m.renderPalette())
}

func (m *Model) paletteLines(width int) []string {
	switch m.palette {
	case paletteCommands:
		entries := m.commandEntries()
		start, end := visiblePaletteWindow(m.paletteSelected, len(entries), m.paletteLineLimit())
		lines := make([]string, 0, end-start)
		for index := start; index < end; index++ {
			entry := entries[index]
			lines = append(lines, m.paletteRow(
				fmt.Sprintf("「/%s」 %s", entry.value, entry.description), index == m.paletteSelected, width,
			))
		}
		if len(lines) == 0 {
			return []string{mutedStyle.Render("no matching command")}
		}
		return lines
	case paletteModelCompletion:
		return m.completionLines(m.modelEntries(), "models", width)
	case paletteCancelCompletion:
		return m.completionLines(m.cancelEntries(), "non-terminal nodes", width)
	case paletteModel:
		return m.modelPickerLines(width)
	case paletteMemory:
		return m.memoryPanelLines(width)
	case paletteHelp:
		lines := make([]string, 0, len(slashCommands)+3)
		for _, command := range slashCommands {
			lines = append(lines, truncate(fmt.Sprintf("「/%s」 %s", command.name, command.description), width))
		}
		lines = append(lines,
			mutedStyle.Render(truncate("voice  you ask · aforge answers · v toggles receipts", width)),
			mutedStyle.Render(truncate("keys   tab focus / graph view · pgup/pgdn scroll · end latest", width)),
			mutedStyle.Render(truncate("menus  tab/↑/↓ choose · enter accept · esc close · ctrl+c quit", width)),
		)
		if limit := m.paletteLineLimit(); len(lines) > limit {
			lines = lines[:limit]
		}
		return lines
	default:
		return nil
	}
}

func (m *Model) memoryPanelLines(width int) []string {
	if len(m.memoryFacts) == 0 {
		return []string{mutedStyle.Render("notebook is empty")}
	}

	order := make([]string, 0)
	groups := make(map[string][]store.Fact)
	for _, fact := range m.memoryFacts {
		scope := strings.TrimSpace(fact.Scope)
		if _, ok := groups[scope]; !ok {
			order = append(order, scope)
		}
		groups[scope] = append(groups[scope], fact)
	}
	lines := make([]string, 0, m.memoryLineCount())
	for _, scope := range order {
		lines = append(lines, mutedStyle.Faint(true).Render(truncate(scope, width)))
		for _, fact := range groups[scope] {
			lines = append(lines, memoryFactRow(fact, width))
		}
	}

	limit := m.paletteLineLimit()
	start := min(m.paletteSelected, max(0, len(lines)-limit))
	end := min(len(lines), start+limit)
	return lines[start:end]
}

func memoryFactRow(fact store.Fact, width int) string {
	glyph := "·"
	style := mutedStyle
	switch fact.Kind {
	case store.FactPreference:
		glyph = "◆"
		style = lipgloss.NewStyle().Foreground(lavender)
	case store.FactQuirk:
		glyph = "▲"
		style = lipgloss.NewStyle().Foreground(peach)
	case store.FactLesson:
		glyph = "●"
		style = lipgloss.NewStyle().Foreground(mint)
	}
	body := truncate(strings.TrimSpace(fact.Body), max(1, width-lipgloss.Width(glyph)-1))
	return style.Render(glyph) + " " + inputTextStyle.Render(body)
}

func (m *Model) completionLines(entries []paletteEntry, label string, width int) []string {
	if len(entries) == 0 {
		return []string{mutedStyle.Render("no matching " + label)}
	}
	start, end := visiblePaletteWindow(m.paletteSelected, len(entries), min(6, m.paletteLineLimit()))
	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		lines = append(lines, m.paletteRow(entries[index].value, index == m.paletteSelected, width))
	}
	return lines
}

func (m *Model) modelPickerLines(width int) []string {
	talk, work := "○ talk", "○ work"
	if m.modelRole == "talk" {
		talk = "◉ talk"
	} else {
		work = "◉ work"
	}
	talkView := mutedStyle.Render(talk)
	workView := mutedStyle.Render(work)
	if m.modelRole == "talk" {
		talkView = lipgloss.NewStyle().Foreground(lavender).Bold(true).Render(talk)
	} else {
		workView = lipgloss.NewStyle().Foreground(lavender).Bold(true).Render(work)
	}
	lines := []string{
		talkView + mutedStyle.Render("    ") + workView,
		mutedStyle.Render("filter: ") + inputTextStyle.Render(m.input.Value()),
	}
	if m.catalogLoading {
		lines = append(lines, mutedStyle.Render("fetching full catalog…"))
	}

	choices := m.filteredModelChoices()
	if len(choices) == 0 {
		empty := "no models configured"
		if strings.TrimSpace(m.input.Value()) != "" {
			empty = "no matching models"
		}
		return append(lines, mutedStyle.Render(empty))
	}
	rowLimit := max(1, min(8, m.paletteLineLimit()-len(lines)))
	start, end := visiblePaletteWindow(m.paletteSelected, len(choices), rowLimit)
	for index := start; index < end; index++ {
		lines = append(lines, m.modelChoiceRow(choices[index], index == m.paletteSelected, width))
	}
	return lines
}

func (m *Model) paletteLineLimit() int {
	minimumMainHeight := 3
	stripHeight := 0
	if !m.horizontal {
		stripHeight = 1
	}
	available := m.height - 3 - (m.input.LineCount() + 2) - minimumMainHeight - stripHeight - 2
	return max(1, available)
}

func (m *Model) modelChoiceRow(choice ModelChoice, selected bool, width int) string {
	marker := "  "
	markerStyle := mutedStyle
	if m.commander != nil && m.commander.CurrentModel(m.modelRole) == choice.Slug {
		marker = "● "
		markerStyle = lipgloss.NewStyle().Foreground(mint)
	}
	if selected {
		marker = "› "
		markerStyle = lipgloss.NewStyle().Foreground(lavender).Bold(true)
	}

	detail := choice.Name
	if detail == choice.Slug {
		detail = ""
	}
	if choice.Price != "" {
		if detail != "" {
			detail += " · "
		}
		detail += choice.Price
	}

	available := max(1, width-lipgloss.Width(marker))
	slug := truncate(choice.Slug, available)
	detailWidth := available - lipgloss.Width(slug) - 2
	if detailWidth > 0 && detail != "" {
		detail = truncate(detail, detailWidth)
	} else {
		detail = ""
	}
	accent := lavender
	if m.modelRole == "work" {
		accent = peach
	}
	slugStyle := lipgloss.NewStyle().Foreground(accent).Bold(selected)
	row := markerStyle.Render(marker) + slugStyle.Render(slug)
	if detail != "" {
		row += mutedStyle.Faint(true).Render("  " + detail)
	}
	return row
}

func (m *Model) paletteRow(text string, selected bool, width int) string {
	text = truncate(text, width)
	if selected {
		return selectedStyle.Width(width).Render(text)
	}
	return text
}

func visiblePaletteWindow(selected, count, limit int) (int, int) {
	if count <= limit {
		return 0, count
	}
	start := max(0, selected-limit/2)
	start = min(start, count-limit)
	return start, start + limit
}

func (m *Model) currentModel(role string) string {
	if m.commander == nil {
		return "–"
	}
	return m.commander.CurrentModel(role)
}

func modelShort(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "–"
	}
	if index := strings.LastIndex(model, "/"); index >= 0 && index+1 < len(model) {
		return model[index+1:]
	}
	return model
}

func (m *Model) newMessageLabel() string {
	if m.newMessages > 1 {
		return fmt.Sprintf("↓ %d new messages", m.newMessages)
	}
	return "↓ new messages"
}

func overlayRight(line, overlay string, width int) string {
	overlayWidth := lipgloss.Width(overlay)
	if overlayWidth >= width {
		return truncate(overlay, width)
	}
	line = truncate(line, width-overlayWidth-1)
	gap := max(1, width-lipgloss.Width(line)-overlayWidth)
	return line + strings.Repeat(" ", gap) + overlay
}

func (m *Model) renderMessages() string {
	if len(m.messages) == 0 {
		return mutedStyle.Render("No messages yet. Start with a thought or a task.")
	}

	blocks := make([]string, 0, len(m.messages))
	for index := 0; index < len(m.messages); {
		message := m.messages[index]
		if secondaryMessage(message) {
			receipts := []store.Message{message}
			index++
			for index < len(m.messages) && secondaryMessage(m.messages[index]) {
				receipts = append(receipts, m.messages[index])
				index++
			}
			blocks = append(blocks, m.renderMessageBlock(store.Message{
				Time: message.Time,
				Role: store.RoleAgent,
			}, receipts))
			continue
		}

		index++
		receipts := make([]store.Message, 0)
		if messageVoice(message) == "aforge" {
			for index < len(m.messages) && secondaryMessage(m.messages[index]) {
				receipts = append(receipts, m.messages[index])
				index++
			}
		}
		blocks = append(blocks, m.renderMessageBlock(message, receipts))
	}
	return strings.Join(blocks, "\n\n")
}

func (m *Model) renderMessageBlock(message store.Message, receipts []store.Message) string {
	accent, label := messagePresentation(message)
	available := max(8, m.chat.Width-2)
	header := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(label)
	header += mutedStyle.Render("  " + relativeTime(message.Time, time.Now()))

	content := header
	if message.Body != "" {
		body := lipgloss.NewStyle().Foreground(accent).Render(wrapText(message.Body, available))
		content += "\n" + body
	}
	for _, receipt := range receipts {
		content += "\n" + m.renderReceipt(receipt, available)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.Border{Left: "│"}, false, false, false, true).
		BorderForeground(accent).
		PaddingLeft(1).
		Render(content)
}

func (m *Model) renderReceipt(message store.Message, width int) string {
	summary := receiptSummary(message)
	if !m.receiptsExpanded {
		return mutedStyle.Render(truncate(summary+" — v to expand", width))
	}
	label := mutedStyle.Render(truncate(summary+" — v to collapse", width))
	body := wrapText(message.Body, max(1, width-2))
	return label + "\n" + mutedStyle.Render(indentLines(body, "  "))
}

func messagePresentation(message store.Message) (lipgloss.AdaptiveColor, string) {
	if message.Role == store.RoleUser {
		return powder, "you"
	}
	return lavender, "aforge"
}

func messageVoice(message store.Message) string {
	_, label := messagePresentation(message)
	return label
}

func secondaryMessage(message store.Message) bool {
	return message.Role == store.RoleSystem && message.NodeID == ""
}

func receiptSummary(message store.Message) string {
	if message.CommandSeq != 0 {
		assumptions := 0
		for _, line := range strings.Split(strings.ReplaceAll(message.Body, "\r\n", "\n"), "\n") {
			if strings.HasPrefix(line, "Assumed:") {
				assumptions++
			}
		}
		return fmt.Sprintf("· reading + %d assumptions", assumptions)
	}
	label := firstLine(message.Body)
	if label == "" {
		label = "update"
	}
	return "· " + label
}

func indentLines(text, prefix string) string {
	return prefix + strings.ReplaceAll(text, "\n", "\n"+prefix)
}

func (m *Model) renderTree(width, height int) string {
	now := time.Now()
	m.graphAnimating = false
	lines := make([]string, 0, len(m.snapshot.Nodes)+len(m.pending))
	for _, command := range m.pending {
		if command.Kind != store.CommandSplice {
			continue
		}
		row := len(lines)
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		instruction := truncate(firstLine(command.Instruction), 30)
		elapsed := "0s elapsed"
		if !command.Time.IsZero() {
			elapsed = formatElapsed(now.Sub(command.Time)) + " elapsed"
		}
		label := "planning…"
		if instruction != "" {
			label += " " + instruction
		}
		label += " · " + elapsed
		prefix := lipgloss.NewStyle().Foreground(peach).Render(frame) + " "
		lines = append(lines, prefix+inputTextStyle.Render(truncate(label, max(1, width-lipgloss.Width(prefix)))))
		m.noteAnimatedGraphRow(row)
	}

	children := make(map[string][]store.Node, len(m.snapshot.Nodes))
	for _, node := range m.snapshot.Nodes {
		if node.ID == store.RootID {
			continue
		}
		children[node.Parent] = append(children[node.Parent], node)
	}
	roots := children[store.RootID]
	if len(roots) == 0 && len(lines) == 0 {
		empty := truncate("the graph is quiet — ask for something", max(1, width))
		return lipgloss.Place(
			max(1, width),
			max(1, height),
			lipgloss.Center,
			lipgloss.Center,
			mutedStyle.Render(empty),
		)
	}

	seen := make(map[string]bool, len(m.snapshot.Nodes))
	var walk func([]store.Node, string)
	walk = func(nodes []store.Node, ancestorGuide string) {
		for index, node := range nodes {
			if seen[node.ID] {
				continue
			}
			seen[node.ID] = true
			last := index == len(nodes)-1
			branch := "├─ "
			nextGuide := ancestorGuide + "│  "
			if last {
				branch = "╰─ "
				nextGuide = ancestorGuide + "   "
			}

			row := len(lines)
			glyph, active := m.nodeGlyphAt(node, now)
			prefix := mutedStyle.Render(ancestorGuide+branch) + glyph + " "
			brief := firstLine(node.Brief)
			if brief == "" {
				brief = node.ID
			}
			lines = append(lines, prefix+lipgloss.NewStyle().Foreground(ink).Render(
				truncate(brief, max(1, width-lipgloss.Width(prefix))),
			))
			if active || m.completionFlashing(node, now) {
				m.noteAnimatedGraphRow(row)
			}

			if active && !node.StartedAt.IsZero() {
				elapsedPrefix := nextGuide + "   "
				elapsed := formatElapsed(now.Sub(node.StartedAt)) + " elapsed"
				lines = append(lines, mutedStyle.Render(elapsedPrefix+truncate(elapsed, max(1, width-lipgloss.Width(elapsedPrefix)))))
			}
			walk(children[node.ID], nextGuide)
		}
	}
	walk(roots, "")

	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) nodeGlyph(node store.Node) (string, bool) {
	return m.nodeGlyphAt(node, time.Now())
}

func (m *Model) nodeGlyphAt(node store.Node, now time.Time) (string, bool) {
	if node.FoldRoot {
		return lipgloss.NewStyle().Foreground(lavender).Render("◆"), false
	}
	switch node.Status {
	case store.Done:
		return lipgloss.NewStyle().Foreground(mint).Bold(m.completionFlashing(node, now)).Render("●"), false
	case store.Claimed, store.Running:
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		return lipgloss.NewStyle().Foreground(peach).Render("● " + frame), true
	case store.Failed, store.Cancelled:
		return lipgloss.NewStyle().Foreground(rose).Render("●"), false
	default:
		return lipgloss.NewStyle().Foreground(butter).Render("○"), false
	}
}

func (m *Model) completionFlashing(node store.Node, now time.Time) bool {
	return node.Status == store.Done && !node.FinishedAt.IsZero() &&
		!now.Before(node.FinishedAt) && now.Sub(node.FinishedAt) < 2*time.Second
}

func (m *Model) noteAnimatedGraphRow(row int) {
	if !m.horizontal && m.focus != focusGraph {
		return
	}
	start := m.graph.YOffset
	end := start + max(1, m.graph.Height)
	if row >= start && row < end {
		m.graphAnimating = true
	}
}

func (m *Model) planningCount() int {
	count := 0
	for _, command := range m.pending {
		if command.Kind == store.CommandSplice {
			count++
		}
	}
	return count
}

func humanizeTokens(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return trimDecimal(float64(tokens)/1_000_000) + "M"
	case tokens >= 1_000:
		return trimDecimal(float64(tokens)/1_000) + "k"
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func trimDecimal(value float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0")
}

func relativeTime(at, now time.Time) string {
	if at.IsZero() || !now.After(at) {
		return "now"
	}
	age := now.Sub(at)
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(age/(24*time.Hour)))
	}
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	elapsed = elapsed.Round(time.Second)
	if elapsed < time.Minute {
		return fmt.Sprintf("%ds", int(elapsed/time.Second))
	}
	if elapsed < time.Hour {
		return fmt.Sprintf("%dm %02ds", int(elapsed/time.Minute), int(elapsed/time.Second)%60)
	}
	return fmt.Sprintf("%dh %02dm", int(elapsed/time.Hour), int(elapsed/time.Minute)%60)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	var result strings.Builder
	used := 0
	for _, char := range text {
		charWidth := lipgloss.Width(string(char))
		if used+charWidth+1 > width {
			break
		}
		result.WriteRune(char)
		used += charWidth
	}
	return strings.TrimRight(result.String(), " ") + "…"
}

func wrapText(text string, width int) string {
	if width <= 1 {
		return text
	}
	paragraphs := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	wrapped := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			wrapped = append(wrapped, "")
			continue
		}
		line := ""
		for _, word := range words {
			for lipgloss.Width(word) > width {
				if line != "" {
					wrapped = append(wrapped, line)
					line = ""
				}
				piece, rest := splitWidth(word, width)
				wrapped = append(wrapped, piece)
				word = rest
			}
			candidate := word
			if line != "" {
				candidate = line + " " + word
			}
			if lipgloss.Width(candidate) > width {
				wrapped = append(wrapped, line)
				line = word
			} else {
				line = candidate
			}
		}
		if line != "" {
			wrapped = append(wrapped, line)
		}
	}
	return strings.Join(wrapped, "\n")
}

func splitWidth(text string, width int) (string, string) {
	used := 0
	index := 0
	for offset, char := range text {
		charWidth := lipgloss.Width(string(char))
		if used+charWidth > width {
			index = offset
			break
		}
		used += charWidth
		index = offset + len(string(char))
	}
	if index == 0 {
		_, size := firstRune(text)
		index = size
	}
	return text[:index], text[index:]
}

func firstRune(text string) (rune, int) {
	for _, char := range text {
		return char, len(string(char))
	}
	return 0, 0
}
