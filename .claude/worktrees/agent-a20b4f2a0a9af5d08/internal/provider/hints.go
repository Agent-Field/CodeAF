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

// effortRequest separates "the harness thinks this phase is cheap" from "the
// operator asked for this". Only the second may be sent to a model whose
// catalog entry does not confirm reasoning support, because a harness default
// that 400s an unknown model would be a self-inflicted outage.
type effortRequest struct {
	effort   Effort
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
