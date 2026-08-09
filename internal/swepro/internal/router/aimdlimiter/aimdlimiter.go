// Package aimdlimiter is a bug-for-bug port of src/router/aimd-limiter.ts —
// the AIMD (additive-increase / multiplicative-decrease) adaptive concurrency
// limiter that fronts every OpenRouter HTTP call.
//
//	on success        → cap += 1
//	on throttle / 429 → cap = floor(cap/2)
//	on low-remaining  → cap -= 2
//
// Concurrency model. The TS banks on the single-threaded event loop (its own
// lines 16-18 say so) and mutates the cap/inflight counters with no lock at
// all. Go has real threads, so this port serializes every public method behind
// ONE mutex and keeps that mutex non-reentrant by routing internal cross-calls
// through the unexported *Locked helpers. The OBSERVABLE contract is
// unchanged: FIFO wake order, the exact counter values after every step, and
// which resize calls emit a change.
//
// Promise<void> → <-chan struct{}. acquire() hands back a promise that is
// either already settled (fast path) or settles when drainWaiters() gives the
// slot away. The Go twin returns a receive-only channel that is either already
// closed or gets closed at the same point, in the same order. Callers write
// `<-limiter.Acquire()`.
//
// The one place Go cannot reproduce JS exactly: resize() invokes onChange
// AFTER releasing the mutex, where the TS calls it inline. Firing it under the
// lock would deadlock any callback that touched the limiter — something the TS
// permits — so the callback is deferred by exactly one unlock. Ordering as
// seen by the calling goroutine is identical (onChange still runs before
// OnSuccess/OnThrottle/OnLowRemaining returns, still after the cap moved and
// the waiters drained). The residue is that two goroutines resizing at once
// can be inside onChange simultaneously, where the event loop would have
// serialized them; a callback with its own mutable state has to guard it.
// Serializing on a second mutex was rejected because it merely swaps one
// deadlock (callback touches the limiter) for another (callback triggers a
// second resize) and can reorder payloads relative to the cap mutations.
//
// Fidelity notes (deliberate, do not "fix"):
//   - jsMax/jsMin are hand-rolled. Go's math.Max/math.Min short-circuit on
//     ±Inf BEFORE testing NaN, so math.Max(+Inf, NaN) is +Inf where JS
//     Math.max(Infinity, NaN) is NaN. Reachable straight from user options:
//     {minCap: Infinity, maxCap: NaN} poisons every later clamp to NaN in TS,
//     and math.Max would have kept it at +Inf.
//   - A NaN cap makes resize()'s `newCap === oldCap` short-circuit
//     unreachable (NaN !== NaN), so EVERY onSuccess/onThrottle/onLowRemaining
//     emits a {oldCap: NaN, newCap: NaN} change forever — while `inflight <
//     cap` is always false, so no acquire ever takes the fast path and no
//     waiter is ever drained. The limiter is permanently wedged. Ported as-is.
//   - Symmetrically, an Infinity cap makes cap+1, floor(cap/2) and cap-2 all
//     equal cap, so no resize ever reports a change.
//   - Release only decrements when inflight > 0, so an unbalanced release
//     floors at 0 instead of going negative.
//   - onChange panics are swallowed exactly like the TS `catch {}`, and the
//     log.info fallback fires ONLY when no onChange was supplied.
//   - Caps are float64, not int: initialCap/minCap/maxCap are JS numbers, so
//     a fractional cap (2.5) is legal and OnThrottle floors it to 1.
//   - drainWaiters' `while` loop is defensive in BOTH implementations: it can
//     never run more than one iteration. Waiters only get queued when
//     inflight >= cap; every release and every cap raise runs the drain to the
//     fixpoint inflight >= cap; and cap only ever grows by exactly 1 (throttle
//     and low-remaining provably never raise it, since cap >= minCap always).
//     So inflight is never more than one slot below cap with a non-empty
//     queue. Kept as a loop anyway, exactly like the TS.
//   - The TS reads no clock and no RNG, so — unlike internal/plandb — there is
//     deliberately NO injectable now/random hook here.
package aimdlimiter

import (
	"math"
	"sync"

	"github.com/Agent-Field/swe-pro-go/internal/fixflag"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/logshim"
)

var log = logshim.Create(map[string]any{"service": "aimd-limiter"})

// ── exported shapes (JSON tags mirror the TS object-literal key order) ────

// ChangeReason is the `reason` union of ConcurrencyChange.
type ChangeReason string

const (
	ReasonSuccess      ChangeReason = "success"
	ReasonThrottle     ChangeReason = "throttle"
	ReasonLowRemaining ChangeReason = "low_remaining"
	// ReasonManual exists in the TS union but nothing constructs it: resize is
	// private and no public method passes "manual". Kept so the type mirrors
	// the original; unreachable in both implementations.
	ReasonManual ChangeReason = "manual"
)

type ConcurrencyChange struct {
	OldCap jscompat.JSNumber `json:"oldCap"`
	NewCap jscompat.JSNumber `json:"newCap"`
	Reason ChangeReason      `json:"reason"`
}

