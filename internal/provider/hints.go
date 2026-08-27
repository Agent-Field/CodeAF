// Package provider is Aforge's model adapter: the one place in the process
// that speaks to an OpenAI-compatible endpoint.
//
// It exists because provider economics are request-shape decisions, not loop
// decisions. The transcript layer earns a byte-stable prompt prefix; only the
// adapter can make a provider actually pay for that stability, by carrying the
// cache key, the usage-accounting opt-in, and the per-phase reasoning knob that
// the pinned AgentField SDK's Request type has no field for. Everything else in
// Aforge — the scheduler, the tool registry, the TUI — keeps seeing the SDK's
// neutral message and response types and never learns a wire detail.
package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Effort is OpenRouter's unified reasoning-effort knob. The empty value means
// "send nothing", which is materially different from "low": omitting the field
// leaves the model at its own default, while sending it commits us to a shape
// some models reject outright.
type Effort string

const (
	EffortNone Effort = ""

	// EffortOff is not a quieter setting than low — it is a different request.
	// It sends {"reasoning": {"enabled": false}}, which suppresses the thinking
	// pass outright. On structuring calls that is worth an order of magnitude in
	// latency: the model spends its whole budget on the answer instead of
	// deliberating first, and the answer is the same size either way.
	EffortOff Effort = "off"

	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
)

// ParseEffort validates operator-supplied configuration. An unrecognized value
// is an error rather than a silent downgrade: a knob that would 400 must never
// reach the wire, and a typo that silently disables an economy the operator
// asked for is worse than a startup failure.
func ParseEffort(value string) (Effort, bool) {
	switch Effort(strings.ToLower(strings.TrimSpace(value))) {
	case EffortNone:
		return EffortNone, true
	case EffortOff:
		return EffortOff, true
	case EffortLow:
		return EffortLow, true
	case EffortMedium:
		return EffortMedium, true
	case EffortHigh:
		return EffortHigh, true
	default:
		return EffortNone, false
	}
}

type cacheKeyContextKey struct{}
type effortContextKey struct{}

// WithCacheKey pins one run's provider affinity. It is set once, at the top of
// a run, and inherited by every nested worker and synthesis call through the
// ordinary context tree, which is exactly the property a prefix cache needs:
// one run is one cache lineage, and no request-time randomness can split it.
func WithCacheKey(ctx context.Context, key string) context.Context {
	key = strings.TrimSpace(key)
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, cacheKeyContextKey{}, key)
}

// CacheKeyFrom returns the run's stable cache key, empty when unset.
func CacheKeyFrom(ctx context.Context) string {
	key, _ := ctx.Value(cacheKeyContextKey{}).(string)
	return key
}

// effortRequest separates "the harness thinks this phase is cheap" from an
// effort the request cannot work without. Only the second may be sent to a model
// whose catalog entry does not confirm reasoning support, because an optional
// harness default that 400s an unknown model would be a self-inflicted outage.
type effortRequest struct {
	effort Effort

	// budget is the thinking allowance in tokens that the two ladder rungs
	// above high carry, and zero on every other request. It lives beside the
	// effort rather than in a context key of its own because it is one half of
	// one decision: nothing can ask for a budget without asking for a level, and
	// a second key would let the two be set apart and disagree.
	budget int

	explicit bool
}

// WithReasoningEffort scopes a harness phase default to one call. The harness
// sets it around the calls whose job is routing or phrasing rather than
// thinking. EffortNone is itself a request — "send nothing, let the model use
// its own default" — and it shadows any effort set further out, which is how an
// inner phase escapes a run-wide economy like EffortOff.
func WithReasoningEffort(ctx context.Context, effort Effort) context.Context {
	return withEffort(ctx, effortRequest{effort: effort})
}

// WithConfiguredReasoningEffort carries an operator-configured effort, which is
// sent even when the catalog cannot vouch for the model.
func WithConfiguredReasoningEffort(ctx context.Context, effort Effort) context.Context {
	return withEffort(ctx, effortRequest{effort: effort, explicit: true})
}

// WithRequiredReasoningEffort carries an effort that is part of the call's
// correctness rather than an optional economy. A reflex with a tiny answer cap
// is the measured case: silently dropping its disable leaves a reasoning model
// no tokens in which to answer, so an unknown catalog row must not erase it.
func WithRequiredReasoningEffort(ctx context.Context, effort Effort) context.Context {
	return withEffort(ctx, effortRequest{effort: effort, explicit: true})
}

func withEffort(ctx context.Context, request effortRequest) context.Context {
	return context.WithValue(ctx, effortContextKey{}, request)
}

// ReasoningEffortFrom returns the effort requested for this call, if any.
func ReasoningEffortFrom(ctx context.Context) Effort {
	request, _ := ctx.Value(effortContextKey{}).(effortRequest)
	return request.effort
}

func effortFrom(ctx context.Context) effortRequest {
	request, _ := ctx.Value(effortContextKey{}).(effortRequest)
	return request
}

// RunCacheKey derives a run's cache key from the run's own identity rather than
// from a clock or a random source. Two requests inside one run must produce the
// same key or the affinity is worthless, and deriving it from the task and
// model also lets a repeated identical run reuse the warm prefix instead of
// paying to write it again.
func RunCacheKey(task, model string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(task) + "\x00" + strings.TrimSpace(model)))
	return "aforge-" + hex.EncodeToString(sum[:16])
}

// WithLeafCacheKey narrows the run's affinity to one leaf.
//
// A routing key is not a cache: it is the answer to "which replica should serve
// this?", and a provider with an automatic prefix cache can only hit when the
// same replica sees the same bytes twice. The run key gets the first half of
// that right — every turn of every leaf of one run asks for one destination —
// and the second half wrong, because the six leaves of a fan-out run
// CONCURRENTLY with six different transcripts. They are then six growing,
// unrelated prefixes competing for one replica's cache, which is what the
// ledgers showed: cache reads quantized in 256-token steps, a third of the
// re-sent dollars missing a cache that a stable prompt should have hit.
//
// Narrowing to the leaf splits those six lineages apart. What it gives up is the
// shared head — the system message and the tool block, which every leaf of a run
// really does share byte for byte — now written cold once per leaf instead of
// once per run. That trade is not close: the head is a few thousand tokens
// written once per leaf, and the tail it protects is the whole transcript
// re-sent on every turn of that leaf. On Anthropic-family endpoints it is not
// even a trade, because their cache is content-addressed and the explicit
// breakpoint on the head is shared across leaves whatever the routing key says.
//
// The leaf key is derived FROM the run key rather than replacing it, so a run's
// requests still share a namespace a provider or an operator can see, and a leaf
// with no run key set stays unkeyed rather than inventing a lineage of its own.
func WithLeafCacheKey(ctx context.Context, leaf string) context.Context {
	run := CacheKeyFrom(ctx)
	if run == "" || strings.TrimSpace(leaf) == "" {
		return ctx
	}
	return WithCacheKey(ctx, LeafCacheKey(run, leaf))
}

// LeafCacheKey derives one leaf's affinity key from its run's. It is a pure
// function of the two identities — no clock, no counter, no attempt number — so
// every turn of one leaf produces the same key and a retried leaf rejoins the
// prefix its first attempt warmed.
func LeafCacheKey(run, leaf string) string {
	run, leaf = strings.TrimSpace(run), strings.TrimSpace(leaf)
	if run == "" || leaf == "" {
		return run
	}
	sum := sha256.Sum256([]byte(run + "\x00" + leaf))
	return run + "-" + hex.EncodeToString(sum[:6])
}
