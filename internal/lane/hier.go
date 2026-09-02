package lane

import (
	"math"
	"strings"
	"time"
)

// ── THE FOUR LEVELS, AND WHY A BELIEF IS A SUM ──────────────────────────────
//
// `waiting.go` states the contract this file implements and the reasoning
// behind it: a belief about a pair is not one history but four, summed,
//
//	ln T(model, lane)  =  μ  +  a[lane]  +  b[model]  +  e[model, lane]
//
// and the whole of cold start falls out of it. What is here is the arithmetic
// — how an observation is shared out between the four, how each of them
// forgets at its own rate, and how a step change in one deployment is caught
// without slandering the three levels it says nothing about.
//
// ── THE FOLD IS ONE SCALAR KALMAN UPDATE ON A DIAGONAL STATE ────────────────
//
// The prediction is the sum, so the innovation variance is the sum, and each
// level's gain is ITS OWN SHARE OF THE SURPRISE:
//
//	ŷ = Σ X_i     S = Σ P_i + R     K_i = P_i / S
//	X_i ← X_i + K_i·(z − ŷ)         P_i ← P_i·(1 − K_i)
//
// Eight lines of arithmetic with the property the design needs: a level that
// is already certain absorbs almost none of a surprise and a level that knows
// nothing absorbs almost all of it. The first sighting of a provider nobody
// has measured moves a[lane] a long way and μ hardly at all; the thousandth
// moves e and nothing else. No level is ever moved by an observation that says
// nothing about it — the sheet is published per model, so it moves b and e and
// leaves μ and a alone, because one refresh may not move every belief this
// process holds.
//
// ── WHAT A PRIOR IS, AND WHY IT AGES ────────────────────────────────────────
//
// A level nobody has moved yet is not a hole: it is the prior, a spread wide
// enough to cover what the measured world does, held from the first moment
// this process looked at anything. It ages like every other belief, because a
// process that has been running for a day without one sighting knows less
// about the world's pace than it did when it opened — and it stops at
// [SheetWeight] times the prior's own variance, which is the point at which
// the prior would outweigh what is held. Ageing may make a belief worthless;
// it may not make it worse than free. That is [age]'s law, said four times
// with four half-lives.

// levelSpread is σ at each level, in nats of log-spread, for a process that has
// seen nothing.
//
// They are priors to be fitted and they are guesses about HOW MUCH OF THE
// VARIATION LIVES WHERE, not about how fast any lane is: first-token medians
// across the seventeen measured lanes of one model span ln(3030/430) = 1.95
// nats, most of that spread is between providers rather than within one, and
// what is left after a provider and a model have spoken is the deployment.
var levelSpread = [Levels]float64{1.2, 0.9, 0.6, 0.5}

// levelHalfLife is how fast each level's fact really turns over: one
// deployment's queue in minutes, a provider's fleet in the half hour a capacity
// event lasts, a model's size and shape in hours, and the world's pace in a day.
//
// They are the only numbers here that are guesses about how fast the world
// changes rather than about how fast a lane is, and `bench/lanelab` fits them
// against replayed sightings.
var levelHalfLife = [Levels]time.Duration{
	LevelWorld: 24 * time.Hour,
	LevelLane:  30 * time.Minute,
	LevelModel: 6 * time.Hour,
	LevelPair:  10 * time.Minute,
}

// cusumSlack and cusumAlarm are the standard slack and alarm of a one-sided
// CUSUM: enough to detect a one-σ step in about eight observations without
// firing on ordinary noise. Both are priors to be fitted in the simulator.
const (
	cusumSlack = 0.5
	cusumAlarm = 4.0
)

// everywhere is the world level's own key. μ is ONE NUMBER FOR EVERYTHING, so
// the level that holds it has exactly one slot; naming it makes "this
// observation moves the world's pace" and "this one does not" the same kind of
// statement as every other level's.
const everywhere = "world"

// worldPace is where each quantity's μ starts before anything has been timed:
// the middle of the seventeen lanes this design was measured against, in the
// chain's own unit. With σ = 1.2 nats around it the prior covers most of their
// span without claiming to know where in it a given lane sits.
var (
	waitPace  = math.Log(1200) // milliseconds to a first token
	ratePace  = math.Log(25)   // tokens a second
	thinkPace = math.Log(8)    // seconds of a whole thinking phase
)

