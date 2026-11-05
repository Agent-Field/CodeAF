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
	owner    *Agent
	cancel   context.CancelFunc
	replying bool
	done     chan struct{}
}

type budgetAdmissionKey struct{}

func (a *Agent) withBudgetProgress(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	call := &budgetCall{owner: a, cancel: cancel, done: make(chan struct{})}
	a.mu.Lock()
	if a.workStopped || a.closed {
		cancel()
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
	return ctx, func() {
		cancel()
		budgetProgress.Lock()
		delete(budgetProgress.calls, call)
		close(call.done)
		budgetProgress.Unlock()
	}
}

func (a *Agent) hasBudgetCall() bool {
	budgetProgress.Lock()
	defer budgetProgress.Unlock()
	for call := range budgetProgress.calls {
		if call.owner == a && call.cancel != nil {
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
			call.cancel()
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
		// Read authority outside the progress lock: the provider's first-token
		// callback must never wait for ledger or profile I/O.
		if !IsDailyLimit(call.owner.dailyBudgetBlock()) {
			continue
		}
		budgetProgress.Lock()
		if _, live := budgetProgress.calls[call]; live {
			if call.replying {
				done = append(done, call.done)
			} else {
				call.cancel()
			}
		}
		budgetProgress.Unlock()
	}
	return done
}
