// Package run is the run: one supervisor that launches every ready task of a
// plan store and owns the lifecycle from first dispatch to the root's
// completion. The chat and the store's other writers are not part of this
// package; steering reaches a run only through the store, as one more writer
// among the workers.
package run

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// Report is what a worker hands back when its task ends well. Result is the
// task's own account of itself and lands in the store verbatim; Steps and USD
// feed the run's counters, and USD in particular feeds the shared cost
// counter the Limits govern.
type Report struct {
	Result string
	Steps  int
	USD    float64
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

// Limits bound a run from the outside. Both fields are optional: a CostUSD
// of zero (or less) sets no cost limit, and a StepsPerTask of zero hands the
// worker no cap.
type Limits struct {
	// CostUSD is what the whole run may spend, as the sum of every Report's
	// USD. When the counter has reached it no new worker starts and the run
	// ends on the limit word of the outcome ladder.
	CostUSD float64
	// StepsPerTask is handed to every worker through its context, so the loop
	// a worker hosts can cap itself without the supervisor counting its steps.
	StepsPerTask int
}

// stepsPerTaskKey is the type behind the context value, so a worker reads its
// cap with a typed lookup rather than a string key another package could
// collide with.
type stepsPerTaskKey struct{}

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
