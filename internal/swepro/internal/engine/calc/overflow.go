package calc

import (
	"math"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/knobs"
)

// ── the slice of Config.Info / Provider.Model overflow.ts actually reads ──

// CompactionConfig mirrors `Config.Info["compaction"]` (config.ts:251-270).
// Every field is `Schema.optional`, so every field is a pointer: overflow.ts
// tests `compaction?.auto === false` (strict, so `undefined` is NOT false) and
// `compaction?.reserved ?? …` (nullish, so an explicit 0 wins).
type CompactionConfig struct {
	Auto                 *bool    `json:"auto"`
	Prune                *bool    `json:"prune"`
	TailTurns            *float64 `json:"tail_turns"`
	PreserveRecentTokens *float64 `json:"preserve_recent_tokens"`
	Reserved             *float64 `json:"reserved"`
}

// Config is the `Config.Info` projection this package needs. The full config
// struct is not this package's to own; only `compaction` is read.
//
// Passed BY VALUE everywhere, deliberately: `input.cfg` is a required parameter
// in TS (`overflow.ts:47`, `:89`, `:125`) and only `.compaction` is guarded with
// `?.`, so a null cfg is not an input the TS accepts — it throws a TypeError at
// `input.cfg.compaction`. A value type makes that state unrepresentable rather
// than silently well-defined.
type Config struct {
	Compaction *CompactionConfig `json:"compaction"`
}

// ModelLimit mirrors provider.ts:902-906's ProviderLimit. `input` is
// `optionalOmitUndefined`, hence the pointer.
type ModelLimit struct {
	Context float64  `json:"context"`
	Input   *float64 `json:"input"`
	Output  float64  `json:"output"`
}

// CacheCost mirrors ProviderCacheCost (provider.ts:884-887).
type CacheCost struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

// Over200KCost mirrors the object literal provider.ts:993-999 assigns to
// `experimentalOver200K` — note the runtime key order is cache, input, output,
// not the schema's input, output, cache.
type Over200KCost struct {
	Cache  *CacheCost `json:"cache"`
	Input  float64    `json:"input"`
	Output float64    `json:"output"`
}

// ModelCost mirrors ProviderCost (provider.ts:889-900) as built at
// provider.ts:986-992. `cache` is required by the schema but read through `?.`
// at session.ts:405-406, so it is a pointer here too.
type ModelCost struct {
	Input                float64       `json:"input"`
	Output               float64       `json:"output"`
	Cache                *CacheCost    `json:"cache"`
	ExperimentalOver200K *Over200KCost `json:"experimentalOver200K"`
}

// Model is the `Provider.Model` projection this package needs: the limit block
// (overflow.ts + transform.maxOutputTokens) and the cost block (getUsage).
// `cost` is required by the schema but read through `?.` at session.ts:397, so
// it is a pointer.
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

// ── overflow.ts constants ────────────────────────────────────────────────

// COMPACTION_BUFFER is overflow.ts:7.
const COMPACTION_BUFFER float64 = 20_000

// TRIGGER_PCT_DEFAULT is overflow.ts:20 — the fraction of usable context at
// which auto-compaction fires.
const TRIGGER_PCT_DEFAULT float64 = 0.6

// EFFECTIVE_CONTEXT_CAP_DEFAULT is overflow.ts:37 — the universal cap on the
// live working set, applied BEFORE the trigger percentage.
const EFFECTIVE_CONTEXT_CAP_DEFAULT float64 = 160_000

// DRIFT_THRESHOLD_DEFAULT is overflow.ts:71.
const DRIFT_THRESHOLD_DEFAULT float64 = 0.7

