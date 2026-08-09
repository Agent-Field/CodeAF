package caprecap

import (
	"math"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Line-for-line translation of src/session/capture-recapture.test.ts.
// Subtest names are verbatim from the TS `test(...)` titles; the describe
// blocks become the top-level Test functions.

func equalEstimate(got ResidualEstimate, total, residual float64) bool {
	// toEqual on two {number, number} objects is value equality; NaN is not
	// reachable here but compare defensively so a NaN never passes silently.
	if math.IsNaN(float64(got.EstimatedTotal)) || math.IsNaN(float64(got.EstimatedResidual)) {
		return false
	}
	return float64(got.EstimatedTotal) == total && float64(got.EstimatedResidual) == residual
}

func TestCaptureRecaptureEstimateResidualDefects(t *testing.T) {
	t.Run("full overlap ⇒ no residual (both audits saw the same defects)", func(t *testing.T) {
		// n1=3, n2=3, m=3: N̂ = (4·4/4)−1 = 3, distinct = 3, residual = 0.
		got := EstimateResidualDefects(ResidualInput{Sample1: 3, Sample2: 3, Overlap: 3})
		if !equalEstimate(got, 3, 0) {
			t.Errorf("got %+v, want {3 0}", got)
		}
	})

	t.Run("partial overlap ⇒ positive residual (Chapman point estimate)", func(t *testing.T) {
		// n1=5, n2=4, m=2: N̂ = (6·5/3)−1 = 9, distinct = 7, residual = 2.
		got := EstimateResidualDefects(ResidualInput{Sample1: 5, Sample2: 4, Overlap: 2})
		if !equalEstimate(got, 9, 2) {
			t.Errorf("got %+v, want {9 2}", got)
		}
	})

	t.Run("zero overlap ⇒ large estimated population", func(t *testing.T) {
		// n1=3, n2=3, m=0: N̂ = (4·4/1)−1 = 15, distinct = 6, residual = 9.
		got := EstimateResidualDefects(ResidualInput{Sample1: 3, Sample2: 3, Overlap: 0})
		if !equalEstimate(got, 15, 9) {
			t.Errorf("got %+v, want {15 9}", got)
		}
	})

	t.Run("fractional estimate rounds and never falls below distinct-found", func(t *testing.T) {
		// n1=10, n2=2, m=1: N̂ = (11·3/2)−1 = 15.5 → round 16, distinct = 11, residual = 5.
		got := EstimateResidualDefects(ResidualInput{Sample1: 10, Sample2: 2, Overlap: 1})
		if !equalEstimate(got, 16, 5) {
			t.Errorf("got %+v, want {16 5}", got)
		}
	})

	t.Run("overlap is clamped to ≤ min(n1, n2); negatives clamp to 0", func(t *testing.T) {
		// overlap 9 clamps to min(2,3)=2 ⇒ same as m=2.
		gotClamped := EstimateResidualDefects(ResidualInput{Sample1: 2, Sample2: 3, Overlap: 9})
		gotExplicit := EstimateResidualDefects(ResidualInput{Sample1: 2, Sample2: 3, Overlap: 2})
		if gotClamped != gotExplicit {
			t.Errorf("got %+v, want %+v", gotClamped, gotExplicit)
		}
		// n1 clamps to 0 ⇒ degenerate single-capture ⇒ residual 0.
		got := EstimateResidualDefects(ResidualInput{Sample1: -4, Sample2: 3, Overlap: -1})
		if !equalEstimate(got, 3, 0) {
			t.Errorf("got %+v, want {3 0}", got)
		}
	})

	t.Run("empty samples ⇒ zero everything, no NaN/Infinity", func(t *testing.T) {
		r := EstimateResidualDefects(ResidualInput{Sample1: 0, Sample2: 0, Overlap: 0})
		if !equalEstimate(r, 0, 0) {
			t.Errorf("got %+v, want {0 0}", r)
		}
		if math.IsNaN(float64(r.EstimatedTotal)) {
			t.Errorf("estimatedTotal is NaN")
		}
	})
}

func TestCaptureRecaptureMatchFindings(t *testing.T) {
	t.Run("identical strings match", func(t *testing.T) {
		if got := MatchFindings([]string{"null deref in parser.ts"}, []string{"null deref in parser.ts"}); got != 1 {
			t.Errorf("got %d, want 1", got)
		}
	})

	t.Run("high token overlap matches above threshold", func(t *testing.T) {
		got := MatchFindings(
			[]string{"null pointer dereference in parser at line 42"},
			[]string{"null pointer dereference parser line 42 crash"},
		)
		if got != 1 {
			t.Errorf("got %d, want 1", got)
		}
	})

	t.Run("unrelated findings do not match", func(t *testing.T) {
		got := MatchFindings([]string{"missing null check in auth"}, []string{"off by one in the pagination loop"})
		if got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})

	t.Run("greedy one-to-one: overlap never exceeds min(a,b)", func(t *testing.T) {
		a := []string{"race condition in the queue drain", "race condition in the queue drain"}
		b := []string{"race condition in the queue drain"}
		if got := MatchFindings(a, b); got != 1 {
			t.Errorf("got %d, want 1", got)
		}
	})

	t.Run("empty inputs yield zero overlap", func(t *testing.T) {
		if got := MatchFindings([]string{}, []string{"anything"}); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
		if got := MatchFindings([]string{"anything"}, []string{}); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})
}

// ── kept-as-is TS behavior, pinned so a "cleanup" cannot silently change it ──

func TestKeptTSQuirks(t *testing.T) {
	t.Run("threshold 0 still matches nothing when similarity is 0", func(t *testing.T) {
		// bestSim starts at 0 and needs a STRICT improvement, so bestIdx
		// stays -1 and the `bestSim >= threshold` test never runs.
		if got := MatchFindings([]string{"xx yy"}, []string{"pp qq"}, 0); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
		if got := MatchFindings([]string{"xx yy"}, []string{"pp qq"}, -1); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})

	t.Run("only the single best candidate is threshold-tested", func(t *testing.T) {
		// b[0] is the better similarity but still below threshold; b[1] is a
		// perfect match yet is never reconsidered... except it IS the best
		// here, so flip it: make the runner-up the only one above threshold.
		a := []string{"aa bb cc dd"}
		b := []string{"aa bb cc dd ee ff", "aa bb cc"}
		// sim(a, b0) = 4/6 = 0.667; sim(a, b1) = 3/4 = 0.75 → best is b1.
		if got := MatchFindings(a, b, 0.7); got != 1 {
			t.Errorf("got %d, want 1", got)
		}
		// Raise the bar above both: the best (0.75) fails, nothing else tried.
		if got := MatchFindings(a, b, 0.8); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})

	t.Run("NaN threshold matches nothing", func(t *testing.T) {
		if got := MatchFindings([]string{"aa bb cc"}, []string{"aa bb cc"}, math.NaN()); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})

	t.Run("one-character tokens are dropped so single letters never match", func(t *testing.T) {
		if got := MatchFindings([]string{"a b c"}, []string{"a b c"}); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})

	t.Run("Chapman overflow yields Infinity total and NaN residual, both JSON null", func(t *testing.T) {
		got := EstimateResidualDefects(ResidualInput{Sample1: 1e308, Sample2: 1e308, Overlap: 0})
		if !math.IsInf(float64(got.EstimatedTotal), 1) {
			t.Errorf("estimatedTotal = %v, want +Inf", float64(got.EstimatedTotal))
		}
		if !math.IsNaN(float64(got.EstimatedResidual)) {
			t.Errorf("estimatedResidual = %v, want NaN", float64(got.EstimatedResidual))
		}
		enc, err := jscompat.Stringify(got)
		if err != nil {
			t.Fatalf("stringify: %v", err)
		}
		if string(enc) != `{"estimatedTotal":null,"estimatedResidual":null}` {
			t.Errorf("got %s", enc)
		}
	})
}
