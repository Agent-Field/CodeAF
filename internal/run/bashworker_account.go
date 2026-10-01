package run

import (
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// settledWorkerUsage closes every call road before reading the cumulative
// account. Failed turns and auxiliary calls already banked there; outstanding
// receipts need their own bounded settlement before the report closes its books.
func settledWorkerUsage(agent *session.Agent, receipts *workerReceipts) session.Usage {
	_ = agent.Close()
	agent.SettleWrites()
	// A broken callback cannot hold a run forever. Figures already banked stay
	// included after the provider's own receipt deadline, without guessing gaps.
	receipts.wait(provider.ReceiptWait)
	return agent.Usage()
}

// workerReceipts tracks the provider's asynchronous answers to cut calls. Its
// channel closes at zero so settlement needs no polling or waiting goroutine.
type workerReceipts struct {
	mu      sync.Mutex
	pending int
	idle    chan struct{}
}

// owe is armed on the worker's call context before any turn starts. Each answer
// retires its registration once, even if a provider calls its completion twice.
func (r *workerReceipts) owe() func() {
	r.mu.Lock()
	if r.pending == 0 {
		r.idle = make(chan struct{})
	}
	r.pending++
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.pending--
			if r.pending == 0 {
				close(r.idle)
			}
		})
	}
}

// wait starts after the seat closes, so no new request can queue a receipt.
// The provider bounds each receipt from its queue time; that same bound covers
// all outstanding registrations and keeps a broken callback from hanging a run.
func (r *workerReceipts) wait(bound time.Duration) {
	r.mu.Lock()
	if r.pending == 0 {
		r.mu.Unlock()
		return
	}
	idle := r.idle
	r.mu.Unlock()
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case <-idle:
	case <-timer.C:
	}
}
