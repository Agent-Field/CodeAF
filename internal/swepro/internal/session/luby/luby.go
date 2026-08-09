// Luby (reluctant-doubling) restart schedule — port of src/session/luby.ts.
//
// Pure module: no I/O, no clock, no RNG. The TS original is two exported
// functions over JS numbers, so the Go twin keeps float64 throughout rather
// than "cleaning up" to int — the observable behaviour of lubySequence(1.9),
// lubySequence(Infinity) and lubyBudgets(n, NaN) all depend on IEEE-754
// doubles and on Math.floor/Math.max semantics, not on integers.
//
// Fidelity notes (deliberate, do not "fix" — when CODEAF_GO_FIX_LUBY_GUARDS is
// unset these behaviours hold):
//   - LubySequence(NaN) NEVER RETURNS, exactly like the TS. luby.ts:36 does
//     `Math.max(1, Math.floor(i))`, which is NaN for a NaN input; the inner
//     `while (pow < n)` is then false, `pow === n` is false, and
//     `n = n - half + 1` keeps n at NaN, so the `for (;;)` spins forever.
//     Go reproduces it bit-for-bit (math.Max(1, NaN) == NaN). Callers in
//     swe-pro never pass NaN, and no fixture may exercise it.
//   - LubyBudgets(+Inf, …) likewise never returns (count = +Inf makes the
//     `i <= count` loop unbounded); TS additionally OOMs on the growing array.
//   - LubyBudgets has NO integer conversion: `lubySequence(i) * unit` is a
//     float multiply, so a NaN/±Inf unit propagates into the result array
//     (JSON.stringify renders those elements as null, and so does
//     jscompat.JSNumber).
//   - The scan `pow = pow*2 + 1` loses the `+ 1` once pow exceeds 2^53. That
//     is a real (harmless) precision artefact of the TS; the Go expression
//     keeps the same shape so it degrades identically.
//   - The result slice is allocated non-nil so an empty result marshals as
//     `[]` (TS `const out: number[] = []`), never `null`.
package luby

import (
	"math"

	"github.com/Agent-Field/swe-pro-go/internal/fixflag"
)

// LubySequence is the i-th value of the Luby sequence (1-indexed).
//
// LubySequence(1) = 1, LubySequence(2) = 1, LubySequence(3) = 2,
// LubySequence(4..6) = 1,1,2, LubySequence(7) = 4, ...
//
// Definition (reluctant doubling): for the smallest k with i <= 2^k - 1,
//   - if i === 2^k - 1  →  2^(k-1)
//   - else              →  LubySequence(i - 2^(k-1) + 1)
//
// i is a 1-indexed position. Values < 1 clamp to 1. NaN does not terminate
// (see the package doc).
func LubySequence(i float64) float64 {
	// Clamp/normalize: the schedule is only defined for positive integers.
	// With CODEAF_GO_FIX_LUBY_GUARDS, NaN and indices < 1 short-circuit to 1
	// instead of spinning forever on NaN.
	if fixflag.Enabled("CODEAF_GO_FIX_LUBY_GUARDS") {
		if math.IsNaN(i) || i < 1 {
			return 1
		}
	}
	n := math.Max(1, math.Floor(i))
	// Iterative unfolding of the recursive definition — O(log n) with no stack.
	// Find the block [2^(k-1), 2^k - 1] that n falls in; if n is the block's
	// right edge (2^k - 1) the answer is the power 2^(k-1), otherwise recurse
	// into the block by re-indexing n to (n - 2^(k-1) + 1).
	for {
		// pow = 2^k - 1 walks 1, 3, 7, 15, ... ; half = 2^(k-1) walks 1, 2, 4, ...
		pow := 1.0
		half := 1.0
		for pow < n {
			half = pow + 1
			pow = pow*2 + 1
		}
		if pow == n {
			return half
		}
		// n is strictly inside the block [half, pow); re-index and repeat.
		n = n - half + 1
	}
}

// LubyBudgets returns the first n attempt budgets, each Luby value scaled by
// unit.
//
// unit is expressed in attempt-count terms: for the current portfolio phase an
// attempt IS the unit, so unit === 1 yields the raw Luby values and each
// attempt still costs one unit. The scaling hook exists for the staged deeper
// scheduler, where unit becomes a per-attempt budget (turns / dollars) and the
// Luby values become the reluctant-doubling multipliers for retrying a stalled
// seed.
//
// n is how many budgets to produce (values < 0 clamp to 0). unit is the
// multiplier applied to each Luby value; it is variadic to model the TS
// default parameter `unit: number = 1` — omitting it (or passing no elements)
// is the TS `undefined` path that takes the default. Extra elements past the
// first are ignored, matching JS's extra-argument behaviour.
func LubyBudgets(n float64, unit ...float64) []float64 {
	u := 1.0
	if len(unit) > 0 {
		u = unit[0]
	}
	if fixflag.Enabled("CODEAF_GO_FIX_LUBY_GUARDS") {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return []float64{}
		}
	}
	count := math.Max(0, math.Floor(n))
	if fixflag.Enabled("CODEAF_GO_FIX_LUBY_GUARDS") {
		if count > 1<<20 {
			count = 1 << 20
		}
	}
	out := []float64{}
	for i := 1.0; i <= count; i++ {
		out = append(out, LubySequence(i)*u)
	}
	return out
}
