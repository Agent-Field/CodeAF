package lane

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// ── THE CHOICE: A GATE, A FRONTIER, AND ONE SCALAR ──────────────────────────
//
// Four things are wanted at once — cheap, quick to start, quick to finish,
// right — and a fixed weight over them is a guess about a trade the person
// never made. The design's move is to stop guessing the weight and DERIVE it
// from the request: the price of a second is a property of who is waiting and
// what their wait costs (λ, in [Request.ValueOfTime]), and the value of
// throughput saturates at reading speed ([PerceivedSeconds]). With those two
// facts the four objectives collapse to one scalar PER REQUEST, different for a
// talk turn and for a background node, and the Pareto frontier over lanes is
// only the candidate set that scalar chooses from.
//
// The order is fixed and each step has a law:
//
//  1. GATE — deterministic, never sampled. Tools, output ceiling, context,
//     quantization, uptime, and the quality posterior against QualityNeed. A
//     gate drop is never a sampled event: a lane that would drop the tool call
//     is a wrong answer rather than a slow one.
//  2. PRUNE — drop a lane another lane beats on all of first token, rate,
//     price and quality at the p75. Scored at p75 rather than the mean so that
//     an UNCERTAIN lane stays in: it might be good, and that is the only kind
//     of exploration worth paying for.
//  3. SCORE — perceived seconds plus price through λ, Thompson-sampled from
//     the posteriors, with the sampling spread scaled by the horizon.
//  4. ASK — Order is the survivors by sampled score, with fallbacks left on.
//
// THE SCORE'S UNIT FOLLOWS λ, and this is the one place the contract's own
// wording needs reading twice. With somebody waiting (λ > 0) a dollar is worth
// λ seconds by construction, the two terms are commensurable, and [Scored.Score]
// is the seconds its documentation describes. With nobody waiting (λ = 0) a
// second is worth nothing, dividing by λ is a division by zero dressed up as a
// preference, and the honest score is the MONEY — perceived time then only
// breaks its ties. Both are "lower is better" and neither is ever compared with
// the other, because one request has one λ.
//
// AN EMPTY LEDGER GETS AN EMPTY CHOICE. Nothing here invents a belief about a
// lane nobody has measured and no sheet has described: with no beliefs for the
// model the answer is the zero [Choice], the transport sends exactly what it
// sent before this package existed, and the first answer that comes back is
// what teaches this file anything at all.
//
// Nothing in this file, in frontier.go or in value.go may read a clock. The
// moment is [Request.Now] and a structural test in this directory enforces it,
// because a choice that reads the wall is a choice no test can pin.