// subject names what each level of one observation is keyed on. An EMPTY NAME
// IS A LEVEL THIS QUANTITY DOES NOT HAVE — a thinking duration has no provider
// term, because a lane cannot make a model think less — and it is the only
// thing that takes a level out of the sum.
//
// IT IS NOT THE SAME QUESTION AS WHICH LEVELS MAY MOVE. See [absorbs]: every
// level a subject names is PREDICTED FROM, and only some of them absorb.
type subject [Levels]string

// absorbs says which of a subject's levels one observation is entitled to move.
//
// THE TWO ARE DIFFERENT QUESTIONS AND CONFLATING THEM COSTS AN ORDER OF
// MAGNITUDE. A sheet row is published per model, so it may move b[model] and
// e[model, lane] and must not move μ or a[lane] — one refresh moving the world's
// pace would move every belief this process holds. But the number it carries is
// an ABSOLUTE first token, and the belief it is folded into is a SUM. Predicting
// from the two levels it is allowed to move and folding the whole absolute into
// them puts μ's own mean into b and e as well, and [Chain.Predict] then adds μ
// a second time: a lane the sheet published at 430 ms was believed to take eight
// seconds, which is a controller that would wait out a person's whole patience
// on the fastest machine it knows.
//
// So the innovation is against the WHOLE belief, exactly as the design's own
// equations write it — ŷ = Σ X over the levels, S = Σ P + R over the same — and
// the constraint is applied where it belongs: to the gains.
//
// ── AND A PUBLISHED ROW IS AN OFFSET, WHICH IS THE OTHER HALF ───────────────
//
// Which levels may move is one question and HOW MUCH OF THE DIFFERENCE they
// take is another. One of our own sightings is evidence about all four levels
// and each takes its own share, which is the Kalman gain and is right: we do
// not know whether a slow answer means a slow world, a slow provider or a slow
// deployment.
//
// A SHEET ROW IS NOT THAT KIND OF EVIDENCE. It names one pair and states where
// that pair sits, so what is left to learn from it is not "how much of this is
// the world" — it is this deployment's own offset from what the parents already
// say. Sharing it out left the pair believed BETWEEN the world's pace and the
// published one: a lane the sheet published at 430 ms predicted about 1.2 s,
// because μ holds most of the variance and may not move, so the level that may
// took a sixth of the difference. `bench/lanelab` measured what that cost — the
// controller wanted a second request on a lane it had been told was fast.
//
// So a published row's levels take the WHOLE difference between the row and
// what the parents say, and the prediction lands exactly where the sheet put
// it. The variances still shrink by each level's own gain, because how much a
// row TEACHES is a separate question from where it puts the median, and a
// half-hour aggregate at [SheetWeight] teaches little: the belief is centred on
// the sheet and honestly unsure of it, which is what a prior with an honest
// floor was always supposed to mean.
type absorbs struct {
	// Levels is which of a subject's levels this observation may move.
	Levels [Levels]bool
	// Whole says the named levels carry the ENTIRE difference between what was
	// observed and what was believed, rather than each level's share of it.
	Whole bool
}

// everyLevel is the ordinary case: an observation about one deployment is
// evidence about the world, the provider, the model and the deployment, and
// each of them takes its own share of the surprise.
var everyLevel = absorbs{Levels: [Levels]bool{true, true, true, true}}

// publishedLevels is what a sheet row may move: the deployment it names, and
// nothing wider — and it moves it the whole way.
//
// IT IS THE PAIR ALONE AND THAT IS WHAT MAKES A SHEET READABLE. A row carries
// an offset now rather than a share, and an offset that landed in b[model] as
// well would be re-aimed by the next row of the same sheet: seventeen lanes of
// one model, folded in turn, would leave every pair but the last centred
// somewhere nobody published. e[model, lane] is where a deployment's own offset
// belongs, it is the only level a row is unambiguously about, and a pair the
// sheet has not published is untouched by one that it has.
var publishedLevels = absorbs{Levels: [Levels]bool{LevelPair: true}, Whole: true}

