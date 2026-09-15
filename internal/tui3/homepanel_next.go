package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// nextPanel is `next up`: the reminders and routines this machine will act on,
// soonest first and the rules that simply hold at the end — the order and the
// `in 20h` / `mon 8:30` / `holds` clause the project card's band always drew
// (homeband_nextup.go's [standByNextDue] and [standWhenClause]), now for the
// whole machine. Every row opens the standing place, where the orders are kept.
type nextPanel struct{ homePanelBase }

func (nextPanel) rows(in *homeGridInput) homePanelRows {
	views := nextActive(in)
	standByNextDue(views)
	shown := min(homeSlotOf(panelNext).most, len(views))
	lines := make([]homeLine, 0, shown)
	for _, view := range views[:shown] {
		item := view.Item
		lines = append(lines, homeLine{kind: homeLedger, project: pageStanding.word(), dir: item.ID,
			view: view, item: item, cell: &homeCell{panel: panelNext,
				title: strings.TrimSpace(item.Words), right: standWhenClause(item, in.now),
				// WHAT WAKES IT, under the cursor. The row's right-hand clause is
				// WHEN the next one is; this is the standing arrangement behind
				// it, in the person's own words where they gave any, and it is
				// drawn only for the row being read ([homeDescLines]).
				grows: in.desc, sub: nextUpSaidOn(in, item)}})
	}
	return homePanelRows{lines: lines, more: len(views) - shown}
}

// nextActive is every standing item that is still keeping its appointment, once
// each however many projects' bands hold it, in the world's project order so
// that two items due at the same moment always draw in the same order.
func nextActive(in *homeGridInput) []StandingItemView {
	var views []StandingItemView
	seen := map[string]bool{}
	for _, project := range in.world.Projects {
		for _, view := range in.items[project.Dir] {
			if view.Item.Status != standing.StatusActive || seen[view.Item.ID] {
				continue
			}
			seen[view.Item.ID] = true
			views = append(views, view)
		}
	}
	return views
}

// nextUpSaid is the sentence the description column draws for a `next up` row:
// the arrangement in the person's own words, and nothing at all where those
// words are the row's title already — the same sentence twice is the emptiness
// law's cousin, and the row has said it once.
func nextUpSaidOn(in *homeGridInput, item standing.Item) string {
	if !in.desc {
		return ""
	}
	return nextUpSaid(item)
}

func nextUpSaid(item standing.Item) string {
	said := strings.TrimSpace(item.When.Words)
	if said == "" || strings.EqualFold(said, strings.TrimSpace(item.Words)) {
		return ""
	}
	return said
}