const (
	// ExplorationHorizon is how many more calls a session must expect to make
	// before exploration is worth its full width.
	//
	// KNOWLEDGE-GRADIENT SCALING, and it is one multiply. Thompson sampling
	// explores in proportion to how uncertain it is and is blind to how many
	// decisions are left to profit from what it learns: a three-call session
	// should never explore, a five-hundred-call swarm should explore early. The
	// sampling spread is scaled by min(1, Horizon/50), so a short session
	// chooses its best guess and a long one pays to find out.
	ExplorationHorizon = 50

	// exploreTTFTMultiple bounds exploration by the clock rather than by the
	// belief. A lane may only be sampled INTO the order when its believed
	// first-token wait is within twice the best lane's: a lane at six tokens a
	// second is not explored on somebody's turn however uncertain it is, because
	// what would be learned costs more than it is worth.
	exploreTTFTMultiple = 2.0

	// ignoreTTFTMultiple and ignoreSureVariance are the two halves of a refusal,
	// and it takes both. A lane is named in `provider.ignore` only when it is
	// believed to be more than three times slower to start than the best lane
	// AND the belief is a sure one — a variance under this, which is a spread of
	// about ±65% in the log domain. THE SECOND HALF IS THE POINT: a lane we are
	// merely unlucky with keeps its place, and a refusal expires by the belief
	// widening under [Posterior.Predict] rather than on a timer, which is why
	// there is no penalty box anywhere in this package.
	ignoreTTFTMultiple = 3.0
	ignoreSureVariance = 0.25

	// hedgeOverhead is what a second request costs before it can answer: a
	// connection, a router hop, the far side reading the prompt again. It is
	// added to the alternative's expected wait so that hedging is only ever
	// asked for when it would genuinely arrive sooner.
	hedgeOverhead = 300 * time.Millisecond

	// hedgeFloor and hedgeCeiling clamp the computed hedge time. Below the floor
	// a hedge fires on the ordinary spread of a healthy lane; above the ceiling
	// nobody is being rescued by it.
	hedgeFloor   = 700 * time.Millisecond
	hedgeCeiling = 8 * time.Second

	// sheetObservations is how many requests a published percentile row stands
	// for WHEN THE QUESTION IS HOW SURE OF THE LANE'S MEDIAN IT MAKES US.
	//
	// A belief's variance is two different quantities at two different moments,
	// and this is the seam where the difference bites.
	//
	// BEFORE THIS PROCESS HAS MEASURED A LANE, P is whatever the sheet's spread
	// was — and the sheet's spread is the lane's per-request VARIABILITY, the
	// distance between its p50 and its p90 over everybody's prompts. A draw from
	// it is a draw of one imagined request, not of one opinion about a lane, and
	// reordering lanes on an imagined request is paying real money for a coin
	// toss nothing can be learned from: the sheet has already published both
	// medians. So a belief that is still purely the sheet's is drawn narrow.
	//
	// AFTER IT HAS, P is the variance of the MEDIAN — it shrank with each
	// sighting and widens again with [Posterior.Predict] as the last one ages —
	// and a draw from THAT is exactly the question Thompson sampling exists to
	// ask. So a measured belief is drawn at its own full width, which is what
	// lets a lane this process gave up on earn its way back once its spread has
	// grown (`ideation/provider-routing.md` §5: there is no penalty box). The
	// same file says the same thing from the other side — [predictive] floors
	// this variance back UP for the hedge, because there the per-request
	// question is the right one and P has stopped answering it.
	//
	// [Belief.At] is what separates the two cases: it is zero for a lane only
	// the sheet has ever spoken about. It is a blunt line — a lane with one
	// sighting is still mostly the sheet's — and it is drawn where it is because
	// it is the one fact that is certainly true on either side of it.
	//
	// Sixteen is stated rather than derived, because nothing the router
	// publishes says how many requests are behind a percentile; it is the
	// design's own discount for a public number ([SheetWeight] = 4) squared,
	// which is what turns a weight in the filter's units into a count.
	//
	// It was found by the case in `ideation/provider-routing.md`, Part III: with
	// two thousand hidden tokens and somebody waiting, Baidu is both quicker and
	// four times cheaper than Cloudflare, and the chooser sent a quarter of
	// those requests to Cloudflare anyway.
	sheetObservations = SheetWeight * SheetWeight

	// predictiveSpreadFloor is the smallest spread the hedge arithmetic will use
	// for a first-token belief, in the log domain.
	//
	// WHY THERE IS A FLOOR AT ALL. [Posterior.P] is the variance of the MEAN,
	// and after a few dozen sightings it is nearly zero — a lane whose median is
	// known to the millisecond. The hedge asks a different question: how long
	// the NEXT wait might be, which is the predictive spread, and that never
	// shrinks below the lane's own variability. One in the log domain is a p90
	// about three and a half times the median, which is the shape the sheet
	// actually published for the seventeen lanes this design was measured on.
	predictiveSpreadFloor = 1.0
)