// AIMDOptions mirrors `{ initialCap?, minCap?, maxCap?, onChange? }`. The
// numeric knobs are pointers so "absent", "undefined" and "null" all arrive as
// nil — which is exactly what TS `??` collapses them to.
type AIMDOptions struct {
	InitialCap *float64
	MinCap     *float64
	MaxCap     *float64
	OnChange   func(ConcurrencyChange)
}

// Inspection is the return of inspect(): `{ cap, inflight, waiters, min, max }`.
type Inspection struct {
	Cap      jscompat.JSNumber `json:"cap"`
	Inflight jscompat.JSNumber `json:"inflight"`
	Waiters  int               `json:"waiters"`
	Min      jscompat.JSNumber `json:"min"`
	Max      jscompat.JSNumber `json:"max"`
}

// waiter is the TS `{ resolve: () => void }`. Closing ch is resolve().
type waiter struct {
	ch chan struct{}
}

type AIMDConcurrencyLimiter struct {
	mu       sync.Mutex
	cap      float64
	minCap   float64
	maxCap   float64
	inflight float64
	waiters  []*waiter
	onChange func(ConcurrencyChange)
}

// NewAIMDConcurrencyLimiter is `new AIMDConcurrencyLimiter(opts)`. The
// zero-value AIMDOptions is the TS default argument `{}`.
func NewAIMDConcurrencyLimiter(opts AIMDOptions) *AIMDConcurrencyLimiter {
	l := &AIMDConcurrencyLimiter{}
	l.minCap = jsMax(1, numOr(opts.MinCap, 1))
	l.maxCap = jsMax(l.minCap, numOr(opts.MaxCap, 64))
	l.cap = clamp(numOr(opts.InitialCap, 8), l.minCap, l.maxCap)
	l.onChange = opts.OnChange
	return l
}

// CurrentCap is the `currentCap` getter. It returns a JSNumber so a wedged
// NaN/Infinity cap marshals to JSON null the way JSON.stringify would.
func (l *AIMDConcurrencyLimiter) CurrentCap() jscompat.JSNumber {
	l.mu.Lock()
	defer l.mu.Unlock()
	return jscompat.JSNumber(l.cap)
}

// CurrentInflight is the `currentInflight` getter.
func (l *AIMDConcurrencyLimiter) CurrentInflight() jscompat.JSNumber {
	l.mu.Lock()
	defer l.mu.Unlock()
	return jscompat.JSNumber(l.inflight)
}

// Acquire is acquire(): the returned channel is already closed when the fast
// path (inflight < cap) took the slot outright, otherwise it closes once
// drainWaiters hands this waiter its slot. The queue is unbounded, like TS.
func (l *AIMDConcurrencyLimiter) Acquire() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inflight < l.cap {
		l.inflight += 1
		// A fresh already-settled channel per call, mirroring the fresh
		// Promise.resolve() the TS hands back.
		settled := make(chan struct{})
		close(settled)
		return settled
	}
	w := &waiter{ch: make(chan struct{})}
	l.waiters = append(l.waiters, w)
	return w.ch
}

// Release is release(). Note the guard: an unbalanced release does NOT push
// inflight negative, it just re-runs the drain.
func (l *AIMDConcurrencyLimiter) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inflight > 0 {
		l.inflight -= 1
	}
	l.drainWaitersLocked()
}

// OnSuccess is the additive increase — call after every successful HTTP
// exchange.
func (l *AIMDConcurrencyLimiter) OnSuccess() {
	l.mu.Lock()
	pending := l.resizeLocked(l.cap+1, ReasonSuccess)
	l.mu.Unlock()
	l.emit(pending)
}

// OnThrottle is the multiplicative decrease — call on a 429 or any other
// explicit throttle signal.
func (l *AIMDConcurrencyLimiter) OnThrottle() {
	l.mu.Lock()
	pending := l.resizeLocked(jsMax(l.minCap, math.Floor(l.cap/2)), ReasonThrottle)
	l.mu.Unlock()
	l.emit(pending)
}

// OnLowRemaining is the early-signal deceleration — call when
// X-RateLimit-Remaining drops below the danger threshold. Pulls the cap down
// by 2 to stop short of a real 429.
func (l *AIMDConcurrencyLimiter) OnLowRemaining() {
	l.mu.Lock()
	pending := l.resizeLocked(l.cap-2, ReasonLowRemaining)
	l.mu.Unlock()
	l.emit(pending)
}

// Inspect is the test/debug introspection hook.
func (l *AIMDConcurrencyLimiter) Inspect() Inspection {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Inspection{
		Cap:      jscompat.JSNumber(l.cap),
		Inflight: jscompat.JSNumber(l.inflight),
		Waiters:  len(l.waiters),
		Min:      jscompat.JSNumber(l.minCap),
		Max:      jscompat.JSNumber(l.maxCap),
	}
}

// pendingChange carries a resize's telemetry out of the critical section so
// emit can run the callback with the mutex released. nil means the resize
// short-circuited and nothing is reported.
type pendingChange struct {
	change   ConcurrencyChange
	inflight float64
}

