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
	taskCap := a.runTaskCap(run)
	spent := 0.0
	if run.store != nil {
		spent = storeSpent(run.store)
	}
	// Every dispatch passes here, including continuation. The ending names
	// the cap the tree runs under, while the engine receives only its unspent
	// allowance.
	run.costMu.Lock()
	defer run.costMu.Unlock()
	run.treeSpent, run.treeOffer = spent, cost
	run.costCeiling = taskCap
	allowance, conversation := taskTreeCost(taskCap, spent, cost)
	run.conversationCostLimit = conversation
	return allowance
}

// runTaskCap is the per-task cap a tree runs under: the profile's, or the
// higher figure the person raised this task to.
//
// A RAISE THE PERSON GAVE IS THE CEILING. A raise reaches the task as the
// hand-off's wish ([crewWish.taskCap]) and, for a routed task, as its crew's
// guard, which a rerun's raise and an earlier live change also wrote. The tree
// ceiling read the profile's cap alone, so a task the person raised to $8
// stopped at $5 and told them to raise a limit they had just raised.
func (a *Agent) runTaskCap(run *beltRun) float64 {
	taskCap, _ := config.CrewTaskSpendCap(a.config.ProfileDir)
	return max(taskCap, run.taskCap, run.crew.guardTaskCap())
}

// treeAllowance is the engine's dollar allowance as it stands now: what the
// dispatch handed it, or more once the person raised the cap while the run
// was going ([RunSpec.CostCeiling]).
func (run *beltRun) treeAllowance() float64 {
	run.costMu.Lock()
	defer run.costMu.Unlock()
	allowance, _ := taskTreeCost(run.costCeiling, run.treeSpent, run.treeOffer)
	return allowance
}

// liveCostCeiling is the ceiling the engine reads while the run goes
// ([RunSpec.CostCeiling]); nil for a program's run, which keeps its own.
func (run *beltRun) liveCostCeiling() func() float64 {
	if run.delegate != nil {
		return nil
	}
	return run.treeAllowance
}

// raiseTaskCap lifts a running tree to the cap the person raised its task to.
// The engine reads the new allowance at its next spend reading, and the ending
// names the raised figure. A cap no higher than the tree's moves nothing.
func (run *beltRun) raiseTaskCap(capUSD float64) {
	if run == nil || run.delegate != nil {
		return
	}
	run.costMu.Lock()
	defer run.costMu.Unlock()
	if capUSD <= run.costCeiling {
		return
	}
	run.costCeiling = capUSD
	_, run.conversationCostLimit = taskTreeCost(capUSD, run.treeSpent, run.treeOffer)
}

// raiseLiveRunCap lifts the running tree whose crew the person just raised.
func (a *Agent) raiseLiveRunCap(crew *taskCrew, capUSD float64) {
	a.beltMu.Lock()
	run := a.beltRun
	a.beltMu.Unlock()
	if run != nil && crew != nil && run.crew == crew {
		run.raiseTaskCap(capUSD)
	}
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

// taskLimitWords is the per-task limit's sentence when the tree's ceiling
// ended the run, and nothing otherwise. A run the engine ended done is not a
// limit ending even when its final receipt crossed the ceiling (the engine
// keeps Limit cost on such a run): it finished, so nothing may say it stopped
// or hold back its completion.
func (run *beltRun) taskLimitWords(summary RunSummary) string {
	if run.delegate != nil || summary.Limit != RunLimitCost || summary.Outcome == beltRunOutcomeDone {
		return ""
	}
	run.costMu.Lock()
	ceiling, conversation := run.costCeiling, run.conversationCostLimit
	run.costMu.Unlock()
	if ceiling > 0 && !conversation {
		return config.CrewTaskCapAction(ceiling)
	}
	return ""
}

// taskLimitEnding is the summary a run's ending is told from. When the tree's
// per-task ceiling ended the run, the limit's sentence is the account the
// ending leads with, in the place a worker's failure stands
// ([runEndingWords]), exactly once.
//
// THE SENTENCE WAS ADDED, NOT PLACED. It was appended to whatever the ending
// already said, so a run the engine's ceiling cut with no call refused led with
// the generic "ran and did not finish" and kept the sentence on a second line,
// and a run whose root the crew's guard refused at the same limit said it
// twice: once as the worker's failure and again appended.
func (run *beltRun) taskLimitEnding(summary RunSummary) RunSummary {
	if words := run.taskLimitWords(summary); words != "" {
		summary.Failure = words
	}
	return summary
}

// stopCrewAtTaskLimit records the per-task limit as the routed crew's stop
// when the tree's ceiling ended the run. The crew's stop is what draws the
// sentence under the run's row and lists the task as needing the person: the
// engine's ceiling can end a run before any seat's call is refused, and then
// nothing else would say why it stopped or how to raise it.
func (run *beltRun) stopCrewAtTaskLimit(summary RunSummary) {
	if words := run.taskLimitWords(summary); words != "" {
		run.crew.stoppedOn(words)
	}
}

func (run *beltRun) outcomeNote(summary RunSummary, landing RunLanding, span time.Duration) string {
	return beltRunOutcomeNote(run.store, run.root, run.taskLimitEnding(summary), landing, span)
}