// ── THE PREFIX MEMORY ───────────────────────────────────────────────────────
//
// Price is path-dependent because of the prompt cache: the cheapest lane on the
// sheet is not the cheapest lane for a request whose prefix another lane
// already holds. Knowing which lane holds it needs one fact the ledger does not
// carry — WHICH CONVERSATION a lane last served — so it is kept here, next to
// the only code that asks.
//
// IT IS A BELIEF AND NEVER A FACT. A lane that answered this conversation four
// minutes ago probably still has its prefix; it may have evicted it, and the
// only proof either way is the `cached_tokens` of the next answer. Believing it
// wrongly costs one comparison. The alternative — the transport's current
// affinity pin, which puts the incumbent in front unconditionally — cannot be
// argued with by a price at all, which is what this replaces.

// prefixNote is one lane's last conversation, and when it served it.
type prefixNote struct {
	prefix string
	at     time.Time
}

// prefixMemory is what each lane last served. It is small by construction: one
// entry per lane per model, and the oldest entries are dropped when a process
// somehow accumulates more lanes than any router publishes.
var prefixMemory = struct {
	mu    sync.Mutex
	notes map[ID]prefixNote
}{notes: map[ID]prefixNote{}}

// prefixMemoryLimit is how many lanes are remembered before the oldest note is
// forgotten. A router publishes a few dozen lanes per model and a session uses
// a handful of models, so this is a bound on a leak rather than a policy.
const prefixMemoryLimit = 256

// RememberPrefix notes that a lane served a conversation, so that the next
// choice can price the prompt cache it probably still holds.
//
// The transport calls it after every answer — it is the one write this package
// takes from the send path — and it takes the moment as an argument for the
// same reason everything else here does.
func RememberPrefix(id ID, prefix string, at time.Time) {
	if id.Zero() || prefix == "" {
		return
	}
	prefixMemory.mu.Lock()
	defer prefixMemory.mu.Unlock()
	if len(prefixMemory.notes) >= prefixMemoryLimit {
		oldest, found := ID{}, false
		for key, note := range prefixMemory.notes {
			if !found || note.at.Before(prefixMemory.notes[oldest].at) {
				oldest, found = key, true
			}
		}
		if found {
			delete(prefixMemory.notes, oldest)
		}
	}
	prefixMemory.notes[id] = prefixNote{prefix: prefix, at: at}
}

// ForgetPrefixes drops every note. It is for tests, which must not inherit one
// another's cache beliefs.
func ForgetPrefixes() {
	prefixMemory.mu.Lock()
	defer prefixMemory.mu.Unlock()
	prefixMemory.notes = map[ID]prefixNote{}
}

// cachedTokens is how many of this request's prompt tokens a lane is believed
// to hold, which is all of them or none: a prefix cache is a prefix, and a
// conversation that has grown since is still hit for everything up to where it
// grew. Being generous here is what makes the incumbent lane cheaper by exactly
// the discount it is about to give, and being wrong costs a comparison.
func cachedTokens(id ID, req Request) int {
	if req.Prefix == "" || req.PromptTokens <= 0 {
		return 0
	}
	prefixMemory.mu.Lock()
	note, seen := prefixMemory.notes[id]
	prefixMemory.mu.Unlock()
	if !seen || note.prefix != req.Prefix {
		return 0
	}
	if age := req.Now.Sub(note.at); age < 0 || age > PrefixHold {
		return 0
	}
	return req.PromptTokens
}

// ── THE OPTION THE REQUEST CANNOT CARRY ─────────────────────────────────────

// lowQuantAllowed is whether four-bit lanes may be chosen. It is package state
// rather than a field on [Request] because it is a standing answer about this
// machine's settings and not a fact about one call, and [Request] is the
// wave-0 contract.
var lowQuantAllowed struct {
	mu      sync.RWMutex
	allowed bool
}

// AllowLowQuantization says whether lanes serving four-bit weights may be
// chosen. It is off by default: four-bit weights are a different model wearing
// the same name, and a router that took them unasked would be trading the
// answer's quality for a price nobody agreed to.
func AllowLowQuantization(allow bool) {
	lowQuantAllowed.mu.Lock()
	defer lowQuantAllowed.mu.Unlock()
	lowQuantAllowed.allowed = allow
}