// pairOf is the subject of a timing observation: the world, the provider, the
// model, and the deployment.
//
// THE PROVIDER'S TERM IS KEYED CASE-INSENSITIVELY, because the two spellings
// arrive from two places on the wire — a chunk's `provider` and a sheet row's
// `provider_name` — and a[lane] is the level whose whole value is that every
// model a machine serves speaks about the same machine. Two spellings would be
// two providers, each with half the evidence.
func pairOf(id ID) subject {
	return subject{
		LevelWorld: everywhere,
		LevelLane:  strings.ToLower(id.Lane),
		LevelModel: id.Model,
		LevelPair:  id.String(),
	}
}

// modelOf is the subject of a sheet row. It is keyed exactly as a timing
// observation is — the belief it is folded into is the same sum — and which of
// those levels a published row may MOVE is [publishedLevels]'s answer, not this
// one's.
func modelOf(id ID) subject { return pairOf(id) }

// thoughtOf is the subject of a thinking duration: how long a model deliberates
// is a property of the model and of the rung it was asked at, so there is no
// lane term at all.
func thoughtOf(model, rung string) subject {
	model = BareModel(model)
	return subject{LevelWorld: everywhere, LevelModel: model, LevelPair: model + "|" + rung}
}

// node is one shared component with the moment it was last moved. A zero moment
// is "as old as this process's first look", which is what [chains.since] holds.
type node struct {
	X  float64   `json:"x,omitempty"`
	P  float64   `json:"p,omitempty"`
	At time.Time `json:"at,omitempty"`
}

// drift is one leaf's two CUSUM sums: evidence that it has become slower than
// believed, and evidence that it has become faster.
type drift struct {
	Up   float64 `json:"up,omitempty"`
	Down float64 `json:"down,omitempty"`
}

// chains is one quantity's four levels, each keyed by what it is shared under.
//
// The world is a single component, the other three are maps, and that asymmetry
// is the design rather than an accident: there is one world, a few dozen
// providers, a few dozen models, and a few hundred pairs.
type chains struct {
	// Pace is the world level's prior mean, in this chain's own unit.
	Pace  float64          `json:"pace,omitempty"`
	World node             `json:"world,omitzero"`
	Lane  map[string]node  `json:"lane,omitempty"`
	Model map[string]node  `json:"model,omitempty"`
	Pair  map[string]node  `json:"pair,omitempty"`
	Drift map[string]drift `json:"drift,omitempty"`
	// Since is the first moment this chain was ever asked about or told
	// anything, and it is what a level nobody has moved is aged from. A prior
	// is a belief held SINCE A MOMENT, not a fact outside time: a process that
	// opened yesterday and has measured nothing knows less about the world's
	// pace than it did when it opened.
	Since time.Time `json:"since,omitzero"`
}

// levelVariance is the prior variance at one level.
func levelVariance(level Level) float64 { return levelSpread[level] * levelSpread[level] }

// slot is the map one level is shared in, and nil for the world, which has one
// component rather than a set of them.
func (c *chains) slot(level Level) *map[string]node {
	switch level {
	case LevelLane:
		return &c.Lane
	case LevelModel:
		return &c.Model
	case LevelPair:
		return &c.Pair
	}
	return nil
}

// anchor records the earliest moment this chain has been asked about, which is
// what an untouched level's prior is dated from.
func (c *chains) anchor(now time.Time) {
	if now.IsZero() {
		return
	}
	if c.Since.IsZero() || now.Before(c.Since) {
		c.Since = now
	}
}

// held is one level's component as it was last written, or its prior dated from
// [chains.Since] when nothing has ever moved it.
func (c *chains) held(level Level, name string) node {
	if level == LevelWorld {
		if c.World.P > 0 {
			return c.World
		}
		return node{X: c.Pace, P: levelVariance(LevelWorld), At: c.Since}
	}
	if slot := c.slot(level); slot != nil {
		if found, ok := (*slot)[name]; ok {
			return found
		}
	}
	return node{P: levelVariance(level), At: c.Since}
}

// put writes one level's component back.
func (c *chains) put(level Level, name string, held node) {
	if level == LevelWorld {
		c.World = held
		return
	}
	slot := c.slot(level)
	if slot == nil {
		return
	}
	if *slot == nil {
		*slot = map[string]node{}
	}
	(*slot)[name] = held
}

