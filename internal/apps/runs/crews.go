package runs

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func (e *engine) crewsAllow(app, trigger string) bool {
	if e.all == nil || e.all.Crews.TriggerGate == nil {
		return true
	}
	return e.all.Crews.TriggerGate.Allow(app, trigger, e.clock())
}

// The effective cap stays on the run's definition so start journals the same
// month that precheck used. Store definitions are loaded for each new run.
func (e *engine) crewsBudget(appID string, app *spec.App) {
	if app == nil || e.all == nil || e.all.Crews.BudgetCap == nil {
		return
	}
	if month, ok := e.all.Crews.BudgetCap.Month(appID); ok {
		app.Budget.Month = month
	}
}

func (e *engine) crewsFire(ctx context.Context, app string, trigger spec.Trigger, diff bridge.RecordDiff) {
	if e.crewsAllow(app, trigger.ID) {
		e.fire(ctx, app, trigger, diff)
	}
}