func lowQuantizationAllowed() bool {
	lowQuantAllowed.mu.RLock()
	defer lowQuantAllowed.mu.RUnlock()
	return lowQuantAllowed.allowed
}

// ── THE CHOOSER ─────────────────────────────────────────────────────────────

// chooser turns a request into a preference. It holds no state of its own: the
// belief is the ledger's and the moment is the request's.
type chooser struct {
	// ledger is where the beliefs come from, nil meaning the registry's own.
	// The registry builds this chooser before it can hand it a ledger, and a
	// test builds one with a ledger of its own, so the seam is read late rather
	// than held.
	ledger Ledger
}

// newChooser builds the chooser. It is called from the registry and nowhere
// else.
func newChooser() *chooser { return &chooser{} }

// beliefs is what is believed about a model's lanes right now.
func (c *chooser) beliefs(model string) []Belief {
	if c.ledger != nil {
		return c.ledger.Beliefs(model)
	}
	return Default().Ledger().Beliefs(model)
}

// Choose is the whole of the decision: age, gate, prune, sample, rank, and say
// why in one sentence.
func (c *chooser) Choose(req Request) Choice {
	beliefs := c.beliefs(req.Model)
	// AN ORDER OF ONE IS NOT A RANKING, and a ledger that has heard of a single
	// lane has nothing to rank. This is the same refusal as the empty one below
	// it and it is worth stating separately, because the case is not
	// hypothetical: on a machine with no sheet the only lanes this package has
	// ever heard of are the ones that have already served, so the first thing it
	// learns about a model is the name of ONE endpoint — quite possibly the slow
	// one it is about to be demoted for. Sending that name as `provider.order`
	// is not a preference among lanes; it is a preference for the only machine
	// we happen to know exists, put in front of a router that knows a dozen, and
	// it silently overrode a strike ledger that had correctly demoted it.
	if len(beliefs) < 2 {
		return Choice{}
	}
	aged := make(map[ID]Belief, len(beliefs))
	for _, belief := range beliefs {
		if belief.ID.Zero() {
			continue
		}
		if since := req.Now.Sub(belief.At); since > 0 && !belief.At.IsZero() {
			belief.TTFT = belief.TTFT.Predict(since, HalfLife)
			belief.Rate = belief.Rate.Predict(since, HalfLife)
		}
		// AND THE QUALITY BELIEF IS AGED HERE TOO, on its own moment, because a
		// lane the gate has dropped is a lane nothing else will ever ask about:
		// no request goes to it, so no sighting and no outcome arrives to age it
		// on the way in. Without this the drop would be permanent — which is the
		// absorbing gate [Beta.Upper] and [Beta.Toward] were written to end.
		if since := req.Now.Sub(belief.QualityAt); since > 0 && !belief.QualityAt.IsZero() {
			belief.Quality = belief.Quality.Toward(qualityPrior(belief.Facts.Quant), since, QualityHalfLife)
		}
		aged[belief.ID] = belief
	}
	list := make([]Belief, 0, len(aged))
	for _, belief := range aged {
		list = append(list, belief)
	}
	survivors := frontierFor(list, req, gateOptions{allowLowQuantization: lowQuantizationAllowed()}, func(id ID) int {
		return cachedTokens(id, req)
	})
	if len(survivors) == 0 {
		return Choice{}
	}

	lambda := valueOfTime(req)
	draws := rand.New(rand.NewSource(seedFor(req)))
	// The exploration width: full for a session with fifty more calls in it,
	// narrowing to nothing for a session that is nearly over.
	width := float64(req.Horizon) / float64(ExplorationHorizon)
	if width > 1 || req.Horizon <= 0 {
		width = 1
	}
	if width < 0 {
		width = 0
	}
	scored := make([]Scored, 0, len(survivors))
	perceived := make(map[ID]float64, len(survivors))
	for _, candidate := range survivors {
		belief := aged[candidate.ID]
		// A belief this process has measured is drawn at its own width; one that
		// is still only the sheet's is drawn narrow. See [sheetObservations].
		measured := !belief.At.IsZero()
		ttft := sample(belief.TTFT, draws, width, measured)
		rate := sample(belief.Rate, draws, width, measured)
		if ttft <= 0 {
			ttft = candidate.TTFT
		}
		if rate <= 0 {
			rate = candidate.Rate
		}
		felt := PerceivedSeconds(ttft/1000, rate, req.Visible, req.Hidden)
		perceived[candidate.ID] = felt
		candidate.Score = scoreOf(candidate.Price, felt, lambda)
		scored = append(scored, candidate)
	}
	sort.SliceStable(scored, func(a, b int) bool {
		if scored[a].Score != scored[b].Score {
			return scored[a].Score < scored[b].Score
		}
		// With λ at zero two equally cheap lanes are separated by the wait, and
		// with λ above it two equal scores are separated by the same thing. It
		// is the only tiebreak either case wants.
		if perceived[scored[a].ID] != perceived[scored[b].ID] {
			return perceived[scored[a].ID] < perceived[scored[b].ID]
		}
		return scored[a].ID.Lane < scored[b].ID.Lane
	})

	choice := Choice{Frontier: scored}
	choice.Order = orderOf(scored, aged)
	if len(choice.Order) == 0 {
		return Choice{}
	}
	if len(choice.Order) > 1 {
		choice.Alt = choice.Order[1]
	}
	choice.Ignore = ignoredOf(scored, aged, choice.Order, lambda)
	choice.Deadline = hedgeTime(
		aged[ID{Model: req.Model, Lane: choice.Order[0]}],
		aged[ID{Model: req.Model, Lane: choice.Alt}],
	)
	choice.Why = whyOf(scored[0], perceived[scored[0].ID])
	return choice
}

