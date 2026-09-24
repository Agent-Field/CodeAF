// Package run is the run: one supervisor that launches every ready task of a
// plan store and owns the lifecycle from first dispatch to the root's
// completion. The chat and the store's other writers are not part of this
// package; steering reaches a run only through the store, as one more writer
// among the workers.
package run

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// Report is what a worker hands back when its task ends well. Result is the
// task's own account of itself and lands in the store verbatim; Steps and USD
// feed the run's counters, and USD in particular feeds the shared cost
// counter the Limits govern.
//
// WAITING IS NOT A RESULT. A worker that called `plandb wait` has not finished
// its task: it parked it, the store released its claim, and it is owed a wake
// when a dependency or a child moves. Such a worker comes home with Waiting
// set and no Result, and the supervisor leaves the task open rather than
// writing a completion.
type Report struct {
	Result  string
	Steps   int
	USD     float64
	Waiting bool
}

// Worker is one task's executor. The supervisor never talks to a model
// itself: it launches a Worker per claimed task, hands it the task as the
// store recorded it, and writes the answer back under the name the task was
// claimed with. A Worker that cannot finish returns an error and the task
// fails; a Worker that finishes returns a Report and its result is written.
type Worker interface {
	Run(ctx context.Context, task plandb.Task) (Report, error)
}

// WorkerFactory is how the supervisor makes a Worker. The factory sees the
// task before the worker does, so a real factory resolves whatever the task
// carries — its role, its kind — into the seat that runs it. It is called
// once per launch, on the supervisor's own goroutine, and may return nil to
// say no seat exists for this task; the task then fails rather than hangs.
type WorkerFactory func(task plandb.Task) Worker

// Limits bound a run from the outside. Every field is optional: a CostUSD or
// Elapsed of zero (or less) sets no corresponding run limit, a StepsPerTask
// of zero hands the worker no cap, and ReviewRound's false is the run every caller had before it.
type Limits struct {
	// CostUSD is what the whole run may spend. Banked calls count while a worker
	// is still working and reconcile with its final Report. When the counter has
	// reached it no new worker starts, work in flight ends, and the run ends on
	// the limit word of the outcome ladder.
	CostUSD float64
	// Elapsed is how long the whole run may remain active. When it passes,
	// workers already in flight are ended and drained, no new worker starts,
	// and the run ends on the same limit word as the cost counter.
	Elapsed time.Duration
	// StepsPerTask is handed to every worker through its context, so the loop
	// a worker hosts can cap itself without the supervisor counting its steps.
	StepsPerTask int
	// StaleAfter is how long a claim may go untouched before a pass takes it
	// over: a claimed task whose owning process has not been seen for this
	// long is released so the ready set offers it again. It is a field here
	// rather than an environment variable because it bounds how long a run
	// waits on a process that may have died, and zero takes the default
	// (defaultStaleAfter) rather than meaning "no stale claim ever".
	StaleAfter time.Duration
	// ReviewRound turns the review round on. When it is set, a work-seat leaf
	// that lands done spawns one check task under its parent — whether the leaf's
	// own `plandb done` wrote the ending or this run did — and a check whose
	// result begins "does not hold" leaves its sentence as a note on the leaf it
	// read AND adds a `fix:` task under that leaf's parent which the run waits on.
	// A `fix:` task is checked in turn, but a finding on one is a note and no
	// second fix task, so a run cannot loop. It is a bool defaulting false so
	// every caller that does not ask for it keeps the run it had — no check
	// tasks, nothing new on the plan.
	ReviewRound bool
}

// costDust is the most a dollar limit may still have left and be reached: a
// billionth of a dollar, far below any call's price and far above the float
// rounding in a sum of prices. The model API a program's calls go through
// reads its ceiling the same way (internal/provider/modelapi's ceilingReached).
const costDust = 1e-9

// costReached reports whether a run's spend has reached its dollar limit.
//
// A LIMIT WITH NOTHING LEFT IS REACHED WITH NOTHING SPENT. The conversation
// hands a run whose person's limit is already spent the smallest positive
// figure, because zero means no limit at all; read as `spent >= limit`,
// nothing spent was still under it, and a run whose program's first call was
// refused for it ended as work that did not finish instead of on the limit the
// person set.
func (l Limits) costReached(spent float64) bool {
	return l.CostUSD > 0 && l.CostUSD-spent <= costDust
}

// stepsPerTaskKey is the type behind the context value, so a worker reads its
// cap with a typed lookup rather than a string key another package could
// collide with.
type stepsPerTaskKey struct{}

// spendBankKey carries the run-owned observer of a worker banking cumulative
// spend. The cumulative figure is reconciled with the worker return, so the
// live limit and the final receipt share one account.
type spendBankKey struct{}

// WithSpendBank returns a context that reports cumulative spend as a worker
// banks calls. Workers without this property remain valid and report at return.
func WithSpendBank(ctx context.Context, bank func(float64)) context.Context {
	return context.WithValue(ctx, spendBankKey{}, bank)
}

// bankSpend publishes the cumulative spend banked by this worker.
func bankSpend(ctx context.Context, usd float64) {
	if bank, _ := ctx.Value(spendBankKey{}).(func(float64)); bank != nil {
		bank(usd)
	}
}

// wakeClauseKey is the type behind the context value that carries a woken
// parent's resume clause, for the same reason as the step cap beside it: a
// typed lookup no other package can collide with.
type wakeClauseKey struct{}

// WithWakeClause returns a context carrying the resume clause a woken parent's
// worker opens with — the clause naming every child that landed and what to do
// with them (internal/run's supervisor composes it). A worker that ignores it
// is one no wake reached, the way an empty clause is no wake at all.
func WithWakeClause(ctx context.Context, clause string) context.Context {
	return context.WithValue(ctx, wakeClauseKey{}, clause)
}

// WakeClause answers the resume clause carried by a context the supervisor
// built for a wake, and "" for every other worker — the ordinary launch, whose
// opening carries the trajectory's own resume sentence instead.
func WakeClause(ctx context.Context) string {
	clause, _ := ctx.Value(wakeClauseKey{}).(string)
	return clause
}

// WithStepsPerTask returns a context that carries the cap a worker should
// hold itself to. The supervisor wraps every worker's context with it; a
// worker that ignores it is uncapped, not broken.
func WithStepsPerTask(ctx context.Context, steps int) context.Context {
	return context.WithValue(ctx, stepsPerTaskKey{}, steps)
}

// StepsPerTask answers the cap carried by a context the supervisor built, and
// zero when there is none — zero being the run's word for "no cap".
func StepsPerTask(ctx context.Context) int {
	steps, _ := ctx.Value(stepsPerTaskKey{}).(int)
	return steps
}
