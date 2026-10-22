package session

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// The ledger cannot name a model until a priced call was saved. The run's
// checkpoint already records its accepted seats, including when recovery was
// refused after an interrupted call. Read that policy without rerouting it or
// borrowing the profile's current settings. Children share their run's crew.
func (a *Agent) planTaskModels(root string) []PlanTaskModel {
	g := a.tasker()
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, notice := range g.runRowsLocked() {
		if notice.PlanTask != planStoreID(root) {
			continue
		}
		var out []PlanTaskModel
		for _, seat := range crewroute.Seats {
			model := ""
			if notice.Crew != nil {
				model = notice.Crew.Seat(seat).Model
			}
			if saved := notice.CrewState; saved != nil && model == "" {
				if saved.Routed {
					model = saved.Decision.Seat(seat).Model
				} else {
					model = map[crewroute.Seat]string{crewroute.Worker: saved.Work, crewroute.Planner: saved.Plan, crewroute.Checker: saved.Check}[seat]
				}
			}
			if seat == crewroute.Worker && model == "" {
				model = notice.Model
			}
			if model = strings.TrimSpace(model); model != "" {
				out = append(out, PlanTaskModel{Seat: string(seat), Model: model})
			}
		}
		return out
	}
	return nil
}
