package lane

import (
	"math"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── WHAT IS BELIEVED, AND WHERE IT IS BORROWED FROM ─────────────────────────
//
// A belief per (model, lane) is blind on the first request to every pair, and
// the first request to a pair is the STEADY STATE: a person picks a model at
// runtime, the sheet is half an hour old at best, and the ledger has never
// heard of the seventeen machines behind it. The measured consequence was a
// three-minute wait with nothing watching it.
//
// So a belief is not one number with one history; it is a SUM OF FOUR, and each
// of them is learned from a different amount of evidence:
//
//	ln T(model, lane)  =  μ  +  a[lane]  +  b[model]  +  e[model, lane]
//
//	μ                the world's own pace, moved by everything
//	a[lane]          this PROVIDER, across every model it serves — a machine
//	                 that is quick for one model is usually quick for another,
//	                 and that is the fact a per-pair ledger threw away
//	b[model]         this MODEL, across every provider — a large model is slow
//	                 everywhere and a small one is quick everywhere
//	e[model, lane]   what is left: this deployment, on this pair, right now
//
// A prediction is the sum of the four means and the sum of the four variances,
// so a pair nobody has measured predicts from μ + a + b with an honestly wide
// spread rather than from nothing at all. That is the whole of cold start, and
// it needs no special case anywhere: [Chain.Known] is true the moment the world
// has a pace.
//
// EACH COMPONENT FORGETS AT ITS OWN RATE, because they are facts about
// different things. One deployment's queue turns over in minutes; a provider's
// fleet in half an hour; a model's size never. See the design for the figures
// and for which of them are priors to be learned.
//
// The equations, the priors, the borrowing rule, the change-point test and the
// file format are all in `docs/design/waiting/DESIGN.md`. This file is the
// contract they are written against.

// Level names one term of the sum above, and the order is the order of the
// terms: broadest first, narrowest last.
type Level uint8

const (
	// LevelWorld is μ: one number for everything this process has ever timed.
	LevelWorld Level = iota
	// LevelLane is a[lane]: the provider's own offset, shared by every model it
	// serves. It is the level that makes a never-seen pair predictable.
	LevelLane
	// LevelModel is b[model]: the model's own offset, shared by every provider
	// serving it.
	LevelModel
	// LevelPair is e[model, lane]: this deployment and nothing else.
	LevelPair
)

// Levels is how many terms a chain has. It is spelled once so that a loop over
// the components and the length of the array cannot drift apart.
const Levels = 4

// Component is one term of the sum, as a Gaussian in the LOG DOMAIN.
//
// X is its mean and P its variance. A component with P at zero is one nothing
// is believed about, which is the honest state of every level of a process that
// has just started and of the pair level for most pairs forever.
type Component struct {
	X float64
	P float64
}

// Known reports whether anything is believed. Variance is strictly positive for
// any real belief, so P at zero is the emptiness law in one field.
func (c Component) Known() bool { return c.P > 0 }

// Chain is the four components of one prediction, in [Level] order.
//
// It is an array and not a map because there are exactly four of them, they are
// always all present, and a loop over four fixed slots is the cheapest thing
// this arithmetic can be.
type Chain [Levels]Component

// Known reports whether the chain can predict at all: any component believed is
// enough, because the sum of the rest is zero and their variances are the
// honest statement of how little is known.
func (c Chain) Known() bool {
	for _, part := range c {
		if part.Known() {
			return true
		}
	}
	return false
}

// Predict is the belief about one pair right now: the sum of the means, and the
// sum of the variances.
//
// SUMMING THE VARIANCES IS WHY COLD START IS NOT A SPECIAL CASE. A pair with a
// measured provider and an unmeasured deployment predicts the provider's mean
// with the provider's certainty plus the pair level's whole prior spread, which
// is exactly "we know roughly, and not precisely" said in arithmetic.
func (c Chain) Predict() (mu, variance float64) {
	for _, part := range c {
		mu += part.X
		variance += part.P
	}
	return mu, variance
}

// Survival is this chain as the distribution the controller waits against, in
// SECONDS, floored at how variable one answer from this pair really is.
//
// THE FLOOR IS THE CORRECTION AND WHOSE VARIABILITY IT IS, IS THE RULE.
// [Chain.Predict] returns the variance of the ESTIMATE — how well the median is
// known — which shrinks toward nothing as evidence accumulates. What a wait is
// judged against is how variable ONE DRAW is, which no amount of watching a
// lane shrinks. A controller handed the estimate's spread alone would believe a
// tail impossible and would never hedge the lane that has one.
//
// So draw is a floor and NOT A CONSTANT. [Hierarchy.Draw] answers it from the
// dispersion the sheet published for this pair — the distance between its own
// p50 and p90, which is that lane's measured variability — and [SpreadFloor] is
// the prior for a pair nothing has been published about. One figure under every
// lane said instead that every lane's tail is the worst tail on the sheet: the
// lane `bench/lanelab` proves this against publishes 0.577 nats, and was waited
// against one.
//
// unit is how many of the chain's own units make a second: the first-token
// chain is in milliseconds and passes 1000, a chain already in seconds passes 1.
func (c Chain) Survival(draw, unit float64) control.Survival {
	if !c.Known() || unit <= 0 {
		return control.Survival{}
	}
	mu, variance := c.Predict()
	return control.Survival{Mu: mu - math.Log(unit), Sigma: math.Max(math.Sqrt(variance), draw)}
}

// Hierarchy is the belief store's door for everything the controller needs.
//
// It is a SECOND DOOR onto the same ledger rather than a second ledger: one
// sighting moves the chain and the flat belief together, because two accounts
// of one lane that were updated separately would disagree the first time one of
// them was fixed. [Ledger] answers "which lane" and this answers "how long",
// and the registry hands out one object that is both.
type Hierarchy interface {
	// Wait is the chain over ln first-token in MILLISECONDS for one pair, aged
	// to now.
	Wait(id ID, now time.Time) Chain
	// Rate is the chain over ln tokens-a-second for one pair, aged to now.
	Rate(id ID, now time.Time) Chain
	// Think is the chain over ln SECONDS of a whole thinking phase for one
	// MODEL. It is keyed on the model alone because how long a model deliberates
	// is a property of the model and of the effort rung it was asked at; a lane
	// can only make the same thought arrive faster, which the rate chain already
	// says.
	Think(model string, rung string, now time.Time) Chain
	// Draw is how much ONE ANSWER from this pair varies around what is
	// believed about it, in nats of log-spread: the first token and the gap
	// between two tokens.
	//
	// IT IS THE OTHER HALF OF [Chain.Survival] AND IT IS NOT THE CHAIN'S. A
	// chain holds how well a median is known and that is a belief this process
	// sharpens by watching; how variable one answer is around it is a property
	// of the machine, and the sheet publishes it as the distance between a p50
	// and a p90. A pair nothing has been published about answers [SpreadFloor],
	// which is the prior for the same quantity.
	Draw(id ID) (first, gap float64)
	// Shifted reports whether a change point has just reset this pair's own
	// component toward its parents, and clears the flag. It is what the call log
	// records and what the HUD may explain a sudden re-route with.
	Shifted(id ID) bool
	// NoteThinking folds in how long one whole run of reasoning lasted.
	//
	// IT IS ON THIS DOOR RATHER THAN ON [Ledger] because it is the only
	// observation in the design that is not about a deployment: a lane cannot
	// make a model think less, it can only make the same thought arrive faster,
	// which the rate chain already says. [Hierarchy.Think] is what reads it back,
	// and the two belong to one another — a build that could predict a thinking
	// phase but never record one would be predicting from the prior forever.
	NoteThinking(model, rung string, took time.Duration, at time.Time)
}

// ── THE TWO DOORS, ASKED WITHOUT ASKING WHICH ───────────────────────────────
//
// [Hierarchy] is a SECOND DOOR onto the same ledger, and a build whose ledger
// is only the plainer kind is a legal build. That leaves every caller with the
// same three lines of type assertion and the same decision about what to do
// when it fails — which is three places to get it wrong and one vocabulary of
// waiting spread across two packages.
//
// So the assertion is made HERE, once per question, and what a caller outside
// this package sees is a function that answers or says it does not know. The
// transport never learns what a [Chain] is.

// Thinks is how long this model's whole thinking phase is expected to last, in
// SECONDS, keyed on the model and the effort rung it was asked at.
//
// An unknown one is a real state: the duration clock then has nothing to say
// and the liveness clock and the ceiling are what bound the phase.
func Thinks(model, rung string, now time.Time) control.Survival {
	chains, ok := Default().Ledger().(Hierarchy)
	if !ok || model == "" {
		return control.Survival{}
	}
	// A THINKING PHASE HAS NO PUBLISHED DISPERSION. No sheet says how much one
	// run of thought varies around this model's usual one, so the prior stands
	// here where a lane's own figure stands in [PaceFor].
	return chains.Think(model, rung, now).Survival(SpreadFloor, 1)
}

// NoteThought folds in how long one whole run of reasoning really lasted. It is
// what makes [Thinks] a measurement rather than a prior, and a ledger that
// cannot hold one drops it.
func NoteThought(model, rung string, took time.Duration, at time.Time) {
	if chains, ok := Default().Ledger().(Hierarchy); ok {
		chains.NoteThinking(model, rung, took, at)
	}
}

// ── THE WAITING SEAM ────────────────────────────────────────────────────────
//
// The controller is installed once per process, in the same spirit as the
// registry's five seams and for the same reason: a caller that built its own
// would be a caller with its own idea of when to act, and the first time the
// two disagreed the surface would be counting down to a moment nothing
// happened at.
//
// It is a package-level knob rather than a sixth field on [Registry] because it
// is a FACTORY and not an implementation — the thing installed makes one
// controller per request — and because a build with no controller wired is a
// legal build that must still send requests. A nil factory is that build, and
// `law_test.go` is what says a shipped one is not it.

var controller struct {
	mu    sync.RWMutex
	build control.Factory
}

// SetController installs the factory every token-generating call is watched
// with, and hands back the one that was there.
//
// IT RETURNS THE PREVIOUS ONE FOR THE SAME REASON [provider.OnPhase] DOES: a
// caller that swaps a seam has to be able to put back what it found, and a
// caller that put back NIL would leave the process with no policy at all —
// which is a build where nothing waits on anything, arrived at by a test
// tidying up after itself. A nil factory is still a legal argument, because a
// caller that really wants the bare stream loop back has to be able to ask.
func SetController(build control.Factory) (previous control.Factory) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	previous, controller.build = controller.build, build
	return previous
}

// Controller is the installed factory, nil when nothing is installed.
func Controller() control.Factory {
	controller.mu.RLock()
	defer controller.mu.RUnlock()
	return controller.build
}

// THE CONTROLLER IS INSTALLED HERE AND NOWHERE ELSE. One line, in the file that
// owns the seam, so that "which controller is this build waiting with" is a
// question with one answer and one place to read it. A caller that built its
// own would be a caller with its own idea of when to act, and the surface's
// countdown and the transport's alarm would be two numbers that agree until the
// day one of them is fixed.
func init() { SetController(control.New) }
