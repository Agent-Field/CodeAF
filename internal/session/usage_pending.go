package session

import (
	"context"
	"sync"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// receiptTracker outlives individual children until their admitted receipts
// have reached the accounting sink.
type receiptTracker struct {
	mu      sync.Mutex
	pending int
	done    chan struct{}
}

func (a *Agent) receiptTracker() *receiptTracker {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.receipts == nil {
		a.receipts = &receiptTracker{}
	}
	return a.receipts
}

// WaitReceipts waits for this agent family's already registered pending receipts.
// It then schedules their seat notes for saving before a handoff.
// Callers stop new requests or observe their receipt admission before waiting.
// It does not join requests that might register more receipts later. Close
// deliberately permits receipt accounting to finish after quit.
func (a *Agent) WaitReceipts(ctx context.Context) error {
	receipts := a.receiptTracker()
	receipts.mu.Lock()
	done := receipts.done
	receipts.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// Late receipts can arrive after the turn's last seal, so seal their notes
	// before a handoff publishes and releases the drive.
	executor.Settle(ctx, a.config.seat())
	return nil
}

func (a *Agent) pendingReceipt() func() {
	receipts := a.receiptTracker()
	receipts.mu.Lock()
	if receipts.pending == 0 {
		receipts.done = make(chan struct{})
	}
	receipts.pending++
	receipts.mu.Unlock()
	return func() {
		receipts.mu.Lock()
		receipts.pending--
		if receipts.pending == 0 {
			close(receipts.done)
		}
		receipts.mu.Unlock()
	}
}

// withReceiptTracking joins accounting after the sink returns without changing
// the caller's stage-readiness callback.
func (a *Agent) withReceiptTracking(ctx context.Context) context.Context {
	return provider.WithReceiptSettlement(ctx, a.pendingReceipt)
}
