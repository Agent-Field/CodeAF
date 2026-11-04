package session

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// JobBudgetUnreadable refuses new paid calls without cancelling existing work.
const JobBudgetUnreadable = "job budget could not be read; new work held"

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
		why := ""
		if live, ok := budget.(JobLiveBudget); ok {
			why = live.RefusalWithSpend(pendingBudgetSpend(a.config.usageLedger, live.FactoryJob()))
		} else {
			why = budget.Refusal()
		}
		if why != "" {
			return spendRailReached{said: why}
		}
	}
	return nil
}

// Idle managers can still own detached commands or delegated work. Keep watching
// until the agent closes, not just until its foreground turn ends. No model call
// is involved. StopWork closes admission before cancelling the complete family.
func (a *Agent) watchJobBudget() {
	changed := make(chan struct{}, 1)
	unwatch := OnUsageWritten(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	budgetProgress.Lock()
	budgetProgress.watchers[changed] = a.config.usageLedger
	budgetProgress.Unlock()
	closed := a.ClosedSignal()
	go func() {
		defer unwatch()
		defer func() { budgetProgress.Lock(); defer budgetProgress.Unlock(); delete(budgetProgress.watchers, changed) }()
		var ticks <-chan time.Time
		if a.jobBudgetOf() != nil {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			ticks = ticker.C
		}
		for {
			ledgerChanged := false
			select {
			case <-closed:
				return
			case <-changed:
				ledgerChanged = true
			case <-ticks:
			}
			a.mu.Lock()
			stopped, epoch, running := a.workStopped, a.workEpoch, a.running
			a.mu.Unlock()
			if stopped {
				continue
			}
			if !running && !a.hasBudgetCall() && !a.ownedWorkIsMoving() {
				continue
			}
			stopOwned := a.factoryOwnedStop()
			var why error
			if ledgerChanged {
				why = a.dailyBudgetBlock()
			}
			if why == nil {
				why = a.jobBudgetBlock()
			}
			if errors.Is(why, ErrSpendRail) && why.Error() != JobBudgetUnreadable {
				_ = a.stopWorkEpoch(epoch, why)
			} else if a.automaticFactoryRefusal() == factory.PersonStopReason {
				stopOwned()
			}
		}
	}()
}

// Daily checks run on recorded spending and at admission, never on the job
// watcher's clock. Each agent reads its captured ledger and selected authority.
func (a *Agent) dailyBudgetBlock() error {
	if a.config.SpendPreauthorized {
		return nil
	}
	capUSD, err := a.dailyCeiling()
	if err != nil {
		return spendUnverified{said: spendLimitUnread}
	}
	if capUSD <= 0 {
		return nil
	}
	spent, err := DailySpendUSD(a.config.usageLedger, time.Now())
	if err != nil {
		return spendUnverified{said: spendAccountingUnread}
	}
	spent += pendingBudgetSpend(a.config.usageLedger, "")
	if spent >= capUSD {
		return dailyLimitReached(spent, capUSD)
	}
	return nil
}

// Check again at each model boundary, including retries and auxiliary calls.
func (a *Agent) admitJobCall(ctx context.Context) error {
	return a.admitModelCall(ctx, "")
}

func (a *Agent) admitModelCall(ctx context.Context, model string) error {
	if err := factoryCallRefusal(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if model != "" && a.config.ModelPrice != nil {
		input, output, known := a.config.ModelPrice(model)
		if known && input <= 0 && output <= 0 {
			return a.jobBudgetBlock()
		}
	}
	if err := a.dailyBudgetBlock(); err != nil {
		return err
	}
	return a.jobBudgetBlock()
}

// A job Stop cancels this manager's owned producers without closing the
// conversation or discarding the person's queued answers.
// Capture producers before sampling authority, so a late answer cannot cancel
// work admitted by a subsequent Start.
func (a *Agent) factoryOwnedStop() func() {
	a.mu.Lock()
	jobs, graph := a.jobs, a.tasks
	cancel, automatic := a.cancel, a.running && a.factoryAutomaticTurn
	a.mu.Unlock()
	tasks := map[uint64]<-chan struct{}{}
	if graph != nil {
		graph.mu.Lock()
		for _, id := range graph.order {
			if node := graph.nodes[id]; node != nil {
				tasks[id] = node.done
			}
		}
		graph.mu.Unlock()
	}
	var owned []*job
	if jobs != nil {
		owned = jobs.all()
	}
	return func() {
		if automatic && cancel != nil {
			cancel(stopFor(StopByWorkStopped))
		}
		for id, done := range tasks {
			_, _ = graph.stopAttempt(id, "", done)
		}
		var stopped sync.WaitGroup
		for _, job := range owned {
			if job.running() {
				stopped.Add(1)
				go func(id int) {
					defer stopped.Done()
					_, _ = jobs.killFor(context.Background(), id)
				}(job.id)
			}
		}
		stopped.Wait()
	}
}
