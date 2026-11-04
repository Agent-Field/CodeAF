package session

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// Live readings bridge measured usage to settlement, including pending receipts.
var budgetProgress = struct {
	sync.Mutex
	calls    map[*budgetCall]map[string]budgetReading
	watchers map[chan struct{}]string
	now      func() time.Time
}{calls: make(map[*budgetCall]map[string]budgetReading), watchers: make(map[chan struct{}]string), now: time.Now}

type budgetReading struct {
	cost     float64
	day      string
	dayStart float64
}

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
			budgetProgress.calls[call] = make(map[string]budgetReading)
		}
		reading := budgetProgress.calls[call][id]
		day := budgetProgress.now().Format(usageDayLayout)
		if day > reading.day {
			reading.day = day
			reading.dayStart = reading.cost
		}
		reading.cost = *usage.Cost
		budgetProgress.calls[call][id] = reading
		for changed, ledger := range budgetProgress.watchers {
			if ledger == call.ledger {
				select {
				case changed <- struct{}{}:
				default:
				}
			}
		}
	}
	ctx = context.WithValue(ctx, budgetAdmissionKey{}, a.admitModelCall)
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
	return pendingBudgetSpendOnDay(ledger, job, "")
}

func pendingBudgetSpendOnDay(ledger, job, day string) float64 {
	budgetProgress.Lock()
	defer budgetProgress.Unlock()
	var total float64
	for call, attempts := range budgetProgress.calls {
		if call.ledger == ledger && (job == "" || call.job == job) {
			for _, reading := range attempts {
				if day == "" {
					total += reading.cost
				} else if reading.day == day {
					total += reading.cost - reading.dayStart
				}
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
