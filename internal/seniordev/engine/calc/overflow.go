//go:build !windows

package calc

import (
	"fmt"
	"math"
)

// ── the slice of config / model the compaction budget reads ──────────────

// CompactionConfig is the `compaction` block of project config. Every field
// is optional, so every field is a pointer: `auto` is tested strictly (an
// absent value is NOT false) and `reserved` nullishly (an explicit 0 wins).
type CompactionConfig struct {
	// Policy names how the compaction budget is derived. The only policy is
	// "window": the budget is the model's own context window, capped by
	// CapacityTokens. It may be spelled out or left empty; any other name is
	// refused by ValidatePolicy.
	Policy string `json:"policy,omitempty"`
	Auto   *bool  `json:"auto"`
	Prune  *bool  `json:"prune"`
	// PreserveRecentTokens overrides the verbatim tail budget a compaction
	// keeps ahead of the summary. The tail is sized in tokens after
	// truncation, never in turns.
	PreserveRecentTokens *float64 `json:"preserve_recent_tokens"`
	// PreserveRecentFraction sizes the verbatim tail as a fraction of the
	// high watermark instead of a fixed token count, so it scales with the
	// window. PreserveRecentTokens wins when both are set.
	PreserveRecentFraction *float64 `json:"preserve_recent_fraction"`
	// CapacityTokens caps the working set below the model's window: a cost
	// decision, or a model known to degrade before its advertised context.
	// Absent means DefaultCapacityTokens.
	CapacityTokens *float64 `json:"capacity_tokens"`
	Reserved       *float64 `json:"reserved"`
}

// PolicyWindow is the compaction policy: the window is the budget.
const PolicyWindow = "window"

// DefaultCapacityTokens caps the working set when no capacity_tokens is
// configured. A model with a smaller window is bounded by the window.
const DefaultCapacityTokens float64 = 500_000

// ValidatePolicy refuses a policy name this binary does not implement, and a
// budget field outside its range. Both are refused at config load so a
// misspelled block fails before any model call.
func ValidatePolicy(cfg Config) error {
	if cfg.Compaction == nil {
		return nil
	}
	switch cfg.Compaction.Policy {
	case "", PolicyWindow:
	default:
		return fmt.Errorf("compaction.policy %q is not %q", cfg.Compaction.Policy, PolicyWindow)
	}
	if f := cfg.Compaction.PreserveRecentFraction; f != nil && (math.IsNaN(*f) || *f <= 0 || *f >= 1) {
		return fmt.Errorf("compaction.preserve_recent_fraction %v must be between 0 and 1 exclusive", *f)
	}
	if c := cfg.Compaction.CapacityTokens; c != nil && (math.IsNaN(*c) || math.IsInf(*c, 0) || *c <= 0) {
		return fmt.Errorf("compaction.capacity_tokens %v must be a positive token count", *c)
	}
	return nil
}

// Config is the project-config projection this package needs: only the
// `compaction` block is read.
type Config struct {
	Compaction *CompactionConfig `json:"compaction"`
}

// ModelLimit is a model's context, input and output limits. Input is
// optional: a catalog entry that names none is budgeted from Context.
type ModelLimit struct {
	Context float64  `json:"context"`
	Input   *float64 `json:"input"`
	Output  float64  `json:"output"`
}

// CacheCost is the per-token price of prompt-cache reads and writes.
type CacheCost struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

// Over200KCost is the price block a provider applies above 200K context.
// The key order is cache, input, output.
type Over200KCost struct {
	Cache  *CacheCost `json:"cache"`
	Input  float64    `json:"input"`
	Output float64    `json:"output"`
}

// ModelCost is a model's price block. `cache` is optional in practice, so it
// is a pointer.
type ModelCost struct {
	Input                float64       `json:"input"`
	Output               float64       `json:"output"`
	Cache                *CacheCost    `json:"cache"`
	ExperimentalOver200K *Over200KCost `json:"experimentalOver200K"`
}

// Model is the catalog projection this package needs: the limit block (for
// the budget and the output reservation) and the cost block (for usage).
type Model struct {
	Cost         *ModelCost        `json:"cost"`
	Limit        ModelLimit        `json:"limit"`
	Capabilities ModelCapabilities `json:"-"`
}

// ModelCapabilities is the models.dev capability slice retained alongside
// cost and limits so provider request assembly does not invent support.
type ModelCapabilities struct {
	Attachment  bool            `json:"attachment"`
	Reasoning   bool            `json:"reasoning"`
	Temperature bool            `json:"temperature"`
	ToolCall    bool            `json:"toolcall"`
	Input       map[string]bool `json:"input"`
	Output      map[string]bool `json:"output"`
}

// ── budget constants ─────────────────────────────────────────────────────

// COMPACTION_BUFFER bounds the output reservation taken off the window.
const COMPACTION_BUFFER float64 = 20_000

