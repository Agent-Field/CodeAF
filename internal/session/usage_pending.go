package session

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// WaitReceipts waits for this agent's already registered pending receipts.
// Callers stop new requests or observe their receipt admission before waiting.
// It does not join requests that might register more receipts later. Close
// deliberately permits receipt accounting to finish after quit.
func (a *Agent) WaitReceipts(ctx context.Context) error {
	a.mu.Lock()
	done := a.receiptsDone
	a.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Agent) pendingReceipt() func() {
	a.mu.Lock()
	if a.receiptsPending == 0 {
		a.receiptsDone = make(chan struct{})
	}
	a.receiptsPending++
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		a.receiptsPending--
		if a.receiptsPending == 0 {
			close(a.receiptsDone)
		}
		a.mu.Unlock()
	}
}

// withReceiptTracking preserves a caller's accounting lifetime while also
// letting this agent's owner join the registered receipt after its sink returns.
func (a *Agent) withReceiptTracking(ctx context.Context) context.Context {
	parent := provider.ReceiptPendingFrom(ctx)
	return provider.WithReceiptPending(ctx, func() func() {
		done := a.pendingReceipt()
		if parent == nil {
			return done
		}
		finish := parent()
		return func() { defer done(); finish() }
	})
}
