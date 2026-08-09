// Package adaptive is a bug-for-bug port of src/router/adaptive.ts — the
// adaptive low/high/frontier model router (Thompson-sampling-ish scoring over
// Beta posteriors, latency/tok-per-sec EWMAs, $/Mtok cost, 429 cooldowns and
// an in-flight pressure penalty).
//
// The TS class is single-threaded: every method except `pick` is one
// synchronous critical section and `pick` only yields at its `await`. Go gets
// real goroutines, so a single router-wide mutex serializes every public
// method; Pick takes and releases it around each TryPick and never holds it
// across a sleep. Internal helpers (choose/score/statsFor/computeExcludeFamilies)
// assume the lock is already held.
//
// Fidelity notes (deliberate, do not "fix"):
//
//   - statsKey strips a LEADING "openrouter/" from the model id (adaptive.ts:170),
//     so `openrouter/qwen/x` and `qwen/x` — two DIFFERENT candidates — share one
//     ModelStats row. Intentional per the TS comment ("stats compare
//     apples-to-apples"); kept, and pinned by fixtures.
//   - The in-flight counter LEAKS. tryPick/pickSync increment `inflight` and
//     only register() decrements it (and only when it is > 0). A caller that
//     skips register — the exact failure mode the doc comment warns about —
//     permanently inflates pressurePen for that (slot, model) and biases every
//     later score. Ported verbatim; fixture `leak/*` pins the drift.
//   - choose()'s forced-fallback pass re-scores EVERY candidate, so it draws a
//     SECOND round of random numbers from the same stream. The RNG stream
//     position after an all-cooling pick therefore differs from a normal pick.
//     Kept — it is what makes the seeded fixtures reproducible at all.
//   - choose() skips blank ids in the main pass (`if (!cand.id.trim()) continue`)
//     but NOT in the forced pass, so an all-cooling pool can return a candidate
//     the main pass refused to consider.
//   - `best.score === -Infinity` survives when every candidate scores NaN (a
//     NaN price makes `cost` NaN), because `NaN > -Infinity` is false in both
//     passes. The returned score is then -Infinity, which JSON.stringify writes
//     as null — hence jscompat.JSNumber on every output-bearing number.
//   - effectiveTier degrades FRONTIER → HIGH only when the frontier pool is
//     empty; candidatesForTier(FRONTIER) still returns the empty pool, and
//     tryPick on an empty pool installs the hard-coded gpt-oss-120b fallback.
//   - ModelCandidate marshals through an explicit key order because the TS
//     produces TWO different property orders for the same interface: the
//     defaults (`{...m, tier, family, priority}` over a `{id, prompt, completion}`
//     literal) and everything else (the interface order). See candidateKeyOrder.
//   - Date.now(), Math.random() and the setTimeout sleep are package vars with
//     SetClockForTesting / SetRandomForTesting / SetSleeperForTesting.
package adaptive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// ── injectable ambient dependencies ──────────────────────────────────────

// nowMillis stands in for Date.now(). float64, not int64: the TS arithmetic
// (`now + cooldownSeconds*1000`, `deadline - now`) is all double math and the
// values reach output through retryAfterMs.
var nowMillis = func() float64 { return float64(time.Now().UnixMilli()) }

// SetClockForTesting pins Date.now(). Returns a restore func.
func SetClockForTesting(f func() float64) func() {
	prev := nowMillis
	nowMillis = f
	return func() { nowMillis = prev }
}

// randomFloat stands in for Math.random(), which makeRng returns verbatim when
// no random_seed is configured.
var randomFloat = func() float64 { return rand.Float64() }

// SetRandomForTesting pins Math.random(). Returns a restore func.
func SetRandomForTesting(f func() float64) func() {
	prev := randomFloat
	randomFloat = f
	return func() { randomFloat = prev }
}

// sleepMillis stands in for `await new Promise(r => setTimeout(r, ms))` plus
// the abort listener that resolves the promise early.
var sleepMillis = defaultSleep

// SetSleeperForTesting pins the pick() backoff sleep. Returns a restore func.
func SetSleeperForTesting(f func(ms float64, signal *AbortSignal)) func() {
	prev := sleepMillis
	sleepMillis = f
	return func() { sleepMillis = prev }
}

func defaultSleep(ms float64, signal *AbortSignal) {
	d := time.Duration(0)
	if !math.IsNaN(ms) && ms > 0 {
		if ms > 1e15 {
			ms = 1e15 // setTimeout clamps oversized delays; keep the Duration finite
		}
		d = time.Duration(ms * float64(time.Millisecond))
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	if signal == nil {
		<-timer.C
		return
	}
	select {
	case <-timer.C:
	case <-signal.Done():
	}
}

// AbortSignal is the port's stand-in for the DOM AbortSignal that pick()
// accepts. Only `aborted` and the "abort" event are observed.
type AbortSignal struct {
	mu      sync.Mutex
	aborted bool
	done    chan struct{}
}

func NewAbortSignal() *AbortSignal { return &AbortSignal{done: make(chan struct{})} }

func (s *AbortSignal) Abort() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.aborted {
		s.aborted = true
		close(s.done)
	}
}

func (s *AbortSignal) Aborted() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aborted
}

func (s *AbortSignal) Done() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.done
}

// ── ModelTier ────────────────────────────────────────────────────────────

type ModelTier string

const (
	ModelTierHigh ModelTier = "high"
	ModelTierLow  ModelTier = "low"
	// ModelTierFrontier is the narrow escalation tier used ONLY at
	// disagreement branch points. Scored like HIGH; degrades to HIGH via
	// EffectiveTier when the frontier pool is empty.
	ModelTierFrontier ModelTier = "frontier"
)

// ── ModelCandidate ───────────────────────────────────────────────────────

// candidateInterfaceOrder is the property order of every ModelCandidate
// literal in adaptive.ts EXCEPT the two default pools: parseModelList,
// choose()'s hard-coded fallback, and any caller-built candidate written in
// interface order.
var candidateInterfaceOrder = []string{
	"id", "tier", "family", "prompt_usd_per_mtok", "completion_usd_per_mtok", "priority",
}

// candidateDefaultsOrder is what defaultHighModels/defaultLowModels produce:
// the source literals list `{id, prompt_usd_per_mtok, completion_usd_per_mtok}`
// and `.map(m => ({...m, tier, family, priority}))` APPENDS the three new keys.
// normalizeConfig's own `{...c, id, tier, family, priority}` then rewrites
// existing keys in place, so the order survives into RouteChoice.candidate.
var candidateDefaultsOrder = []string{
	"id", "prompt_usd_per_mtok", "completion_usd_per_mtok", "tier", "family", "priority",
}

