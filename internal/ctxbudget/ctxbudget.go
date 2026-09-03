// Package ctxbudget is the one place that turns a model's context window into
// byte and token budgets. The law it implements: an agent — head turn, planner
// pass, leaf worker, judge — may fill its window to FillPercent (60% by
// default) before compaction fires, and it always keeps CompletionReserve
// tokens of room for the answer and its reasoning. Nothing downstream of this
// package may carry an absolute byte ceiling of its own: every cap is either a
// Budget, a Share of one, or an explicitly-named fallback for the case where
// the window is unknown.
//
// Two clauses govern material an agent re-sends rather than material it sends
// once. WithinWorkingSet takes the fill percentage of min(window, working-set
// ceiling), because a window is what a provider accepts and not evidence that
// carrying that much is useful. ReuseCeiling bounds the SUM over turns of what
// was sent, which is the quantity a runaway loop actually moves and the one a
// per-turn window and a cache-discounted spend meter both structurally cannot
// see. Both are opt-in, both go inert on an unknown window.
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

	// DefaultWorkingSetTokens is the ceiling on the LIVE WORKING SET — how much
	// material one agent may keep quoted in front of it at once — applied before
	// the fill law, and it is the correction to a category error in the law's own
	// first sentence.
	//
	// Fill-to-60% was written as a statement about a window. A window is what the
	// provider will accept in one request; it is not a statement that carrying
	// that much material is useful, and the two parted company the moment
	// million-token windows arrived. Measured on a 1M-context model, the fill law
	// alone granted a leaf a 2.2MB observation window against 181KB of tool output
	// across twelve whole nodes: the decayer fired ZERO times all run, nothing
	// ever left the transcript, and the leaf's bill — Σ over turns of (base + all
	// prior growth) — carried a duplication factor of 7.45x, with 90% of every
	// input token a re-send of something the model had already been shown.
	//
	// So the pot the law fills is min(window, this) rather than the window. The
	// number is not this program's invention: another agent engine, measured
	// independently, caps its live working set at 160,000 tokens and applies that
	// cap BEFORE the same 0.6 trigger. Two engines reaching the same shape from
	// different evidence is the strongest argument available for it, and
	// AFORGE_WORKING_SET is here for the operator who has evidence of their own.
	//
	// A model whose whole window is smaller than this is unaffected: the minimum
	// keeps the law exactly what it was for it.
	DefaultWorkingSetTokens = 160_000

	// DefaultReusePercent is how many times over one agent loop may re-send its
	// whole working set before it has to land, as a percentage.
	//
	// It is the one bound that reads the quantity a leaf is actually billed for.
	// Cost meters discount the cache-served prefix, which is honest about money
	// and blind to runaway: a measured node carrying 265k raw tokens metered at
	// 54% of its grant, its raw count sat at 58% of the raw ceiling, and neither
	// bound ever bound — the node stopped itself at turn 17, after a verification
	// spiral nothing in the harness could see coming.
	//
	// 250% is where the measurement puts it. On a 1M-context model the working set
	// above fills to 96k tokens, so this lands a leaf at ~240k cumulative prompt
	// tokens — the 0.25x-of-window threshold the trace ledgers show would have
	// landed every runaway node just after its real work and before its spiral,
	// while sitting well clear of an honest leaf (8-16 turns at a measured ~11k
	// per turn is 88k-176k). Writing it against the working set rather than
	// against the window is what keeps it sane on a small model, where a quarter
	// of the window would land an ordinary leaf at turn three.
	//
	// Below 100 it would stop a leaf before it had sent its own context once,
	// which is not a governor but a refusal; the clamp says so.
	DefaultReusePercent = 250
)

// The configured values arrive from the settings sheet at process start via
// Configure. The environment still wins — a pin is a pin — and the defaults
// carry when neither has spoken. Plain ints behind a mutex: read on every
// call so a settings write lands in the running process.
var (
	configMu   sync.RWMutex
	configured Limits
)

