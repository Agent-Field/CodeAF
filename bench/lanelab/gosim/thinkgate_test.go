package main

// ── WHEN THE THINK CLOCK'S ABNORMALITY GATE CLOSES ──────────────────────────
//
// §B's second test asks whether a wait is past the (1 − p) quantile of the very
// survival that clock reads. For the DURATION clock that survival is
// [lane.Thinks] — the four-level chain over how long this model's whole run of
// thought lasts — and the spread it is judged at is `Chain.Survival`'s larger of
// two: the estimate's own, and how much ONE DRAW of the quantity varies
// (waiting.go: `Sigma: math.Max(math.Sqrt(variance), draw)`).
//
// A THINKING PHASE HAS NO PUBLISHED DRAW, WHICH IS WHY THIS TEST EXISTS. While
// `Thinks` passed the bare [lane.SpreadFloor] the second term was a constant,
// it was always the larger, and the quantile sat at fifteen times the believed
// median — permanently past the role's ceiling, at any amount of evidence. The
// gate could not close, and this test asserted that it did not, so the
// limitation could not go stale unnoticed.
//
// IT IS OBSERVED WHERE IT IS NOT PUBLISHED (#316). Every thinking duration
// folded through the real [lane.NoteThought] door is one draw of exactly that
// quantity, so the chain keeps its own account of them and the floor gives way
// to what has been measured. This test now measures WHERE it gives way, in
// observations through that same door.
import (
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// gateCloses is the n this build closes the gate at, and gateClosesBy is how
// much later than that a change is allowed to move it before somebody has to
// say why.
//
// THE BOUND IS WHAT KEEPS "IT CLOSES" FROM MEANING "IT CLOSES EVENTUALLY". A
// regression toward the old behaviour would not have to reach infinity to
// matter: a gate that needed two hundred thoughts is a gate no conversation
// ever reaches, and the whole point of the close is that an afternoon's use of
// one model is enough.
const (
	gateCloses   = 31
	gateClosesBy = 40
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
	for _, n := range []int{0, 1, 2, 5, 10, 20, 30, 31, 40, 60} {
		think := thinkBelief(t, n, 0)
		quantile := think.Quantile(z)
		inside := quantile > 0 && quantile < ceiling
		if inside && closed < 0 {
			closed = n
		}
		t.Logf("n=%2d  mu=%7.3f sigma=%6.3f  median=%8.2fs  quantile(z)=%10.2fs  inside the ceiling: %v",
			n, think.Mu, think.Sigma, think.Quantile(0), quantile, inside)
	}
	// ── THE MEASURED ANSWER: IT CLOSES, AND IT CLOSES IN AN AFTERNOON ───────
	//
	// σ starts at the four level priors summed, falls with the estimate while
	// the estimate is the wider of the two, and then falls with the DRAW the
	// leaf has measured for itself — pooled with [lane.SpreadFloor] at one
	// observation's weight, so a single thought cannot collapse it, and floored
	// at [lane.SpreadTightest], so the tail is never impossible.
	if closed < 0 {
		t.Fatalf("the gate never closed at any n tried, which is the state #316 was filed about — "+
			"the draw the think chain measures for itself is not reaching Chain.Survival "+
			"(lane.Thinks passes lane.Hierarchy.ThinkDraw, and %.2f is the prior it starts at)",
			lane.SpreadFloor)
	}
	if closed != gateCloses {
		if closed > gateClosesBy {
			t.Fatalf("the gate closed at n = %d, past the %d this build allows — a duration clock "+
				"that needs that many thoughts is one no conversation reaches", closed, gateClosesBy)
		}
		t.Errorf("the gate closed at n = %d, not the pinned %d; if the new figure is right, "+
			"move gateCloses and say why in bench/lanelab/REPORT.md", closed, gateCloses)
	}
	// AND THE FLOOR IS NOT MERELY LOWER, IT IS THE MEASURED ONE. A build that
	// closed the gate by lowering the constant would pass the assertion above
	// and would have learned nothing from the model it was watching.
	settled := thinkBelief(t, 60, 0)
	if settled.Sigma >= lane.SpreadFloor {
		t.Errorf("sigma settled at %.4f, still at or above the %.4f prior — the observed draw "+
			"never outranked it", settled.Sigma, lane.SpreadFloor)
	}
	if settled.Sigma < lane.SpreadTightest {
		t.Errorf("sigma settled at %.4f, under the %.4f a predictive spread may claim — "+
			"a tail this build calls impossible is one it will act on", settled.Sigma, lane.SpreadTightest)
	}
	t.Logf("THE GATE CLOSES AT n = %d. At n = 60 sigma is %.3f against the %.2f prior, and the "+
		"quantile is %.2fs inside the %.0fs ceiling.",
		closed, settled.Sigma, lane.SpreadFloor, settled.Quantile(z), ceiling)
}

// TestAModelWhoseThinkingReallyVariesKeepsItsGateOpen is the other half of the
// close, and it is the half that could have been got wrong silently.
//
// A GATE THAT CLOSED BECAUSE A CONSTANT WAS DELETED WOULD PASS THE TEST ABOVE.
// The estimate's own spread shrinks with evidence whatever the evidence says,
// so a build that simply stopped flooring σ would call every well-measured
// model steady — including the one that thinks for two seconds on one question
// and forty on the next — and would hedge its legitimate long thoughts. What
// makes the close safe is that the floor gives way to a MEASUREMENT: the same
// sixty observations, spread the way a real model's are, leave the gate exactly
// where it was.
func TestAModelWhoseThinkingReallyVariesKeepsItsGateOpen(t *testing.T) {
	const k = 2 + thinkVisible
	z := deviateFor(falseActBudgetOf(), k)
	ceiling := proofRole.Ceiling().Seconds()

	steady := thinkBelief(t, 60, 0)
	varied := thinkBelief(t, 60, thinkVaries)
	t.Logf("60 steady thoughts: sigma %.3f, quantile %.2fs; 60 varied ones: sigma %.3f, quantile %.2fs; ceiling %.0fs",
		steady.Sigma, steady.Quantile(z), varied.Sigma, varied.Quantile(z), ceiling)

	if varied.Sigma <= steady.Sigma {
		t.Fatalf("a model that varies by %.2f nats is believed no wider (%.3f) than one that does not "+
			"(%.3f) — the dispersion the chain keeps is not reaching the survival",
			thinkVaries, varied.Sigma, steady.Sigma)
	}
	if quantile := varied.Quantile(z); quantile < ceiling {
		t.Fatalf("the gate closed at %.2fs on a model whose thinking really varies, inside the %.0fs "+
			"ceiling — its legitimate long thoughts are now abnormal", quantile, ceiling)
	}
}

// thinkVaries is how much one run of thought moves around this model's usual
// one in the varied sweep, in nats. It is about what the measured sheet
// publishes for its WORST lanes, which is the shape a model that answers some
// questions in a breath and others in a minute really has.
const thinkVaries = 0.9

// thinkBelief is what [lane.Thinks] answers after n thoughts have been folded
// in through the real door, in a home of this test's own.
//
// varies is how far each thought sits from the model's own median, in nats:
// zero is a model that deliberates for the same time every time, which is what
// the proof rows script.
//
// A MINUTE APART AND ENDING AT THE MOMENT, because a thinking chain ages like
// every other belief: a sweep that stamped them all at once would be measuring
// a history no session ever has.
func thinkBelief(t *testing.T, n int, varies float64) control.Survival {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	lane.Default().Reset()
	at := theMoment.Add(-time.Duration(n) * time.Minute)
	for i := range n {
		// A SPREAD WITH NO NET DRIFT, so that the two sweeps are believed at the
		// same median and the only thing that separates them is the width.
		away := math.Exp(varies * float64(2*(i%2)-1))
		took := time.Duration(float64(thinkFor) * away)
		lane.NoteThought(thinkModel, "", took, at.Add(time.Duration(i)*time.Minute))
	}
	return lane.Thinks(thinkModel, "", theMoment)
}

// thinkModel is the model the proof rows are run against, spelled once.
const thinkModel = "deepseek/deepseek-v4-flash"

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
