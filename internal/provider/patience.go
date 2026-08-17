package provider

import "context"

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
// ── WHY THE TWO ARE DIFFERENT ANSWERS TO ONE FACT ──
//
// A 429 is not a fault. It is the provider saying "not yet", and the right
// response to "not yet" depends entirely on whether anybody is sitting there:
// a conversation's call gives up after the bounded patience in retry.go
// because a person watching a cursor deserves an error long before they
// deserve a ten-minute silence, while a task child's call has nobody to
// disappoint and everything to lose — abandoning a node over pacing throws
// away a worktree of work for a condition that was always going to clear. So
// the child waits, and the surface is told it is waiting.
//
// Neither seam weakens the context: every wait is [Client.wait] against the
// caller's own ctx, so an interrupt, a stop, or a deadline cuts through a
// parked call at the next select and not one moment later.

type patienceKey struct{}

type pacingKey struct{}

// WithPatientRateLimits marks every call made under ctx as one that will WAIT
// OUT a provider's pacing rather than give up on it: the bounded 429 patience
// in retry.go stops applying, the backoff still climbs and is still capped at
// maxProviderWait per wait, and the context is still the only thing that ends
// the call.
//
// It says nothing about faults. A timeout, a torn connection and a 500 keep
// the short patience they always had, here as everywhere: those are the
// provider failing, and repeating a failure is not patience.
func WithPatientRateLimits(ctx context.Context) context.Context {
	return context.WithValue(ctx, patienceKey{}, true)
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