// stale widens a component that has been sitting still and stops where the
// prior stands. See the file's own note: the estimate is never moved, because
// there is no reason to think a lane got faster, only a reason to be less sure.
func stale(held node, level Level, now time.Time) node {
	if held.P <= 0 || held.At.IsZero() {
		return held
	}
	if elapsed := now.Sub(held.At); elapsed > 0 {
		held.P *= math.Exp2(elapsed.Seconds() / levelHalfLife[level].Seconds())
	}
	if ceiling := SheetWeight * levelVariance(level); held.P > ceiling {
		held.P = ceiling
	}
	return held
}

// look is the belief about one subject right now: the four components, each
// aged to this moment.
func (c *chains) look(of subject, now time.Time) Chain {
	c.anchor(now)
	var chain Chain
	for level, name := range of {
		if name == "" {
			continue
		}
		found := stale(c.held(Level(level), name), Level(level), now)
		chain[level] = Component{X: found.X, P: found.P}
	}
	return chain
}

// fold shares one observation z, with observation noise R, out among the levels
// the subject names, and returns the standardized residual — how surprising the
// observation was in standard deviations of what was predicted.
func (c *chains) fold(of subject, take absorbs, z, R float64, now time.Time) float64 {
	if R <= 0 || math.IsNaN(z) || math.IsInf(z, 0) {
		return 0
	}
	c.anchor(now)
	var held [Levels]node
	predicted, total := 0.0, R
	// EVERY LEVEL THE SUBJECT NAMES IS IN BOTH SUMS, whether or not it may move.
	// The prediction is the belief and the innovation is the surprise against
	// it; a partial prediction would make the surprise the difference between a
	// whole number and half a belief. See [absorbs].
	for level, name := range of {
		if name == "" {
			continue
		}
		held[level] = stale(c.held(Level(level), name), Level(level), now)
		predicted += held[level].X
		total += held[level].P
	}
	if total <= 0 {
		return 0
	}
	surprise := z - predicted
	// carried is what the levels that may move hold between them, and it is the
	// denominator of an OFFSET: each of them takes its share of the whole
	// difference rather than its share of the belief. For an ordinary sighting
	// it is zero and the gain is the denominator, which is the Kalman update.
	carried := 0.0
	if take.Whole {
		for level, name := range of {
			if name != "" && take.Levels[level] {
				carried += held[level].P
			}
		}
	}
	for level, name := range of {
		if name == "" || !take.Levels[level] {
			continue
		}
		// HOW MUCH THIS LEVEL EXPLAINS AND HOW MUCH IT CARRIES ARE TWO
		// QUESTIONS. What it explains is what its variance shrinks by; what it
		// carries is how far its estimate moves. They are the same number for a
		// sighting and they are not for a published row. See [absorbs].
		gain := held[level].P / total
		moved := gain
		if carried > 0 {
			moved = held[level].P / carried
		}
		held[level].X += moved * surprise
		held[level].P *= 1 - gain
		held[level].At = now
		c.put(Level(level), name, held[level])
	}
	return surprise / math.Sqrt(total)
}

// note folds an observation in and then asks whether the leaf has STEPPED
// rather than drifted. It reports true when it has.
//
// ── WHY ONLY THE LEAF IS RESET ──────────────────────────────────────────────
//
// Exponential forgetting handles drift and is far too slow for a step: a
// provider adds capacity, a deployment is re-quantized, a region fails over,
// and a half-life measured in minutes still takes an hour to catch up. A
// one-sided CUSUM on the standardized residual catches it in about eight
// observations.
//
// What it may reset is e[model, lane] AND NOTHING ELSE. μ, a and b are shared
// facts, held with evidence from every other pair that touches them, and a step
// in one deployment is not evidence about the provider's whole fleet or about
// the model's size. Resetting the leaf toward its parents is exactly the claim
// the alarm supports: this deployment is no longer what it was, and what we
// know about it now is what its provider and its model say.
//
// It is the only mechanism in this package that moves an ESTIMATE rather than a
// variance, which is why it is the only one with an alarm on it.
func (c *chains) note(of subject, z, R float64, now time.Time) bool {
	residual := c.fold(of, everyLevel, z, R, now)
	leaf := of[LevelPair]
	if leaf == "" || residual == 0 {
		return false
	}
	sums := c.Drift[leaf]
	sums.Up = math.Max(0, sums.Up+residual-cusumSlack)
	sums.Down = math.Max(0, sums.Down-residual-cusumSlack)
	if sums.Up <= cusumAlarm && sums.Down <= cusumAlarm {
		if c.Drift == nil {
			c.Drift = map[string]drift{}
		}
		c.Drift[leaf] = sums
		return false
	}
	c.put(LevelPair, leaf, node{P: levelVariance(LevelPair), At: now})
	delete(c.Drift, leaf)
	return true
}

