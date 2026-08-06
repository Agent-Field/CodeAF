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
		main = lipgloss.JoinVertical(lipgloss.Left, chat, "", graph)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		top,
		"",
		main,
		"",
		m.renderInput(),
	)
}

func (m *Model) renderTopBar() string {
	wordmark := lipgloss.NewStyle().Foreground(lavender).Bold(true).Render("aforge")
	session := mutedStyle.Render("  " + m.sessionID)
	left := wordmark + session

	right := m.renderTally()
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
	running, done := 0, 0
	for _, node := range m.snapshot.Nodes {
		if node.ID == store.RootID {
			continue
		}
		switch node.Status {
		case store.Claimed, store.Running:
			running++
		case store.Done:
			done++
		}
	}
	return mutedStyle.Render(fmt.Sprintf("%d running · %d done · $–", running, done))
}

func (m *Model) renderChatPane() string {
	border := muted
	if !m.inputFocused {
		border = lavender
	}

	title := mutedStyle.Render("CHAT")
	if m.newMessages > 0 {
		hint := "↓ new"
		if m.newMessages > 1 {
			hint = fmt.Sprintf("↓ %d new", m.newMessages)
		}
		gap := max(1, m.chat.Width-lipgloss.Width("CHAT")-lipgloss.Width(hint))
		title += strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(peach).Render(hint)
	}

	content := title + "\n\n" + m.chat.View()
	return paneStyle(border, m.chatWidth, m.chatHeight).Render(content)
}

func (m *Model) renderGraphPane() string {
	title := mutedStyle.Render("GRAPH")
	contentHeight := max(1, m.graphHeight-4)
	tree := m.renderTree(m.graphWidth-4, contentHeight)
	return paneStyle(muted, m.graphWidth, m.graphHeight).Render(title + "\n\n" + tree)
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
		return lipgloss.Place(
			max(1, width),
			max(1, height),
			lipgloss.Center,
			lipgloss.Center,
			mutedStyle.Render("the graph is quiet — ask for something"),
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

	if len(lines) > height {
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
