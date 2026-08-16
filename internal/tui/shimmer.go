package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

const shimmerJobLimit = 3

// shimmerStaleAfter is how long a status line may stand still before its line
// stops breathing. cardWorking is the default state and is cleared only by a
// settled subtree, so a wedged worker would otherwise pin the 120ms repaint
// loop forever — a fan that never stops for a job that never moves. The line
// stays on screen; only the sweep gives up.
const shimmerStaleAfter = 10 * time.Minute

// shimmerStamp remembers when a live card's status line last said something
// new, so staleness is a fact about the work rather than about the clock.
type shimmerStamp struct {
	line string
	at   time.Time
}

func (m *Model) shimmerVisible() bool {
	return !m.graphVisible() && len(m.shimmerCards()) > 0
}

// shimmerAnimating reports whether any visible shimmer line still has news.
// It, not shimmerVisible, is what keeps the animation ticking.
func (m *Model) shimmerAnimating() bool {
	if m.graphVisible() {
		return false
	}
	cards := m.shimmerCards()
	for index := 0; index < min(shimmerJobLimit, len(cards)); index++ {
		if !m.shimmerStale(cards[index]) {
			return true
		}
	}
	return false
}

// shimmerCards is the live half of the card list. Three callers ask for it per
// frame — is there a line, does it still have news, draw it — and each answer
// was a fresh copy of every working card, eight times a second. The filter is
// cheap and the copy was not, so it refills one buffer instead: every caller
// reads it out before the next one asks, and the answer is always the card
// list as it stands rather than as it stood.
func (m *Model) shimmerCards() []jobCard {
	cards := m.shimmerLive[:0]
	for _, card := range m.cards {
		if card.State != cardWorking && card.State != cardCompiling {
			continue
		}
		cards = append(cards, card)
	}
	m.shimmerLive = cards
	return cards
}

func (m *Model) shimmerStale(card jobCard) bool {
	stamp, ok := m.shimmerSeen[card.ID]
	if !ok || stamp.at.IsZero() {
		return false
	}
	return m.standingTime().Sub(stamp.at) >= shimmerStaleAfter
}

// noteShimmerActivity stamps every live card whose status line moved. It runs
// where the cards are rebuilt, so the stamp tracks the work rather than the
// frame rate.
func (m *Model) noteShimmerActivity() {
	now := m.standingTime()
	live := make(map[string]bool, len(m.cards))
	for _, card := range m.cards {
		if card.State != cardWorking && card.State != cardCompiling {
			continue
		}
		live[card.ID] = true
		line := shimmerStatus(card)
		if stamp, ok := m.shimmerSeen[card.ID]; ok && stamp.line == line {
			continue
		}
		if m.shimmerSeen == nil {
			m.shimmerSeen = make(map[string]shimmerStamp, len(m.cards))
		}
		m.shimmerSeen[card.ID] = shimmerStamp{line: line, at: now}
	}
	for id := range m.shimmerSeen {
		if !live[id] {
			delete(m.shimmerSeen, id)
		}
	}
}

// shimmerStatus is the card's current word on itself — the text whose standing
// still is what staleness means.
func shimmerStatus(card jobCard) string {
	current := card.Latest
	if card.State != cardCompiling && len(card.Narration) > 0 {
		current = card.Narration[len(card.Narration)-1]
	}
	return strings.TrimSpace(current)
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
		current := shimmerStatus(card)
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
		line = truncate(line, width)
		// A job that has said nothing for ten minutes keeps its line and loses
		// its breath.
		if m.shimmerStale(card) {
			lines = append(lines, sweepBaseStyle.Render(line))
			continue
		}
		lines = append(lines, m.matteSweep(line))
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
	sweepSoftStyle = inkStyle.Faint(true)
	sweepCoreStyle = inkStyle
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

// sweepBand names which of the three inks a cell falls under. Cells are
// grouped into runs by band and each run is rendered once: a per-rune Render
// is three style lookups and an escape pair per character, thousands a second
// for a line that changes one cell per tick.
func sweepBand(distance int) int {
	if distance < 0 {
		distance = -distance
	}
	switch {
	case distance <= sweepCoreRadius:
		return 2
	case distance <= sweepSoftRadius:
		return 1
	default:
		return 0
	}
}

func (m *Model) matteSweep(text string) string {
	if text == "" {
		return ""
	}
	// Every swept line on the frame is drawn from the same phase, and a frame
	// can be rebuilt several times per tick. One sweep per (frame, text).
	if m.sweepFrame != m.shimmerFrame || m.sweepCache == nil {
		m.sweepFrame = m.shimmerFrame
		m.sweepCache = make(map[string]string, 4)
	}
	if cached, ok := m.sweepCache[text]; ok {
		return cached
	}
	styles := [3]lipgloss.Style{sweepBaseStyle, sweepSoftStyle, sweepCoreStyle}
	center := sweepCenter(m.shimmerFrame, utf8.RuneCountInString(text))
	var rendered strings.Builder
	rendered.Grow(len(text) + 64)
	index := 0
	runStart := 0
	runBand := -1
	for offset := range text {
		band := sweepBand(index - center)
		switch {
		case runBand < 0:
			runBand, runStart = band, offset
		case band != runBand:
			rendered.WriteString(styles[runBand].Render(text[runStart:offset]))
			runBand, runStart = band, offset
		}
		index++
	}
	if runBand >= 0 {
		rendered.WriteString(styles[runBand].Render(text[runStart:]))
	}
	swept := rendered.String()
	m.sweepCache[text] = swept
	return swept
}
