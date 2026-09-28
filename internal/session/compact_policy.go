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
	// manual is a person's /compact. It may write a summary even when the
	// conversation is under every line, once the free rungs found nothing.
	manual bool
	// summarize lets the pass end with a summary (compact_summary.go) when the
	// free rungs leave the transcript above summarizeAbove; the summary then
	// aims for summarizeTo. Both are TRANSCRIPT tokens — the messages alone,
	// with the belt's definitions already taken off the window's lines
	// ([Agent.beltTokens]) — because the transcript is the only part a summary
	// can shrink.
	summarize      bool
	summarizeAbove int
	summarizeTo    int
}

// automaticCompactPolicy is the pass the loop runs on its own: the threshold
// fires it, the fold aims below it, and a summary is written only when the free
// rungs leave the conversation above the threshold.
func (a *Agent) automaticCompactPolicy() compactPolicy {
	belt := a.beltTokens()
	return compactPolicy{
		target:         a.compactTargetTokens(),
		keep:           a.keepRecentTokens(),
		summarize:      true,
		summarizeAbove: a.compactThreshold() - belt,
		summarizeTo:    a.compactTargetTokens() - belt,
	}
}

// compactRecentTokens keeps a useful working tail without protecting an entire
// small-window conversation. This is a token allowance, not a summary length.
const compactRecentTokens = 4096
const contextRecoveryAttempts = 2

func (a *Agent) requestedCompactPolicy() compactPolicy {
	// A person who typed /compact wants the conversation as short as it may
	// be made, so a line the belt has already crossed becomes the smallest
	// one there is rather than a reason to do nothing.
	line := max(1, a.compactTargetTokens()-a.beltTokens())
	return compactPolicy{
		keep:           min(compactRecentTokens, a.trustedWindow()/8),
		active:         true,
		manual:         true,
		summarize:      true,
		summarizeAbove: line,
		summarizeTo:    line,
	}
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
	policy.manual = false
	a.mu.Lock()
	before := a.transcriptTokensLocked()
	a.mu.Unlock()
	reclaim := max(1, before/4)
	if failure != nil && failure.ContextLimit > 0 && failure.InputTokens > 0 {
		allowed := failure.ContextLimit - failure.OutputTokens - provider.ContextSafetyTokens(failure.ContextLimit)
		reclaim = max(reclaim, failure.InputTokens-allowed)
	}
	policy.target = max(1, before-reclaim)
	// A refused request needs this line reached, by a summary if the free
	// rungs cannot get there. The target is already in transcript tokens: the
	// reclaim above was measured against the refused request as a whole.
	policy.summarizeAbove = policy.target
	policy.summarizeTo = policy.target
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
