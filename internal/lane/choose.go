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

// prefixNote is one lane's last conversation, HOW LONG THE PROMPT WAS WHEN IT
// LAST SERVED IT, and when.
//
// THE LENGTH IS THE HALF THIS NOTE USED TO BE MISSING. Without it the only
// honest answer to "how much of this request does that lane hold" was "the
// prefix matched", and the caller turned that into a discount on the WHOLE of
// a request that had since grown. The tokens appended after the lane last
// answered were never sent to it and cannot be in its cache. So the length is
// carried, and it is the length the PROVIDER counted rather than anything this
// process estimated before the send (internal/provider's noteLane): a discount
// granted on a guessed schema size is a discount nobody's usage frame agreed
// to. Zero is "not known", and it earns nothing.
type prefixNote struct {
	prefix string
	tokens int
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

// RememberPrefix notes that a lane served a conversation AT A PROMPT LENGTH, so
// that the next choice can price the prompt cache it probably still holds
// WITHOUT pricing the part of the next prompt that lane has never seen.
//
// The transport calls it after every answer — it is the one write this package
// takes from the send path — and it takes the moment as an argument for the
// same reason everything else here does. `tokens` is the served answer's own
// reported prompt length; a caller that does not know it passes zero, and zero
// is remembered as "not known" rather than as a length.
func RememberPrefix(id ID, prefix string, tokens int, at time.Time) {
	if id.Zero() || prefix == "" {
		return
	}
	if tokens < 0 {
		tokens = 0
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
	prefixMemory.notes[id] = prefixNote{prefix: prefix, tokens: tokens, at: at}
}

// RememberedPrefix is the read half of [RememberPrefix]: which conversation a
// lane last served and at what prompt length, and whether anything is held at
// all. It is exported for the reason [ForgetPrefixes] is — the transport that
// WRITES these notes lives in another package, so the test that its answers
// arrive with the served identity and the settled length cannot reach the map
// otherwise. Nothing in the routing path calls it; [cachedTokens] reads the map
// directly.
func RememberedPrefix(id ID) (string, int, bool) {
	prefixMemory.mu.Lock()
	defer prefixMemory.mu.Unlock()
	note, seen := prefixMemory.notes[id]
	return note.prefix, note.tokens, seen
}

// ForgetPrefixes drops every note. It is for tests, which must not inherit one
// another's cache beliefs.
func ForgetPrefixes() {
	prefixMemory.mu.Lock()
	defer prefixMemory.mu.Unlock()
	prefixMemory.notes = map[ID]prefixNote{}
}

// cachedTokens is how many of this request's prompt tokens a lane is believed
// to hold: a prefix cache is a prefix, so a conversation that has grown since
// is still hit for everything up to WHERE IT GREW — and for nothing past it.
//
// THE BOUND IS THE SHORTER OF TWO KNOWN LENGTHS: what that lane's last answer
// reported it read, and what this request is sending. A grown prompt earns
// credit for the part the lane actually saw and pays full price for the rest; a
// SHRUNK one — a compaction, a rewrite — earns credit for at most what it is
// now sending, because a discount on tokens this request does not contain is
// arithmetic about a message nobody is going to send.
//
// IT REMAINS AN ESTIMATE AND NOT A RECEIPT. A matching lineage inside the hold
// says this lane answered these bytes recently, not that it still holds them:
// the lane may have evicted the prefix, and a compaction or a rewrite can
// change bytes UNDER an unchanged lineage so that a prompt of the same length
// is a different prompt. Only the next answer's `cached_tokens` settles it.
// What the bound removes is the one error that needed no cache behaviour to be
// wrong — crediting a lane for tokens that were appended after it last spoke.
//
// A length that is not known earns NOTHING. An answer whose usage frame carried
// no prompt count leaves a zero here, and a zero is unknown rather than "all of
// it": inventing a length would be exactly the fabricated discount this bound
// exists to end.
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
	if note.tokens <= 0 {
		return 0
	}
	if age := req.Now.Sub(note.at); age < 0 || age > PrefixHold {
		return 0
	}
	if note.tokens < req.PromptTokens {
		return note.tokens
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
// belief is the ledger's, the rows are the sheet's, and the moment is the
// request's.
type chooser struct {
	// ledger, pages and hier are where what is believed comes from, nil meaning
	// the registry's own. The registry builds this chooser before it can hand it
	// any of them, and a test builds one with its own, so every seam is read
	// late rather than held.
	ledger Ledger
	pages  Sheet
	hier   Hierarchy
}

// newChooser builds the chooser. It is called from the registry and nowhere
// else.
func newChooser() *chooser { return &chooser{} }

// beliefs is what is believed about a model's lanes right now.
func (c *chooser) beliefs(model string) []Belief {
	return c.ledgerOf().Beliefs(model)
}

func (c *chooser) ledgerOf() Ledger {
	if c.ledger != nil {
		return c.ledger
	}
	return Default().Ledger()
}

func (c *chooser) sheetOf() Sheet {
	if c.pages != nil {
		return c.pages
	}
	return Default().Sheet()
}

// hierarchy is the belief store's second door, nil on a build whose ledger does
// not answer it. Everything below treats that as "borrow nothing", which is the
// behaviour this package had before there was a hierarchy at all.
func (c *chooser) hierarchy() Hierarchy {
	if c.hier != nil {
		return c.hier
	}
	if borrowed, ok := c.ledgerOf().(Hierarchy); ok {
		return borrowed
	}
	return nil
}

// ── COLD START: A MODEL NOBODY HAS MEASURED STILL GETS AN ORDER ─────────────
//
// The first request to a (model, lane) pair is the STEADY STATE — a person
// picks a model at runtime, the sheet is half an hour old at best, and the
// ledger has never heard of the machines behind it — and until this the whole
// package refused it: fewer than two beliefs and the answer was the zero
// [Choice]. The measured consequence was a request with no routing opinion AND
// no clock on it, waiting three minutes.
//
// The refusal was right about ONE thing and wrong about the other. An order of
// one is genuinely not a ranking, and sending the name of the only endpoint we
// happen to know exists is a preference for our own ignorance. But "one lane in
// the ledger" was never the same fact as "one lane known": the sheet names
// every machine that serves this model, and the hierarchy knows how quick each
// PROVIDER is from every other model it has served (waiting.go's sum). Both are
// beliefs about lanes; neither was being read.
//
// So the candidate set is widened in three steps, cheapest first, and it stops
// as soon as there is something to rank:
//
//  1. what the ledger holds for this model — the measured account, untouched;
//  2. every lane the SHEET names for this model, fitted from its published
//     percentiles exactly as [Ledger.Prime] would fit them;
//  3. every lane this process has seen serving ANY model, priced from the
//     hierarchy's provider level.
//
// A candidate that comes out of all three with nothing believed about it is
// dropped rather than ranked, because that is the old refusal's true half: a
// name with no belief behind it is not a preference.
//
// AND A MODEL WITH NO SHEET ASKS FOR ONE, on the way past, without waiting:
// [Wanter.Wants] queues it for the beat. That is the one write this path makes
// and it is not a fetch — the law that nothing is fetched on a send path is
// exactly what the queue exists to keep.

// widened is the candidate set the choice is made from. It is the ledger's own
// list unchanged whenever that list can be ranked, and the three steps above
// when it cannot.
func (c *chooser) widened(req Request, known []Belief) []Belief {
	if len(known) >= 2 {
		return known
	}
	held := make(map[string]bool, len(known))
	for _, belief := range known {
		held[belief.ID.Lane] = true
	}
	rows := c.sheetOf().Rows(req.Model)
	if len(rows) == 0 {
		c.wantSheet(req.Model)
	}
	// THE LIST IS COPIED BEFORE IT GROWS. What a ledger hands out is its answer
	// and not this file's scratch space, and appending into somebody else's
	// spare capacity is the kind of aliasing nobody finds twice.
	widened := append(make([]Belief, 0, len(known)+len(rows)), known...)
	for _, row := range rows {
		if row.ID.Lane == "" || held[row.ID.Lane] {
			continue
		}
		held[row.ID.Lane] = true
		widened = append(widened, fromRow(ID{Model: req.Model, Lane: row.ID.Lane}, row))
	}
	if len(widened) < 2 {
		for _, name := range c.rosterOf() {
			if held[name] {
				continue
			}
			held[name] = true
			widened = append(widened, Belief{ID: ID{Model: req.Model, Lane: name}})
		}
	}
	return c.borrowedAll(widened, req.Now)
}

// borrowedAll fills every candidate that believes nothing of its own from the
// hierarchy, and drops the ones that still believe nothing after it.
func (c *chooser) borrowedAll(candidates []Belief, now time.Time) []Belief {
	hier := c.hierarchy()
	// A FRESH SLICE, NOT A FILTER IN PLACE. The list may still be the ledger's
	// own, and a package that quietly rewrote what a seam handed it would be a
	// bug nobody could see from the seam's side.
	kept := make([]Belief, 0, len(candidates))
	for _, belief := range candidates {
		if hier != nil {
			if !belief.TTFT.Known() {
				belief.TTFT = flatten(hier.Wait(belief.ID, now))
			}
			if !belief.Rate.Known() {
				belief.Rate = flatten(hier.Rate(belief.ID, now))
			}
		}
		if !belief.Known() {
			continue
		}
		kept = append(kept, belief)
	}
	return kept
}

// flatten is one chain read as the flat belief the gate and the score are made
// of: the sum of the four means, with the sum of the four variances. An unknown
// chain borrows nothing, which the caller reads as a candidate to drop.
func flatten(chain Chain) Posterior {
	if !chain.Known() {
		return Posterior{}
	}
	mu, variance := chain.Predict()
	return Posterior{X: mu, P: variance}
}

// fromRow is a sheet row read as a belief nobody has measured yet: the facts as
// published and the timing fitted from the percentiles, with NO MOMENT on it —
// so the Thompson draw stays narrow ([sheetObservations]) and nothing here is
// ever mistaken for a sighting.
func fromRow(id ID, row Row) Belief {
	belief := Belief{ID: id, Facts: row.Facts, Quality: qualityPrior(row.Facts.Quant)}
	if row.Known() {
		belief.TTFT = fit(row.TTFTp50, row.TTFTp90)
		belief.Rate = fit(row.Ratep50, row.Ratep90)
	}
	return belief
}

// rosterOf is every lane this process has seen serving any model, empty for a
// sheet that cannot answer.
func (c *chooser) rosterOf() []string {
	if pages, ok := c.sheetOf().(Roster); ok {
		return pages.Roster()
	}
	return nil
}

// wantSheet queues a model nobody has a sheet for. It returns before anything
// is fetched and it is safe on the send path for that reason alone.
func (c *chooser) wantSheet(model string) {
	if pages, ok := c.sheetOf().(Wanter); ok {
		pages.Wants(model)
	}
}

// Choose is the whole of the decision: age, gate, prune, sample, rank, and say
// why in one sentence.
func (c *chooser) Choose(req Request) Choice {
	// A TIER IS NOT A DEPLOYMENT. `model:high` is the same seventeen machines as
	// `model` and the ledger files them under the bare id ([BareModel]), so the
	// request is bared here — once, before anything is looked up — and every
	// lookup below, including the two that key on [Request.Model] directly,
	// asks about the model whose beliefs exist.
	req.Model = BareModel(req.Model)
	// AN ANSWER LENGTH GETS ONE READING. Fill an unknown request here, before
	// anything below is looked up, priced, or timed, so every part of the choice
	// compares the same answer.
	req = req.answered()
	// THE LEDGER IS ASKED FIRST AND THE WORLD SECOND. What this process has
	// measured about this model is the best account there is; the sheet and the
	// hierarchy are what stand in when there is not enough of it to rank, which
	// on a cold store is every model and on a picked one is most of them
	// ([chooser.widened] carries the whole argument).
	beliefs := c.widened(req, c.beliefs(req.Model))
	// AN ORDER OF ONE IS STILL NOT A RANKING. The refusal below is the old one
	// with its true half kept: one lane KNOWN is nothing to rank, and sending
	// its name as `provider.order` is a preference for the only machine we
	// happen to know exists, put in front of a router that knows a dozen. What
	// has changed is what counts as known — the widening above — so the case
	// this fires in is now a process that has seen nothing at all rather than a
	// process that has not seen THIS model.
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
	if req.Typical {
		// A LIST IS NOT A REQUEST. Typical is the display question: rank
		// on the posterior means, the way a short session already does
		// when it has nothing left to learn. Sampling here is what made
		// every /model frame name a different machine.
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

	// AND WHAT IS UNKNOWN GOES BEHIND WHAT IS KNOWN. A lane the sheet has never
	// published a tool flag for is a candidate and not a favourite: it is ranked
	// last among the survivors when the request carries tools, so the choice
	// tries the machines we know will take the call first and still has
	// somewhere to go when we know nothing at all (frontier.go's [toolsLast]).
	scored = toolsLast(scored, req, aged)

	choice := Choice{Frontier: scored}
	choice.Order = orderOf(scored, aged)
	if len(choice.Order) == 0 {
		return Choice{}
	}
	choice.Ignore = ignoredOf(scored, aged, choice.Order, lambda)
	// AND NOTHING ABOUT TIME. Where a rescue would go and when it would go
	// there are [PlanFor]'s, built for every call out of the same frontier this
	// carries — see the note on [Choice].
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

// THE HEDGE TIME USED TO BE COMPUTED HERE and it is not computed anywhere in
// this file any more. When to act on a wait is `internal/lane/control`'s one
// question, asked of every call from [PlanFor]'s plan rather than of the calls
// that happened to produce a routing opinion — see the note on [Choice]. What
// went with it: an eight-second clamp that only existed when a belief did, a
// three-hundred-millisecond overhead figure the alternative's own survival now
// carries, and a second copy of the log-normal arithmetic that
// [control.Survival.Remaining] states once.

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
	// AND A LANE NOBODY HAS TIMED SAYS NOTHING RATHER THAN "STARTS IN 0.0s".
	// The widening puts lanes into the order that are believed in only through
	// their provider, and a sentence built on a zero is the one thing this
	// surface may not print (the emptiness law).
	if start <= 0 {
		return ""
	}
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
