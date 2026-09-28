//go:build !windows

package app

import (
	"context"
	"fmt"
	"math"

	"github.com/Agent-Field/codeaf/internal/seniordev/session/runbudget"
)

// initializeRun sets the run's starting verdict and budget. The verdict starts
// at "crashed" so a process that dies before its terminal event is reported as
// having died, not as having quietly produced nothing.
func (runner *pipeline) initializeRun() pipelineResult {
	result := pipelineResult{Status: "crashed", WallStart: runner.wallStart}
	runner.budgetRun = runbudget.MakeBudgetTracker(
		runner.budget, float64(runner.wallStart.UnixMilli()), runner.priorCost,
	)
	runner.budgetCost = 0
	runner.noteRunBudget()
	return result
}

func (runner *pipeline) noteRunBudget() {
	if !runbudget.IsBounded(runner.budget) {
		return
	}
	cost := "cost=unbounded"
	if runner.budget.MaxCostUSD != nil {
		cost = fmt.Sprintf("maxCost=$%v", *runner.budget.MaxCostUSD)
	}
	wall := "wall=unbounded"
	if runner.budget.MaxWallMS != nil {
		wall = fmt.Sprintf("maxWall=%vh", math.Round(*runner.budget.MaxWallMS/36_000)/100)
	}
	restored := ""
	if runner.priorCost > 0 {
		restored = fmt.Sprintf(" (restored: $%.4f already spent)", runner.priorCost)
	}
	runner.note("[senior-dev] run budget: " + cost + " " + wall + restored + "\n")
}

func (runner *pipeline) prepareRunBase(
	ctx context.Context, result *pipelineResult,
) (string, bool, error) {
	if err := runner.prepareWorkspace(ctx); err != nil {
		return "", false, err
	}
	if exhausted, reason := runner.budgetExhausted(); exhausted {
		result.Status, result.Reason = "budget-exhausted", reason
		result.CostUSD = runner.totalCost()
		return "", true, nil
	}
	baseSHA, err := runner.resolveRunBase(ctx)
	if err != nil {
		return "", false, err
	}
	result.BaseSHA = baseSHA
	return baseSHA, false, nil
}
