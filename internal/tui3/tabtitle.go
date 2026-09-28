package tui3

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// tabTitlePreview reveals the full name beneath the hovered label, on the head's
// blank row under the rule (head.go) so the rule that seals the head stays drawn.
// It overlays existing rows so neither the tab targets nor the transcript scroll
// position move. Reading text stays still, including in the reduced-motion tier.
//
// THE NAME ENDS WHERE ITS TAB ENDS. It is right-aligned to the tab's right edge,
// close cells included, and a name wider than the tab sticks out to the left, so
// the eye reads it as belonging to the tab above rather than to the conversation
// below. Only a name that cannot fit between the left margin and that edge takes
// cells to the right of it, and only as many as it needs; one wider than the
// whole row wraps onto the rows below, over the top of the conversation.
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
	if at := tabStripRow; at >= len(rows) || rows[at] != a.chatTabBar.line {
		return frame
	}
	preview := strings.Split(ansi.Wrap(title, width-2*headLabelAt, ""), "\n")
	block := 0
	for _, line := range preview {
		block = max(block, ansi.StringWidth(line))
	}
	end := min(width, max(a.tabBoxEnd(hit), headLabelAt+block))
	start := end - block
	for i, line := range preview {
		at := chatHeadRows - 1 + i
		if at >= len(rows) {
			break
		}
		rows[at] = a.pal.ink(strings.Repeat(" ", start) + line + strings.Repeat(" ", max(0, width-start-ansi.StringWidth(line))))
	}
	return strings.Join(rows, "\n")
}

// tabBoxEnd is the column just past a tab's filled box: its close cells when the
// strip drew them beside the label, which it does on every tab, and the label's
// own edge otherwise.
func (a *app) tabBoxEnd(hit tabHit) int {
	for _, other := range a.chatTabHits {
		if other.kind == tabClose && other.span.from == hit.span.to {
			return other.span.to
		}
	}
	return hit.span.to
}