type ModelCandidate struct {
	// ID is the full model id, e.g. "openrouter/qwen/qwen3.6-plus".
	ID   string    `json:"id"`
	Tier ModelTier `json:"tier"`
	// Family is the vendor family derived from the model id. Used by the
	// cross-role adversarial constraints — see RoleAdversaries.
	Family               string            `json:"family"`
	PromptUSDPerMtok     jscompat.JSNumber `json:"prompt_usd_per_mtok"`
	CompletionUSDPerMtok jscompat.JSNumber `json:"completion_usd_per_mtok"`
	// Priority is the config index: lower = preferred when stats are absent.
	Priority jscompat.JSNumber `json:"priority"`

	// keyOrder records the JS property insertion order so JSON.stringify
	// parity holds for both literal shapes. Empty means interface order.
	keyOrder []string
}

func (c ModelCandidate) marshalOrder() []string {
	if len(c.keyOrder) == 0 {
		return candidateInterfaceOrder
	}
	return c.keyOrder
}

func (c ModelCandidate) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, key := range c.marshalOrder() {
		if i > 0 {
			b.WriteByte(',')
		}
		var value any
		switch key {
		case "id":
			value = c.ID
		case "tier":
			value = string(c.Tier)
		case "family":
			value = c.Family
		case "prompt_usd_per_mtok":
			value = c.PromptUSDPerMtok
		case "completion_usd_per_mtok":
			value = c.CompletionUSDPerMtok
		case "priority":
			value = c.Priority
		default:
			continue
		}
		encodedKey, err := jscompat.Stringify(key)
		if err != nil {
			return nil, err
		}
		encodedValue, err := jscompat.Stringify(value)
		if err != nil {
			return nil, err
		}
		b.Write(encodedKey)
		b.WriteByte(':')
		b.Write(encodedValue)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON preserves the incoming key order so a candidate decoded from a
// fixture re-marshals byte-identically.
func (c *ModelCandidate) UnmarshalJSON(data []byte) error {
	type plain struct {
		ID                   string            `json:"id"`
		Tier                 ModelTier         `json:"tier"`
		Family               string            `json:"family"`
		PromptUSDPerMtok     jscompat.JSNumber `json:"prompt_usd_per_mtok"`
		CompletionUSDPerMtok jscompat.JSNumber `json:"completion_usd_per_mtok"`
		Priority             jscompat.JSNumber `json:"priority"`
	}
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	order, err := jsonObjectKeyOrder(data)
	if err != nil {
		return err
	}
	*c = ModelCandidate{
		ID:                   p.ID,
		Tier:                 p.Tier,
		Family:               p.Family,
		PromptUSDPerMtok:     p.PromptUSDPerMtok,
		CompletionUSDPerMtok: p.CompletionUSDPerMtok,
		Priority:             p.Priority,
		keyOrder:             order,
	}
	return nil
}

// jsonObjectKeyOrder returns the top-level keys of a JSON object in the order
// they appear in the encoded bytes.
func jsonObjectKeyOrder(data []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("adaptive: expected JSON object, got %v", tok)
	}
	var keys []string
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
			case '}', ']':
				if depth == 0 {
					return keys, nil
				}
				depth--
			}
			continue
		}
		if depth == 0 {
			key, ok := tok.(string)
			if !ok {
				return nil, fmt.Errorf("adaptive: expected object key, got %v", tok)
			}
			keys = append(keys, key)
			// Consume the value; nested containers are skipped by depth.
			value, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if delim, ok := value.(json.Delim); ok && (delim == '{' || delim == '[') {
				depth++
			}
		}
	}
}

// DeriveFamily derives the vendor family from a model id. Ids follow
// `<provider>/<vendor>/<model>`; shorter ids fall back gracefully.
func DeriveFamily(id string) string {
	var parts []string
	for _, p := range strings.Split(jscompat.Trim(id), "/") {
		if len(p) > 0 {
			parts = append(parts, p)
		}
	}
	if len(parts) >= 3 {
		return jsLowerCase(parts[1])
	}
	if len(parts) == 2 {
		return jsLowerCase(parts[0])
	}
	if len(parts) == 0 {
		// `(parts[0] ?? "unknown")` — an all-empty id yields no parts at all.
		return jsLowerCase("unknown")
	}
	return jsLowerCase(parts[0])
}

// RoleAdversaries mirrors ROLE_ADVERSARIES: the key role must NOT use any
// family currently in use by any listed adversary role. Hard constraint — if
// the only acceptable family is busy/cooling the router waits rather than
// falling back to an adversary's family.
var RoleAdversaries = map[string][]string{
	"review-prover": {"review-lens-investigator"},
}

// ── event / config / choice shapes ───────────────────────────────────────

type AdaptiveRouteEvent struct {
	Slot          string            `json:"slot"`
	Tier          ModelTier         `json:"tier"`
	Model         string            `json:"model"`
	PreviousModel string            `json:"previous_model"`
	Switched      bool              `json:"switched"`
	Reason        string            `json:"reason"`
	Score         jscompat.JSNumber `json:"score"`
	ElapsedS      jscompat.JSNumber `json:"elapsed_s"`
	Attempts      jscompat.JSNumber `json:"attempts"`
	Successes     jscompat.JSNumber `json:"successes"`
	Failures      jscompat.JSNumber `json:"failures"`
	RateLimits    jscompat.JSNumber `json:"rate_limits"`
	LatencyEwma   jscompat.JSNumber `json:"latency_ewma"`
	ToksecEwma    jscompat.JSNumber `json:"toksec_ewma"`
	Error         ErrorText         `json:"error"`
}

// ErrorText is register()'s clipped error string.
//
// DIVERGENCE (type only, never value): the TS field is a plain `string`. Go
// needs a distinct type because register() clips with a UTF-16 slice, which can
// cut an astral character in half and leave an UNPAIRED SURROGATE. ES2019
// well-formed JSON.stringify emits that as "\ud83d"; encoding/json substitutes
// U+FFFD and destroys it. Convert with string(x) at any call site that wants a
// plain string.
type ErrorText string

