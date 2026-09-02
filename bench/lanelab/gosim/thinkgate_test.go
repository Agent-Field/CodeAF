package main

// ── WHEN THE THINK CLOCK'S ABNORMALITY GATE CLOSES ──────────────────────────
//
// §B's second test asks whether a wait is past the (1 − p) quantile of the very
// survival that clock reads. For the DURATION clock that survival is
// [lane.Thinks] — the four-level chain over how long this model's whole run of
// thought lasts — and a thinking phase has no published dispersion, so
// `Chain.Survival` floors its spread at [lane.SpreadFloor] (waiting.go: `Sigma:
// math.Max(math.Sqrt(variance), draw)`, called from `Thinks` with
// `SpreadFloor`) and the prediction is the sum of the level priors
// (`Chain.Predict`, hier.go's `levelSpread` = 1.2/0.9/0.6/0.5).
//
// SO THE COLD QUANTILE IS ENORMOUS AND THE GATE STANDS OPEN. Not because the
// code disables it, but because a belief that wide puts its own tail far past
// the role's ceiling: nothing can be abnormal before the ceiling has already
// fired. This test measures where that stops being true, in observations
// through the real [lane.NoteThought] door.
import (
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lane"
)

func TestWhenTheThinkGateCloses(t *testing.T) {
	// The z the proof rows' thinking case runs at: k = 2 + Expected, and that
	// row streams [thinkVisible] visible tokens.
	const k = 2 + thinkVisible
	z := deviateFor(falseActBudgetOf(), k)
	ceiling := proofRole.Ceiling().Seconds()
	t.Logf("z = %.4f at k = %d; the role's ceiling is %.0fs; one thought is %.2fs",
		z, k, ceiling, thinkFor.Seconds())

	closed := -1
	for _, n := range []int{0, 1, 2, 3, 4, 5, 10, 20, 60} {
		t.Setenv(home.EnvVar, t.TempDir())
		lane.Default().Reset()
		at := theMoment.Add(-time.Duration(n) * time.Minute)
		for i := 0; i < n; i++ {
			lane.NoteThought("deepseek/deepseek-v4-flash", "", thinkFor, at.Add(time.Duration(i)*time.Minute))
		}
		think := lane.Thinks("deepseek/deepseek-v4-flash", "", theMoment)
		quantile := think.Quantile(z)
		inside := quantile > 0 && quantile < ceiling
		if inside && closed < 0 {
			closed = n
		}
		t.Logf("n=%2d  mu=%7.3f sigma=%6.3f  median=%8.2fs  quantile(z)=%10.2fs  inside the ceiling: %v",
			n, think.Mu, think.Sigma, think.Quantile(0), quantile, inside)
	}
	// ── THE MEASURED ANSWER: IT NEVER CLOSES ────────────────────────────────
	//
	// σ bottoms out at exactly [lane.SpreadFloor] and stays there however many
	// thoughts are folded in, because `Chain.Survival` takes the LARGER of the
	// estimate's spread and the draw's, and a thinking phase has no published
	// draw. So the quantile is pinned at `median · e^(z·SpreadFloor)` forever,
	// and for this role that is about fifteen times the median.
	//
	// The duration clock can therefore act before the ceiling ONLY for a model
	// believed to think for less than `ceiling · e^(−z·SpreadFloor)` — about two
	// thirds of a second here. Any model that deliberates for longer than that
	// is in the ungated regime permanently, at any amount of evidence.
	if closed >= 0 {
		t.Fatalf("the gate closed at n = %d, which contradicts the floor in Chain.Survival — "+
			"if this is now true the limitation in REPORT.md and the changelog is stale", closed)
	}
	think := lane.Thinks("deepseek/deepseek-v4-flash", "", theMoment)
	if think.Sigma != lane.SpreadFloor {
		t.Errorf("sigma settled at %.4f, not at the %.4f floor — the mechanism named in the report "+
			"is not the mechanism running", think.Sigma, lane.SpreadFloor)
	}
	longest := ceiling * math.Exp(-z*lane.SpreadFloor)
	t.Logf("THE GATE NEVER CLOSES. sigma is pinned at the %.2f floor, so the duration clock can "+
		"act before the %.0fs ceiling only for a model believed to think for under %.3fs.",
		lane.SpreadFloor, ceiling, longest)
}

// falseActBudgetOf and deviateFor reach the two figures §B derives. They are
// here rather than exported from `internal/lane/control` because a bench may
// read a package's arithmetic without the package growing a door for it.
func falseActBudgetOf() float64 { return 0.02 }

func deviateFor(budget float64, k int) float64 {
	tail := budget / float64(k)
	low, high := 0.0, 40.0
	for range 60 {
		middle := low + (high-low)/2
		if 1-phiOf(middle) > tail {
			low = middle
		} else {
			high = middle
		}
	}
	return high
}

// phiOf is the standard normal distribution function, the same one
// `internal/lane/control` uses to size the gate.
func phiOf(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }
