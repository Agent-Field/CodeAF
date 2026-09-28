//go:build !windows

// Package adaptive is the adaptive model router: it picks a model for each
// call from the configured pool for the caller's tier (high, low, frontier)
// by sampling a score from each candidate's recorded reliability, speed,
// price and current load, and it learns from every registered outcome
// (latency, throughput, rate limits and other failures, each with its own
// cooldown).
//
// A tier whose pool is empty resolves to the high pool, so a run configured
// with nothing but a high pool routes every tier on it.
//
// A single router-wide mutex serialises every public method; Pick takes and
// releases it around each TryPick and never holds it across a sleep.
package adaptive

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ── injectable ambient dependencies ──────────────────────────────────────

// nowMillis is the wall clock in milliseconds.
var nowMillis = func() float64 { return float64(time.Now().UnixMilli()) }

// SetClockForTesting pins the clock. Returns a restore func.
func SetClockForTesting(f func() float64) func() {
	prev := nowMillis
	nowMillis = f
	return func() { nowMillis = prev }
}

// randomFloat is the unseeded random source used when no RandomSeed is
// configured.
var randomFloat = func() float64 { return rand.Float64() }

// SetRandomForTesting pins the unseeded random source. Returns a restore func.
func SetRandomForTesting(f func() float64) func() {
	prev := randomFloat
	randomFloat = f
	return func() { randomFloat = prev }
}

// sleepMillis is the backoff sleep inside Pick, interruptible by the signal.
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
			ms = 1e15
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

// AbortSignal lets a caller interrupt a blocking Pick.
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

// ModelTier names one of the three configurable model pools. A caller asks
// for a tier; EffectiveTier turns that request into the tier actually routed
// on, which is the high tier whenever the requested pool is empty.
type ModelTier string

const (
	ModelTierHigh     ModelTier = "high"
	ModelTierLow      ModelTier = "low"
	ModelTierFrontier ModelTier = "frontier"
)

// ── ModelCandidate ───────────────────────────────────────────────────────

// ModelCandidate is one routable model.
type ModelCandidate struct {
	// ID is the full model id, e.g. "openrouter/qwen/qwen3.6-plus".
	ID string `json:"id"`
	// Tier is the pool the candidate was configured into.
	Tier                 ModelTier `json:"tier"`
	PromptUSDPerMtok     float64   `json:"prompt_usd_per_mtok"`
	CompletionUSDPerMtok float64   `json:"completion_usd_per_mtok"`
	// Priority is the config index: lower = preferred when stats are absent.
	Priority float64 `json:"priority"`
}

// ── event / config / choice shapes ───────────────────────────────────────

// AdaptiveRouteEvent describes one registered outcome or cancellation.
type AdaptiveRouteEvent struct {
	Slot          string    `json:"slot"`
	Tier          ModelTier `json:"tier"`
	Model         string    `json:"model"`
	PreviousModel string    `json:"previous_model"`
	Switched      bool      `json:"switched"`
	Reason        string    `json:"reason"`
	Score         float64   `json:"score"`
	ElapsedS      float64   `json:"elapsed_s"`
	Attempts      float64   `json:"attempts"`
	Successes     float64   `json:"successes"`
	Failures      float64   `json:"failures"`
	RateLimits    float64   `json:"rate_limits"`
	LatencyEwma   float64   `json:"latency_ewma"`
	ToksecEwma    float64   `json:"toksec_ewma"`
	Error         string    `json:"error"`
}