// TRIGGER_PCT is the fraction of the capacity at which auto-compaction
// fires: the high watermark.
const TRIGGER_PCT float64 = 0.6

// COMPACTION_LOW_TO_HIGH_RATIO is the low-watermark half of the 40/60
// hysteresis: low is two-thirds of high.
const COMPACTION_LOW_TO_HIGH_RATIO float64 = 2.0 / 3.0

// MaxOutputTokens is `min(model.limit.output, OUTPUT_TOKEN_MAX)`, falling
// back to OUTPUT_TOKEN_MAX when the minimum is 0 or NaN. A negative
// limit.output is passed through as is.
func MaxOutputTokens(model Model) float64 {
	minimum := math.Min(model.Limit.Output, outputTokenMax)
	if minimum != 0 && !math.IsNaN(minimum) {
		return minimum
	}
	return outputTokenMax
}

// UsableInput is what the budget is derived from: the compaction config and
// the model's limits.
type UsableInput struct {
	Cfg   Config
	Model Model
}

// EffectiveInputCapacity is the model-visible input capacity after reserving
// output space and applying the capacity cap. It deliberately does not apply
// the trigger percentage; Watermarks derives both marks from this one
// underlying capacity.
func EffectiveInputCapacity(input UsableInput) float64 {
	context := input.Model.Limit.Context
	if context == 0 {
		return 0
	}

	reserved := math.Min(COMPACTION_BUFFER, MaxOutputTokens(input.Model))
	if input.Cfg.Compaction != nil && input.Cfg.Compaction.Reserved != nil {
		reserved = *input.Cfg.Compaction.Reserved
	}

	var raw float64
	// An input limit of 0 (or NaN, or absent) takes the context branch.
	if input.Model.Limit.Input != nil && *input.Model.Limit.Input != 0 && !math.IsNaN(*input.Model.Limit.Input) {
		raw = math.Max(0, *input.Model.Limit.Input-reserved)
	} else {
		raw = math.Max(0, context-MaxOutputTokens(input.Model))
	}
	capacity := DefaultCapacityTokens
	if input.Cfg.Compaction != nil && input.Cfg.Compaction.CapacityTokens != nil &&
		*input.Cfg.Compaction.CapacityTokens > 0 {
		capacity = *input.Cfg.Compaction.CapacityTokens
	}
	return math.Min(raw, capacity)
}

// CompactionWatermarks describes the preferred post-compaction target and the
// occupancy at which another compaction becomes necessary.
type CompactionWatermarks struct {
	Capacity float64
	Low      float64
	High     float64
}

// Watermarks returns the 40/60 hysteresis around the capacity.
func Watermarks(input UsableInput) CompactionWatermarks {
	capacity := EffectiveInputCapacity(input)
	high := math.Floor(capacity * TRIGGER_PCT)
	low := math.Floor(high * COMPACTION_LOW_TO_HIGH_RATIO)
	return CompactionWatermarks{Capacity: capacity, Low: low, High: high}
}

// ── the token counter the trigger scores ─────────────────────────────────

// TokenCache is the persisted cache-token pair, declared read then write.
// Contrast UsageCache, which is the same data in the order usage builds it.
type TokenCache struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

// Tokens is the persisted assistant token block. `total` is optional, so it
// is a pointer and the key is dropped when it is absent.
type Tokens struct {
	Total     *float64   `json:"total,omitempty"`
	Input     float64    `json:"input"`
	Output    float64    `json:"output"`
	Reasoning float64    `json:"reasoning"`
	Cache     TokenCache `json:"cache"`
}

// tokenCount is `tokens.total || input + output + cache.read + cache.write`:
// a total of 0 or NaN falls through to the sum.
func tokenCount(tokens Tokens) float64 {
	if tokens.Total != nil && *tokens.Total != 0 && !math.IsNaN(*tokens.Total) {
		return *tokens.Total
	}
	return tokens.Input + tokens.Output + tokens.Cache.Read + tokens.Cache.Write
}

// autoDisabled is `compaction.auto === false` -- a STRICT comparison, so an
// absent block or an absent `auto` does not disable compaction.
func autoDisabled(cfg Config) bool {
	return cfg.Compaction != nil && cfg.Compaction.Auto != nil && !*cfg.Compaction.Auto
}

// OverflowInput is what the trigger decides on.
type OverflowInput struct {
	Cfg    Config
	Tokens Tokens
	Model  Model
}

// IsOverflow reports whether the assistant's token count has reached the
// high watermark.
func IsOverflow(input OverflowInput) bool {
	if autoDisabled(input.Cfg) {
		return false
	}
	if input.Model.Limit.Context == 0 {
		return false
	}
	return tokenCount(input.Tokens) >= Watermarks(UsableInput{Cfg: input.Cfg, Model: input.Model}).High
}