// TriggerPct is overflow.ts:21-26. Read fresh on every call, by design.
func TriggerPct() float64 {
	raw, ok := envLookup("CODEAF_COMPACT_TRIGGER_PCT")
	n := TRIGGER_PCT_DEFAULT
	if ok {
		n = jsNumberOfString(raw)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 1 {
		return TRIGGER_PCT_DEFAULT
	}
	return n
}

// EffectiveContextCap is overflow.ts:38-45. Note the empty string: `Number("")`
// is 0, and 0 means "disable the cap", so CODEAF_EFFECTIVE_CONTEXT="" returns
// +Infinity — the opposite of leaving it unset.
func EffectiveContextCap() float64 {
	raw, ok := envLookup("CODEAF_EFFECTIVE_CONTEXT")
	if !ok {
		return EFFECTIVE_CONTEXT_CAP_DEFAULT
	}
	n := jsNumberOfString(raw)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return EFFECTIVE_CONTEXT_CAP_DEFAULT
	}
	if n == 0 {
		return math.Inf(1)
	}
	return n
}

// DriftThreshold is overflow.ts:72-79. 0 disables the drift path entirely;
// out-of-range or malformed falls back to the default.
func DriftThreshold() float64 {
	raw, ok := envLookup("CODEAF_WS_DRIFT")
	if !ok {
		return DRIFT_THRESHOLD_DEFAULT
	}
	n := jsNumberOfString(raw)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1 {
		return DRIFT_THRESHOLD_DEFAULT
	}
	return n
}

// LeafTriggerTokens is overflow.ts:119-123 — the W6c absolute input-token
// compaction trigger for coder leaf sessions, and the ONLY knob read anywhere
// in the port. `resolveKnobs({}, process.env)` already clamps to the registry's
// [20_000, 160_000], so the bound is never re-derived here. The `?? DEFAULT`
// tail is dead (the registry always carries the key) but is reproduced because
// it is the TS expression.
func LeafTriggerTokens() float64 {
	if raw, ok := envLookup("CODEAF_LEAF_CONTEXT_TRIGGER"); ok && raw == "0" {
		return math.Inf(1)
	}
	resolved, ok := knobs.ResolveKnobs(nil, envProvider()).Values.Get("LEAF_CONTEXT_TRIGGER_TOKENS")
	if !ok {
		return knobs.LEAF_CONTEXT_TRIGGER_TOKENS_DEFAULT
	}
	return resolved
}

// MaxOutputTokens is `ProviderTransform.maxOutputTokens` (transform.ts:1280-1282):
//
//	Math.min(model.limit.output, OUTPUT_TOKEN_MAX) || OUTPUT_TOKEN_MAX
//
// The trailing `||` is JS truthiness on a float: a min of 0 or NaN falls
// through to OUTPUT_TOKEN_MAX, but a NEGATIVE limit.output is truthy and
// survives as a negative reserve.
func MaxOutputTokens(model Model) float64 {
	minimum := math.Min(model.Limit.Output, outputTokenMax)
	if jscompat.Truthy(minimum) {
		return minimum
	}
	return outputTokenMax
}

// UsableInput is overflow.ts:47's parameter object.
type UsableInput struct {
	Cfg   Config
	Model Model
}

// Usable is overflow.ts:47-68.
func Usable(input UsableInput) float64 {
	context := input.Model.Limit.Context
	if context == 0 {
		return 0
	}

	reserved := math.Min(COMPACTION_BUFFER, MaxOutputTokens(input.Model))
	if input.Cfg.Compaction != nil && input.Cfg.Compaction.Reserved != nil {
		reserved = *input.Cfg.Compaction.Reserved
	}

	var raw float64
	// `input.model.limit.input ? … : …` — truthiness, so a limit.input of 0
	// (or NaN, or absent) takes the context branch.
	if input.Model.Limit.Input != nil && jscompat.Truthy(*input.Model.Limit.Input) {
		raw = math.Max(0, *input.Model.Limit.Input-reserved)
	} else {
		raw = math.Max(0, context-MaxOutputTokens(input.Model))
	}
	// Clamp to the effective working-set cap BEFORE the trigger percentage.
	trusted := math.Min(raw, EffectiveContextCap())
	return math.Floor(trusted * TriggerPct())
}

// ── the token counter overflow.ts scores ─────────────────────────────────

