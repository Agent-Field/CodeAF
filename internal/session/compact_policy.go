package session

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// A requested or necessary reduction has different priorities from background
// cache maintenance. It may archive completed work in the current turn, but
// never user instructions, the system prompt, or the newest tool batch.
type compactPolicy struct {
	target int
	keep   int
	active bool
}

// compactRecentTokens keeps a useful working tail without protecting an entire
// small-window conversation. This is a token allowance, not a summary length.
const compactRecentTokens = 4096
const contextRecoveryAttempts = 2

func (a *Agent) requestedCompactPolicy() compactPolicy {
	return compactPolicy{keep: min(compactRecentTokens, a.trustedWindow()/8), active: true}
}

// recoverContext only retries a changed request. An endpoint's explicit window
// is evidence; the failed prompt's estimated size is not a context limit.
func (a *Agent) recoverContext(ctx context.Context, hub *eventHub, err error) bool {
	failure, _ := provider.RefusalFrom(err)
	calibrated := false
	if failure != nil && failure.InputTokens > 0 && !failure.Local {
		a.mu.Lock()
		calibrated = failure.InputTokens > a.contextTokens
		if calibrated {
			a.contextTokens = failure.InputTokens
		}
		a.mu.Unlock()
	}
	if failure != nil && failure.ContextLimit > 0 {
		a.servedWindow.Store(int64(failure.ContextLimit))
	}
	policy := a.requestedCompactPolicy()
	a.mu.Lock()
	before := a.transcriptTokensLocked()
	a.mu.Unlock()
	reclaim := max(1, before/4)
	if failure != nil && failure.ContextLimit > 0 && failure.InputTokens > 0 {
		allowed := failure.ContextLimit - failure.OutputTokens - provider.ContextSafetyTokens(failure.ContextLimit)
		reclaim = max(reclaim, failure.InputTokens-allowed)
	}
	policy.target = max(1, before-reclaim)
	changed, compactErr := a.compactWithPolicy(ctx, hub, policy)
	a.mu.Lock()
	smaller := a.transcriptTokensLocked() < before
	a.mu.Unlock()
	return (changed && compactErr == nil && smaller) || calibrated || (failure != nil && failure.BudgetChanged)
}

// transcriptMessageBytesLocked includes the working carried beside assistant
// messages. Folding only visible prose while replaying its reasoning can leave
// the provider request large after the local meter claims it shrank.
func (a *Agent) transcriptMessageBytesLocked(index int) int {
	size := messageBytes(a.messages[index])
	if index < len(a.messageReasoning) {
		working := a.messageReasoning[index]
		size += len(working.Text) + len(working.Details)
	}
	return size
}
func (a *Agent) transcriptTokensLocked() int {
	total := 0
	for i := range a.messages {
		total += a.transcriptMessageBytesLocked(i)
	}
	return EstimateTokens(total)
}
