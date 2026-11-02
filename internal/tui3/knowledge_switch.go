package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const savedMemoryHeadRows = 2

func savedMemorySwitchRow(a *app, width int) placeRow {
	const label = "Knowledge"
	prefix := " Saved memory  "
	text := " " + a.pal.bold(a.pal.muted("Saved memory")) + "  " + a.pal.muted(label)
	hit := knowledgeHit{tree: -1}
	if a.mem.edit == nil {
		painted := a.pal.muted(label)
		if a.kb.hoverLink == "@switch" {
			painted = a.pal.underline(painted)
		}
		text = " " + a.pal.bold(a.pal.muted("Saved memory")) + "  " + painted
		start := ansi.StringWidth(prefix)
		hit.links = []knowledgeCellLink{{start, start + ansi.StringWidth(label), "@switch"}}
	}
	text += a.pal.dim(a.chords.say(" · alt+u"))
	return placeRow{text: fit(text, max(1, width-1)), hit: hit}
}

func (a *app) savedKnowledgeSwitchAt(x, y int) (tea.Cmd, bool) {
	_, hits, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	if y < 0 || y >= len(hits) {
		return nil, false
	}
	if h, ok := hits[y].(knowledgeHit); ok {
		for _, l := range h.links {
			if l.id == "@switch" && x >= l.start && x < l.end {
				return a.knowledgeOwns(tea.KeyPressMsg{Code: 'u', Mod: tea.ModAlt})
			}
		}
	}
	return nil, false
}

func (a *app) savedKnowledgeHoverAt(x, y int) bool {
	next := ""
	// The fixed header always occupies the first body row. No snapshot or frame
	// layout is needed for pointer motion over the saved-memory switch.
	prefix := " Saved memory  "
	start := ansi.StringWidth(prefix)
	if a.mem.edit == nil && y == placeHeadRows && x >= start && x < start+len("Knowledge") {
		next = "@switch"
	}
	if a.kb.hoverLink != next {
		a.kb.hoverLink = next
		a.touch()
	}
	return placeMemory{}.hover(a, y)
}
