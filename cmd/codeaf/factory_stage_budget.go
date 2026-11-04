package main

import (
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// One live meter per round; descendants share it. The store is the worker's
// authoritative mirror, also read by the manager's meter, not a copied cap.
type stageBudget struct {
	floor       *store.Store
	memoryOwner *store.Store
	item        factory.Item
	stage       string
	round       int
	mu          sync.Mutex
	spent       float64
	failed      bool
	stopped     string
	reached     bool
}

func (b *stageBudget) FactoryAuto() bool {
	if b.floor == nil {
		return false
	}
	it, err := b.floor.Get(b.item.ID)
	return err == nil && factory.Automatic(it)
}

// FactoryJob names the job on the ledger rows of every call this step makes.
func (b *stageBudget) FactoryJob() string { return factory.LedgerName(b.item) }

func (b *stageBudget) Spend(usd float64) { b.book(usd, false, "", 0, 0, 0) }

// SpendModel books one priced call and the model that answered it.
func (b *stageBudget) SpendModel(usd float64, model string, calls, input, output int) {
	b.book(usd, false, model, calls, input, output)
}

// SpendMemory records optional learning separately inside the same job total.
func (b *stageBudget) SpendMemory(usd float64) { b.book(usd, true, "", 0, 0, 0) }

// SpendMemoryModel is SpendMemory with the model that answered the call.
func (b *stageBudget) SpendMemoryModel(usd float64, model string, calls, input, output int) {
	b.book(usd, true, model, calls, input, output)
}

func (b *stageBudget) book(usd float64, memory bool, model string, calls, input, output int) {
	if usd <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spent += usd
	if b.floor == nil {
		b.failed = true
		return
	}
	if err := b.floor.Annotate(b.item.ID, func(it *factory.Item) error {
		if it.Repo != b.item.Repo {
			return errNotThisChat
		}
		if memory {
			factory.AddMemorySpend(it, usd)
		} else {
			factory.AddChatSpend(it, usd)
		}
		factory.AddModelUse(it, model, usd, calls, int64(input), int64(output))
		if it.Stream != nil {
			for i := range it.Stream.Phases {
				ph := &it.Stream.Phases[i]
				if ph.Name == b.stage && b.stage != "" {
					ph.RecordRoundSpend(b.round, ph.RoundCosts[b.round]+usd)
					break
				}
			}
		}
		return nil
	}); err != nil {
		b.failed = true
	}
}
func (b *stageBudget) Refusal() string { return b.RefusalWithSpend(0) }

func (b *stageBudget) RefusalWithSpend(pending float64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped != "" {
		return b.stopped
	}
	if b.failed || b.floor == nil {
		b.stopped = "job spending could not be recorded; work stopped"
		return b.stopped
	}
	it, err := b.floor.Get(b.item.ID)
	if err != nil || it.Repo != b.item.Repo {
		return session.JobBudgetUnreadable
	}
	factory.AddChatSpend(&it, pending)
	b.stopped = factoryrun.ChatRefusal(it)
	b.reached = b.stopped != ""
	return b.stopped
}
func (b *stageBudget) total() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spent
}

func (b *stageBudget) reachedCap() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.reached }

// MemoryRefusal keeps optional learning inside the job's active work, even if
// a foreground conversation is still open after its job published or paused.
func (b *stageBudget) MemoryRefusal() string {
	owner := b.memoryOwner
	if owner == nil {
		owner = b.floor
	}
	if owner == nil {
		return "job could not be read; memory stopped"
	}
	it, err := owner.Get(b.item.ID)
	if err != nil {
		return "job could not be read; memory stopped"
	}
	return jobMemoryRefusal(it)
}

func jobMemoryRefusal(it factory.Item) string {
	if it.Stream != nil && it.Stream.Paused {
		return "job is paused; memory stopped"
	}
	if it.State == factory.StateShipped || factory.JobState(it) == "done" || (it.Stream != nil && !it.Stream.Ended.IsZero()) {
		return "job has ended; memory stopped"
	}
	return ""
}

// memoryCap reports a crossing caused only by memory since this round began.
func (b *stageBudget) memoryCap() bool {
	if b.floor == nil {
		return false
	}
	it, err := b.floor.Get(b.item.ID)
	if err != nil || it.Repo != b.item.Repo || it.Cap <= 0 {
		return false
	}
	return factory.JobSpent(it) >= it.Cap && factory.JobSpent(it)-max(0, it.MemorySpent-b.item.MemorySpent) < it.Cap
}

// roundTotal includes receipts booked before this chat was resumed.
func (b *stageBudget) roundTotal() float64 {
	if b.floor != nil && b.stage != "" {
		if it, err := b.floor.Get(b.item.ID); err == nil && it.Stream != nil {
			for _, ph := range it.Stream.Phases {
				if ph.Name == b.stage && ph.RoundCosts != nil {
					return ph.RoundCosts[b.round]
				}
			}
		}
	}
	return b.total()
}
