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

	spinnerFrames = []string{"◜", "◠", "◝", "◞", "◡", "◟"}
)

// View composes the complete frame once, avoiding terminal-clearing redraws.
func (m *Model) View() string {
	top := m.renderTopBar()
	chat := m.renderChatPane()
	graph := m.renderGraphPane()

	var main string
	if m.horizontal {
		main = lipgloss.JoinHorizontal(lipgloss.Top, chat, "  ", graph)
	} else {
		if m.stackGap() == 0 {
			main = lipgloss.JoinVertical(lipgloss.Left, chat, graph)
		} else {
			main = lipgloss.JoinVertical(lipgloss.Left, chat, "", graph)
		}
	}

	parts := []string{top, "", main, ""}
	if m.paletteOpen() {
		parts = append(parts, m.renderPalette())
	}
	parts = append(parts, m.renderInput())
	if !m.paletteOpen() {
		hint := "/ commands · tab complete · pgup/pgdn scroll · ctrl+c quit"
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
	running, done, failed := 0, 0, 0
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
	return mutedStyle.Render(fmt.Sprintf("%d running · %d done · %d failed", running, done, failed))
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
	case paletteHelp:
		lines := make([]string, 0, len(slashCommands)+1)
		for _, command := range slashCommands {
			lines = append(lines, truncate(fmt.Sprintf("「/%s」 %s", command.name, command.description), width))
		}
		cheatsheet := truncate("keys  tab/↑/↓ choose · enter accept · esc close · ctrl+c quit", width)
		lines = append(lines, mutedStyle.Render(cheatsheet))
		if limit := m.paletteLineLimit(); len(lines) > limit {
			lines = lines[:limit]
		}
		return lines
	default:
		return nil
	}
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
	lines := []string{talkView + mutedStyle.Render("    ") + workView}

	models := m.models()
	if len(models) == 0 {
		return append(lines, mutedStyle.Render("no models configured"))
	}
	columns := 2
	if width < 56 {
		columns = 1
	}
	rows := (len(models) + columns - 1) / columns
	selectedRow := m.paletteSelected % rows
	rowLimit := max(1, min(4, m.paletteLineLimit()-1))
	startRow, endRow := visiblePaletteWindow(selectedRow, rows, rowLimit)
	cellWidth := max(8, width/columns)
	for row := startRow; row < endRow; row++ {
		cells := make([]string, 0, columns)
		for column := range columns {
			index := row + column*rows
			if index >= len(models) {
				cells = append(cells, strings.Repeat(" ", cellWidth))
				continue
			}
			cells = append(cells, m.modelCell(models[index], index == m.paletteSelected, cellWidth))
		}
		lines = append(lines, strings.Join(cells, ""))
	}
	return lines
}

func (m *Model) paletteLineLimit() int {
	minimumMainHeight := 3
	if !m.horizontal {
		minimumMainHeight = 6 + m.stackGap()
	}
	available := m.height - 3 - (m.input.LineCount() + 2) - minimumMainHeight - 2
	return max(1, available)
}

func (m *Model) modelCell(model string, selected bool, width int) string {
	marker := "  "
	current := m.commander != nil && m.commander.CurrentModel(m.modelRole) == model
	if current {
		marker = "● "
	}
	text := truncate(model, max(1, width-2))
	padding := strings.Repeat(" ", max(0, width-2-lipgloss.Width(text)))
	if !selected {
		markerView := mutedStyle.Render(marker)
		if current {
			markerView = lipgloss.NewStyle().Foreground(mint).Render(marker)
		}
		return markerView + inputTextStyle.Render(text) + padding
	}
	markerStyle := selectedStyle
	if current {
		markerStyle = lipgloss.NewStyle().Foreground(mint).Background(lavender)
	}
	return markerStyle.Render(marker) + selectedStyle.Render(text+padding)
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
	for _, message := range m.messages {
		accent, label, marker, indent := rolePresentation(message.Role)
		available := max(8, m.chat.Width-2-indent)
		if marker != "" {
			available = max(8, available-lipgloss.Width(marker+" "))
		}
		header := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(label)
		header += mutedStyle.Render("  " + relativeTime(message.Time, time.Now()))

		body := wrapText(message.Body, available)
		if marker != "" {
			body = marker + " " + body
		}
		body = lipgloss.NewStyle().Foreground(accent).Render(body)

		block := lipgloss.NewStyle().
			Border(lipgloss.Border{Left: "│"}, false, false, false, true).
			BorderForeground(accent).
			PaddingLeft(1).
			MarginLeft(indent).
			Render(header + "\n" + body)
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n\n")
}

func rolePresentation(role store.Role) (lipgloss.AdaptiveColor, string, string, int) {
	switch role {
	case store.RoleUser:
		return powder, "USER", "", 0
	case store.RoleSystem:
		return mint, "SYSTEM", "◇", 2
	default:
		return lavender, "AGENT", "", 0
	}
}

func (m *Model) renderTree(width, height int) string {
	children := make(map[string][]store.Node, len(m.snapshot.Nodes))
	for _, node := range m.snapshot.Nodes {
		if node.ID == store.RootID {
			continue
		}
		children[node.Parent] = append(children[node.Parent], node)
	}
	roots := children[store.RootID]
	if len(roots) == 0 {
		empty := truncate("the graph is quiet — ask for something", max(1, width))
		return lipgloss.Place(
			max(1, width),
			max(1, height),
			lipgloss.Center,
			lipgloss.Center,
			mutedStyle.Render(empty),
		)
	}

	lines := make([]string, 0, len(m.snapshot.Nodes))
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

			glyph, active := m.nodeGlyph(node)
			prefix := mutedStyle.Render(ancestorGuide+branch) + glyph + " "
			brief := firstLine(node.Brief)
			if brief == "" {
				brief = node.ID
			}
			lines = append(lines, prefix+lipgloss.NewStyle().Foreground(ink).Render(
				truncate(brief, max(1, width-lipgloss.Width(prefix))),
			))

			if active && !node.StartedAt.IsZero() {
				elapsedPrefix := nextGuide + "   "
				elapsed := formatElapsed(time.Since(node.StartedAt)) + " elapsed"
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
	if node.FoldRoot {
		return lipgloss.NewStyle().Foreground(lavender).Render("◆"), false
	}
	switch node.Status {
	case store.Done:
		return lipgloss.NewStyle().Foreground(mint).Render("●"), false
	case store.Claimed, store.Running:
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		return lipgloss.NewStyle().Foreground(peach).Render("● " + frame), true
	case store.Failed, store.Cancelled:
		return lipgloss.NewStyle().Foreground(rose).Render("●"), false
	default:
		return lipgloss.NewStyle().Foreground(butter).Render("○"), false
	}
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