// Limits is the settings sheet's half of the context law, in one value.
//
// It is a struct rather than a widening argument list because the law has grown
// twice and will again: a caller that has to remember the order of four integers
// is a caller that will one day swap two of them, and every field here is a
// plain count that would swap silently.
//
// Zero in any field means unset — the environment pin, then the default, carry
// that field on its own. Configuring one setting therefore never has to know
// what the other three are.
type Limits struct {
	// FillPercent is how full a window may get before compaction fires.
	FillPercent int
	// CompletionReserveTokens is the room every call keeps for its answer.
	CompletionReserveTokens int
	// WorkingSetTokens is the ceiling on the live working set, applied before
	// the fill percentage. See DefaultWorkingSetTokens.
	WorkingSetTokens int
	// ReusePercent is how many times over an agent loop may re-send its whole
	// working set before it must land. See DefaultReusePercent.
	ReusePercent int
}

// Configure hands the persisted settings values down. Zero means unset; the
// environment and the defaults are unaffected either way.
func Configure(limits Limits) {
	configMu.Lock()
	configured = limits
	configMu.Unlock()
}

// configuredValue reads one field of the persisted law under the lock. Every
// resolver below is environment, then this, then its own default.
func configuredValue(read func(Limits) int) int {
	configMu.RLock()
	defer configMu.RUnlock()
	return read(configured)
}

// FillPercent is the process-wide fill law: environment pin, then the
// configured setting, then the default. Clamped so a typo can neither starve
// nor overrun a window.
func FillPercent() int {
	percent, _ := PinnedFillPercent()
	return percent
}

// PinnedFillPercent is [FillPercent] with the one thing the integer alone could
// never say: whether a PERSON chose it.
//
// A fill of sixty that somebody typed and a fill of sixty nobody has ever
// touched are the same number and completely different instructions, and every
// consumer that only needs the number is right not to care. The conversation's
// compaction trigger is the consumer that has to: honouring an untouched sixty
// would fold every session at sixty percent of its window, which is the opposite
// of what the derived law arrived at (internal/session's CompactThresholdFor).
//
// The two sources it reads are the settings registry's own two ways of saying a
// person set a row — the environment variable the registry names on that row
// (config's Setting.PinnedBy), and the value written down in the profile
// (config's Settings.PersistedKeys, handed here by [Configure]). Neither is a
// second flag invented for this question; both already decided it and nobody
// had asked them.
//
// The default comes back with false, so a caller that wants the number either
// way still gets the law's own figure.
func PinnedFillPercent() (int, bool) {
	if v, ok := envInt("AFORGE_CONTEXT_FILL_PCT"); ok {
		return clampFill(v), true
	}
	if v := configuredValue(func(l Limits) int { return l.FillPercent }); v > 0 {
		return clampFill(v), true
	}
	return DefaultFillPercent, false
}

// CompletionReserve is the process-wide completion+reasoning reserve:
// environment pin, then the configured setting, then the default.
func CompletionReserve() int {
	if v, ok := envInt("AFORGE_COMPLETION_RESERVE"); ok && v > 0 {
		return v
	}
	if v := configuredValue(func(l Limits) int { return l.CompletionReserveTokens }); v > 0 {
		return v
	}
	return DefaultCompletionReserveTokens
}

// WorkingSetCeiling is the process-wide cap on the live working set, in tokens,
// resolved the same way: environment pin, then the configured setting, then the
// default. See DefaultWorkingSetTokens for what it is and why it exists.
func WorkingSetCeiling() int {
	if v, ok := envInt("AFORGE_WORKING_SET"); ok && v > 0 {
		return v
	}
	if v := configuredValue(func(l Limits) int { return l.WorkingSetTokens }); v > 0 {
		return v
	}
	return DefaultWorkingSetTokens
}