func (t ErrorText) MarshalJSON() ([]byte, error) {
	raw := string(t)
	if !hasLoneSurrogate(raw) {
		return jscompat.Stringify(raw)
	}
	var out bytes.Buffer
	out.WriteByte('"')
	var run strings.Builder
	flush := func() error {
		if run.Len() == 0 {
			return nil
		}
		encoded, err := jscompat.Stringify(run.String())
		if err != nil {
			return err
		}
		out.Write(encoded[1 : len(encoded)-1]) // strip the quotes
		run.Reset()
		return nil
	}
	for i := 0; i < len(raw); {
		cp, size := decodeWTF8(raw, i)
		i += size
		if cp >= 0xD800 && cp <= 0xDFFF {
			if err := flush(); err != nil {
				return nil, err
			}
			fmt.Fprintf(&out, "\\u%04x", cp)
			continue
		}
		run.WriteRune(cp)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	out.WriteByte('"')
	return out.Bytes(), nil
}

func hasLoneSurrogate(s string) bool {
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		if cp >= 0xD800 && cp <= 0xDFFF {
			return true
		}
		i += size
	}
	return false
}

// AdaptiveRouterConfig mirrors the TS options bag. Pointer fields are the TS
// `?:` optionals — nil is `undefined`, which is what the `??` / truthiness
// guards actually test for.
type AdaptiveRouterConfig struct {
	HighModels []ModelCandidate `json:"high_models"`
	LowModels  []ModelCandidate `json:"low_models"`
	// FrontierModels has NO hard-coded default: an unset pool stays empty and
	// callers fall back to HIGH via EffectiveTier.
	FrontierModels []ModelCandidate `json:"frontier_models"`
	MaxAttempts    *float64         `json:"max_attempts"`
	// RandomSeed nil == the TS `undefined`, which makes makeRng return
	// Math.random itself. Note the TS coerces with `seed >>> 0`, so an
	// explicit JS `null` seeds with 0 — pass a pointer to 0 for that.
	RandomSeed *float64                 `json:"random_seed"`
	OnEvent    func(AdaptiveRouteEvent) `json:"-"`
}

type modelStats struct {
	ModelID         string
	Slot            string
	Tier            ModelTier
	Attempts        float64
	Successes       float64
	Failures        float64
	RateLimits      float64
	Inflight        float64
	CooldownUntilMs float64
	LatencyEwma     float64
	ToksecEwma      float64
	QualityEwma     float64
	RewardCount     float64
	LastError       string
}

type RouteChoice struct {
	Slot          string            `json:"slot"`
	Tier          ModelTier         `json:"tier"`
	Candidate     ModelCandidate    `json:"candidate"`
	Score         jscompat.JSNumber `json:"score"`
	PreviousModel string            `json:"previous_model"`
	Switched      bool              `json:"switched"`
	Reason        string            `json:"reason"`
}

// ── defaults (verbatim from v3-harness) ──────────────────────────────────

func defaultPool(tier ModelTier, rows [][3]any) []ModelCandidate {
	out := make([]ModelCandidate, 0, len(rows))
	for _, row := range rows {
		id := row[0].(string)
		out = append(out, ModelCandidate{
			ID:                   id,
			Tier:                 tier,
			Family:               DeriveFamily(id),
			PromptUSDPerMtok:     jscompat.JSNumber(row[1].(float64)),
			CompletionUSDPerMtok: jscompat.JSNumber(row[2].(float64)),
			Priority:             0,
			keyOrder:             candidateDefaultsOrder,
		})
	}
	return out
}

func DefaultHighModels() []ModelCandidate {
	return defaultPool(ModelTierHigh, [][3]any{
		{"openrouter/deepseek/deepseek-v4-flash-0731", 0.09, 0.18},
		{"openrouter/qwen/qwen3.6-plus", 0.325, 1.95},
		{"openrouter/qwen/qwen3.5-plus-20260420", 0.4, 2.4},
		{"openrouter/deepseek/deepseek-v4-pro", 0.435, 0.87},
		{"openrouter/moonshotai/kimi-k2.6", 0.74, 3.49},
		// GLM 5.1 — pricing refreshed from OpenRouter catalogue (was 1.05/3.5).
		{"openrouter/z-ai/glm-5.1", 0.98, 3.08},
		// MiniMax — bumped 2.5 -> 2.7 (latest stable). New pricing 0.20/1.20.
		{"openrouter/minimax/minimax-m2.7", 0.2, 1.2},
		{"openrouter/qwen/qwen3-coder-next", 0.14, 0.8},
	})
}

func DefaultLowModels() []ModelCandidate {
	// qwen/qwen3.6-35b-a3b is intentionally excluded (no tools support).
	return defaultPool(ModelTierLow, [][3]any{
		{"openrouter/deepseek/deepseek-v4-flash-0731", 0.09, 0.18},
		{"openrouter/qwen/qwen3.6-flash", 0.25, 1.5},
		{"openrouter/deepseek/deepseek-v4-flash", 0.14, 0.28},
		{"openrouter/google/gemma-4-26b-a4b-it", 0.06, 0.33},
		{"openrouter/qwen/qwen3.5-35b-a3b", 0.1625, 1.3},
		{"openrouter/qwen/qwen3.6-27b", 0.325, 3.25},
		{"openrouter/moonshotai/kimi-k2.5", 0.44, 2.0},
	})
}

// ── normalization ────────────────────────────────────────────────────────

// NormalizeCandidateModel keeps the "openrouter/" prefix intact (only statsKey
// strips it) and merely trims.
func NormalizeCandidateModel(model string) string {
	return jscompat.Trim(model)
}

func statsKey(slot string, candidate ModelCandidate) string {
	id := jscompat.Trim(candidate.ID)
	if strings.HasPrefix(id, "openrouter/") {
		id = id[len("openrouter/"):]
	}
	return jscompat.Trim(slot) + ":" + id
}

type normalizedConfig struct {
	HighModels     []ModelCandidate
	LowModels      []ModelCandidate
	FrontierModels []ModelCandidate
	MaxAttempts    float64
	RandomSeed     *float64
	OnEvent        func(AdaptiveRouteEvent)
}

func normalizePool(in []ModelCandidate, tier ModelTier) []ModelCandidate {
	out := make([]ModelCandidate, 0, len(in))
	for i, c := range in {
		id := NormalizeCandidateModel(c.ID)
		family := DeriveFamily(id)
		if len(c.Family) > 0 {
			family = jsLowerCase(c.Family)
		}
		normalized := c
		normalized.ID = id
		normalized.Tier = tier
		normalized.Family = family
		normalized.Priority = jscompat.JSNumber(float64(i))
		out = append(out, normalized)
	}
	return out
}

