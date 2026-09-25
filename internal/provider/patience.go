package provider

import (
	"context"
	"net/http"
)

// Patience is how ONE OUTBOUND CALL answers a provider that is pacing it, and
// the two seams here are the whole of it: how long a 429 is worth waiting out,
// and who is told while the waiting happens.
//
// ── WHY IT RIDES THE CONTEXT AND NOT THE CLIENT ──
//
// The adapter is SHARED. A task node's agent is built on the very same
// *Client the person's conversation talks through (internal/session's
// newTaskAgent hands the parent's completer down), so a field on the client —
// or on its Config — would make the conversation patient the moment a node
// was, and would make a node impatient the moment somebody opened a second
// one. The call is the only thing that knows whose call it is, and the context
// is what the call already carries.
//
// ── "NOT YET" IS ANSWERED BY GOING SOMEWHERE ELSE ──
//
// A 429 is not a fault. It is the provider saying "not yet", and the FIRST
// answer to "not yet" has nothing to do with patience at all: the machine that
// said it comes off the next body and the next body goes out at once, to
// another machine, with no wait of any kind (retry.go). Waiting is what is left
// when there is nowhere else to go, and this file is only about that remainder.
//
// ── WHY THE TWO ARE DIFFERENT ANSWERS TO ONE FACT ──
//
// The remainder still depends on whether anybody is sitting there. When every
// machine this request may go to is being held, a conversation's call hands the
// refusal straight back so the session can offer the NEXT MODEL — which beats
// any window, and is the owner's ruling of 2026-09-10: nobody should ever have
// to work a rate limit around by switching models themselves. A task child's
// call has nobody to disappoint, no fallback of its own and everything to lose
// — abandoning a node over pacing throws away a worktree of work for a
// condition that was always going to clear. So the child waits, for the window
// the machine itself named, and the surface is told what it is waiting for and
// until when.
//
// SO [WithPatientRateLimits] NO LONGER MEANS "sixty attempts against one
// machine". It means MAY WAIT FOR THE EARLIEST WINDOW WHEN THERE IS NOWHERE
// ELSE TO GO. The attempt ceiling below it is the arithmetic backstop for a
// provider that answers 429 with no delay at all, and nothing else.
//
// Neither seam weakens the context: every wait is [Client.wait] against the
// caller's own ctx, so an interrupt, a stop, or a deadline cuts through a
// parked call at the next select and not one moment later.

type patienceKey struct{}

type pacingKey struct{}

// WithPatientRateLimits marks every call made under ctx as one that MAY WAIT for
// a window when there is nowhere else to send the request: the bounded 429
// patience in retry.go stops applying, the wait is the window the machine itself
// named (capped at maxProviderWait), and the context is still the only thing
// that ends the call.
//
// IT IS NOT PERMISSION TO ASK THE SAME MACHINE AGAIN WHILE ANOTHER IS FREE. A
// patient call walks the machines exactly as a watched one does and reaches a
// wait by the same road — every machine it may use being held at once.
//
// It says nothing about faults. A timeout, a torn connection and a 500 keep
// the short patience they always had, here as everywhere: those are the
// provider failing, and repeating a failure is not patience.
func WithPatientRateLimits(ctx context.Context) context.Context {
	return context.WithValue(ctx, patienceKey{}, true)
}

// WithoutPatientRateLimits takes the patience off every call made under ctx,
// whatever an outer context granted: a 429 on every machine the request may
// use is handed straight back. A crew seat asks this way, because its answer
// to "not yet" is its next route or its next model, never a wait — a pool at
// its daily limit that is waited on a minute at a time holds a task for as
// long as the limit lasts.
//
// AND IT NEVER WAITS ON THE SAME MACHINE AFTER A 429: a free move to another
// machine is still made at once, but the wait that would follow — the
// machine's named window, or our doubling — is not sat out; the refusal is
// handed back instead ([handsBackRateLimits]).
func WithoutPatientRateLimits(ctx context.Context) context.Context {
	return context.WithValue(context.WithValue(ctx, patienceKey{}, false), handBackKey{}, true)
}

// handBackKey marks a call that hands a rate limit back rather than wait.
type handBackKey struct{}

// handsBackRateLimits is whether lastErr is a rate limit this call hands back
// rather than wait out.
func handsBackRateLimits(ctx context.Context, lastErr error) bool {
	if ctx == nil || lastErr == nil {
		return false
	}
	if on, _ := ctx.Value(handBackKey{}).(bool); !on {
		return false
	}
	refusal, ok := RefusalFrom(lastErr)
	return ok && refusal.Status == http.StatusTooManyRequests
}

// patientRateLimits reports whether this call waits pacing out.
func patientRateLimits(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	patient, _ := ctx.Value(patienceKey{}).(bool)
	return patient
}

// WithPacingNotice attaches the one callback the retry loop makes: true when a
// call parks on a provider's pacing, false when it stops being parked —
// because it got through, or because it gave up.
//
// It is a bool and not a reason, and that is the contract rather than a
// shortcut. What is upstream of this is a surface drawing a queue, and a
// surface has no use for a status code; what it needs is whether the thing is
// moving. The word a person eventually reads is chosen where the words live
// (internal/session's task_contract.go), not here.
//
// The notice is called from the sending goroutine, synchronously, so it must
// not work: the one live implementation sets a field and announces, which is
// the budget it has.
//
// ITS SIBLING IS [WithCallProgress] (callprogress.go), which carries the other
// half of a call's life — it went out, it is writing, it ended — under the very
// same law about doing no work. This one is about the wait BEFORE the request
// reaches a machine; that one begins where this one ends.
func WithPacingNotice(ctx context.Context, notice func(bool)) context.Context {
	if notice == nil {
		return ctx
	}
	return context.WithValue(ctx, pacingKey{}, notice)
}

// pacingNoticeFrom is the attached callback, or nil when nobody is listening.
func pacingNoticeFrom(ctx context.Context) func(bool) {
	if ctx == nil {
		return nil
	}
	notice, _ := ctx.Value(pacingKey{}).(func(bool))
	return notice
}
