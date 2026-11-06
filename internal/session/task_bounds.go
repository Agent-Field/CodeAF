package session

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

type silentTaskStartKey struct{}

func withSilentTaskStart(ctx context.Context, silent bool) context.Context {
	return context.WithValue(ctx, silentTaskStartKey{}, silent)
}

func silentTaskStart(ctx context.Context) bool {
	silent, _ := ctx.Value(silentTaskStartKey{}).(bool)
	return silent
}

func addJoinedTasks(ctx context.Context, store *plandb.Store, specs []plandb.TaskSpec) ([]*plandb.Task, error) {
	if silentTaskStart(ctx) {
		return store.AddManyBounded(specs)
	}
	return store.AddMany(specs)
}

// taskRunCost bounds the whole tree even when no crew is routed for it.
func (a *Agent) taskRunCost(run *beltRun, cost float64) float64 {
	if run.delegate != nil {
		return cost
	}
	taskCap, _ := config.CrewTaskSpendCap(a.config.ProfileDir)
	if cost <= 0 || taskCap < cost {
		return taskCap
	}
	return cost
}

func (a *Agent) holdTaskCeiling(run *beltRun, spec RunSpec) {
	taskCap, _ := config.CrewTaskSpendCap(a.config.ProfileDir)
	run.costCeiling = spec.CostUSD
	left := runCostLeft(a.railCap(0), a.Usage().CostUSD)
	run.conversationCostLimit = left > 0 && left <= taskCap
}

func (run *beltRun) taskLimitWords(summary RunSummary) string {
	if run.delegate == nil && summary.Limit == RunLimitCost && run.costCeiling > 0 && !run.conversationCostLimit {
		return config.CrewTaskCapAction(run.costCeiling)
	}
	return ""
}

func (run *beltRun) outcomeNote(summary RunSummary, landing RunLanding, span time.Duration) string {
	line := beltRunOutcomeNote(run.store, run.root, summary, landing, span)
	return withReport(line, run.taskLimitWords(summary))
}