func normalizeConfig(cfg AdaptiveRouterConfig) normalizedConfig {
	high := cfg.HighModels
	if len(high) == 0 {
		high = DefaultHighModels()
	}
	low := cfg.LowModels
	if len(low) == 0 {
		low = DefaultLowModels()
	}
	maxAttempts := 3.0
	if cfg.MaxAttempts != nil && jscompat.Truthy(*cfg.MaxAttempts) && *cfg.MaxAttempts > 0 {
		maxAttempts = *cfg.MaxAttempts
	}
	onEvent := cfg.OnEvent
	if onEvent == nil {
		onEvent = func(AdaptiveRouteEvent) {}
	}
	return normalizedConfig{
		HighModels: normalizePool(high, ModelTierHigh),
		LowModels:  normalizePool(low, ModelTierLow),
		// Frontier: NO default fill.
		FrontierModels: normalizePool(cfg.FrontierModels, ModelTierFrontier),
		MaxAttempts:    maxAttempts,
		RandomSeed:     cfg.RandomSeed,
		OnEvent:        onEvent,
	}
}

// ── error classification ─────────────────────────────────────────────────

func errorText(err *JSValue) string {
	if isNullish(err) {
		return ""
	}
	if err.Kind == JSError {
		if jsTruthy(err.Cause) {
			return err.Message + " cause=" + errorText(err.Cause)
		}
		return err.Message
	}
	if err.Kind == JSString {
		return err.Str
	}
	if encoded, ok := jsonStringify(err); ok {
		return encoded
	}
	return jsString(err)
}

func statusCodeOf(err *JSValue) (float64, bool) {
	if isNullish(err) || !isObjectLike(err) {
		return 0, false
	}
	for _, key := range []string{"status", "statusCode", "status_code"} {
		if v := getProp(err, key); v != nil && v.Kind == JSNumber {
			return v.Num, true
		}
	}
	return 0, false
}

func IsLikelyRateLimit(err *JSValue) bool {
	if code, ok := statusCodeOf(err); ok && code == 429 {
		return true
	}
	text := jsLowerCase(errorText(err))
	return strings.Contains(text, "429") ||
		strings.Contains(text, "rate limit") ||
		strings.Contains(text, "rate_limit") ||
		strings.Contains(text, "too many requests")
}

func IsLikelyProviderIncompatible(err *JSValue) bool {
	text := jsLowerCase(errorText(err))
	return containsAny(text,
		"no endpoints found",
		"can handle the requested parameters",
		"unsupported parameter",
		"unsupported parameters",
		"does not support tools",
		"doesn't support tools",
		"does not support structured",
		"doesn't support structured",
	)
}

func IsLikelyTimeout(err *JSValue) bool {
	if jsTruthy(err) && isObjectLike(err) {
		if name := getProp(err, "name"); name != nil && name.Kind == JSString && name.Str == "AbortError" {
			return true
		}
	}
	text := jsLowerCase(errorText(err))
	return containsAny(text, "timeout", "timed out", "deadline exceeded")
}

func IsLikelyStructuredFailure(err *JSValue) bool {
	text := jsLowerCase(errorText(err))
	if containsAny(text, "structured output failed", "response_format", "json_schema", "invalid json") {
		return true
	}
	if strings.Contains(text, "json") && containsAny(text, "unmarshal", "decode", "parse") {
		return true
	}
	if strings.Contains(text, "schema") && containsAny(text, "validation", "parse", "400") {
		return true
	}
	return false
}

// IsLikelyTransientProviderError covers HTTP 5xx and generic "upstream provider
// hiccup" errors that are neither rate limits nor schema issues.
func IsLikelyTransientProviderError(err *JSValue) bool {
	if code, ok := statusCodeOf(err); ok && code >= 500 && code <= 599 {
		return true
	}
	text := jsLowerCase(errorText(err))
	return containsAny(text,
		"provider returned error",
		`error_type":"unmapped"`,
		"upstream",
		"bad gateway",
		"service unavailable",
		"gateway timeout",
	)
}

func IsRetryableRouteError(err *JSValue) bool {
	return IsLikelyRateLimit(err) ||
		IsLikelyProviderIncompatible(err) ||
		IsLikelyTimeout(err) ||
		IsLikelyStructuredFailure(err) ||
		IsLikelyTransientProviderError(err)
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// ── math helpers ─────────────────────────────────────────────────────────

// The float64() conversions are load-bearing: Go permits fusing `x*y + z` into
// an FMA (arm64/ppc64/s390x/riscv64 backends do), which would round once where
// V8 rounds twice. An explicit conversion forces the intermediate rounding, so
// the port keeps bit-parity with the TS on every architecture.

func updateEwma(prev float64, next float64, alpha float64) float64 {
	if prev <= 0 {
		return next
	}
	return float64(alpha*next) + float64((1-alpha)*prev)
}

func updateRewardEwma(prev float64, next float64, count float64, alpha float64) float64 {
	if count <= 0 {
		return next
	}
	return float64(alpha*next) + float64((1-alpha)*prev)
}

func rateLimitCooldownSeconds(count float64) float64 {
	if count <= 0 {
		return 10.0
	}
	c := count
	if count > 5 {
		c = 5
	}
	return 10 * jsPow(2, c-1)
}

// immediateFailureReward returns (reward, true) or (0, false) for the TS
// `number | null`.
func immediateFailureReward(err *JSValue) (float64, bool) {
	if IsLikelyRateLimit(err) || IsLikelyProviderIncompatible(err) {
		return -1.0, true
	}
	if IsLikelyTimeout(err) {
		return -0.95, true
	}
	if IsLikelyStructuredFailure(err) {
		return -0.9, true
	}
	if IsLikelyTransientProviderError(err) {
		return -0.85, true
	}
	if strings.Contains(jsLowerCase(errorText(err)), "schema") {
		return -0.9, true
	}
	return 0, false
}

// ── seedable RNG (mulberry32) ────────────────────────────────────────────

// toUint32 is the JS `>>> 0` coercion applied to the configured seed.
func toUint32(f float64) uint32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	m := math.Mod(math.Trunc(f), 4294967296)
	if m < 0 {
		m += 4294967296
	}
	return uint32(m)
}

