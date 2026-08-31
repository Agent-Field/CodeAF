package lane

import (
	"math"
	"time"
)

// ── THE BELIEF, AND WHY IT IS A FILTER ──────────────────────────────────────
//
// The thing this package replaces is a strike table: count two slow answers,
// demote; count three, refuse for five minutes. Its notion of slow was a fixed
// two seconds, which is wrong for a hundred-thousand-token prompt and wrong for
// a lane whose normal is four hundred milliseconds, and it forgot everything at
// exit.
//
// A scalar Kalman filter in the log domain answers both complaints with one
// mechanism. LOG DOMAIN because first-token wait and tokens-per-second are
// multiplicative quantities with heavy right tails: taking logs makes the noise
// near-Gaussian and makes "twice as slow" the same size of event everywhere.
// TWO INDEPENDENT SCALARS rather than one two-dimensional filter because the
// two fail for different reasons — queueing before the first token, GPU
// contention during the writing — and coupling them buys a covariance nobody
// can explain.
//
// What the filter gives that the strike table wanted is the INNOVATION: the
// size of (z − X) measured against the spread the filter expected. That is a
// free, calibrated "how surprising was that", relative to what this lane was
// doing ten minutes ago. It is the only notion of slow in this package.

// Posterior is one scalar belief in the log domain: X is the mean of the log of
// the quantity, P the variance of that mean.
//
// The natural-unit reading of X is the MEDIAN, not the mean — exp of the mean
// of a log is a median — and that is deliberate: a median first-token wait is
// what a person experiences, while a log-normal's mean is dragged up by a tail
// that happens one time in a hundred.
//
// A zero Posterior is NO BELIEF and reads as one everywhere: [Posterior.Known]
// is false, [Posterior.Mean] is zero rather than one, and [Posterior.Update]
// adopts its first observation outright instead of averaging with a certainty
// it does not have.
type Posterior struct {
	X float64
	P float64
}

// Known reports whether anything has been believed yet. Variance is strictly
// positive for any real belief — a filter that has seen one observation with
// noise R has P = R — so P at zero is the honest signal for "nothing here".
func (p Posterior) Known() bool { return p.P > 0 }

// Mean is the belief in its natural unit: milliseconds for a first-token
// posterior, tokens per second for a rate one. It is zero when nothing is
// believed, which the emptiness law then renders as nothing at all.
func (p Posterior) Mean() float64 {
	if !p.Known() {
		return 0
	}
	return math.Exp(p.X)
}

// Quantile is the belief at z standard deviations, in the natural unit:
// Quantile(1.2816) is the p90 and Quantile(-1.2816) the p10.
//
// It is how the tail gets priced. A lane that is fastest at the median and
// among the worst at the ninety-ninth percentile loses here, because a person
// remembers the twelve-second wait and not the four-hundred-millisecond one.
func (p Posterior) Quantile(z float64) float64 {
	if !p.Known() {
		return 0
	}
	return math.Exp(p.X + z*math.Sqrt(p.P))
}

// Predict ages a belief that has been sitting still.
//
// THE HALF-LIFE IS A HALF-LIFE OF CONFIDENCE, NOT OF THE ESTIMATE. After one
// half-life with no evidence the variance has doubled, so the information in
// the belief — which is one over the variance — has halved. That is exactly
// what "a ten-minute half-life" should mean and it needs nothing but the
// posterior itself: the estimate is not moved, because we have no reason to
// think a lane got faster or slower, only a reason to be less sure.
//
// A belief therefore never has to be un-demoted. Its variance widens until the
// sheet's pseudo-observation or a sampled draw puts it back in the running,
// which is why there is no penalty box and no cooldown timer anywhere in this
// package. The ledger clamps P at the prior's variance so that ageing can make
// a belief worthless but never worse than the public sheet.
func (p Posterior) Predict(dt, halfLife time.Duration) Posterior {
	if !p.Known() || dt <= 0 || halfLife <= 0 {
		return p
	}
	p.P *= math.Exp2(dt.Seconds() / halfLife.Seconds())
	return p
}

// Update folds one observation z — already in the log domain — with observation
// noise R, and returns the posterior that results.
//
// R is where the judgement lives and it is the caller's: a first-token
// measurement behind a sixty-thousand-token prompt is a noisy claim about the
// lane, because most of that wait was prefill nobody's endpoint could avoid, so
// it arrives with a large R. A probe measured on ten tokens is the sharpest
// claim there is and arrives with a small one. The sheet arrives as a
// pseudo-observation with R inflated by a constant, so that a public aggregate
// keeps pulling the belief toward reality without drowning our own answers.
//
// ON A POSTERIOR THAT KNOWS NOTHING the gain would be zero and the observation
// would be discarded, which is the one thing a first measurement must not be.
// An unknown belief is an infinitely wide prior, and the limit of the update
// there is to adopt the observation outright.
func (p Posterior) Update(z, R float64) Posterior {
	if R <= 0 {
		return p
	}
	if !p.Known() {
		return Posterior{X: z, P: R}
	}
	gain := p.P / (p.P + R)
	return Posterior{X: p.X + gain*(z-p.X), P: (1 - gain) * p.P}
}

// Innovation is how surprising an observation is, in standard deviations of
// what was expected. It is what a strike wanted to be, and unlike a strike it
// is comparable between a lane whose normal is four hundred milliseconds and
// one whose normal is four seconds.
func (p Posterior) Innovation(z, R float64) float64 {
	spread := math.Sqrt(p.P + R)
	if !p.Known() || spread <= 0 {
		return 0
	}
	return (z - p.X) / spread
}

// Beta is the belief that a lane's answers are usable: A successes, B failures.
//
// Quality is a lane property because lanes really do differ on it — a lane
// serving four-bit weights, a lane that truncates at its own undisclosed
// ceiling, a lane whose tool-call JSON the decoder refuses. It is kept as a
// count pair rather than a rate so that "nine out of ten" and "nine hundred out
// of a thousand" are not the same belief, which is the whole reason a gate can
// be honest about a lane it has barely seen.
type Beta struct {
	A float64
	B float64
}

// Known reports whether anything has been observed or assumed.
func (b Beta) Known() bool { return b.A+b.B > 0 }

// Mean is the believed share of answers that will be usable, zero when nothing
// is known — never a hopeful one.
func (b Beta) Mean() float64 {
	if !b.Known() {
		return 0
	}
	return b.A / (b.A + b.B)
}

// Observe folds one outcome in.
func (b Beta) Observe(accepted bool) Beta {
	if accepted {
		b.A++
		return b
	}
	b.B++
	return b
}

// Belief is everything this process thinks about one lane.
//
// Facts ride along with the posteriors because the gate and the score are asked
// in the same breath and a caller that had to fetch the facts separately would
// be a caller that could ask about a lane the sheet has since dropped.
//
// TTFT is a belief about MILLISECONDS and Rate about TOKENS PER SECOND, both in
// the log domain — the same units the sheet publishes, so that no seam in this
// package has to remember a conversion.
type Belief struct {
	ID      ID
	Facts   Facts
	TTFT    Posterior
	Rate    Posterior
	Quality Beta
	At      time.Time
}

// Known reports whether the belief carries any timing at all. A lane with facts
// and no timing is a lane the gate can judge and the score cannot, and the
// difference matters enough to be asked rather than inferred.
func (b Belief) Known() bool { return b.TTFT.Known() || b.Rate.Known() }
