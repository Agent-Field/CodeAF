package provider

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// ── the effort ladder, translated ───────────────────────────────────────────
//
// THIS FILE IS THE ONLY PLACE A RUNG BECOMES A REQUEST FIELD.
//
// Above the adapter, how hard to think is a word on a five-rung ladder and
// nothing else (internal/effort). Here it becomes OpenRouter's unified
// `reasoning` object, which speaks two dialects at once: an `effort` word, which
// is what OpenAI-family endpoints understand, and a `max_tokens` thinking
// budget, which is what Anthropic- and Gemini-family endpoints understand.
//
// "high" is the top of the word dialect. There is nothing above it to SAY, so
// the two rungs above high say it with the budget instead — the same word, and
// an allowance the endpoint that reads budgets will spend. An endpoint that
// reads only the word sees high on all three, which is the honest degradation:
// it cannot think harder than its own ceiling, and inventing a word for it would
// be a 400 on every call.

const (
	// xhighReasoningTokens is a deep pass that still leaves the answer room.
	//
	// It is sized against the WORKING WINDOW rather than against the model's
	// published maximum: 32k of thinking is minutes of deliberation on the
	// models this program is pointed at, and it still leaves a 200k window most
	// of its room for the transcript and the reply. That is what makes xhigh a
	// rung somebody can leave switched on rather than one they reach for once.
	xhighReasoningTokens = 32000

	// maxReasoningTokens is the top of the ladder — the rung for the one
	// question worth waiting on.
	//
	// 64k is about where the published budgets stop paying: past it the extra
	// tokens buy restatement rather than reasoning, and a 200k window has spent
	// a third of itself before the answer begins. It is deliberately not the
	// largest number an endpoint would accept. A ceiling nobody can tell apart
	// from the rung below it is a ceiling that only costs money.
	maxReasoningTokens = 64000
)

// WithConfiguredEffortRung carries a rung a PERSON chose — a dialled
// conversation, a task somebody set, the install's own default. It is sent even
// when the catalog cannot vouch for the model, which is the difference
// [WithConfiguredReasoningEffort] draws and the reason it exists: an operator's
// choice is worth one round-trip to discover an endpoint refuses it, and a
// harness's guess is not.
func WithConfiguredEffortRung(ctx context.Context, rung effort.Rung) context.Context {
	return withEffort(ctx, effortRequestFor(rung, true))
}

// WithEffortRung carries a rung the HARNESS chose for one phase — a sentinel's
// yes-or-no, a standing check. The catalog gate drops it on a model that cannot
// be vouched for, so a default depth can never break a run on an unknown model.
func WithEffortRung(ctx context.Context, rung effort.Rung) context.Context {
	return withEffort(ctx, effortRequestFor(rung, false))
}

// effortRequestFor is the mapping itself, and it is a total function over the
// ladder: an unknown rung asks for nothing rather than guessing, because a rung
// this adapter has not been taught is a rung whose wire shape nobody decided.
func effortRequestFor(rung effort.Rung, explicit bool) effortRequest {
	switch rung {
	case effort.Low:
		return effortRequest{effort: EffortLow, explicit: explicit}
	case effort.Medium:
		return effortRequest{effort: EffortMedium, explicit: explicit}
	case effort.High:
		return effortRequest{effort: EffortHigh, explicit: explicit}
	case effort.XHigh:
		return effortRequest{effort: EffortHigh, budget: xhighReasoningTokens, explicit: explicit}
	case effort.Max:
		return effortRequest{effort: EffortHigh, budget: maxReasoningTokens, explicit: explicit}
	default:
		return effortRequest{explicit: explicit}
	}
}