// AdaptiveRouterConfig configures a router. A nil pointer field takes its
// default, and so does an empty high pool; an empty low or frontier pool
// stays empty and degrades to high instead.
type AdaptiveRouterConfig struct {
	HighModels []ModelCandidate `json:"high_models"`
	// LowModels and FrontierModels have no default: an unset pool stays empty
	// and every caller asking for it routes on HIGH instead.
	LowModels      []ModelCandidate `json:"low_models"`
	FrontierModels []ModelCandidate `json:"frontier_models"`
	MaxAttempts    *float64         `json:"max_attempts"`
	// RandomSeed makes the router's sampling reproducible. Nil uses the
	// process-wide random source.
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

// RouteChoice is a picked candidate together with why it was picked.
type RouteChoice struct {
	Slot string `json:"slot"`
	// Tier is the tier actually routed on, after degradation.
	Tier          ModelTier      `json:"tier"`
	Candidate     ModelCandidate `json:"candidate"`
	Score         float64        `json:"score"`
	PreviousModel string         `json:"previous_model"`
	Switched      bool           `json:"switched"`
	Reason        string         `json:"reason"`
}

// ── defaults ─────────────────────────────────────────────────────────────

func defaultPool(tier ModelTier, rows [][3]any) []ModelCandidate {
	out := make([]ModelCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, ModelCandidate{
			ID:                   row[0].(string),
			Tier:                 tier,
			PromptUSDPerMtok:     row[1].(float64),
			CompletionUSDPerMtok: row[2].(float64),
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
		{"openrouter/z-ai/glm-5.1", 0.98, 3.08},
		{"openrouter/minimax/minimax-m2.7", 0.2, 1.2},
		{"openrouter/qwen/qwen3-coder-next", 0.14, 0.8},
	})
}

// ── normalization ────────────────────────────────────────────────────────

// NormalizeCandidateModel trims a model id; the "openrouter/" prefix stays.
func NormalizeCandidateModel(model string) string {
	return strings.TrimSpace(model)
}

// statsKey identifies a stats row. A leading "openrouter/" is stripped so
// the prefixed and bare spellings of one model share their history.
func statsKey(slot string, candidate ModelCandidate) string {
	id := strings.TrimSpace(candidate.ID)
	id = strings.TrimPrefix(id, "openrouter/")
	return strings.TrimSpace(slot) + ":" + id
}

type normalizedConfig struct {
	HighModels     []ModelCandidate
	LowModels      []ModelCandidate
	FrontierModels []ModelCandidate
	MaxAttempts    float64
	OnEvent        func(AdaptiveRouteEvent)
}

// normalizePool trims ids, drops blank entries, stamps the tier and numbers
// priorities by position.
func normalizePool(in []ModelCandidate, tier ModelTier) []ModelCandidate {
	out := make([]ModelCandidate, 0, len(in))
	for _, c := range in {
		id := NormalizeCandidateModel(c.ID)
		if id == "" {
			continue
		}
		normalized := c
		normalized.ID = id
		normalized.Tier = tier
		normalized.Priority = float64(len(out))
		normalized.PromptUSDPerMtok = finiteOrZero(c.PromptUSDPerMtok)
		normalized.CompletionUSDPerMtok = finiteOrZero(c.CompletionUSDPerMtok)
		out = append(out, normalized)
	}
	return out
}

func normalizeConfig(cfg AdaptiveRouterConfig) normalizedConfig {
	high := cfg.HighModels
	if len(high) == 0 {
		high = DefaultHighModels()
	}
	maxAttempts := 3.0
	if cfg.MaxAttempts != nil && *cfg.MaxAttempts > 0 {
		maxAttempts = *cfg.MaxAttempts
	}
	onEvent := cfg.OnEvent
	if onEvent == nil {
		onEvent = func(AdaptiveRouteEvent) {}
	}
	return normalizedConfig{
		HighModels:     normalizePool(high, ModelTierHigh),
		LowModels:      normalizePool(cfg.LowModels, ModelTierLow),
		FrontierModels: normalizePool(cfg.FrontierModels, ModelTierFrontier),
		MaxAttempts:    maxAttempts,
		OnEvent:        onEvent,
	}
}

// ── error classification ─────────────────────────────────────────────────

// StatusCoder is implemented by errors that carry an HTTP status.
type StatusCoder interface {
	ErrorStatusCode() (float64, bool)
}

// Namer is implemented by errors with a symbolic name; "AbortError" marks a
// timeout regardless of message text.
type Namer interface {
	ErrorName() string
}

// Detailer is implemented by errors that carry extra text the classifiers
// should search, such as a provider's raw error body.
type Detailer interface {
	ErrorDetail() string
}

// errorText is the text the classifiers match against: the error message
// plus any detail an error exposes.
func errorText(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	var detailer Detailer
	if errors.As(err, &detailer) {
		if detail := detailer.ErrorDetail(); detail != "" && !strings.Contains(text, detail) {
			text += " " + detail
		}
	}
	return text
}

func statusCodeOf(err error) (float64, bool) {
	var coder StatusCoder
	if err != nil && errors.As(err, &coder) {
		return coder.ErrorStatusCode()
	}
	return 0, false
}

func IsLikelyRateLimit(err error) bool {
	if code, ok := statusCodeOf(err); ok && code == 429 {
		return true
	}
	text := strings.ToLower(errorText(err))
	return containsAny(text, "429", "rate limit", "rate_limit", "too many requests")
}

func IsLikelyProviderIncompatible(err error) bool {
	text := strings.ToLower(errorText(err))
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

func IsLikelyTimeout(err error) bool {
	var namer Namer
	if err != nil && errors.As(err, &namer) && namer.ErrorName() == "AbortError" {
		return true
	}
	text := strings.ToLower(errorText(err))
	return containsAny(text, "timeout", "timed out", "deadline exceeded")
}

func IsLikelyStructuredFailure(err error) bool {
	text := strings.ToLower(errorText(err))
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

// IsLikelyTransientProviderError covers HTTP 5xx and generic upstream
// provider errors that are neither rate limits nor schema issues.
func IsLikelyTransientProviderError(err error) bool {
	if code, ok := statusCodeOf(err); ok && code >= 500 && code <= 599 {
		return true
	}
	text := strings.ToLower(errorText(err))
	return containsAny(text,
		"provider returned error",
		`error_type":"unmapped"`,
		"upstream",
		"bad gateway",
		"service unavailable",
		"gateway timeout",
	)
}

func IsRetryableRouteError(err error) bool {
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

func updateEwma(prev float64, next float64, alpha float64) float64 {
	if prev <= 0 {
		return next
	}
	return alpha*next + (1-alpha)*prev
}

func updateRewardEwma(prev float64, next float64, count float64, alpha float64) float64 {
	if count <= 0 {
		return next
	}
	return alpha*next + (1-alpha)*prev
}

func rateLimitCooldownSeconds(count float64) float64 {
	if count <= 0 {
		return 10.0
	}
	return 10 * math.Pow(2, math.Min(count, 5)-1)
}

// immediateFailureReward maps a failure to a quality reward, or reports
// that the failure carries no signal about the model's quality.
func immediateFailureReward(err error) (float64, bool) {
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
	if strings.Contains(strings.ToLower(errorText(err)), "schema") {
		return -0.9, true
	}
	return 0, false
}

// ── sampling ─────────────────────────────────────────────────────────────

// makeRng returns the router's random source: a seeded generator when a
// seed is configured, otherwise the package source.
func makeRng(seed *float64) func() float64 {
	if seed == nil {
		return func() float64 { return randomFloat() }
	}
	return rand.New(rand.NewSource(int64(*seed))).Float64
}

// gauss is a Box-Muller standard normal draw.
func gauss(rng func() float64) float64 {
	u, v := 0.0, 0.0
	for u == 0 {
		u = rng()
	}
	for v == 0 {
		v = rng()
	}
	return math.Sqrt(-2.0*math.Log(u)) * math.Cos(2.0*math.Pi*v)
}

// sampleGamma is Marsaglia & Tsang, recursive for shape < 1.
func sampleGamma(rng func() float64, shape float64) float64 {
	if shape <= 0 {
		return 0
	}
	if shape < 1 {
		u := math.Max(rng(), 1e-12)
		return sampleGamma(rng, shape+1) * math.Pow(u, 1/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9*d)
	for {
		x := gauss(rng)
		v := 1 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng()
		if u < 1-0.0331*x*x*x*x {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
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

// TryPickResult is the non-blocking pick outcome. Ok==false means every
// candidate in the pool is cooling.
type TryPickResult struct {
	Ok           bool        `json:"ok"`
	Choice       RouteChoice `json:"choice,omitzero"`
	Reason       string      `json:"reason,omitempty"`
	RetryAfterMs float64     `json:"retryAfterMs,omitempty"`
}

// ── the router ───────────────────────────────────────────────────────────

type AdaptiveModelRouter struct {
	mu  sync.Mutex
	cfg normalizedConfig
	rng func() float64
	// stats is keyed by statsKey(slot, candidate).
	stats map[string]*modelStats
	last  map[string]string
}

func NewAdaptiveModelRouter(cfg AdaptiveRouterConfig) *AdaptiveModelRouter {
	return &AdaptiveModelRouter{
		cfg:   normalizeConfig(cfg),
		rng:   makeRng(cfg.RandomSeed),
		stats: map[string]*modelStats{},
		last:  map[string]string{},
	}
}

// MaxAttempts is the configured attempt budget per call.
func (r *AdaptiveModelRouter) MaxAttempts() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.MaxAttempts
}

// CandidatesForTier is the pool the tier routes on, after degradation.
func (r *AdaptiveModelRouter) CandidatesForTier(tier ModelTier) []ModelCandidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.candidatesForTier(r.effectiveTierLocked(tier))
}

// candidatesForTier takes an already-degraded tier.
func (r *AdaptiveModelRouter) candidatesForTier(tier ModelTier) []ModelCandidate {
	switch tier {
	case ModelTierLow:
		return r.cfg.LowModels
	case ModelTierFrontier:
		return r.cfg.FrontierModels
	default:
		return r.cfg.HighModels
	}
}

// EffectiveTier resolves the tier a caller should actually route on: a tier
// whose pool is empty degrades to HIGH, so no run depends on LOW or FRONTIER
// being configured, and an unrecognised tier routes on HIGH.
func (r *AdaptiveModelRouter) EffectiveTier(tier ModelTier) ModelTier {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.effectiveTierLocked(tier)
}

func (r *AdaptiveModelRouter) effectiveTierLocked(tier ModelTier) ModelTier {
	switch tier {
	case ModelTierLow:
		if len(r.cfg.LowModels) > 0 {
			return ModelTierLow
		}
	case ModelTierFrontier:
		if len(r.cfg.FrontierModels) > 0 {
			return ModelTierFrontier
		}
	}
	return ModelTierHigh
}

// TryPick is the non-blocking pick. Pass at most one nowMs to override the
// clock.
func (r *AdaptiveModelRouter) TryPick(slot string, tier ModelTier, nowMs ...float64) TryPickResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tryPickLocked(slot, tier, nowMs...)
}

func (r *AdaptiveModelRouter) tryPickLocked(
	slot string, tier ModelTier, nowMsOpt ...float64,
) TryPickResult {
	nowMs := nowMillis()
	if len(nowMsOpt) > 0 {
		nowMs = nowMsOpt[0]
	}
	// Degrade once, here: the pool, the score weighting and the tier on the
	// choice all read the tier actually routed on.
	tier = r.effectiveTierLocked(tier)

	candidates := r.candidatesForTier(tier)
	if len(candidates) == 0 {
		// No candidates configured at all: choose installs the hard-coded
		// fallback. Treat as a successful pick.
		return TryPickResult{Ok: true, Choice: r.lease(slot, tier, nowMs, nil, false)}
	}

	anyAvailable := false
	for _, c := range candidates {
		if r.statsFor(slot, c).CooldownUntilMs <= nowMs {
			anyAvailable = true
			break
		}
	}
	if !anyAvailable {
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
		return TryPickResult{
			Ok:           false,
			Reason:       "all-busy",
			RetryAfterMs: retryAfterMs,
		}
	}

	return TryPickResult{Ok: true, Choice: r.lease(slot, tier, nowMs, candidates, false)}
}

// lease chooses a candidate and takes an in-flight slot on it.
func (r *AdaptiveModelRouter) lease(
	slot string, tier ModelTier, nowMs float64, candidates []ModelCandidate, relaxed bool,
) RouteChoice {
	choice := r.choose(slot, tier, nowMs, candidates, relaxed)
	st := r.statsFor(slot, choice.Candidate)
	st.Inflight += 1
	return choice
}

// PickOptions bounds a blocking Pick.
type PickOptions struct {
	TimeoutMs *float64
	Signal    *AbortSignal
}

// PickContext binds waiting for a route to the caller's actual lifetime.
// A cancellation before a lease is returned must not leak an in-flight slot.
func (r *AdaptiveModelRouter) PickContext(
	ctx context.Context, slot string, tier ModelTier,
) (RouteChoice, error) {
	signal := NewAbortSignal()
	stop := context.AfterFunc(ctx, signal.Abort)
	defer stop()
	if ctx.Err() != nil {
		signal.Abort()
	}
	choice, err := r.Pick(slot, tier, PickOptions{Signal: signal})
	if ctx.Err() != nil {
		if err == nil {
			r.RegisterCanceled(choice)
		} else {
			// The effective tier, so every emitted tier means the tier
			// routed on and never the one asked for.
			r.emitCancellation(AdaptiveRouteEvent{
				Slot: slot, Tier: r.EffectiveTier(tier),
				Reason: "caller-canceled-pick",
			})
		}
		return RouteChoice{}, context.Cause(ctx)
	}
	return choice, err
}

// RegisterCanceled releases a caller-canceled lease without attributing a
// success, failure, latency sample or cooldown to the provider. The owner must
// serialize this with Register: exactly one terminal accounting action per pick.
func (r *AdaptiveModelRouter) RegisterCanceled(choice RouteChoice) {
	r.mu.Lock()
	st := r.statsFor(choice.Slot, choice.Candidate)
	if st.Inflight > 0 {
		st.Inflight--
	}
	event := AdaptiveRouteEvent{
		Slot: choice.Slot, Tier: choice.Tier, Model: choice.Candidate.ID,
		Reason: "caller-canceled-request", Score: choice.Score,
		Attempts: st.Attempts, Successes: st.Successes,
		Failures: st.Failures, RateLimits: st.RateLimits,
		LatencyEwma: st.LatencyEwma, ToksecEwma: st.ToksecEwma,
	}
	r.mu.Unlock()
	r.emitCancellation(event)
}

func (r *AdaptiveModelRouter) emitCancellation(event AdaptiveRouteEvent) {
	defer func() { _ = recover() }()
	r.cfg.OnEvent(event)
}

// Pick blocks (polling TryPick with bounded exponential backoff) until a
// candidate is available or `timeoutMs` (default 5 minutes) elapses.
//
// The caller must settle the returned lease exactly once, with Register or
// RegisterCanceled.
func (r *AdaptiveModelRouter) Pick(
	slot string, tier ModelTier, opts ...PickOptions,
) (RouteChoice, error) {
	var opt PickOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	timeoutMs := 5 * 60 * 1000.0
	if opt.TimeoutMs != nil {
		timeoutMs = *opt.TimeoutMs
	}
	deadline := nowMillis() + timeoutMs
	waitMs := 100.0
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
		now := nowMillis()
		if now >= deadline {
			return RouteChoice{}, fmt.Errorf(
				"router.pick timeout after %sms (%s) — slot=%s tier=%s",
				strconv.FormatFloat(timeoutMs, 'f', -1, 64), result.Reason, slot, tier)
		}
		sleepMs := math.Min(math.Min(waitMs, math.Max(50, deadline-now)), result.RetryAfterMs)
		sleepMillis(sleepMs, opt.Signal)
		waitMs = math.Min(waitMs*2, maxWaitMs)
	}
}

// PickSync always returns a choice: when every candidate is cooling it takes
// the least bad one from the pool and says so.
func (r *AdaptiveModelRouter) PickSync(slot string, tier ModelTier) RouteChoice {
	r.mu.Lock()
	defer r.mu.Unlock()
	tier = r.effectiveTierLocked(tier)
	result := r.tryPickLocked(slot, tier)
	if result.Ok {
		return result.Choice
	}
	return r.lease(slot, tier, nowMillis(), r.candidatesForTier(tier), true)
}

// Register records the outcome of a call started by Pick. Pass err == nil on
// success.
func (r *AdaptiveModelRouter) Register(choice RouteChoice, elapsedSeconds float64, completionTokens float64, err error) AdaptiveRouteEvent {
	event, onEvent := r.registerLocked(choice, elapsedSeconds, completionTokens, err)
	// The listener runs after the lock is dropped, so one that calls back
	// into the router re-enters instead of deadlocking.
	func() {
		defer func() { _ = recover() }()
		onEvent(event)
	}()
	return event
}

func (r *AdaptiveModelRouter) registerLocked(choice RouteChoice, elapsedSeconds float64, completionTokens float64, err error) (AdaptiveRouteEvent, func(AdaptiveRouteEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	st := r.statsFor(choice.Slot, choice.Candidate)
	if st.Inflight > 0 {
		st.Inflight -= 1
	}
	st.Attempts += 1
	if err == nil {
		st.Successes += 1
	} else {
		st.Failures += 1
		st.LastError = truncateChars(errorText(err), 400)
		now := nowMillis()
		switch {
		case IsLikelyRateLimit(err):
			st.RateLimits += 1
			st.CooldownUntilMs = now + rateLimitCooldownSeconds(st.RateLimits)*1000
		case IsLikelyProviderIncompatible(err):
			st.CooldownUntilMs = now + 3600*1000
		case IsLikelyTimeout(err):
			st.CooldownUntilMs = now + 120*1000
		case IsLikelyTransientProviderError(err):
			// Short cooldown: encourage a different model next call, but let
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
	if err != nil {
		errText = truncateChars(errorText(err), 300)
	}
	event := AdaptiveRouteEvent{
		Slot:          choice.Slot,
		Tier:          choice.Tier,
		Model:         choice.Candidate.ID,
		PreviousModel: choice.PreviousModel,
		Switched:      choice.Switched,
		Reason:        choice.Reason,
		Score:         choice.Score,
		ElapsedS:      elapsedSeconds,
		Attempts:      st.Attempts,
		Successes:     st.Successes,
		Failures:      st.Failures,
		RateLimits:    st.RateLimits,
		LatencyEwma:   st.LatencyEwma,
		ToksecEwma:    st.ToksecEwma,
		Error:         errText,
	}
	return event, r.cfg.OnEvent
}

// truncateChars keeps the first n characters of s without splitting a
// multi-byte character.
func truncateChars(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
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
		candidates = []ModelCandidate{{ID: "openrouter/openai/gpt-oss-120b", Tier: tier}}
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
		Score:         math.Inf(-1),
		PreviousModel: previous,
	}
	for _, cand := range candidates {
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
		if score > best.Score {
			best = RouteChoice{
				Slot: slot, Tier: tier, Candidate: cand,
				Score: score, PreviousModel: previous,
			}
		}
	}
	// Forced fallback: every candidate is cooling. Pick the least bad with a
	// -2.0 penalty.
	if math.IsInf(best.Score, -1) {
		for _, cand := range candidates {
			st := r.statsFor(slot, cand)
			score := r.score(tier, cand, st) - 2.0
			if score > best.Score {
				best = RouteChoice{Slot: slot, Tier: tier, Candidate: cand, Score: score,
					PreviousModel: previous, Reason: "forced"}
			}
		}
	}
	if math.IsInf(best.Score, -1) {
		// Nothing produced a finite score; keep the first candidate rather
		// than reporting an unrepresentable score.
		best.Score = -2.0
		best.Reason = "forced"
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

func (r *AdaptiveModelRouter) score(
	tier ModelTier, cand ModelCandidate, st *modelStats,
) float64 {
	// Cold-model optimism: an unattempted model gets a uniform draw in
	// [0.72, 0.90) instead of a Beta posterior.
	var reliability float64
	if st.Attempts == 0 {
		reliability = 0.72 + r.rng()*0.18
	} else {
		reliability = sampleBeta(r.rng, st.Successes+2, st.Failures+1)
	}
	latency := 15.0 + cand.Priority
	if st.LatencyEwma > 0 {
		latency = st.LatencyEwma
	}
	toksec := 35.0
	if st.ToksecEwma > 0 {
		toksec = st.ToksecEwma
	}
	price := cand.PromptUSDPerMtok + cand.CompletionUSDPerMtok
	if !(price > 0) {
		price = 1.0
	}
	cost := 1.0 / (1.0 + price)
	speed := toksec / (toksec + 80.0)
	latencyPen := latency / (latency + 45.0)
	pressurePen := st.Inflight * 0.12
	rlRisk := 0.0
	if st.Attempts > 0 {
		rlRisk = st.RateLimits / st.Attempts
	}
	quality := 0.0
	if st.RewardCount > 0 {
		quality = math.Max(-1.0, math.Min(1.0, st.QualityEwma))
	}
	var score float64
	if tier == ModelTierLow {
		// The low tier buys throughput and price: speed and cost carry more
		// than twice the weight they do on the other tiers.
		score = 0.38*reliability + 0.3*speed + 0.16*cost +
			0.06*quality - 0.1*latencyPen - pressurePen - 0.45*rlRisk
	} else {
		// HIGH and FRONTIER: reliability dominates, cost and speed matter
		// little.
		score = 0.68*reliability + 0.14*speed + 0.08*cost +
			0.08*quality - 0.08*latencyPen - pressurePen - 0.5*rlRisk
	}
	if math.IsNaN(score) {
		return math.Inf(-1)
	}
	return score
}

func (r *AdaptiveModelRouter) statsFor(slot string, cand ModelCandidate) *modelStats {
	key := statsKey(slot, cand)
	st, ok := r.stats[key]
	if !ok {
		st = &modelStats{}
		r.stats[key] = st
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
	if !math.IsInf(previousScore, 0) && !math.IsNaN(previousScore) && best.Score > previousScore {
		return "better-score"
	}
	return "switch"
}

// ── CLI parsing ──────────────────────────────────────────────────────────

// ParseModelList parses a comma-separated `--high`, `--low` or `--frontier`
// flag value into the named tier's pool. Each entry is either
// "openrouter/qwen/qwen3.6-plus" or
// "openrouter/qwen/qwen3.6-plus@0.325/1.95" (id + prompt$/completion$ per Mtok).
// Cost defaults to zero, which degenerates the cost term to a neutral 0.5.
func ParseModelList(raw *string, tier ModelTier) []ModelCandidate {
	if raw == nil || *raw == "" {
		return []ModelCandidate{}
	}
	var entries []string
	for _, s := range strings.Split(*raw, ",") {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	out := make([]ModelCandidate, 0, len(entries))
	for i, entry := range entries {
		id := entry
		prompt := 0.0
		completion := 0.0
		if at := strings.LastIndex(entry, "@"); at > 0 {
			id = entry[:at]
			if p, c, ok := strings.Cut(entry[at+1:], "/"); ok {
				prompt = parsePrice(p)
				completion = parsePrice(c)
			}
		}
		out = append(out, ModelCandidate{
			ID:                   NormalizeCandidateModel(id),
			Tier:                 tier,
			PromptUSDPerMtok:     prompt,
			CompletionUSDPerMtok: completion,
			Priority:             float64(i),
		})
	}
	return out
}

// parsePrice reads a $/Mtok figure; anything unparsable or non-finite is 0.
func parsePrice(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return finiteOrZero(f)
}

// finiteOrZero maps a non-finite price to 0 so every candidate stays
// encodable and the cost term degenerates to neutral.
func finiteOrZero(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}
