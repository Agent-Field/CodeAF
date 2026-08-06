package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

const shimmerJobLimit = 3

func (m *Model) shimmerVisible() bool {
	return !m.graphVisible() && len(m.shimmerCards()) > 0
}

func (m *Model) shimmerCards() []jobCard {
	cards := make([]jobCard, 0, len(m.cards))
	for _, card := range m.cards {
		if card.State != cardWorking && card.State != cardCompiling {
			continue
		}
		cards = append(cards, card)
	}
	return cards
}

// renderShimmerLines is the rail-closed voice of live work. The sweep uses
// only the existing muted and primary inks, stepped across terminal cells;
// its slow phase keeps it matte rather than turning status into spectacle.
func (m *Model) renderShimmerLines(width int) string {
	if !m.shimmerVisible() || width <= 0 {
		return ""
	}
	cards := m.shimmerCards()
	shown := min(shimmerJobLimit, len(cards))
	lines := make([]string, 0, shown+1)
	for index := 0; index < shown; index++ {
		card := cards[index]
		current := card.Latest
		if len(card.Narration) > 0 {
			current = card.Narration[len(card.Narration)-1]
		}
		current = strings.TrimSpace(current)
		if current == "" {
			current = "working"
		}
		title := strings.TrimSpace(card.Title)
		if title == "" {
			title = firstLine(card.Ask)
		}
		line := "◐ " + title
		if current != "" && current != title {
			line += " · " + current
		}
		lines = append(lines, m.matteSweep(truncate(line, width)))
	}
	if hidden := len(cards) - shown; hidden > 0 {
		lines = append(lines, mutedStyle.Faint(true).Render(
			truncate(fmt.Sprintf("… %d more running", hidden), width),
		))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) matteSweep(text string) string {
	if text == "" {
		return ""
	}
	colors := []lipgloss.AdaptiveColor{muted, muted, ink, ink, muted, muted}
	runes := utf8.RuneCountInString(text)
	phase := (m.shimmerFrame / 4) % len(colors)
	var rendered strings.Builder
	rendered.Grow(len(text) * 2)
	index := 0
	for _, char := range text {
		band := 0
		if runes > 1 {
			band = index * len(colors) / runes
		}
		color := colors[(band+phase)%len(colors)]
		rendered.WriteString(lipgloss.NewStyle().Foreground(color).Render(string(char)))
		index++
	}
	return rendered.String()
}
