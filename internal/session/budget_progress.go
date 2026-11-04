package session

import (
	"context"
	"sync"
)

// Track running calls only so the shared stop route can cancel them.
var budgetProgress = struct {
	sync.Mutex
	calls map[*budgetCall]struct{}
}{calls: make(map[*budgetCall]struct{})}

type budgetCall struct {
	owner  *Agent
	cancel context.CancelFunc
}

type budgetAdmissionKey struct{}

func (a *Agent) withBudgetProgress(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	call := &budgetCall{owner: a, cancel: cancel}
	a.mu.Lock()
	if a.workStopped || a.closed {
		cancel()
	}
	budgetProgress.Lock()
	budgetProgress.calls[call] = struct{}{}
	budgetProgress.Unlock()
	a.mu.Unlock()
	ctx = context.WithValue(ctx, budgetAdmissionKey{}, a.admitModelCall)
	return ctx, func() {
		cancel()
		budgetProgress.Lock()
		delete(budgetProgress.calls, call)
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