// orderNames is how many lanes the router is told to try in order. Three is the
// design's figure: past the third the fallbacks are doing the choosing anyway,
// and a longer list is a claim about lanes this process has barely seen.
const orderNames = 3

// orderOf is the lanes to ask for, best first.
//
// THE EXPLORATION BOUND LIVES HERE. A lane may enter the order only when its
// believed first-token wait is within [exploreTTFTMultiple] of the best lane's:
// a sampled draw is allowed to reorder lanes that are in the same league and is
// not allowed to put a lane nobody would wait for in front of somebody's turn.
// The top-scoring lane is always kept, because a bound that could empty the
// order would turn an opinion into a silence.
func orderOf(scored []Scored, aged map[ID]Belief) []string {
	best := 0.0
	for _, candidate := range scored {
		mean := aged[candidate.ID].TTFT.Mean()
		if mean <= 0 {
			continue
		}
		if best == 0 || mean < best {
			best = mean
		}
	}
	order := make([]string, 0, orderNames)
	for _, candidate := range scored {
		if len(order) == orderNames {
			break
		}
		mean := aged[candidate.ID].TTFT.Mean()
		if len(order) > 0 && best > 0 && mean > best*exploreTTFTMultiple {
			continue
		}
		order = append(order, candidate.ID.Lane)
	}
	return order
}

// ignoredOf names the lanes this process is SURE about rather than the ones it
// has been unlucky with. See [ignoreTTFTMultiple] for the rule and why it takes
// two halves; a lane in the order is never refused, whatever the arithmetic
// says about it.
//
// AND NOTHING IS REFUSED FOR SLOWNESS WHEN NOBODY IS WAITING. With λ at zero a
// lane that starts three times slower and costs half as much is the RIGHT
// answer, and putting it in `provider.ignore` would be this process refusing a
// lane on an objective the request does not have.
func ignoredOf(scored []Scored, aged map[ID]Belief, order []string, lambda float64) []string {
	if lambda <= 0 {
		return nil
	}
	best := 0.0
	for _, candidate := range scored {
		mean := aged[candidate.ID].TTFT.Mean()
		if mean <= 0 {
			continue
		}
		if best == 0 || mean < best {
			best = mean
		}
	}
	if best == 0 {
		return nil
	}
	inOrder := map[string]bool{}
	for _, lane := range order {
		inOrder[lane] = true
	}
	var ignore []string
	for _, candidate := range scored {
		belief := aged[candidate.ID]
		if inOrder[candidate.ID.Lane] || !belief.TTFT.Known() {
			continue
		}
		if belief.TTFT.Mean() > best*ignoreTTFTMultiple && belief.TTFT.P <= ignoreSureVariance {
			ignore = append(ignore, candidate.ID.Lane)
		}
	}
	return ignore
}

