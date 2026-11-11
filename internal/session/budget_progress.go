package session

import (
	"context"
	"sync"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// Track running calls only so the shared stop route can cancel them.
var budgetProgress = struct {
	sync.Mutex
	calls map[*budgetCall]struct{}
}{calls: make(map[*budgetCall]struct{})}

type budgetCall struct {
	owner       *Agent
	dailyExempt bool
	jobBudgeted bool
	epoch       uint64
	limit       error
	cancel      context.CancelCauseFunc
	replying    bool
	done        chan struct{}
}

type budgetAdmissionKey struct{}

func (a *Agent) withBudgetProgress(ctx context.Context, model string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	call := &budgetCall{owner: a, dailyExempt: a.dailyModelExempt(model), jobBudgeted: a.jobBudgetOf() != nil, cancel: cancel, done: make(chan struct{})}
	a.mu.Lock()
	call.epoch = a.workEpoch
	if a.closed {
		cancel(stopFor(StopByClosing))
	} else if a.workStopped {
		cancel(stopFor(StopByWorkStopped))
	}
	budgetProgress.Lock()
	budgetProgress.calls[call] = struct{}{}
	budgetProgress.Unlock()
	a.mu.Unlock()
	ctx = provider.WithReplyStarted(ctx, func() {
		budgetProgress.Lock()
		call.replying = true
		budgetProgress.Unlock()
	})
	ctx = context.WithValue(ctx, budgetAdmissionKey{}, a.admitModelCall)
	a.wakeBudgetForWork()
	return ctx, func() {
		// This retires only the completed call context, not its owning turn.
		cancel(context.Canceled)
		budgetProgress.Lock()
		delete(budgetProgress.calls, call)
		close(call.done)
		limit := call.limit
		budgetProgress.Unlock()
		// The shared drain observed this owner while its call was live. Its
		// own watcher may next see an idle agent, so retain that stop here.
		if limit != nil {
			_ = a.stopWorkEpoch(call.epoch, limit)
		}
	}
}

func (a *Agent) hasBudgetCall() bool {
	budgetProgress.Lock()
	defer budgetProgress.Unlock()
	for call := range budgetProgress.calls {
		if call.owner == a && call.cancel != nil && (!call.dailyExempt || call.jobBudgeted) {
			return true
		}
	}
	return false
}

func (a *Agent) cancelBudgetCalls() {
	budgetProgress.Lock()
	defer budgetProgress.Unlock()
	for call := range budgetProgress.calls {
		if call.owner == a && call.cancel != nil {
			call.cancel(stopFor(StopByWorkStopped))
		}
	}
}

// A daily stop drains replies across the capped family before cancelling parent
// lifetimes. Otherwise an idle owner's cancellation would cut its child's reply.
// The same lock orders the first token against the crossing: a waiting call
// either starts replying first or is cancelled, never both.
func dailyReplyDrain() []<-chan struct{} {
	budgetProgress.Lock()
	calls := make([]*budgetCall, 0, len(budgetProgress.calls))
	for call := range budgetProgress.calls {
		calls = append(calls, call)
	}
	budgetProgress.Unlock()
	var done []<-chan struct{}
	for _, call := range calls {
		if call.dailyExempt {
			continue
		}
		// Read authority outside the progress lock: the provider's first-token
		// callback must never wait for ledger or profile I/O.
		limit := call.owner.dailyBudgetBlock()
		if !IsDailyLimit(limit) {
			continue
		}
		budgetProgress.Lock()
		if _, live := budgetProgress.calls[call]; live {
			call.limit = limit
			if call.replying {
				done = append(done, call.done)
			} else {
				// The cause names the limit, so the caller reports it without
				// reading the ledger again.
				call.cancel(limit)
			}
		}
		budgetProgress.Unlock()
	}
	return done
}
