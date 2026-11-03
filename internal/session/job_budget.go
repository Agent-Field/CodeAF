package session

import (
	"context"
	"time"
)

// jobBudgetOf is shared by the whole owned family. Children charge their own
// provider receipts immediately; folding their usage home must not charge again.
func (a *Agent) jobBudgetOf() JobChat {
	if a.config.JobBudget != nil {
		return a.config.JobBudget
	}
	return a.jobChatOf()
}

func (a *Agent) jobBudgetBlock() error {
	if budget := a.jobBudgetOf(); budget != nil {
		if why := budget.Refusal(); why != "" {
			return spendRailReached{said: why}
		}
	}
	return nil
}

// Idle managers can still own detached commands or delegated work. Keep watching
// until the agent closes, not just until its foreground turn ends. No model call
// is involved. StopWork closes admission before cancelling the complete family.
func (a *Agent) watchJobBudget() {
	if a.jobBudgetOf() == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			a.mu.Lock()
			closed, stopped := a.closed, a.workStopped
			a.mu.Unlock()
			if closed {
				return
			}
			if !stopped {
				if a.jobBudgetBlock() != nil {
					_ = a.StopWork()
				} else if a.automaticFactoryRefusal() != "" {
					a.stopFactoryOwnedWork()
				}
			}
		}
	}()
}

// Check again at each model boundary, including retries and auxiliary calls.
func (a *Agent) admitJobCall(ctx context.Context) error {
	if err := factoryCallRefusal(ctx); err != nil {
		return err
	}
	if a.jobBudgetOf() == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.jobBudgetBlock()
}

// A job Stop cancels this manager's owned producers without closing the
// conversation or discarding the person's queued answers.
func (a *Agent) stopFactoryOwnedWork() {
 a.mu.Lock()
 jobs, graph := a.jobs, a.tasks
 cancel, automatic := a.cancel, a.running && a.factoryAutomaticTurn
 a.mu.Unlock()
 if automatic && cancel != nil { cancel(stopFor(StopByWorkStopped)) }
 if graph != nil {
  graph.mu.Lock()
  ids := append([]uint64(nil), graph.order...)
  graph.mu.Unlock()
  for _, id := range ids { _, _ = graph.stopFor(id, "") }
 }
 if jobs != nil {
  for _, job := range jobs.all() { if job.running() { _, _ = jobs.killFor(context.Background(), job.id) } }
 }
}
