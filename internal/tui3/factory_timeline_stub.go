package tui3

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE TIMELINE, STANDING IN ───────────────────────────────────────────────
//
// THE CENTER OF THE ITEM PAGE FOR THE `manager`, `run` AND STAGE ROWS IS THE
// RUN'S STORY, which the timeline lane draws in factory_timeline.go. This
// file stands in for it so the item page builds without it, and IS REPLACED
// WHOLE when that file lands: the four functions keep their signatures.
//
// The pointer's x and y are the PANE'S OWN: x counted from the pane's first
// cell (past the left column, its rule and the pane's margin) and y from the
// pane's first line (factory_item.go's [app.factoryPaneAt]).

// factoryTimelinePane is the center for the item's run, at most room lines of
// at most width cells.
func (a *app) factoryTimelinePane(it factory.Item, width, room int) []string {
	if room <= 0 || width <= 0 {
		return nil
	}
	var out []string
	for _, line := range wrap("the run's story stands here", width) {
		out = append(out, a.pal.dim(line))
	}
	if len(out) > room {
		out = out[:room]
	}
	return out
}

// factoryTimelineKey is a key on the timeline, taken or handed on.
func (a *app) factoryTimelineKey(it factory.Item, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return nil, false
}

// factoryTimelinePress is a press at the pane's (x, y), taken or handed on.
func (a *app) factoryTimelinePress(it factory.Item, x, y int) (tea.Cmd, bool) {
	return nil, false
}

// factoryTimelineHover is the pointer resting at the pane's (x, y).
func (a *app) factoryTimelineHover(it factory.Item, x, y int) bool {
	return false
}
