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
	return store.AddJoinedMany(specs, silentTaskStart(ctx))
}

// taskRunCost bounds the whole tree even when no crew is routed for it.
func (a *Agent) taskRunCost(run *beltRun, cost float64) float64 {
	if run.delegate != nil {
		return cost
	}
	taskCap, _ := config.CrewTaskSpendCap(a.config.ProfileDir)
	spent := 0.0
	if run.store != nil {
		spent = storeSpent(run.store)
	}
	// Every dispatch passes here, including continuation. The ending names
	// the configured cap, while the engine receives only its unspent allowance.
	run.costCeiling = taskCap
	allowance, conversation := taskTreeCost(taskCap, spent, cost)
	run.conversationCostLimit = conversation
	return allowance
}

// taskTreeCost chooses the same remaining allowance for either task road.
func taskTreeCost(taskCap, spent, cost float64) (float64, bool) {
	left := runCostLeft(taskCap, spent)
	conversation := cost > 0 && cost <= left
	if cost <= 0 || left < cost {
		return left, conversation
	}
	return cost, conversation
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
