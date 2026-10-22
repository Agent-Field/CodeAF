package main

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/store"
)

// startClockMemoryMaintenance gives local activation its daily batch refresh
// in the existing clock process. Startup catches up after closed windows; the
// store's transactional due marker makes repeated clock launches harmless.
// Cancellation joins the worker before the clock process closes its store.
func startClockMemoryMaintenance(parent context.Context, memory *store.Store, logf func(string)) func() {
	if memory == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	guard.Go("memory/maintenance", func() {
		defer close(done)
		maintain := func(now time.Time) {
			if err := memory.RefreshMemoryActivationDue(now); err != nil && logf != nil {
				logf("memory maintenance: " + err.Error())
			}
		}
		maintain(time.Now())
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				maintain(now)
			}
		}
	})
	return func() { cancel(); <-done }
}