// sample is one Thompson draw of a lane's MEDIAN, in its natural unit, with the
// spread scaled by the horizon. A belief that knows nothing draws nothing —
// zero — and the caller falls back to the frontier's own p75 figure rather than
// inventing a number here.
//
// THE DRAW IS OF THE LANE AND NEVER OF ONE OF ITS REQUESTS; see
// [sheetObservations] for why those are different, for what `measured` selects
// between, and for the run that found the difference. The tail is priced
// elsewhere and on purpose: the frontier prunes at the p75 ([quartileZ]) and
// the hedge deadline is computed from the full predictive spread
// ([predictive]), so nothing is lost here by asking a narrower question.
func sample(posterior Posterior, draws *rand.Rand, width float64, measured bool) float64 {
	if !posterior.Known() {
		return 0
	}
	variance := posterior.P
	if !measured {
		variance /= sheetObservations
	}
	spread := math.Sqrt(variance) * width
	return math.Exp(posterior.X + spread*draws.NormFloat64())
}

// seedFor is the request's own seed: the moment it was made, mixed with the
// model it is for. THE POINT IS REPRODUCIBILITY — a test that pins a Tuesday in
// August gets the same draws every run — and the mixing is what stops two
// models chosen in the same nanosecond exploring in lockstep.
func seedFor(req Request) int64 {
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(req.Model))
	return req.Now.UnixNano() ^ int64(digest.Sum64())
}

// ── THE HEDGE TIME ──────────────────────────────────────────────────────────

// hedgeTime is when a request should start thinking about asking somebody else.
//
// FOR A LOG-NORMAL, THE LONGER YOU HAVE WAITED, THE LONGER YOU SHOULD EXPECT TO
// GO ON WAITING. A stream four seconds late is not four seconds from finishing;
// it is a draw from the tail. So the hedge time is the smallest wait t at which
// the expected REMAINING wait exceeds what the alternative would take from
// cold, plus what a second request costs to start:
//
//	P(T > t)          = 1 − Φ((ln t − μ) / s)
//	E[T·1{T>t}]       = exp(μ + s²/2) · Φ((μ + s² − ln t) / s)
//	E[T − t | T > t]  = E[T·1{T>t}] / P(T > t) − t
//	t*                = min t where E[T − t | T > t] > E_alt[T] + hedgeOverhead
//
// The remaining wait grows with t for a log-normal, so t* is found by halving
// the interval rather than by walking it. It is clamped to [hedgeFloor,
// hedgeCeiling] and it is per lane and per belief: a lane whose normal is four
// hundred milliseconds hedges at about a second, and a lane whose normal is two
// seconds does not hedge there at all.
//
// WITH NO ALTERNATIVE THERE IS NOTHING TO HEDGE TO, and the deadline is then
// only the p90 of the lane's own belief — a wait that surprising is worth
// telling the watch about even when the answer is to keep waiting.
func hedgeTime(top, alt Belief) time.Duration {
	mu, spread, ok := predictive(top.TTFT)
	if !ok {
		return 0
	}
	if !alt.TTFT.Known() {
		return clampHedge(time.Duration(math.Exp(mu+1.2816*spread) * float64(time.Second)))
	}
	// WHAT THE ALTERNATIVE WOULD TAKE IS AN EXPECTATION OVER ITS BELIEF, and it
	// is taken with that belief's own spread rather than with the floored one
	// above. The floor is a statement about how variable a single wait is, which
	// is the question the tail asks of the lane already running; applying it here
	// too would inflate every alternative by two thirds and make every hedge
	// systematically later than the arithmetic it is derived from.
	altMu := alt.TTFT.X - math.Log(1000)
	threshold := math.Exp(altMu+alt.TTFT.P/2) + hedgeOverhead.Seconds()
	low, high := 0.01, 60.0
	if remaining(mu, spread, high) <= threshold {
		return clampHedge(hedgeCeiling)
	}
	if remaining(mu, spread, low) > threshold {
		return clampHedge(time.Duration(low * float64(time.Second)))
	}
	for range 40 {
		mid := (low + high) / 2
		if remaining(mu, spread, mid) > threshold {
			high = mid
			continue
		}
		low = mid
	}
	return clampHedge(time.Duration(high * float64(time.Second)))
}