// ReusePercent is the process-wide cumulative-re-send allowance, resolved the
// same way and clamped so it can never stop a loop before its first full send.
func ReusePercent() int {
	if v, ok := envInt("AFORGE_CONTEXT_REUSE_PCT"); ok && v > 0 {
		return clampReuse(v)
	}
	if v := configuredValue(func(l Limits) int { return l.ReusePercent }); v > 0 {
		return clampReuse(v)
	}
	return DefaultReusePercent
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

// clampReuse keeps the allowance a governor rather than a refusal: a leaf that
// may not re-send its working set even once has been stopped before it can read
// its own brief twice.
func clampReuse(v int) int {
	if v < 100 {
		return 100
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
	// WorkingSetTokens is the ceiling on the live working set — the pot the
	// fill percentage is taken of, once WithinWorkingSet has been asked for.
	// It is carried on the budget rather than read at the point of use so a
	// budget stays a value: the same budget answers the same way whoever holds
	// it and whenever they ask.
	WorkingSetTokens int
	// ReusePercent is how many times over the working set may be re-sent
	// before the loop holding this budget has to land.
	ReusePercent int
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
		WorkingSetTokens:        WorkingSetCeiling(),
		ReusePercent:            ReusePercent(),
	}
}

// WithinWorkingSet puts the budget under the working-set law: the pot the fill
// percentage is taken of becomes min(window, working-set ceiling) instead of the
// whole window.
//
// It is opt-in rather than folded into For, and the asymmetry is deliberate. A
// working set is a statement about material an agent keeps QUOTED IN FRONT OF
// ITSELF turn after turn — a leaf's observations, the transcript it re-sends —
// and for that material an unbounded window is the pathology this whole concept
// exists to stop. It is not a statement about a one-shot pot: a planner
// assembling a single prompt, a judge reading one node, a fan-in sized to what
// it must carry once, all pay their bytes exactly once and are rightly sized by
// the window alone. Making every consumer opt in keeps that distinction visible
// at the call site rather than buried here.
//
// An unknown window stays unknown: there is nothing to clamp and the caller's
// named fallback still carries.
func (b Budget) WithinWorkingSet() Budget {
	if !b.Known() {
		return b
	}
	ceiling := b.WorkingSetTokens
	if ceiling <= 0 {
		ceiling = DefaultWorkingSetTokens
	}
	if b.ContextTokens > ceiling {
		b.ContextTokens = ceiling
	}
	return b
}

// ReuseCeiling is the cumulative bound: how many tokens of prompt one agent loop
// may send IN TOTAL, summed over every turn, before it has to land.
//
// It is the only bound written in the quantity a runaway actually moves. A
// per-turn prompt is bounded by the window and a discounted spend is bounded by
// the grant; what neither can see is the sum — Σ over turns of (base + all prior
// growth) — which is what a loop spends when nothing ever leaves its transcript.
// Measured at 7.45x duplication across a real run, one node at 11.2x.
//
// It is stated against the working set rather than against the window because
// the working set is what gets re-sent. See DefaultReusePercent.
//
// Zero means the window was never known, and an unknown window may not be
// governed: a bound derived from a guess would fire on evidence nobody has.
func (b Budget) ReuseCeiling() int {
	if !b.Known() {
		return 0
	}
	working := b.WithinWorkingSet()
	fill := working.FillPercent
	if fill <= 0 {
		fill = DefaultFillPercent
	}
	reuse := working.ReusePercent
	if reuse <= 0 {
		reuse = DefaultReusePercent
	}
	return working.ContextTokens * fill / 100 * reuse / 100
}

// WithFloor returns the budget with the consumer's fixed floor stated.
func (b Budget) WithFloor(tokens int) Budget {
	if tokens > 0 {
		b.FixedFloorTokens = tokens
	}
	return b
}

// WithCompletionReserve states a completion reserve of this consumer's own, for
// the case where the process-wide constant is wrong about it.
//
// The constant is a constant because most calls write about as much as each
// other. A node whose job is to reproduce what fed it writes as much as fed it,
// and for that node a constant reserve is the difference between landing the
// assembly and stopping halfway through it.
//
// It only ever raises. A consumer that can say why it needs more room gets more
// room; nothing gets to quietly claim it needs less than the law grants.
//
// The clamp is stated in the consumer's own units rather than in a fraction
// invented here. The reserve may grow until the budgeted material the prompt can
// still carry has shrunk to the size of the prompt's own fixed cost — that is,
// until Tokens() would fall below FixedFloorTokens:
//
//	reserve ≤ window*fill/100 − 2*FixedFloorTokens
//
// Past that point the turn is mostly its own overhead and the node is being
// asked to assemble from nothing, which is the opposite failure to the one the
// reserve exists to prevent. A consumer that stated no floor has said it cannot
// measure that point, and gets the fill allowance as its only bound.
func (b Budget) WithCompletionReserve(tokens int) Budget {
	if !b.Known() || tokens <= b.CompletionReserveTokens {
		return b
	}
	fill := b.FillPercent
	if fill <= 0 {
		fill = DefaultFillPercent
	}
	if ceiling := b.ContextTokens*fill/100 - 2*b.FixedFloorTokens; tokens > ceiling {
		tokens = ceiling
	}
	if tokens <= b.CompletionReserveTokens {
		return b
	}
	b.CompletionReserveTokens = tokens
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

// The observation window: how many bytes of material one worker holds in front
// of it at a time. It lives here rather than beside the leaf loop that spends
// it because it is no longer only that loop's question — the planner asks it
// too, before any worker exists, to measure what a goal names against what the
// worker it is about to hand the goal to will be able to hold. A number two
// packages both need is a number that drifts unless exactly one of them owns
// it, and this is the package that owns the law it is derived from.
const (
	// ObservationFloorBytes is the minimum the unknown-context default may fall
	// to, and nothing else. It is deliberately not a floor under the computed
	// window: when a model's context really is small, the honest answer is a
	// small window, and clamping it upwards would size a prompt past what the
	// model accepts in order to look generous.
	ObservationFloorBytes = 24 << 10

	// MinObservationBytes is arithmetic protection rather than policy. Below it
	// the fixed floor and the completion reserve have already eaten the model's
	// whole context and a worker cannot run there at all; the window stops at a
	// value the decay pass can still reason about instead of going to zero or
	// negative.
	MinObservationBytes = 4 << 10

	// DefaultObservationBytes is the window a worker carries when nothing can
	// say how much context its model actually has.
	//
	// The number is conservative on purpose, and that is the correction to the
	// obvious first instinct. Not knowing a model's context length is not
	// evidence that it is large: an offline catalog, an unrecognised slug and a
	// row cached before the field was kept all look identical here, and a
	// small-context model is exactly the kind that goes unrecognised. A generous
	// default would be a silent regression for it — prompts sized past what it
	// accepts, failing as provider rejections rather than as degradation.
	DefaultObservationBytes = 32 << 10

	// ObservationFixedFloorTokens is the measured per-turn cost of everything
	// that is not observations — the standing contract, the tool schemas, the
	// brief. Measured at ~3,778 tokens across 332 leaf turns; rounded up,
	// because the window must not be sized from an optimistic floor.
	ObservationFixedFloorTokens = 4_000
)

// ObservationBytes sizes one worker's observation window from the one quantity
// that governs it: how much the model can hold in a single request.
//
// The share of the context it may take is the process-wide fill law above —
// how full a window may get before compaction fires, and the completion reserve
// every call keeps for its answer and its reasoning. What is left after the
// fixed floor and that reserve is what observations may carry. The pot the law
// is taken of is min(window, working set) rather than the window itself, which
// is the difference between a statement about what a provider accepts and a
// statement about how much material carrying is useful.
//
// A known context under the ceiling gets the law's own answer, including when
// that answer is small. Only the unknown case takes a default, and only the
// default gets a floor under it.
func ObservationBytes(contextTokens int) int {
	budget := For(contextTokens).WithinWorkingSet().WithFloor(ObservationFixedFloorTokens)
	if !budget.Known() {
		if DefaultObservationBytes < ObservationFloorBytes {
			return ObservationFloorBytes
		}
		return DefaultObservationBytes
	}
	// Deliberately not BytesOr: an unaffordable window on a genuinely small
	// model must come back small, not fall through to the unknown-case default.
	// Sizing a prompt past what the model accepts in order to look generous is
	// a provider rejection rather than a shorter memory.
	if window := budget.Bytes(); window > MinObservationBytes {
		return window
	}
	return MinObservationBytes
}