// makeRng mirrors adaptive.ts:344 — an unset seed hands back Math.random
// itself, so the router's stream is the process-global one.
//
// jscompat.Mulberry32 was verified to reproduce the TS body bit-for-bit
// (Math.imul == uint32 multiply, `>>> 0` == uint32 wrap) across seeds
// 0/1/7/42/123456789/2^31-1/2^32-1; the fixture cases rng/mulberry32/* pin
// the first 50 draws of each.
func makeRng(seed *float64) func() float64 {
	if seed == nil {
		return func() float64 { return randomFloat() }
	}
	return jscompat.Mulberry32(toUint32(*seed))
}

// gauss is Box-Muller. The draw ORDER matters: u is drawn (and re-drawn while
// zero) before v, and the returned expression consumes them as
// sqrt(-2*log(u)) * cos(2*PI*v).
func gauss(rng func() float64) float64 {
	u, v := 0.0, 0.0
	for u == 0 {
		u = rng()
	}
	for v == 0 {
		v = rng()
	}
	return math.Sqrt(-2.0*jsLog(u)) * jsCos(2.0*jsPi*v)
}

// sampleGamma is Marsaglia & Tsang, recursive for shape < 1. The rejection
// loop draws a VARIABLE number of randoms per call (2 per gauss, 1 for u,
// repeated on rejection), which is exactly why the whole router's stream
// position is state-dependent.
func sampleGamma(rng func() float64, shape float64) float64 {
	if shape <= 0 {
		return 0
	}
	if shape < 1 {
		u := math.Max(rng(), 1e-12)
		return sampleGamma(rng, shape+1) * jsPow(u, 1/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9*d)
	for {
		x := gauss(rng)
		v := 1 + float64(c*x)
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng()
		if u < 1-float64(0.0331*x*x*x*x) {
			return d * v
		}
		if jsLog(u) < float64(0.5*x*x)+float64(d*(1-v+jsLog(v))) {
			return d * v
		}
	}
}

func sampleBeta(rng func() float64, alpha float64, beta float64) float64 {
	x := sampleGamma(rng, alpha)
	y := sampleGamma(rng, beta)
	if x <= 0 && y <= 0 {
		return 0.5
	}
	return x / (x + y)
}

// ── TryPickResult ────────────────────────────────────────────────────────

// TryPickResult is the TS discriminated union. Ok==false means the constraint
// is TEMPORARILY unsatisfiable (every acceptable candidate is cooling); a
// structurally unsatisfiable constraint returns Ok==true with the reason
// prefixed `constraint-relaxed:` instead of blocking forever.
type TryPickResult struct {
	Ok               bool
	Choice           RouteChoice
	Reason           string
	ExcludedFamilies []string
	RetryAfterMs     jscompat.JSNumber
}

// MarshalJSON emits only the union arm's own keys, in literal order:
// `{ok, choice}` or `{ok, reason, excludedFamilies, retryAfterMs}`.
func (r TryPickResult) MarshalJSON() ([]byte, error) {
	if r.Ok {
		return jscompat.Stringify(struct {
			Ok     bool        `json:"ok"`
			Choice RouteChoice `json:"choice"`
		}{true, r.Choice})
	}
	families := r.ExcludedFamilies
	if families == nil {
		families = []string{}
	}
	return jscompat.Stringify(struct {
		Ok               bool              `json:"ok"`
		Reason           string            `json:"reason"`
		ExcludedFamilies []string          `json:"excludedFamilies"`
		RetryAfterMs     jscompat.JSNumber `json:"retryAfterMs"`
	}{false, r.Reason, families, r.RetryAfterMs})
}

// ── the router ───────────────────────────────────────────────────────────

type AdaptiveModelRouter struct {
	mu  sync.Mutex
	cfg normalizedConfig
	rng func() float64
	// stats is keyed by statsKey(slot, candidate) — note the leading
	// "openrouter/" strip, which deliberately merges two provider spellings.
	stats *jscompat.OrderedMap[string, *modelStats]
	last  map[string]string
	// lastFamilyBySlot is the last family successfully picked, by slot. Drives
	// the cross-role adversarial constraints.
	lastFamilyBySlot map[string]string
}

func NewAdaptiveModelRouter(cfg AdaptiveRouterConfig) *AdaptiveModelRouter {
	return &AdaptiveModelRouter{
		cfg:              normalizeConfig(cfg),
		rng:              makeRng(cfg.RandomSeed),
		stats:            jscompat.NewOrderedMap[string, *modelStats](),
		last:             map[string]string{},
		lastFamilyBySlot: map[string]string{},
	}
}

// MaxAttempts is the TS `get maxAttempts()`.
func (r *AdaptiveModelRouter) MaxAttempts() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.MaxAttempts
}

func (r *AdaptiveModelRouter) CandidatesForTier(tier ModelTier) []ModelCandidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.candidatesForTier(tier)
}

func (r *AdaptiveModelRouter) candidatesForTier(tier ModelTier) []ModelCandidate {
	if tier == ModelTierFrontier {
		return r.cfg.FrontierModels
	}
	if tier == ModelTierHigh {
		return r.cfg.HighModels
	}
	return r.cfg.LowModels
}

// EffectiveTier resolves the tier a caller should actually route on. FRONTIER
// degrades to HIGH when the frontier pool is empty, so no run ever depends on
// frontier being configured.
func (r *AdaptiveModelRouter) EffectiveTier(tier ModelTier) ModelTier {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tier == ModelTierFrontier && len(r.cfg.FrontierModels) == 0 {
		return ModelTierHigh
	}
	return tier
}

// CandidatesForTierFiltered is CandidatesForTier pre-filtered by the role's
// adversarial exclusions. If the filter would empty the pool (structurally
// unsatisfiable) the full pool comes back unchanged.
func (r *AdaptiveModelRouter) CandidatesForTierFiltered(tier ModelTier, forSlot string) []ModelCandidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := r.candidatesForTier(tier)
	excludeFams := r.computeExcludeFamilies(forSlot)
	if len(excludeFams) == 0 {
		return all
	}
	var filtered []ModelCandidate
	for _, c := range all {
		if !containsString(excludeFams, c.Family) {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) > 0 {
		return filtered
	}
	return all
}