// TokenCache mirrors `MessageV2.Assistant["tokens"]["cache"]` as DECLARED
// (message-v2.ts:276-279): read, then write. Contrast UsageCache, which is the
// same data in the order `getUsage`'s object literal builds it.
type TokenCache struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

// Tokens mirrors `MessageV2.Assistant["tokens"]` (message-v2.ts:568-578).
// `total` is `Schema.optional`, so it is a pointer and JSON.stringify drops the
// key when it is absent.
type Tokens struct {
	Total     *float64
	Input     float64
	Output    float64
	Reasoning float64
	Cache     TokenCache
}

// MarshalJSON writes the declared key order and drops an absent `total`,
// exactly as JSON.stringify does for an `undefined` property.
func (t Tokens) MarshalJSON() ([]byte, error) {
	return marshalTokens(t.Total, t.Input, t.Output, t.Reasoning, t.Cache.Read, t.Cache.Write, false)
}

// tokenCount is the `tokens.total || input + output + cache.read + cache.write`
// expression shared by shouldScanDrift (overflow.ts:96-98) and isOverflow
// (overflow.ts:142-144). JS `||` on a float: a total of 0 — or NaN — falls
// through to the sum.
func tokenCount(tokens Tokens) float64 {
	if tokens.Total != nil && jscompat.Truthy(*tokens.Total) {
		return *tokens.Total
	}
	return tokens.Input + tokens.Output + tokens.Cache.Read + tokens.Cache.Write
}

// autoDisabled is `input.cfg.compaction?.auto === false` — a STRICT comparison,
// so an absent block or an absent `auto` does not disable compaction.
func autoDisabled(cfg Config) bool {
	return cfg.Compaction != nil && cfg.Compaction.Auto != nil && !*cfg.Compaction.Auto
}

// ScanDriftInput is overflow.ts:89's parameter object.
type ScanDriftInput struct {
	Cfg    Config
	Tokens Tokens
	Model  Model
}

// ShouldScanDrift is overflow.ts:89-107 — the cheap pre-scan guard the live
// call site (processor.ts:517-525) tests against.
func ShouldScanDrift(input ScanDriftInput) bool {
	if autoDisabled(input.Cfg) {
		return false
	}
	if input.Model.Limit.Context == 0 {
		return false
	}
	if DriftThreshold() <= 0 {
		return false
	}
	usableCap := Usable(UsableInput{Cfg: input.Cfg, Model: input.Model})
	if usableCap <= 0 {
		return false
	}
	count := tokenCount(input.Tokens)
	// Already at/over the hard cap ⇒ pure-occupancy overflow fires regardless
	// of drift; scanning would be wasted work.
	if count >= usableCap {
		return false
	}
	return count >= math.Floor(usableCap/2)
}

// OverflowInput is overflow.ts:125's parameter object. Drift and Agent are
// pointers because both are optional and both branch on presence, not value:
// the drift path is entered only when `input.drift !== undefined`, and the leaf
// trigger applies only when `input.agent === "coder"`.
type OverflowInput struct {
	Cfg    Config
	Tokens Tokens
	Model  Model
	Drift  *float64
	Agent  *string
}

// IsOverflow is overflow.ts:125-160.
func IsOverflow(input OverflowInput) bool {
	if autoDisabled(input.Cfg) {
		return false
	}
	if input.Model.Limit.Context == 0 {
		return false
	}

	count := tokenCount(input.Tokens)
	usableCap := Usable(UsableInput{Cfg: input.Cfg, Model: input.Model})
	// Coder leaf sessions trigger at the tighter of the global usable cap and
	// the absolute leaf token bound; all other agents at the usable cap only.
	trigger := usableCap
	if input.Agent != nil && *input.Agent == "coder" {
		trigger = math.Min(usableCap, LeafTriggerTokens())
	}
	if count >= trigger {
		return true
	}

	if input.Drift != nil {
		threshold := DriftThreshold()
		if threshold > 0 && *input.Drift > threshold && count >= math.Floor(usableCap/2) {
			return true
		}
	}
	return false
}
