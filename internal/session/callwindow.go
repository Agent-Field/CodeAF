package session

import (
	"context"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── a window the model is told ──────────────────────────────────────────────
//
// A WINDOW THE MODEL IS NEVER SHOWN IS A STOPWATCH, NOT A CONTRACT. The checker
// was the measured case (#941): a task's check with nothing to run gets a minute
// ([auditReadingDeadline]), one call may hold half of it ([auditCallShare]), and
// a reasoning model asked to check twenty files thought through both halves
// without writing a word, because nothing on the wire had ever said how long it
// had. Five of thirty-four landings in one drive ended `your call` that way, on
// work that was right. The planner's wall was the same defect one package over
// (#927), and its fix is the half of this that is not here.
//
// So a call this package bounds for a reason the model could act on is opened
// with [openCallWindow] rather than with a bare deadline, and the one client
// door ([Agent.completeWithModel]) tells every request made under it how long
// is left ([toldItsWindow]). WHAT IS TOLD IS THE WALL AND NOTHING ELSE. The
// thinking allowance it implies is derived in exactly one place — the adapter's
// effort ladder, from the machine the request asked for and that machine's
// measured pace (internal/provider's effortladder.go, [provider.WithThinkingWall])
// — so there is no second budget here to disagree with it.
//
// IT IS APPLIED AT THE DOOR AND NOT WHERE THE WINDOW IS OPENED, which is the
// wall's own rule: last, on the context the completion is actually sent with.
// Between the two, the turn loop stamps the rung onto every attempt (loop.go's
// completeWithRetryReasoning), and a rung stamp replaces the whole effort
// request — so a wall set by the caller would be shadowed by the first rung
// anybody dialled, which is every install with an `effort` row.

// callWindow is how one call is told about the window it runs under. The bound
// itself is the context's deadline, which is the fact the clock already holds;
// this says only that the bound is one the model is told, and how.
type callWindow struct {
	// answer says the reading this call belongs to is OVER and the call is asked
	// only for its answer, so its thinking pass is switched off.
	//
	// IT IS A DIFFERENT QUESTION FROM THE ONE THAT WAS CUT, NOT A SMALLER ONE. The
	// reading that came before it — every file opened, every command run and what
	// it printed — is in the transcript the answer is asked over, so nothing the
	// call could have thought is lost by not thinking again; what thinking would
	// buy here is a second investigation of the same evidence inside the last of
	// a window that the first one already spent (task_audit.go's
	// [Agent.askForTheWord]). The pass is switched off as a REQUIREMENT of the
	// call, which is the one effort a person's pinned rung yields to (the
	// adapter's WithRequiredReasoningEffort states the rule).
	answer bool
}

// callWindowKey is the context key [callWindow] rides under.
type callWindowKey struct{}

// openCallWindow bounds ctx at `bound` and marks the bound as one the model is
// told. It is the one way this package opens a told window, and the checker's
// calls are held to it by a law (callwindow_law_test.go).
func openCallWindow(ctx context.Context, bound time.Duration, window callWindow) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, bound)
	return context.WithValue(ctx, callWindowKey{}, window), cancel
}

// toldItsWindow is the door's half: a request sent under an opened window
// carries the time the window has left, and an answer-only call carries its
// thinking switched off. A request under no opened window is returned exactly as
// it came, which is every call this package makes but the ones that asked.
//
// THE TIME LEFT IS READ OFF THE CONTEXT'S OWN DEADLINE, the nearest of every
// bound the call is under, and at the moment this request goes out. A turn
// that has spent twenty seconds reading files before its next request is told
// the ten that remain rather than the thirty it started with, because the ten
// are what that request has.
func toldItsWindow(ctx context.Context) context.Context {
	window, opened := ctx.Value(callWindowKey{}).(callWindow)
	if !opened {
		return ctx
	}
	if window.answer {
		ctx = provider.WithRequiredReasoningEffort(ctx, provider.EffortOff)
	}
	deadline, bounded := ctx.Deadline()
	if !bounded {
		return ctx
	}
	return provider.WithThinkingWall(ctx, time.Until(deadline))
}