// predictive is a first-token belief as a log-normal over SECONDS, with the
// spread floored at what a lane's own variability never goes below
// ([predictiveSpreadFloor]). It reports false for a belief that knows nothing,
// which is a request that gets no hedge rather than one that gets a guessed
// deadline.
func predictive(posterior Posterior) (mu, spread float64, ok bool) {
	if !posterior.Known() {
		return 0, 0, false
	}
	spread = math.Sqrt(posterior.P)
	if spread < predictiveSpreadFloor {
		spread = predictiveSpreadFloor
	}
	// The belief is about milliseconds and the arithmetic above is in seconds.
	return posterior.X - math.Log(1000), spread, true
}

// remaining is E[T − t | T > t] in seconds for a log-normal (mu, spread).
func remaining(mu, spread, t float64) float64 {
	if t <= 0 {
		return math.Exp(mu+spread*spread/2) - t
	}
	survival := 1 - phi((math.Log(t)-mu)/spread)
	if survival < 1e-12 {
		// So far into the tail that the ratio below is two vanishing numbers
		// divided by each other. Anything still running there should have been
		// hedged long ago, and saying so is more honest than a quotient of
		// rounding errors.
		return math.Inf(1)
	}
	weighted := math.Exp(mu+spread*spread/2) * phi((mu+spread*spread-math.Log(t))/spread)
	return weighted/survival - t
}

// phi is the standard normal distribution function, from the error function the
// standard library already has.
func phi(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }

// clampHedge holds a deadline inside the band a hedge is worth having in.
func clampHedge(deadline time.Duration) time.Duration {
	if deadline < hedgeFloor {
		return hedgeFloor
	}
	if deadline > hedgeCeiling {
		return hedgeCeiling
	}
	return deadline
}

// ── SAYING WHY ──────────────────────────────────────────────────────────────

// whyOf is one plain sentence about the lane that won, naming the two numbers
// that won it: how soon the answer starts, and what it costs — or, when nobody
// published a tariff, how fast it writes. It is what the picker shows under the
// cursor, so it carries no machinery vocabulary and no vendor's name beyond the
// lane's own, which arrived from the wire a moment ago.
func whyOf(top Scored, felt float64) string {
	if top.ID.Lane == "" {
		return ""
	}
	start := top.TTFT / 1000
	switch {
	case top.Price >= 0.01:
		return fmt.Sprintf("%s starts in %.1fs and costs about $%.2f for this answer.", top.ID.Lane, start, top.Price)
	case top.Price >= 0.00005:
		return fmt.Sprintf("%s starts in %.1fs and costs about $%.4f for this answer.", top.ID.Lane, start, top.Price)
	case top.Rate > 0 && !math.IsInf(felt, 1):
		return fmt.Sprintf("%s starts in %.1fs and writes %.0f tokens a second.", top.ID.Lane, start, top.Rate)
	default:
		return fmt.Sprintf("%s starts in %.1fs, which is the shortest wait believed of any lane here.", top.ID.Lane, start)
	}
}
