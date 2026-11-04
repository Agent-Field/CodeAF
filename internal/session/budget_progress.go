package session

import (
	"context"
	"math"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// Live readings bridge measured usage to settlement, including pending receipts.
var budgetProgress = struct {
	sync.Mutex
	calls    map[*budgetCall]map[string]float64
	watchers map[chan struct{}]string
}{calls: make(map[*budgetCall]map[string]float64), watchers: make(map[chan struct{}]string)}

type budgetCall struct {
	ledger, job string
	owner       *Agent
	cancel      context.CancelFunc
}

type budgetAdmissionKey struct{}

func (a *Agent) withBudgetProgress(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	call := &budgetCall{ledger: a.config.usageLedger, job: a.factoryJobName(), owner: a, cancel: cancel}
	a.mu.Lock()
	if a.workStopped || a.closed {
		cancel()
	}
	budgetProgress.Lock()
	budgetProgress.calls[call] = nil
	budgetProgress.Unlock()
	a.mu.Unlock()
	closed := false
	observe := func(id string, usage *ai.Usage) {
		budgetProgress.Lock()
		defer budgetProgress.Unlock()
		if closed && usage != nil {
			return
		}
		if usage == nil {
			delete(budgetProgress.calls[call], id)
			if closed && len(budgetProgress.calls[call]) == 0 {
				delete(budgetProgress.calls, call)
			}
			return
		}
		if usage.Cost == nil || *usage.Cost <= 0 || math.IsNaN(*usage.Cost) || math.IsInf(*usage.Cost, 0) {
			return
		}
		if budgetProgress.calls[call] == nil {
			budgetProgress.calls[call] = make(map[string]float64)
		}
		budgetProgress.calls[call][id] = *usage.Cost
		for changed, ledger := range budgetProgress.watchers {
			if ledger == call.ledger {
				select {
				case changed <- struct{}{}:
				default:
				}
			}
		}
	}
	ctx = context.WithValue(ctx, budgetAdmissionKey{}, a.admitJobCall)
	return provider.WithUsageProgress(ctx, observe), func() {
		budgetProgress.Lock()
		defer budgetProgress.Unlock()
		closed = true
		cancel()
		call.cancel = nil
		if len(budgetProgress.calls[call]) == 0 {
			delete(budgetProgress.calls, call)
		}
	}
}

func pendingBudgetSpend(ledger, job string) float64 {
	budgetProgress.Lock()
	defer budgetProgress.Unlock()
	var total float64
	for call, attempts := range budgetProgress.calls {
		if call.ledger == ledger && (job == "" || call.job == job) {
			for _, cost := range attempts {
				total += cost
			}
		}
	}
	return total
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
