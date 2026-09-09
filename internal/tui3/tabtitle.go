package tui3

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// tabTitlePreview reveals the full name beneath the hovered label. It overlays
// existing rows so neither the tab targets nor the transcript scroll position
// move. Reading text stays still, including in the reduced-motion tier.
func (a *app) tabTitlePreview(frame string) string {
	hit, ok := a.hotTab()
	if !ok || (hit.kind != tabHere && hit.kind != tabOther) || hit.tab.start {
		return frame
	}
	title := strings.Join(strings.Fields(hit.tab.full), " ")
	if title == "" {
		return frame
	}
	width, _ := a.size()
	if width <= 2*headLabelAt {
		return frame
	}
	rows := strings.Split(frame, "\n")
	// A modal or another place can hide the strip while its last hit map remains.
	// Only a frame actually drawing this strip may reveal its title.
	if at := a.tabsLineRow(); at >= len(rows) || rows[at] != a.chatTabBar.line {
		return frame
	}
	preview := strings.Split(ansi.Wrap(title, width-2*headLabelAt, ""), "\n")
	for i, line := range preview {
		at := a.tabsLineRow() + 1 + i
		if at >= len(rows) {
			break
		}
		rows[at] = a.pal.ink(strings.Repeat(" ", headLabelAt) + line + strings.Repeat(" ", max(0, width-headLabelAt-ansi.StringWidth(line))))
	}
	return strings.Join(rows, "\n")
}
