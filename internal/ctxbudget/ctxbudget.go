// Package ctxbudget is the one place that turns a model's context window into
// byte and token budgets. The law it implements: an agent — head turn, planner
// pass, leaf worker, judge — may fill its window to FillPercent (60% by
// default) before compaction fires, and it always keeps CompletionReserve
// tokens of room for the answer and its reasoning. Nothing downstream of this
// package may carry an absolute byte ceiling of its own: every cap is either a
// Budget, a Share of one, or an explicitly-named fallback for the case where
// the window is unknown.
//
// The package deliberately imports nothing from the rest of the tree. Callers
// hand it the window size (the surface owns the catalog and hands facts down,
// the same doctrine as Linear.WithContextLength); zero means unknown, and
// unknown is never treated as small — every consumer names its own fallback.
package ctxbudget

import (
	"os"
	"strconv"
	"sync"
)

// BytesPerToken is the estimator used everywhere a budget is spent in bytes.
// It matches the leaf decayer's long-standing constant; when a real tokenizer
// arrives it replaces this in one place.
const BytesPerToken = 4

const (
	// DefaultFillPercent is the law's number: fill to 60% of the window,
	// then compact. AFORGE_CONTEXT_FILL_PCT overrides it (clamped 10–90).
	DefaultFillPercent = 60

	// DefaultCompletionReserveTokens is the room every call keeps for its
	// visible answer plus reasoning. It is deliberately high: a reasoning
	// pass routinely spends more thinking than writing, and a ceiling only
	// costs on the turns that use it. AFORGE_COMPLETION_RESERVE overrides.
	DefaultCompletionReserveTokens = 65536
)

// The configured values arrive from the settings sheet at process start via
// Configure. The environment still wins — a pin is a pin — and the defaults
// carry when neither has spoken. Plain ints behind a mutex: read on every
// call so a settings write lands in the running process.
var (
	configMu          sync.RWMutex
	configuredFill    int
	configuredReserve int
)

// Configure hands the persisted settings values down. Zero means unset; the
// environment and the defaults are unaffected either way.
func Configure(fillPercent, reserveTokens int) {
	configMu.Lock()
	configuredFill = fillPercent
	configuredReserve = reserveTokens
	configMu.Unlock()
}

// FillPercent is the process-wide fill law: environment pin, then the
// configured setting, then the default. Clamped so a typo can neither starve
// nor overrun a window.
func FillPercent() int {
	if v, ok := envInt("AFORGE_CONTEXT_FILL_PCT"); ok {
		return clampFill(v)
	}
	configMu.RLock()
	v := configuredFill
	configMu.RUnlock()
	if v > 0 {
		return clampFill(v)
	}
	return DefaultFillPercent
}

// CompletionReserve is the process-wide completion+reasoning reserve:
// environment pin, then the configured setting, then the default.
func CompletionReserve() int {
	if v, ok := envInt("AFORGE_COMPLETION_RESERVE"); ok && v > 0 {
		return v
	}
	configMu.RLock()
	v := configuredReserve
	configMu.RUnlock()
	if v > 0 {
		return v
	}
	return DefaultCompletionReserveTokens
}

func clampFill(v int) int {
	if v < 10 {
		return 10
	}
	if v > 90 {
		return 90
	}
	return v
}

// Budget is a window turned into spendable room. The zero value is unusable
// on purpose — construct through For, which applies the law.
type Budget struct {
	// ContextTokens is the model's window. Zero means unknown.
	ContextTokens int
	// FixedFloorTokens is the measured cost of everything in the prompt
	// that is not the budgeted material — system prefix, tool schemas,
	// standing blocks. The consumer states it; this package cannot know it.
	FixedFloorTokens int
	// CompletionReserveTokens is room kept for the reply and its reasoning.
	CompletionReserveTokens int
	// FillPercent is how much of the window the law permits filling.
	FillPercent int
}

// For builds the standard budget for a window under the process-wide law.
// A zero window returns a zero budget — Known reports false and the caller
// uses its named fallback rather than a guess.
func For(contextTokens int) Budget {
	if contextTokens <= 0 {
		return Budget{}
	}
	return Budget{
		ContextTokens:           contextTokens,
		CompletionReserveTokens: CompletionReserve(),
		FillPercent:             FillPercent(),
	}
}

// WithFloor returns the budget with the consumer's fixed floor stated.
func (b Budget) WithFloor(tokens int) Budget {
	if tokens > 0 {
		b.FixedFloorTokens = tokens
	}
	return b
}

// Known reports whether the window was known at construction. An unknown
// budget spends nothing; the caller's fallback carries the day.
func (b Budget) Known() bool { return b.ContextTokens > 0 }

// Tokens is the prompt room the law grants: fill% of the window, minus the
// fixed floor, minus the completion reserve. Never negative.
func (b Budget) Tokens() int {
	if !b.Known() {
		return 0
	}
	fill := b.FillPercent
	if fill <= 0 {
		fill = DefaultFillPercent
	}
	t := b.ContextTokens*fill/100 - b.FixedFloorTokens - b.CompletionReserveTokens
	if t < 0 {
		return 0
	}
	return t
}

// Bytes is Tokens in transport bytes, via the shared estimator.
func (b Budget) Bytes() int { return b.Tokens() * BytesPerToken }

// BytesOr spends the budget when the window is known and the stated fallback
// when it is not. Every converted call site names its old literal here, so
// behaviour without a catalog is exactly what it was before the law.
func (b Budget) BytesOr(fallback int) int {
	if !b.Known() {
		return fallback
	}
	if v := b.Bytes(); v > 0 {
		return v
	}
	return fallback
}

// TokensOr is BytesOr in tokens.
func (b Budget) TokensOr(fallback int) int {
	if !b.Known() {
		return fallback
	}
	if v := b.Tokens(); v > 0 {
		return v
	}
	return fallback
}

// Share is a weighted slice of the budget's bytes, for composite prompts
// whose blocks split one pot (thread 35, board 20, …). Weights are relative;
// total is their sum. Falls back the same way BytesOr does.
func (b Budget) Share(weight, total, fallback int) int {
	if !b.Known() || weight <= 0 || total <= 0 {
		return fallback
	}
	v := b.Bytes() * weight / total
	if v <= 0 {
		return fallback
	}
	return v
}

func envInt(key string) (int, bool) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}