// computeExcludeFamilies is the family-set the role at `slot` MUST avoid,
// derived from RoleAdversaries[slot] ∪ {last family used by each adversary}.
// Returned as an insertion-ordered slice because Array.from(Set) order reaches
// output through TryPickResult.excludedFamilies.
func (r *AdaptiveModelRouter) computeExcludeFamilies(slot string) []string {
	adversarySlots := RoleAdversaries[slot]
	if len(adversarySlots) == 0 {
		return nil
	}
	var fams []string
	for _, adv := range adversarySlots {
		f := r.lastFamilyBySlot[adv]
		// `if (f)` — an empty-string family is falsy and never added.
		if f != "" && !containsString(fams, f) {
			fams = append(fams, f)
		}
	}
	return fams
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TryPick is the non-blocking pick. nowMs is the TS default parameter
// `nowMs: number = Date.now()` — pass at most one.
func (r *AdaptiveModelRouter) TryPick(slot string, tier ModelTier, nowMs ...float64) TryPickResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tryPickLocked(slot, tier, nowMs...)
}

func (r *AdaptiveModelRouter) tryPickLocked(slot string, tier ModelTier, nowMsOpt ...float64) TryPickResult {
	nowMs := nowMillis()
	if len(nowMsOpt) > 0 {
		nowMs = nowMsOpt[0]
	}

	allCandidates := r.candidatesForTier(tier)
	if len(allCandidates) == 0 {
		// No candidates configured at all — choose() installs the hard-coded
		// fallback. Treat as a successful pick.
		choice := r.choose(slot, tier, nowMs, nil, false)
		st := r.statsFor(slot, choice.Candidate)
		st.Inflight += 1
		r.lastFamilyBySlot[slot] = choice.Candidate.Family
		return TryPickResult{Ok: true, Choice: choice}
	}

	excludeFams := r.computeExcludeFamilies(slot)
	candidates := allCandidates
	if len(excludeFams) > 0 {
		filtered := make([]ModelCandidate, 0, len(allCandidates))
		for _, c := range allCandidates {
			if !containsString(excludeFams, c.Family) {
				filtered = append(filtered, c)
			}
		}
		candidates = filtered
	}
	structurallyRelaxed := false
	if len(candidates) == 0 {
		// Every configured model belongs to an excluded family. Relax with
		// telemetry rather than blocking forever.
		candidates = allCandidates
		structurallyRelaxed = true
	}

	// Is at least one acceptable candidate not currently cooling?
	anyAvailable := false
	for _, c := range candidates {
		if r.statsFor(slot, c).CooldownUntilMs <= nowMs {
			anyAvailable = true
			break // Array.prototype.some short-circuits
		}
	}
	if !anyAvailable && !structurallyRelaxed {
		earliestMs := math.Inf(1)
		for _, c := range candidates {
			st := r.statsFor(slot, c)
			if st.CooldownUntilMs > nowMs && st.CooldownUntilMs < earliestMs {
				earliestMs = st.CooldownUntilMs
			}
		}
		retryAfterMs := 1000.0
		if !math.IsInf(earliestMs, 0) && !math.IsNaN(earliestMs) {
			retryAfterMs = math.Max(100, earliestMs-nowMs)
		}
		families := excludeFams
		if families == nil {
			families = []string{}
		}
		return TryPickResult{
			Ok:               false,
			Reason:           "all-acceptable-busy",
			ExcludedFamilies: families,
			RetryAfterMs:     jscompat.JSNumber(retryAfterMs),
		}
	}

	choice := r.choose(slot, tier, nowMs, candidates, structurallyRelaxed)
	st := r.statsFor(slot, choice.Candidate)
	st.Inflight += 1
	r.lastFamilyBySlot[slot] = choice.Candidate.Family
	return TryPickResult{Ok: true, Choice: choice}
}

// PickOptions mirrors `{ timeoutMs?: number; signal?: AbortSignal }`.
type PickOptions struct {
	TimeoutMs *float64
	Signal    *AbortSignal
}

// Pick blocks (polling TryPick with bounded exponential backoff) until a
// candidate is available or `timeoutMs` (default 5 minutes) elapses. The TS
// throws on timeout and on abort; Go returns the same messages as errors.
//
// Caller MUST call Register exactly once with the same RouteChoice to clear
// the in-flight counter — see the package note on the leak.
func (r *AdaptiveModelRouter) Pick(slot string, tier ModelTier, opts ...PickOptions) (RouteChoice, error) {
	var opt PickOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	timeoutMs := 5 * 60 * 1000.0
	if opt.TimeoutMs != nil {
		timeoutMs = *opt.TimeoutMs
	}
	deadline := nowMillis() + timeoutMs
	waitMs := 100.0 // start small; exponential backoff
	maxWaitMs := 2000.0

	for {
		if opt.Signal.Aborted() {
			return RouteChoice{}, fmt.Errorf("router.pick aborted for slot=%s tier=%s", slot, tier)
		}
		r.mu.Lock()
		result := r.tryPickLocked(slot, tier)
		r.mu.Unlock()
		if result.Ok {
			return result.Choice, nil
		}
		failedReason := result.Reason
		excludedFamilies := result.ExcludedFamilies
		retryAfterMs := float64(result.RetryAfterMs)
		now := nowMillis()
		if now >= deadline {
			fams := strings.Join(excludedFamilies, ",")
			if fams == "" {
				fams = "(none)"
			}
			return RouteChoice{}, fmt.Errorf(
				"router.pick timeout after %sms (%s) — slot=%s tier=%s excludedFamilies=[%s]",
				jscompat.FormatNumber(timeoutMs), failedReason, slot, tier, fams)
		}
		sleepMs := math.Min(math.Min(waitMs, math.Max(50, deadline-now)), retryAfterMs)
		sleepMillis(sleepMs, opt.Signal)
		waitMs = math.Min(waitMs*2, maxWaitMs)
	}
}

// PickSync always returns (soft fallback on every failure). Retained for tests
// and legacy synchronous call sites — the review pipeline prefers Pick.
func (r *AdaptiveModelRouter) PickSync(slot string, tier ModelTier) RouteChoice {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := r.tryPickLocked(slot, tier)
	if result.Ok {
		return result.Choice
	}
	// Forced fallback for sync callers: bypass the adversarial constraint and
	// pick best-available from the full pool. Note the fresh Date.now() — it is
	// NOT the nowMs tryPick used.
	choice := r.choose(slot, tier, nowMillis(), r.candidatesForTier(tier), true)
	st := r.statsFor(slot, choice.Candidate)
	st.Inflight += 1
	r.lastFamilyBySlot[slot] = choice.Candidate.Family
	return choice
}