// ── QUALITY IS HIERARCHICAL TOO, AND IT IS BORROWED RATHER THAN SUMMED ──────
//
// A provider that truncates on one model usually truncates on another, and a
// model whose tool-call JSON the decoder refuses refuses it everywhere. So the
// same shape applies — but NOT the same arithmetic. A success count is not
// additive in the log domain: adding a provider's Beta to a model's Beta says
// nothing anybody can interpret. What a parent is good for is telling a pair
// nobody has judged WHICH WAY TO START.
//
// So the parent is borrowed as a PRIOR. The pair begins at its own flat prior
// moved toward what its provider and its model have actually shown, and it
// begins there with the flat prior's own MASS — a parent is evidence about
// which way to lean, never about how sure to be, and a lane that inherited its
// provider's certainty could be gated out before it had answered once.

// tally is one parent's quality evidence with the moment of its last outcome,
// so that it forgets at [QualityHalfLife] like every belief below it.
type tally struct {
	Beta
	At time.Time `json:"at,omitzero"`
}

// tallies is the quality evidence held ABOVE a pair: what a provider has shown
// across every model it serves, and what a model has shown across every
// provider serving it.
type tallies struct {
	Lane  map[string]tally `json:"lane,omitempty"`
	Model map[string]tally `json:"model,omitempty"`
}

// observe folds one outcome into both parents, forgetting first so that three
// refusals this afternoon and three from last week are not the same evidence.
func (t *tallies) observe(id ID, accepted bool, at time.Time, prior Beta) {
	if t.Lane == nil {
		t.Lane, t.Model = map[string]tally{}, map[string]tally{}
	}
	t.Lane[id.Lane] = judge(t.Lane[id.Lane], accepted, at, prior)
	t.Model[id.Model] = judge(t.Model[id.Model], accepted, at, prior)
}

// judge ages one parent to the moment and folds the outcome in.
func judge(held tally, accepted bool, at time.Time, prior Beta) tally {
	if !held.Known() {
		held.Beta = prior
	}
	if !held.At.IsZero() && !at.IsZero() {
		held.Beta = held.Beta.Toward(prior, at.Sub(held.At), QualityHalfLife)
	}
	held.Beta = held.Beta.Observe(accepted)
	if !at.IsZero() {
		held.At = at
	}
	return held
}

// start is what a pair nobody has judged begins at: its flat prior, leaned the
// way its provider and its model have been shown to lean.
func (t tallies) start(id ID, prior Beta) Beta {
	return borrow(prior, t.Lane[id.Lane].Beta, t.Model[id.Model].Beta)
}

// borrow moves a prior toward its parents' share, at the prior's own mass.
//
// A parent counts only for the evidence it holds ABOVE its own prior: a
// provider whose every lane is still sitting at Beta(8, 1) has been shown
// nothing and must not be allowed to sound like it has.
func borrow(prior Beta, parents ...Beta) Beta {
	mass := prior.A + prior.B
	if mass <= 0 {
		return prior
	}
	shown, weight := 0.0, 0.0
	for _, parent := range parents {
		if excess := parent.A + parent.B - mass; excess > 0 {
			shown += parent.Mean() * excess
			weight += excess
		}
	}
	if weight <= 0 {
		return prior
	}
	share := (prior.Mean()*mass + shown) / (mass + weight)
	return Beta{A: share * mass, B: (1 - share) * mass}
}
