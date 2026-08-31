package provider

import (
	"context"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── WHERE THE LANE CHOICE CROSSES THE BOUNDARY ──────────────────────────────
//
// `internal/lane` decides which endpoint a request should prefer and how long
// it is worth waiting before asking somebody else. This file is the one place
// that decision enters the transport, and it is a context value for the same
// reason the cache key and the effort are: one call's routing is a fact about
// that call, and threading it through the six signatures between the session
// and the stream loop would put it on every request that does not want it.
//
// THE PACKAGE IS IMPORTED UNDER AN ALIAS AND THAT IS NOT A STYLE CHOICE. This
// package already has an unexported type called `lane` (velocity.go), so a
// plain import would shadow it — silently, with no warning from the compiler,
// resolving to whichever one is in scope.
//
// Whoever asks the chooser calls [WithLaneChoice]; the stream loop reads it.
// The size hint is optional and separate because it is a different fact: how
// long the ANSWER is expected to be, which is what the watch's commitment rule
// needs and what nothing on the wire can tell it.
//
// WHERE THE SETTER BELONGS, for whoever lands the adapter half: the choice is
// computed in velocity.go, where the encoder turns it into `provider.order` and
// `provider.only` immediately before the send. That is the last moment it is
// known and the only place it is known once, so the call that carries it here —
// `ctx = WithLaneChoice(ctx, choice)` — goes beside it, on the context the
// completion is about to run under. Until it does, no request carries a choice
// and the stream loop behaves exactly as it did before this package existed,
// which is the tested empty state rather than a gap.

type laneChoiceContextKey struct{}

type laneAnswerContextKey struct{}

// WithLaneChoice carries one request's lane choice to the transport.
func WithLaneChoice(ctx context.Context, choice lanes.Choice) context.Context {
	return context.WithValue(ctx, laneChoiceContextKey{}, choice)
}

// laneChoiceFromContext is the choice in force, false when nobody made one —
// which is every call in a build where the router is not wired in.
func laneChoiceFromContext(ctx context.Context) (lanes.Choice, bool) {
	choice, ok := ctx.Value(laneChoiceContextKey{}).(lanes.Choice)
	return choice, ok && !choice.Empty()
}

// WithExpectedAnswer says how many output tokens this answer is expected to
// run to. It is the sunk cost in the watch's commitment rule and nothing else.
func WithExpectedAnswer(ctx context.Context, tokens int) context.Context {
	if tokens <= 0 {
		return ctx
	}
	return context.WithValue(ctx, laneAnswerContextKey{}, tokens)
}

// expectedAnswerFrom is the expected answer length, zero when nobody said.
func expectedAnswerFrom(ctx context.Context) int {
	tokens, _ := ctx.Value(laneAnswerContextKey{}).(int)
	return tokens
}
