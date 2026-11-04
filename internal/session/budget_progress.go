package session

import (
	"context"
	"math"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// Live readings bridge measured usage to settlement. They are retired before
// billing, so the ledger and a still-open stream never count the same charge.
var budgetProgress = struct {
	sync.Mutex
	calls    map[*budgetCall]map[string]float64
	watchers map[chan struct{}]string
}{calls: make(map[*budgetCall]map[string]float64), watchers: make(map[chan struct{}]string)}

type budgetCall struct{ ledger, job string }

type budgetAdmissionKey struct{}

func (a *Agent) withBudgetProgress(ctx context.Context) (context.Context, func()) {
	call := &budgetCall{ledger: a.config.usageLedger, job: a.factoryJobName()}
	closed := false
	observe := func(id string, usage *ai.Usage) {
		budgetProgress.Lock()
		defer budgetProgress.Unlock()
		if closed {
			return
		}
		if usage == nil {
			delete(budgetProgress.calls[call], id)
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
		delete(budgetProgress.calls, call)
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