// Register records the outcome of a call started by Pick. Pass err == nil (or
// a nullish JSValue) on success.
func (r *AdaptiveModelRouter) Register(choice RouteChoice, elapsedSeconds float64, completionTokens float64, err *JSValue) AdaptiveRouteEvent {
	event, onEvent := r.registerLocked(choice, elapsedSeconds, completionTokens, err)
	// The TS calls on_event synchronously at the tail of register(), wrapped in
	// try/catch. Go calls it AFTER dropping the lock: the event is a value copy,
	// so a listener sees exactly the snapshot a TS listener would, but one that
	// calls back into the router re-enters (as it does in JS) instead of
	// deadlocking on a non-reentrant mutex.
	func() {
		defer func() { _ = recover() }() /* swallow */
		onEvent(event)
	}()
	return event
}

func (r *AdaptiveModelRouter) registerLocked(choice RouteChoice, elapsedSeconds float64, completionTokens float64, err *JSValue) (AdaptiveRouteEvent, func(AdaptiveRouteEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	st := r.statsFor(choice.Slot, choice.Candidate)
	if st.Inflight > 0 {
		st.Inflight -= 1
	}
	st.Attempts += 1
	if isNullish(err) {
		st.Successes += 1
	} else {
		st.Failures += 1
		st.LastError = utf16SliceTo(errorText(err), 400)
		now := nowMillis()
		switch {
		case IsLikelyRateLimit(err):
			st.RateLimits += 1
			st.CooldownUntilMs = now + float64(rateLimitCooldownSeconds(st.RateLimits)*1000)
		case IsLikelyProviderIncompatible(err):
			st.CooldownUntilMs = now + 3600*1000
		case IsLikelyTimeout(err):
			st.CooldownUntilMs = now + 120*1000
		case IsLikelyTransientProviderError(err):
			// Short cooldown — encourage a different model next call, but let
			// this one back quickly since 5xx is usually genuinely transient.
			st.CooldownUntilMs = now + 60*1000
		}
		if reward, ok := immediateFailureReward(err); ok {
			st.QualityEwma = updateRewardEwma(st.QualityEwma, reward, st.RewardCount, 0.08)
			st.RewardCount += 1
		}
	}
	st.LatencyEwma = updateEwma(st.LatencyEwma, elapsedSeconds, 0.35)
	if completionTokens > 0 && elapsedSeconds > 0 {
		st.ToksecEwma = updateEwma(st.ToksecEwma, completionTokens/elapsedSeconds, 0.35)
	}

	errText := ""
	if jsTruthy(err) {
		errText = utf16SliceTo(errorText(err), 300)
	}
	event := AdaptiveRouteEvent{
		Slot:          choice.Slot,
		Tier:          choice.Tier,
		Model:         choice.Candidate.ID,
		PreviousModel: choice.PreviousModel,
		Switched:      choice.Switched,
		Reason:        choice.Reason,
		Score:         choice.Score,
		ElapsedS:      jscompat.JSNumber(elapsedSeconds),
		Attempts:      jscompat.JSNumber(st.Attempts),
		Successes:     jscompat.JSNumber(st.Successes),
		Failures:      jscompat.JSNumber(st.Failures),
		RateLimits:    jscompat.JSNumber(st.RateLimits),
		LatencyEwma:   jscompat.JSNumber(st.LatencyEwma),
		ToksecEwma:    jscompat.JSNumber(st.ToksecEwma),
		Error:         ErrorText(errText),
	}
	return event, r.cfg.OnEvent
}

// ── internals ────────────────────────────────────────────────────────────

// choose scores and selects from a pre-filtered candidate list. `relaxed`
// prefixes the reason with `constraint-relaxed:` so telemetry can tell a clean
// pick from a relaxed one.
func (r *AdaptiveModelRouter) choose(
	slot string,
	tier ModelTier,
	nowMs float64,
	preFilteredCandidates []ModelCandidate,
	relaxed bool,
) RouteChoice {
	candidates := preFilteredCandidates
	if len(candidates) == 0 {
		candidates = r.candidatesForTier(tier)
	}
	if len(candidates) == 0 {
		candidates = []ModelCandidate{{
			ID:                   "openrouter/openai/gpt-oss-120b",
			Tier:                 tier,
			Family:               "openai",
			PromptUSDPerMtok:     0,
			CompletionUSDPerMtok: 0,
			Priority:             0,
		}}
	}
	previous := r.last[slot]
	previousScore := math.Inf(-1)
	previousCooling := false
	previousInflight := 0.0
	previousRateLimits := 0.0
	best := RouteChoice{
		Slot:          slot,
		Tier:          tier,
		Candidate:     candidates[0],
		Score:         jscompat.JSNumber(math.Inf(-1)),
		PreviousModel: previous,
		Switched:      false,
		Reason:        "",
	}
	for _, cand := range candidates {
		if jscompat.Trim(cand.ID) == "" {
			continue
		}
		st := r.statsFor(slot, cand)
		if st.CooldownUntilMs > nowMs {
			if cand.ID == previous {
				previousCooling = true
				previousInflight = st.Inflight
				previousRateLimits = st.RateLimits
			}
			continue
		}
		score := r.score(tier, cand, st)
		if cand.ID == previous {
			previousScore = score
			previousInflight = st.Inflight
			previousRateLimits = st.RateLimits
		}
		if score > float64(best.Score) {
			best = RouteChoice{Slot: slot, Tier: tier, Candidate: cand, Score: jscompat.JSNumber(score),
				PreviousModel: previous, Switched: false, Reason: ""}
		}
	}
	// Forced fallback: every candidate is cooling. Pick least-bad with a -2.0
	// penalty. NOTE: this re-scores every candidate — a SECOND round of RNG
	// draws — and does not skip blank ids.
	if math.IsInf(float64(best.Score), -1) {
		for _, cand := range candidates {
			st := r.statsFor(slot, cand)
			score := r.score(tier, cand, st) - 2.0
			if score > float64(best.Score) {
				best = RouteChoice{Slot: slot, Tier: tier, Candidate: cand, Score: jscompat.JSNumber(score),
					PreviousModel: previous, Switched: false, Reason: "forced"}
			}
		}
	}
	best.PreviousModel = previous
	best.Switched = previous != "" && previous != best.Candidate.ID
	best.Reason = routeReason(best, previousScore, previousCooling, previousInflight, previousRateLimits)
	if relaxed {
		best.Reason = "constraint-relaxed:" + best.Reason
	}
	r.last[slot] = best.Candidate.ID
	return best
}

func (r *AdaptiveModelRouter) score(tier ModelTier, cand ModelCandidate, st *modelStats) float64 {
	// Cold-model optimism: an unattempted model gets a uniform draw in
	// [0.72, 0.90) instead of a Beta posterior.
	reliability := 0.0
	if st.Attempts == 0 {
		reliability = 0.72 + float64(r.rng()*0.18)
	} else {
		reliability = sampleBeta(r.rng, st.Successes+2, st.Failures+1)
	}
	latency := 15.0 + float64(cand.Priority)
	if st.LatencyEwma > 0 {
		latency = st.LatencyEwma
	}
	toksec := 35.0
	if st.ToksecEwma > 0 {
		toksec = st.ToksecEwma
	}
	price := float64(cand.PromptUSDPerMtok) + float64(cand.CompletionUSDPerMtok)
	if price <= 0 {
		price = 1.0
	}
	cost := 1.0 / (1.0 + price)
	speed := toksec / (toksec + 80.0)
	latencyPen := latency / (latency + 45.0)
	pressurePen := float64(st.Inflight * 0.12)
	rlRisk := 0.0
	if st.Attempts > 0 {
		rlRisk = st.RateLimits / st.Attempts
	}
	quality := 0.0
	if st.RewardCount > 0 {
		quality = math.Max(-1.0, math.Min(1.0, st.QualityEwma))
	}
	// FRONTIER scores like HIGH — deep-reasoning adjudication work, so
	// reliability dominates and cost/speed matter little.
	if tier == ModelTierHigh || tier == ModelTierFrontier {
		return float64(0.68*reliability) + float64(0.14*speed) + float64(0.08*cost) +
			float64(0.08*quality) - float64(0.08*latencyPen) - pressurePen - float64(0.5*rlRisk)
	}
	return float64(0.38*reliability) + float64(0.3*speed) + float64(0.16*cost) +
		float64(0.06*quality) - float64(0.1*latencyPen) - pressurePen - float64(0.45*rlRisk)
}

func (r *AdaptiveModelRouter) statsFor(slot string, cand ModelCandidate) *modelStats {
	key := statsKey(slot, cand)
	st, ok := r.stats.Get(key)
	if !ok {
		st = &modelStats{
			ModelID: cand.ID,
			Slot:    slot,
			Tier:    cand.Tier,
		}
		r.stats.Set(key, st)
	}
	st.ModelID = cand.ID
	st.Slot = slot
	st.Tier = cand.Tier
	return st
}

func routeReason(
	best RouteChoice,
	previousScore float64,
	previousCooling bool,
	previousInflight float64,
	previousRateLimits float64,
) string {
	if best.Reason == "forced" {
		return "all-cooling"
	}
	if best.PreviousModel == "" {
		return "initial"
	}
	if best.PreviousModel == best.Candidate.ID {
		return "stay"
	}
	if previousCooling {
		return "previous-cooling"
	}
	if previousRateLimits > 0 {
		return "previous-rate-limited"
	}
	if previousInflight > 0 {
		return "previous-busy"
	}
	if !math.IsInf(previousScore, 0) && !math.IsNaN(previousScore) && float64(best.Score) > previousScore {
		return "better-score"
	}
	return "switch"
}

// ── CLI parsing ──────────────────────────────────────────────────────────

// ParseModelList parses a comma-separated `--high` / `--low` flag value.
// Each entry is either "openrouter/qwen/qwen3.6-plus" or
// "openrouter/qwen/qwen3.6-plus@0.325/1.95" (id + prompt$/completion$ per Mtok).
// Cost defaults to zero, which degenerates the cost term to a neutral 0.5.
func ParseModelList(raw *string, tier ModelTier) []ModelCandidate {
	// `if (!raw) return []` — undefined AND the empty string both bail out.
	if raw == nil || *raw == "" {
		return []ModelCandidate{}
	}
	var entries []string
	for _, s := range strings.Split(*raw, ",") {
		trimmed := jscompat.Trim(s)
		if trimmed != "" { // .filter(Boolean)
			entries = append(entries, trimmed)
		}
	}
	out := make([]ModelCandidate, 0, len(entries))
	for i, entry := range entries {
		at := strings.LastIndex(entry, "@")
		id := entry
		prompt := 0.0
		completion := 0.0
		if at > 0 {
			id = entry[:at]
			rest := entry[at+1:]
			slash := strings.Index(rest, "/")
			if slash > 0 {
				// `Number.parseFloat(x) || 0` — NaN and 0 alike become 0.
				prompt = orZero(parseFloatJS(rest[:slash]))
				completion = orZero(parseFloatJS(rest[slash+1:]))
			}
		}
		normalizedID := NormalizeCandidateModel(id)
		out = append(out, ModelCandidate{
			ID:                   normalizedID,
			Tier:                 tier,
			Family:               DeriveFamily(normalizedID),
			PromptUSDPerMtok:     jscompat.JSNumber(prompt),
			CompletionUSDPerMtok: jscompat.JSNumber(completion),
			Priority:             jscompat.JSNumber(float64(i)),
		})
	}
	return out
}

func orZero(f float64) float64 {
	if !jscompat.Truthy(f) {
		return 0
	}
	return f
}

// parseFloatJS is Number.parseFloat: it consumes the LONGEST valid decimal
// prefix (including "Infinity") and returns NaN when there is none. Unlike
// Number(), trailing garbage is ignored and radix prefixes are not honored.
func parseFloatJS(s string) float64 {
	t := jscompat.Trim(s)
	i := 0
	if i < len(t) && (t[i] == '+' || t[i] == '-') {
		i++
	}
	if strings.HasPrefix(t[i:], "Infinity") {
		return jscompat.ToNumber(t[:i] + "Infinity")
	}
	digits := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
		digits++
	}
	if i < len(t) && t[i] == '.' {
		i++
		for i < len(t) && t[i] >= '0' && t[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return math.NaN()
	}
	end := i
	if i < len(t) && (t[i] == 'e' || t[i] == 'E') {
		j := i + 1
		if j < len(t) && (t[j] == '+' || t[j] == '-') {
			j++
		}
		expDigits := 0
		for j < len(t) && t[j] >= '0' && t[j] <= '9' {
			j++
			expDigits++
		}
		if expDigits > 0 {
			end = j
		}
	}
	return jscompat.ToNumber(t[:end])
}
