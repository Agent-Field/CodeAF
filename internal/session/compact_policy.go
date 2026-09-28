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
	// recovering is the pass a refused request runs: it is asked for a
	// specific reclaim, so a smaller region is worth summarizing
	// ([summaryWorthIt]).
	recovering bool
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

// recoveryRoomDivisor sizes the room a refusal's recovery frees beyond what
// was missing: a thirty-second of the window, 512 tokens of a 16k window and
// 4,096 of a 128k one — enough for the next message, not a quarter of the
// conversation.
const recoveryRoomDivisor = 32

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
	// A manual pass may be buying the very room this refused turn needs.
	// Wait for its signal once, then send the changed request; if it did not
	// shrink the transcript, run the ordinary recovery policy below.
	a.mu.Lock()
	beforePass := a.transcriptTokensLocked()
	done := a.compactDone
	compacting := a.compacting
	a.mu.Unlock()
	if compacting && done != nil {
		waitCtx, cancel := context.WithTimeout(ctx, CompactPatience)
		select {
		case <-done:
		case <-waitCtx.Done():
			cancel()
			return false
		}
		cancel()
		a.mu.Lock()
		roomMade := a.transcriptTokensLocked() < beforePass
		a.mu.Unlock()
		if roomMade {
			return true
		}
	}
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
	// WITH NO FIGURES, A QUARTER. A refusal that says neither the limit nor the
	// size gives no measure of what is missing, and a generous guess costs one
	// pass where a stingy one costs a second refusal.
	reclaim := max(1, before/4)
	if failure != nil && failure.ContextLimit > 0 && failure.InputTokens > 0 {
		allowed := failure.ContextLimit - failure.OutputTokens - provider.ContextSafetyTokens(failure.ContextLimit)
		// WITH FIGURES, WHAT IS MISSING AND A LITTLE ROOM. The quarter used to
		// be a floor here too, and it made a request 78 tokens over reclaim 1,800
		// — a summary of the very answer the person was asking about, when
		// what was missing was a sentence. The room ([recoveryRoomDivisor])
		// is what keeps the next turn's message from being refused at once.
		// A refusal whose figures say it already fits leaves the quarter
		// standing, because then the estimate is what is wrong.
		if need := failure.InputTokens - allowed; need > 0 {
			reclaim = min(before, need+failure.ContextLimit/recoveryRoomDivisor)
		}
	}
	policy.target = max(1, before-reclaim)
	// A refused request needs this line reached, by a summary if the free
	// rungs cannot get there. The target is already in transcript tokens: the
	// reclaim above was measured against the refused request as a whole.
	policy.summarizeAbove = policy.target
	policy.summarizeTo = policy.target
	policy.recovering = true
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
