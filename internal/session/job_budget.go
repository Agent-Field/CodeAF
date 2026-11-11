package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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
		why := budget.Refusal()
		if why != "" {
			return spendRailReached{said: why}
		}
	}
	return nil
}

// budgetWatchActive is whether the watcher must poll: work that can spend is
// moving and a job or daily cap could stop it. Without a cap it never polls.
func (a *Agent) budgetWatchActive() bool {
	if a.toolsInFlight.Load() == 0 && !a.hasBudgetCall() && !a.ownedWorkIsMoving() {
		return false
	}
	return a.jobBudgetOf() != nil || a.dailyCeilingUSD() > 0
}

// wakeBudgetForWork rechecks recorded spending when an owned producer starts.
func (a *Agent) wakeBudgetForWork() {
	select {
	case a.budgetWorkStarted <- struct{}{}:
	default:
	}
}

// budgetPollEvery bounds ledger detection while capped work is moving.
const budgetPollEvery = 100 * time.Millisecond

// Idle managers can still own detached commands or delegated work. Keep watching
// until the agent closes, not just until its foreground turn ends. No model call
// is involved. StopWork closes admission before cancelling the complete family.
func (a *Agent) watchJobBudget() {
	a.checkInitialJobBudget()
	a.budgetWatchOnce.Do(a.startBudgetWatch)
	a.wakeBudgetForWork()
}

func (a *Agent) startBudgetWatch() {
	// THE LEDGER'S WRITER ONLY BUMPS A COUNTER; THIS GOROUTINE POLLS IT. The
	// writer is process-wide, so a channel send from it would cross into
	// whatever scheduler owns this agent (a synctest bubble in tests). The
	// cost is one poll interval of latency between a write and the stop.
	var writes atomic.Uint64
	unwatch := OnUsageWritten(func() { writes.Add(1) })
	closed := a.ClosedSignal()
	go func() {
		defer unwatch()
		// AN IDLE AGENT HOLDS NO TIMER. The poll runs only while this agent has
		// paid or owned work moving under a cap that could stop it; every start
		// of such work wakes it ([Agent.wakeBudgetForWork]).
		var ticker *time.Ticker
		defer func() {
			if ticker != nil {
				ticker.Stop()
			}
		}()
		seen := writes.Load()
		dailyCap := a.dailyCeilingUSD()
		for {
			var ticks <-chan time.Time
			if a.budgetWatchActive() {
				if ticker == nil {
					ticker = time.NewTicker(budgetPollEvery)
				}
				ticks = ticker.C
			} else if ticker != nil {
				ticker.Stop()
				ticker = nil
			}
			ledgerChanged := false
			select {
			case <-closed:
				return
			case <-a.budgetWorkStarted:
				ledgerChanged = true
			case <-ticks:
			}
			if n := writes.Load(); n != seen {
				seen, ledgerChanged = n, true
			}
			// A profile edit can cross the cap without another usage receipt.
			if cap := a.dailyCeilingUSD(); cap != dailyCap {
				dailyCap, ledgerChanged = cap, true
			}
			a.mu.Lock()
			stopped, epoch := a.workStopped, a.workEpoch
			a.mu.Unlock()
			if stopped {
				continue
			}
			// A turn marked running may still be waiting for its first admission.
			// Only producers already in flight belong to this watcher.
			if a.toolsInFlight.Load() == 0 && !a.hasBudgetCall() && !a.ownedWorkIsMoving() {
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
			} else if a.watchedFactoryRefusal() == factory.PersonStopReason {
				stopOwned()
			}
		}
	}()
}

// checkInitialJobBudget warms the job snapshot before the watcher starts. An
// idle conversation reports a refusal through its turn gate; stopping it here
// would race the first submission and turn that refusal into a lifecycle error.
func (a *Agent) checkInitialJobBudget() {
	why := a.jobBudgetBlock()
	if why == nil || why.Error() == JobBudgetUnreadable || !a.budgetWatchActive() {
		return
	}
	a.mu.Lock()
	epoch := a.workEpoch
	a.mu.Unlock()
	_ = a.stopWorkEpoch(epoch, why)
}

// Daily checks read settled ledger changes and new-work admission, not interim
// stream usage. Each agent reads its captured ledger and selected authority.
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
	now := time.Now()
	spent, err := DailySpendUSD(a.config.usageLedger, now)
	if err != nil {
		return spendUnverified{said: spendAccountingUnread}
	}
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
	// The transport owns context cancellation. Budget admission must preserve
	// uncapped auxiliary calls' existing cancellation and cleanup path.
	if a.dailyModelExempt(model) {
		return a.jobBudgetBlock()
	}
	if err := a.dailyBudgetBlock(); err != nil {
		return err
	}
	return a.jobBudgetBlock()
}

// Known-free calls keep their daily exemption through admission and draining.
func (a *Agent) dailyModelExempt(model string) bool {
	if model == "" || a.config.ModelPrice == nil {
		return false
	}
	input, output, known := a.config.ModelPrice(model)
	return known && input <= 0 && output <= 0
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