// resizeLocked is the private resize(). Caller must hold l.mu.
func (l *AIMDConcurrencyLimiter) resizeLocked(requested float64, reason ChangeReason) *pendingChange {
	oldCap := l.cap
	newCap := clamp(requested, l.minCap, l.maxCap)
	// CODEAF_GO_FIX_AIMD_NAN_CAP: a NaN cap permanently wedges the limiter
	// (BUGS-KEPT.md "aimdlimiter"). When the fix is enabled, collapse a NaN
	// newCap to minCap (or 1), which allows the limiter to recover and drain
	// waiters normally.
	if fixflag.Enabled("CODEAF_GO_FIX_AIMD_NAN_CAP") && math.IsNaN(newCap) {
		if l.minCap > 0 && !math.IsInf(l.minCap, 0) && !math.IsNaN(l.minCap) {
			newCap = l.minCap
		} else {
			newCap = 1
		}
	}
	// Go float64 == matches JS ===: NaN never equals NaN, so a wedged NaN cap
	// falls straight through and re-reports a no-op change every single time.
	if newCap == oldCap {
		return nil
	}
	l.cap = newCap
	if newCap > oldCap || (fixflag.Enabled("CODEAF_GO_FIX_AIMD_NAN_CAP") && math.IsNaN(oldCap)) {
		l.drainWaitersLocked()
	}
	return &pendingChange{
		change: ConcurrencyChange{
			OldCap: jscompat.JSNumber(oldCap),
			NewCap: jscompat.JSNumber(newCap),
			Reason: reason,
		},
		inflight: l.inflight,
	}
}

// emit runs the telemetry tail of resize() with the mutex released. onChange
// is assigned once in the constructor and never mutated, so reading it here is
// race-free.
func (l *AIMDConcurrencyLimiter) emit(pending *pendingChange) {
	if pending == nil {
		return
	}
	if l.onChange != nil {
		func() {
			defer func() { _ = recover() /* swallow telemetry errors */ }()
			l.onChange(pending.change)
		}()
		return
	}
	log.Info("cap changed", map[string]any{
		"oldCap":   pending.change.OldCap,
		"newCap":   pending.change.NewCap,
		"reason":   pending.change.Reason,
		"inflight": jscompat.JSNumber(pending.inflight),
	})
}

// drainWaitersLocked is the private drainWaiters(). Caller must hold l.mu.
// The shift is an O(n) compaction, matching Array.prototype.shift, and the
// vacated tail slot is nilled so a long-lived limiter does not pin dead
// waiters.
func (l *AIMDConcurrencyLimiter) drainWaitersLocked() {
	for len(l.waiters) > 0 && l.inflight < l.cap {
		w := l.waiters[0]
		copy(l.waiters, l.waiters[1:])
		l.waiters[len(l.waiters)-1] = nil
		l.waiters = l.waiters[:len(l.waiters)-1]
		l.inflight += 1
		close(w.ch)
	}
}

// ── numeric helpers ──────────────────────────────────────────────────────

// numOr is TS `opts.x ?? fallback`.
func numOr(p *float64, fallback float64) float64 {
	if p == nil {
		return fallback
	}
	return *p
}

// jsMax is Math.max(a, b). Go's math.Max answers ±Inf before it looks at NaN
// (math.Max(+Inf, NaN) == +Inf), whereas JS lets NaN poison the result
// unconditionally. Both branches are reachable here, so the NaN test comes
// first.
func jsMax(a, b float64) float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.NaN()
	}
	return math.Max(a, b)
}

// jsMin is Math.min(a, b), NaN-poisoned for the same reason as jsMax.
func jsMin(a, b float64) float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.NaN()
	}
	return math.Min(a, b)
}

func clamp(value, lo, hi float64) float64 {
	return jsMax(lo, jsMin(hi, value))
}

// ── module singleton ─────────────────────────────────────────────────────

// One limiter per process, shared across every OpenRouter call — same pattern
// as the merge-coordinator. The TS `let singleton` needs no guard on the event
// loop; Go guards it so two goroutines racing the first GetOpenRouterLimiter
// cannot each construct one.
var (
	singletonMu sync.Mutex
	singleton   *AIMDConcurrencyLimiter
)

func GetOpenRouterLimiter() *AIMDConcurrencyLimiter {
	singletonMu.Lock()
	defer singletonMu.Unlock()
	if singleton == nil {
		initialCap, minCap, maxCap := 8.0, 1.0, 64.0
		singleton = NewAIMDConcurrencyLimiter(AIMDOptions{
			InitialCap: &initialCap,
			MinCap:     &minCap,
			MaxCap:     &maxCap,
		})
	}
	return singleton
}

// SetOpenRouterLimiterForTest is the test seam. A nil limiter is the TS
// `undefined`, which makes the next GetOpenRouterLimiter build a fresh one.
// Not used at runtime.
func SetOpenRouterLimiterForTest(limiter *AIMDConcurrencyLimiter) {
	singletonMu.Lock()
	defer singletonMu.Unlock()
	singleton = limiter
}
