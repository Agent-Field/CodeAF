package runs

import (
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func (e *engine) precheck(appID string, app *spec.App, def *spec.Run) (float64, error) {
	e.crewsBudget(appID, app)
	capUSD := 0.0
	if app != nil {
		capUSD = app.Budget.PerRun
	}
	if def != nil && def.Budget != nil && def.Budget.PerRun > 0 {
		capUSD = def.Budget.PerRun
	}
	month := 0.0
	if app != nil {
		month = app.Budget.Month
	}
	if month > 0 && capUSD > 0 && e.committed(appID)+capUSD > month+1e-9 {
		return 0, bridge.Errorf(bridge.CodeBudget, fmt.Sprintf("up to $%.2f", capUSD))
	}
	return capUSD, nil
}

func (e *engine) room(job *job, add float64) bool {
	next := job.State.Cost.Spent + add
	if capUSD := job.State.Cost.Cap; capUSD > 0 && next > capUSD+1e-9 {
		return false
	}
	if job.Month > 0 && e.committedExcept(job)+next > job.Month+1e-9 {
		return false
	}
	return true
}

// committed is what this month already holds: finished runs by what they
// spent, and a run still going by its full per-run cap.
func (e *engine) committed(app string) float64 {
	return e.committedSkip(app, "")
}

func (e *engine) committedExcept(job *job) float64 {
	return e.committedSkip(job.State.App, job.State.ID)
}

func (e *engine) committedSkip(app, skip string) float64 {
	jobs, err := e.loadJobs(app)
	if err != nil {
		return 0
	}
	month := e.clock().UTC().Format("2006-01")
	var total float64
	for _, item := range jobs {
		if item.State.ID == skip || !sameMonth(item.State.StartedAt, month) {
			continue
		}
		used := item.State.Cost.Spent
		if !terminal(item.State.Status) && item.State.Cost.Cap > used {
			used = item.State.Cost.Cap
		}
		total += used
	}
	return total
}

func sameMonth(started, month string) bool {
	when, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return true
	}
	return when.UTC().Format("2006-01") == month
}
