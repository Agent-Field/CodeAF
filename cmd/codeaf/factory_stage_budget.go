package main

import (
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// One live meter per round; descendants share it. The store is the worker's
// authoritative mirror, also read by the manager's meter, not a copied cap.
type stageBudget struct {
	floor   *store.Store
	item    factory.Item
	mu      sync.Mutex
	spent   float64
	failed  bool
	stopped string
	reached bool
}

func (b *stageBudget) Spend(usd float64) {
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
		factory.AddChatSpend(it, usd)
		return nil
	}); err != nil {
		b.failed = true
	}
}
func (b *stageBudget) Refusal() string {
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
		b.stopped = "job budget could not be read; work stopped"
		return b.stopped
	}
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
