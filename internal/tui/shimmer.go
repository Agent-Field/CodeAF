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

// The sweep is a small bright band crossing the line left→right: a primary-ink
// core with softened-ink shoulders (the leading edge eases in, matte, no new
// colors) over the muted base. One full sweep takes ~12 animation ticks —
// about 1.4s at the 120ms tick — regardless of line length.
const (
	sweepCoreRadius = 2
	sweepSoftRadius = 5
	sweepTicks      = 12
)

var (
	sweepBaseStyle = lipgloss.NewStyle().Foreground(muted)
	sweepSoftStyle = lipgloss.NewStyle().Foreground(ink).Faint(true)
	sweepCoreStyle = lipgloss.NewStyle().Foreground(ink)
)

// sweepCenter returns the highlight's cell position at an animation frame.
// The center starts off-screen left, advances rightward every tick, and exits
// off-screen right before wrapping, so the phase math can only move the band
// left→right.
func sweepCenter(frame, cells int) int {
	period := cells + 2*sweepSoftRadius + 1
	step := max(1, period/sweepTicks)
	return (frame*step)%period - sweepSoftRadius
}

func (m *Model) matteSweep(text string) string {
	if text == "" {
		return ""
	}
	center := sweepCenter(m.shimmerFrame, utf8.RuneCountInString(text))
	var rendered strings.Builder
	rendered.Grow(len(text) * 2)
	index := 0
	for _, char := range text {
		distance := index - center
		if distance < 0 {
			distance = -distance
		}
		style := sweepBaseStyle
		switch {
		case distance <= sweepCoreRadius:
			style = sweepCoreStyle
		case distance <= sweepSoftRadius:
			style = sweepSoftStyle
		}
		rendered.WriteString(style.Render(string(char)))
		index++
	}
	return rendered.String()
}
