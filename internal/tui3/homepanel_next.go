package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// nextPanel is `automations`: what this machine will do on the clock, soonest
// first — one row per automation that is still keeping its appointment, its
// title the person's own name for it and its one sentence the same one the
// automations place draws (automationsplace.go's [automationRowSaid]). Every row
// opens the place on that automation, where it can be run, paused or changed.
type nextPanel struct{ homePanelBase }

func (nextPanel) rows(in *homeGridInput) homePanelRows {
	upcoming := nextAutomations(in.autos)
	shown := min(in.cap(panelNext), len(upcoming))
	lines := make([]homeLine, 0, shown)
	for _, item := range upcoming[:shown] {
		// A ROW IS THE PERSON'S OWN WORDS AND NOTHING AT ITS RIGHT (owner,
		// 2026-09-15): the row's time is said ONCE, in its description, under
		// the cursor — and at every width, as the second line under the cursor's
		// row where the description column does not fit.
		lines = append(lines, homeLine{kind: homeLedger, project: placeAutomationsWord, dir: item.ID,
			folder: item.Workspace, cell: &homeCell{panel: panelNext,
				title: strings.TrimSpace(item.Title),
				grows: true, sub: automationRowSaid(item, in.active, in.now)}})
	}
	return homePanelRows{lines: lines, more: len(upcoming) - shown}
}

// nextAutomations is every automation that will wake again, soonest first. A
// paused or finished one is not coming up, and the place is where it is kept.
func nextAutomations(all []automation.Automation) []automation.Automation {
	var upcoming []automation.Automation
	for _, item := range all {
		if item.Status == automation.StatusActive && !item.Next.IsZero() {
			upcoming = append(upcoming, item)
		}
	}
	for i := 1; i < len(upcoming); i++ {
		for j := i; j > 0 && upcoming[j].Next.Before(upcoming[j-1].Next); j-- {
			upcoming[j], upcoming[j-1] = upcoming[j-1], upcoming[j]
		}
	}
	return upcoming
}
