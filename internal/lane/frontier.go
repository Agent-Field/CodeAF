package lane

import (
	"math"
	"sort"
	"strings"
	"time"
)

// ── THE FRONTIER IS THE CANDIDATE SET ───────────────────────────────────────
//
// With seventeen lanes on one model, most of them are beaten outright: some
// other lane starts sooner AND writes faster AND costs less AND is more often
// right. A lane like that can never be the answer to any request, whatever λ
// is, so it should never be sampled, never probed and never hedged to. Pruning
// them first is what makes the exploration budget land only where it could
// change a decision, and it usually leaves three to five.
//
// THE COMPARISON IS AT THE p75, NOT THE MEAN. A lane nobody has measured much
// has a wide posterior, and at the p75 that width keeps it in the set: it might
// be good. Pruning on the mean would quietly make this a router that only ever
// uses what it already knows.
//
// Two steps happen here and they are different kinds of judgement. THE GATE is
// deterministic and is about capability: a lane that cannot write the requested
// number of tokens, cannot read the prompt, serves four-bit weights, or returns
// answers this build could not use, is dropped outright — never sampled and
// found wanting. THE PRUNE is about dominance and nothing else: it removes lanes
// no request could want, and it removes them without knowing what this request
// wants.
//
// ── AND THREE OF THE SHEET'S CLAIMS ARE PRIORS RATHER THAN GATES ────────────
//
// THE MEASURED FAILURE (docs/design/recovery/DESIGN.md §1). On 2026-09-10 a
// tool-carrying request drew nine identical refusals from a saturated Fireworks
// while GMICloud — which had served the three previous tool calls of the very
// same task, in six seconds each — was not a candidate at all, because the
// fetched sheet marks it `Tools: false` and this file removed it from every tool
// request for good. The router's flag was simply wrong, and this build had
// already measured the same class of wrongness once (quirks.go's MiniMax memo).
//
// THE LAW: THE SHEET SEEDS A BELIEF AND NEVER ENDS AN ARGUMENT. `Tools`,
// `Uptime5m` and `Status` are the router's opinion about a machine, published
// minutes ago, about a fleet that moves; what this process has SEEN is evidence,
// and evidence outranks an opinion. So a lane the sheet doubts is DEMOTED — it
// sits behind every lane nothing is doubted about, it is never asked first while
// a better answer exists, and one request in [probeInEvery] is allowed to
// disprove the doubt ([sheetDoubtsLast]). A lane whose own answers have since
// proven it usable is not doubted at all, and that belief decays back toward the
// sheet's prior on [QualityHalfLife] like every other belief here, so nothing
// learned is learned forever.
//
// ONE HARD EXCLUSION SURVIVES and it is the wire's own negative: [Serves]. That
// is not the sheet's opinion, it is the router answering a request we actually
// made, and a machine the router will not send this model to is not a candidate
// at any rank.
//
// This file may not read a clock — see the note in choose.go. The beliefs it is
// handed have already been aged to [Request.Now] by the chooser.

const (
	// quartileZ is the standard normal deviate of the third quartile. Every
	// number this file compares is taken at it, on the pessimistic side of each
	// axis: the p75 of the first-token wait, and the p75 of the time per token,
	// which is the p25 of the RATE.
	quartileZ = 0.6744897501960817

	// UptimeFloor is the share of the last five minutes a lane must have been
	// answering to be worth choosing, in per cent. A lane below it is not slow,
	// it is intermittently absent, and the router's own fallback handles that
	// better than a preference for it would.
	UptimeFloor = 95.0

	// probeInEvery is how often a request may be sent to a lane the sheet doubts
	// ahead of the lanes it does not — the one draw that can disprove a published
	// claim, because a lane that is never asked can never prove the sheet wrong.
	//
	// IT IS THE HEDGE PURSE'S OWN RATE, WRITTEN AS A PERIOD. [DefaultBudget]
	// allows two rescues in any twenty requests, and a probe is bought for
	// exactly the same reason a rescue is: one request in ten pays a little to
	// find out something every request after it spends better. The purse itself
	// cannot be asked here — it is spent in DOLLARS and answers against a clock,
	// and this file may read neither (see the note in choose.go) — so the rate is
	// stated and the draw is the chooser's own sampler, which is seeded per
	// request and therefore reproducible.
	probeInEvery = 10

	// PriceCeilingMultiple is how far above the cheapest acceptable lane's own
	// output tariff another lane may charge, with nobody waiting.
	//
	// IT IS THE SAME 1.25 THE TRANSPORT ALREADY SENDS as `max_price`
	// (internal/provider's latencyPriceCeiling) and it is the same reasoning: a
	// lane 25% dearer is buying a latency edge somebody can feel, and a lane 4×
	// dearer is buying nothing a person notices. The difference is that this
	// ceiling is relative to the CHEAPEST LANE SERVING THIS MODEL rather than
	// to the model's published list price, because the list price is a figure
	// no endpoint is obliged to match.
	PriceCeilingMultiple = 1.25
)

// lowQuantization reports whether a lane serves weights too coarse to be
// chosen unasked. Four-bit weights are a different model wearing the same name
// — measurably worse at exactly the long-horizon work this build does — so they
// are opt-in rather than a lane that quietly wins on price.
func lowQuantization(quant string) bool {
	switch strings.ToLower(strings.TrimSpace(quant)) {
	case "fp4", "int4", "nf4", "q4", "int4_w4a16", "fp4_e2m1":
		return true
	}
	return false
}

// gateOptions are the answers the gate cannot get from the request.
type gateOptions struct {
	// allowLowQuantization lets four-bit lanes into the candidate set. It is
	// the settings row's answer and it defaults to no.
	allowLowQuantization bool
}

// capable reports whether a lane could serve this request at all.
//
// EVERY TEST HERE IS "KNOWN AND FAILING", never "unknown and assumed bad". The
// sheet does not publish every field for every lane, and a gate that read a
// zero context length as "cannot read this prompt" would refuse the whole
// candidate set the first time a router stopped publishing a column.
//
// ── AND A LANE NOBODY HAS LOOKED UP IS UNKNOWN ON EVERY FIELD ───────────────
//
// There was one exception here and it was wrong. Tools was read as a flag whose
// absence states a fact — "this lane does not take tool calls" — which is true
// of a row the sheet published and false of a lane that has only ever been
// SEEN. A lane arrives that way whenever an answer comes back from a machine
// this process has no row for: the id is real, the timing is real, and every
// other field is a zero nobody wrote. Read as a published false, that zero
// removed the lane from every request carrying tools — which on a machine whose
// only kimi beliefs were two sightings meant the whole candidate set went, the
// choice came back empty, and the request went out with no opinion on it at all.
//
// THAT FIX WAS HALF OF ONE. It spared the lane nobody had looked up and left the
// lane the sheet had spoken about WRONGLY removed for good, which is the
// GMICloud case above. Both halves are now answered the same way and in the same
// place: neither zero nor published-false is a gate here, and [sheetDoubtsLast]
// ranks what is doubted behind what is not. A gate answers "could this lane
// serve the request"; only evidence answers "and is it the one to send".
func capable(belief Belief, req Request, opts gateOptions) bool {
	// THE WIRE OVERRULES THE SHEET ABOUT WHO SERVES THIS MODEL. Every other
	// gate here reads the endpoints page; this one reads what the router
	// answered when we actually asked, and the two disagreed for a whole run
	// (issue #266, sheet.go's negative half). A lane the router has refused
	// this model from is not a slow lane or a derated one — it is not a
	// candidate at all, and leaving it in is how a pin gets chosen from a set
	// the wire has already emptied.
	if !Serves(belief.ID.Model, belief.ID.Lane) {
		return false
	}
	facts := belief.Facts
	if req.MaxTokens > 0 && facts.MaxOut > 0 && facts.MaxOut < req.MaxTokens {
		return false
	}
	if need := req.PromptTokens + req.MaxTokens; need > 0 && facts.Context > 0 && facts.Context < need {
		return false
	}
	if !opts.allowLowQuantization && lowQuantization(facts.Quant) {
		return false
	}
	// UPTIME AND THE ROUTER'S OWN STATUS WORD ARE NOT GATES HERE ANY MORE. They
	// used to be, and the argument for the second was a good one: a non-zero
	// `status` is the operator of the router saying it has derated this endpoint,
	// which is a view of every request that lane serves and one nothing here can
	// produce. What that argument omits is that it is a view published minutes
	// ago about a fleet that moves, and that we hold a better kind of evidence
	// about the same machine — whether it answered US, just now. So both are read
	// where the ranking is decided ([sheetDoubtsLast]), the lane goes behind
	// everything the sheet is happy about, and a machine that is genuinely half
	// down simply never wins the comparison it is now allowed to enter.
	//
	// QUALITY IS A GATE AND NEVER A WEIGHT. A lane whose believed share of
	// usable answers is under what this request needs leaves the candidate set
	// until the belief recovers; it is never traded off against a cheaper price,
	// because that trade is how a router learns to ship wrong answers cheaply.
	//
	// THE BOUND AND NOT THE MEAN — see [Beta.Upper], which carries the whole
	// argument and the run that found it. In one line: the question is "could
	// this lane be good enough", and a lane nobody has judged yet must answer
	// yes, or the gate refuses the entire candidate set for lack of evidence
	// and then never sends the request that would have supplied it.
	if req.QualityNeed > 0 && belief.Quality.Known() && belief.Quality.Upper(z90) < req.QualityNeed {
		return false
	}
	return true
}

// doubted reports whether the SHEET has something against this lane for this
// request, and it is the whole of what demotes one.
//
// THREE CLAIMS, each of which used to end the argument in [capable]:
//
//	tools     the request carries tool calls and the sheet does not say this
//	          lane honours one — either it published a false, or it has never
//	          spoken about the lane at all
//	uptime    the sheet published a share of the last five minutes below
//	          [UptimeFloor]; a zero is the sheet not saying and doubts nothing
//	status    the router's own health word is not its healthy zero
//
// AND EVIDENCE BEATS ALL THREE. A lane whose own answers have come back usable
// — the quality belief this process built from outcomes it saw, at the standard
// this request needs — is not doubted whatever the sheet says, because that is a
// measurement of the very thing the sheet is guessing at. It is the same belief
// [capable] gates on, read here from the other side, and it decays back toward
// the sheet's own prior on [QualityHalfLife]: a machine that stops answering
// well is doubted again within the hour, with nobody having to remember to
// forget anything.
func doubted(belief Belief, req Request) bool {
	if req.QualityNeed > 0 && belief.Quality.Known() && belief.Quality.Mean() >= req.QualityNeed {
		return false
	}
	facts := belief.Facts
	switch {
	case req.Tools && !facts.Tools:
		return true
	case facts.Uptime5m > 0 && facts.Uptime5m < UptimeFloor:
		return true
	case facts.Status != 0:
		return true
	}
	return false
}

// ── WHAT A ROLE'S OWN COLUMNS SAY ABOUT CHOOSING A MACHINE ──────────────────
//
// THERE IS NO RULE IN THIS PACKAGE ABOUT ANY NAMED ROLE AND NO LIST OF MACHINES
// ANYWHERE IN IT. Every policy below is derived from two columns the role table
// already declares ([RoleFacts.Visible], [RoleFacts.Patience] through
// [Role.Ceiling]) and from what has been MEASURED of a lane. A role added to the
// table tomorrow gets the right behaviour with nobody editing this file, which
// is the whole reason the columns exist; a rule spelled `if role == …` would be
// a second table that disagrees with the first one within a month.
//
// THREE COLUMNS, THREE QUESTIONS, AND NO TWO OF THEM ARE THE SAME QUESTION:
//
//	is anybody         [RoleFacts.Interactive]. A call nobody waits on may be
//	waiting?           sent to the machine we think is worst, to find out; a
//	                   call in front of a keypress may not, because the whole
//	                   of it is dead time. It is also what says whether being
//	                   slower than the alternative is a REFUSAL or merely a
//	                   ranking — with nobody waiting, cheap and slow is right.
//	is it read?        [RoleFacts.Visible]. Somebody watches this stream
//	                   arrive, so the cost they really pay is the TAIL — the
//	                   request that took a minute, not the median that took
//	                   three seconds, and a median cannot tell the two apart.
//	how long is        [Role.Ceiling]. The moment we have already decided to
//	the patience?      act on a silence. A machine believed to still be silent
//	                   then is not a fallback, it is the fault.
//
// THE ONE READING IS [rolePatience.expected] AND EVERY POLICY IS DERIVED FROM IT. The
// order, the veto, the refusal list and the tie-break are four uses of one
// number, computed once per candidate, so that no two of them can come to
// disagree about which machine is the fast one. The two measured cases this was
// written from are both in it and neither is special-cased:
//
//	2026-09-11 09:33  the reflex tier's whole wait doubled on a machine nothing
//	                  had ever measured, because `provider.order` is a RANKING
//	                  the router may ignore once `allow_fallbacks` is true and
//	                  only `provider.ignore` takes a machine off the table
//	                  (#850) — so a veto has to reach `ignore`, not the order.
//	2026-09-11 10:02  a task step served at two tokens a second took 219
//	                  seconds while two machines on the same model were writing
//	                  at ninety, and the person read a `cd` row as stuck for
//	                  three and a half minutes. Its first token was healthy;
//	                  only the whole wait says anything about it, which is why
//	                  nothing here compares first tokens on their own.

// patience is what one request's role declares about waiting, read once and
// passed around rather than re-derived.
type rolePatience struct {
	// ceiling is [Role.Ceiling]: the moment something is done about a silence.
	ceiling time.Duration
	// waited is [RoleFacts.Interactive]: somebody is sitting in front of this.
	waited bool
	// read is [RoleFacts.Visible]: somebody watches this stream arrive.
	read bool
}

// patienceFor reads a request's role columns.
//
// A REQUEST THAT NAMED NO ROLE HAS NO DEADLINE, AND THEREFORE REFUSES NOTHING.
// [RoleUnknown]'s row is a sensible default for how a call BEHAVES — patient,
// unwatched, exploring a little — and it is a bad answer to "what is this call
// unwilling to wait for", because nobody said. Reading its thirty seconds as a
// declared deadline would strike machines off the wire's table on behalf of
// every call site in the tree that has not yet been taught to say what it is
// for, which is a real refusal made out of a default nobody wrote down.
//
// So the ceiling is zero for an unnamed role and [beyondThePatience] refuses
// nothing, while the two behavioural columns still read from [RoleUnknown]'s
// row — those are about how this call is treated, not about what it will
// refuse. It is the same reading internal/provider's workloadFor keeps at the
// other end of the seam ([Role.Known] there too), so the two packages cannot
// come to mean different things by "unknown".
func patienceFor(req Request) rolePatience {
	facts := req.Role.Facts()
	said := rolePatience{waited: facts.Interactive, read: facts.Visible}
	if req.Role.Known() {
		said.ceiling = req.Role.Ceiling()
	}
	return said
}

// probes reports whether a request in this role may be spent settling a doubt
// about a machine — which is every role nobody is waiting on, and no other.
// See [sheetDoubtsLast].
func (p rolePatience) probes() bool { return !p.waited }

// riskZ is the standard normal deviate this role's expectation is taken at: how
// unlucky a request has to be before its wait is the one that counts.
//
// IT IS DERIVED FROM WHO PAYS, NOT CHOSEN. A role nobody watches makes many
// small calls whose SUM is what matters, and the estimator of a sum is a
// median — the typical draw, z = 0. A role somebody watches pays each draw
// separately and remembers the bad one: the person who waited a minute for a
// machine that usually takes three seconds did not experience a three-second
// machine. So its expectation is taken where an unlucky draw lands, which is
// [z90] — the same ninetieth this package already measures every lane's
// published dispersion at, so the two are one statement of risk and not two.
func (p rolePatience) riskZ() float64 {
	if p.read {
		return z90
	}
	return 0
}

// oneDraw is how far ONE answer from this lane sits from its median, in nats —
// what has been measured of it ([Belief.Spread]) and nothing at all where
// nothing has been. A lane nobody has watched vary is not accused of varying.
func oneDraw(belief Belief) float64 {
	if belief.Spread <= 0 || !finite(belief.Spread) {
		return 0
	}
	return belief.Spread
}

// expected is THE ONE READING: how long this request is expected to take to a
// usable answer on this lane, in seconds. Every policy in this package that
// compares two lanes, refuses one, or explains a choice is this number.
//
// ── THE DERIVATION, WHICH IS THE WHOLE OF IT ────────────────────────────────
//
// A lane's answer arrives in two parts and this process holds a posterior over
// each: how long until it says its first word, and how fast it writes after
// that. [PerceivedSeconds] already turns the pair into the seconds a person
// actually waits — first token, plus every hidden token at its full rate,
// plus the part of the visible text that arrives slower than anybody reads.
// Three things are then folded into that same number and nothing else is:
//
//	the role's risk      the pair is read at [rolePatience.riskZ] rather than
//	                     at the median, so the quantity is the wait this role
//	                     is actually exposed to and not the wait it averages
//	the lane's own       the quantile is taken over how far ONE ANSWER from
//	spread               this lane sits from its median ([Belief.Spread]),
//	                     measured, so a steady machine is widened by nothing
//	                     and a machine that alternates between a second and a
//	                     minute is widened by the minute
//	how often it         the caller divides by [Belief.Serving], because a lane
//	answers at all       that answers one request in five is asked five times
//	                     for one answer and the person sits through all five
//
// THE VETO IS NOT A SECOND RULE. A machine is refused exactly when this number
// is longer than the role's own declared patience ([beyondThePatience]) — "we
// are not waiting that long" — and the ranking is this number sorted. There is
// no ratio anywhere, no threshold anybody chose, and nothing that names a role
// or a machine: a role added to the table tomorrow, or a machine seen for the
// first time this minute, is handled by the same arithmetic.
//
// AND IT IS NEVER THE FIRST TOKEN ALONE, which is the half a reader is most
// likely to assume. A machine can say its first word promptly and then write at
// two tokens a second, which is a healthy first token and a four-minute answer;
// the generation term is what says so, and it is why a first-token comparison
// could not see the 2026-09-11 task step at all.
//
// ttft is in milliseconds and rate in tokens a second, as everywhere else here.
func (p rolePatience) expected(belief Belief, req Request, ttft, rate float64) float64 {
	if z := p.riskZ(); z != 0 {
		if spread := oneDraw(belief); spread > 0 {
			ttft = math.Exp(math.Log(ttft) + z*spread)
		}
	}
	return PerceivedSeconds(ttft/1000, rate, req.Visible, req.Hidden)
}

// beyondThePatience is the set of lanes this role will not ask at all: the ones
// whose [rolePatience.expected] time to a usable answer is longer than the role
// itself waits before acting on a silence — AND NEVER ALL OF THEM.
//
// IT IS ONE COMPARISON AND THERE IS DELIBERATELY NOTHING ELSE HERE. Every
// candidate's expectation was computed by one function from the same posteriors;
// the role's deadline is declared once in the role table; a machine is refused
// when the first exceeds the second. A reviewer who knows only that sentence can
// predict this package's answer for a pool it has never seen.
//
// A SET THAT IS ENTIRELY REFUSED IS A MODEL WITH NO GOOD MACHINE, and the honest
// answer there is the least bad one rather than no answer — [aboveServiceFloor]'s
// law, kept here for the same reason: an empty frontier is "no opinion", which
// sends the request out on the sort word to whichever of those same machines the
// router picks, blind. The refusal is therefore self-limiting in exactly the way
// a relative bound would have to be written to be: a machine stops being refused
// the moment it is the best there is.
//
// waits holds each candidate's expectation, in the order the candidates are
// given.
//
// A CANDIDATE WITH NO EXPECTATION IS NOT REFUSED, AND THE TEST FOR THAT IS THE
// FINITENESS OF THE NUMBER. [PerceivedSeconds] answers +∞ for a machine whose
// generation rate is not believed at all — "this lane never finishes", reported
// as such rather than as a large figure somebody might then compare — and a
// machine nothing has been measured of is exactly the shape that produces it: a
// lane that has only ever answered short, or only ever been probed, has a
// first-token belief and no rate belief, because [ledger.see] teaches the rate
// only past [ratedFloor]. Refusing THAT is refusing a machine for want of
// evidence rather than because of it, which is the very fault this whole change
// exists to end — the veto list built from what we know while the router picks
// from what exists. So it is ranked last on its infinity, as it always was, and
// stays in the set where a hedge, a walk or a rescue can still reach it. It is
// [ignoredOf]'s rule once more: we name the lanes we are SURE about, never the
// ones we have been unlucky with.
//
// A role with no declared ceiling refuses nothing, which is the honest reading
// of a call site that did not say what it was for; see [patienceFor].
func beyondThePatience(candidates []Scored, waits []float64, p rolePatience) map[ID]bool {
	if p.ceiling <= 0 {
		return nil
	}
	deadline := p.ceiling.Seconds()
	refused := map[ID]bool{}
	for index, candidate := range candidates {
		if wait := waits[index]; finite(wait) && wait > deadline {
			refused[candidate.ID] = true
		}
	}
	if len(refused) >= len(candidates) {
		return nil
	}
	return refused
}

// sheetDoubtsLast moves every lane the sheet has something against behind every
// lane it has nothing against, keeping the order otherwise — and lets one
// request in [probeInEvery] send the best doubted lane first anyway.
//
// IT IS A RANKING AND NOT A GATE, which is the whole difference between it and
// what [capable] used to do. A doubted lane stays in the candidate set: it can
// be hedged to, it can be walked to when the lanes in front of it refuse, and
// when nothing else is left it is simply the answer. What it may not do is win a
// request outright on a score, while a machine nothing is doubted about is sitting
// there able to serve it.
//
// THE PROBE IS WHY THE DOUBT CAN EVER END. A lane that is always ranked last is
// a lane that is asked only when everything else has failed, which is the worst
// possible moment to find out it was fine all along — and on a healthy model it
// is never asked at all, so a wrong flag is wrong forever. One draw in
// [probeInEvery] promotes the best-scoring doubted lane to the front, its answer
// is folded into the belief through the same outcome path every other answer
// takes (internal/provider's noteLaneOutcome), and the doubt is then settled by
// evidence: a usable tool answer lifts it through [doubted], a refusal the
// decoder could not use — `tool_json` — pushes the belief back down and confirms
// the sheet was right.
//
// AND A PROBE NEVER RIDES A CALL SOMEBODY IS WAITING ON. Asking the machine we
// think is worst, first, is one request spent to settle a doubt every request
// after it profits from — a good bet on an errand and a bad one in front of a
// keypress, where the whole of the call is dead time and the same doubt could be
// settled by the next background pass for nothing. So the promotion is off for
// every role [rolePatience.probes] refuses the draw to, which is a property of the
// role and not a list of them. The demotion stays: ranking a doubted machine last costs nobody
// anything.
//
// probe is the draw, in [0,1), and the caller supplies it because this file may
// not read a clock or a random source of its own. Anything outside the unit
// interval is nobody having drawn, and nothing is promoted.
func sheetDoubtsLast(order []Scored, req Request, known map[ID]Belief, probe float64) []Scored {
	if len(order) < 2 {
		return order
	}
	if !patienceFor(req).probes() {
		probe = -1
	}
	sure := make([]Scored, 0, len(order))
	unsure := make([]Scored, 0, len(order))
	for _, candidate := range order {
		if doubted(known[candidate.ID], req) {
			unsure = append(unsure, candidate)
			continue
		}
		sure = append(sure, candidate)
	}
	// Everything is doubted, or nothing is: either way there is nothing to rank
	// behind anything and the order stands exactly as it was scored.
	if len(sure) == 0 || len(unsure) == 0 {
		return order
	}
	if probe >= 0 && probe < 1 && probe*probeInEvery < 1 {
		return append(append(unsure[:1:1], sure...), unsure[1:]...)
	}
	return append(sure, unsure...)
}

// scoredAt is one capable lane with its p75 numbers, before any request-shaped
// scalar has been applied. Score is left at zero here: this file ranks nothing.
func scoredAt(belief Belief, req Request, cached int) Scored {
	return Scored{
		ID: belief.ID,
		// The pessimistic quartile of each: the wait is asked at its long side
		// and the rate at its slow one, which are the same three-quarter point
		// seen from the two directions a person notices.
		TTFT:    belief.TTFT.Quantile(quartileZ),
		Rate:    belief.Rate.Quantile(-quartileZ),
		Price:   PriceWithCache(belief.Facts, req, cached),
		Quality: belief.Quality.Mean(),
	}
}

// frontierFor is the candidate set for one request: the lanes that could serve
// it, minus the ones no request could want.
//
// The chooser fills an unknown answer length in [chooser.Choose] before it
// reaches this function, so every candidate is priced for the same answer.
//
// cached answers how many of this request's prompt tokens a lane is believed to
// be holding already, which is what makes the price path-dependent (see
// [PriceWithCache]). It is a function rather than a map so that the chooser's
// prefix memory stays the chooser's.
func frontierFor(beliefs []Belief, req Request, opts gateOptions, cached func(ID) int) []Scored {
	candidates := make([]Scored, 0, len(beliefs))
	facts := make([]Facts, 0, len(beliefs))
	for _, belief := range beliefs {
		if belief.ID.Zero() || !capable(belief, req, opts) {
			continue
		}
		held := 0
		if cached != nil {
			held = cached(belief.ID)
		}
		candidates = append(candidates, scoredAt(belief, req, held))
		facts = append(facts, belief.Facts)
	}
	// A stable order before anything else, so that two runs of the same choice
	// with the same beliefs agree down to the ties. The ledger promises no
	// order at all.
	order := make([]int, len(candidates))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return candidates[order[a]].ID.Lane < candidates[order[b]].ID.Lane
	})
	sorted := make([]Scored, 0, len(candidates))
	sortedFacts := make([]Facts, 0, len(candidates))
	for _, index := range order {
		sorted = append(sorted, candidates[index])
		sortedFacts = append(sortedFacts, facts[index])
	}
	sorted, sortedFacts = aboveServiceFloor(sorted, sortedFacts, beliefsByID(beliefs), req.Now)
	sorted = pricedPessimistically(sorted, sortedFacts)
	sorted = underPriceCeiling(sorted, sortedFacts, req)
	return paretoFront(sorted, req.QualityNeed)
}

// pricedPessimistically charges a lane nobody has published a tariff for what
// the dearest lane that DID publish costs.
//
// AN UNKNOWN TARIFF IS NOT A FREE ONE. Every price in [Facts] is a zero when
// the sheet said nothing, and a zero is a number the rest of this file compares
// — so a lane that has only ever been SEEN would beat every priced lane on the
// price axis outright, survive the prune on that alone, and then, with nobody
// waiting and the score in dollars, win every request forever. That is a router
// choosing the machine it knows least about because it knows least about it.
//
// The pessimistic reading is the safe one and it is the honest one: the lane may
// well be dear, and until the sheet says otherwise it is not allowed to win an
// argument it has no evidence in. It costs the lane nothing on speed, which is
// the axis it does have evidence on. A candidate set in which nobody published
// a price is left alone — there is nothing to compare it with.
func pricedPessimistically(candidates []Scored, facts []Facts) []Scored {
	dearest := 0.0
	for _, candidate := range candidates {
		if candidate.Price > dearest {
			dearest = candidate.Price
		}
	}
	if dearest <= 0 {
		return candidates
	}
	for index := range candidates {
		if facts[index].Known() || candidates[index].Price > 0 {
			continue
		}
		candidates[index].Price = dearest
	}
	return candidates
}

// underPriceCeiling drops lanes that charge more than this request can justify.
//
// THE RULE, and it has two halves because λ is what decides which applies:
//
//   - With nobody waiting (λ = 0) the ceiling is [PriceCeilingMultiple] times
//     the cheapest capable lane's own output tariff. A dearer lane is buying
//     speed that is worth nothing here, so it is not worth its money.
//   - With somebody waiting (λ > 0) the ceiling widens by what λ can justify:
//     the most a lane may cost above the cheapest is the seconds it could
//     possibly save, priced through λ — and no lane can save more than the
//     whole of the cheapest lane's perceived wait. That bound is deliberately
//     generous, because the score below already pays the difference honestly;
//     the ceiling is here to refuse the absurd, not to make the decision.
//
// THE SECOND HALF NEEDS AN ANSWER LENGTH TO DIVIDE BY, and it always has one:
// a request that states none is read at [AssumedAnswerTokens] before it reaches
// here ([chooser.Choose]). It used to be handed the zero instead, and a ceiling
// with no denominator was skipped whole — so a first turn, which is exactly the
// turn nobody has measured an answer for, was allowed any tariff at all (#686).
//
// A model whose lanes publish no output tariff at all has no ceiling, for the
// reason the transport's own ceiling has none: refusing lanes on a number
// nobody published is worse than paying an unknown price.
func underPriceCeiling(candidates []Scored, facts []Facts, req Request) []Scored {
	cheapest := 0.0
	cheapestIndex := -1
	for index, lane := range facts {
		if lane.PriceOut <= 0 {
			continue
		}
		if cheapestIndex < 0 || lane.PriceOut < cheapest {
			cheapest, cheapestIndex = lane.PriceOut, index
		}
	}
	if cheapestIndex < 0 {
		return candidates
	}
	ceiling := cheapest * PriceCeilingMultiple
	if lambda := valueOfTime(req); lambda > 0 {
		answer := req.Visible + req.Hidden
		floor := candidates[cheapestIndex]
		perceived := PerceivedSeconds(floor.TTFT/1000, floor.Rate, req.Visible, req.Hidden)
		if answer > 0 && perceived > 0 && !math.IsInf(perceived, 1) {
			if headroom := cheapest + (perceived/lambda)/float64(answer); headroom > ceiling {
				ceiling = headroom
			}
		}
	}
	kept := make([]Scored, 0, len(candidates))
	for index, candidate := range candidates {
		if facts[index].PriceOut > 0 && facts[index].PriceOut > ceiling {
			continue
		}
		kept = append(kept, candidate)
	}
	return kept
}

// paretoFront is the subset of candidates that no other candidate beats on
// every axis at once. It returns them in the order they were given.
//
// FOUR AXES, all at the p75: the first-token wait, the time per token, the
// cache-aware price of this request, and the believed share of usable answers.
// A lane is dropped only when another lane is at least as good on all four and
// strictly better on one — which is the definition of a lane that could not be
// the answer to ANY request, whatever λ turns out to be.
//
// unknownQuality is what a lane nobody has judged counts as on the quality
// axis, and the caller passes the request's own need. It is the neutral
// reading: an unmeasured lane neither wins nor loses on quality, so it is
// pruned on speed and price like everything else instead of being condemned for
// a measurement nobody took — and [Scored.Quality] keeps reporting the honest
// zero, because the emptiness law is about what a person is shown.
func paretoFront(candidates []Scored, unknownQuality float64) []Scored {
	quality := func(candidate Scored) float64 {
		if candidate.Quality <= 0 {
			return unknownQuality
		}
		return candidate.Quality
	}
	// The time per token, which is the axis a rate belongs on: a lane that
	// never finishes is worse than any finite one, and saying so here keeps the
	// comparison below free of special cases.
	perToken := func(candidate Scored) float64 {
		if candidate.Rate <= 0 {
			return math.Inf(1)
		}
		return 1 / candidate.Rate
	}
	survivors := make([]Scored, 0, len(candidates))
	for _, candidate := range candidates {
		beaten := false
		for _, rival := range candidates {
			if rival.ID == candidate.ID {
				continue
			}
			atLeastAsGood := rival.TTFT <= candidate.TTFT &&
				perToken(rival) <= perToken(candidate) &&
				rival.Price <= candidate.Price &&
				quality(rival) >= quality(candidate)
			betterSomewhere := rival.TTFT < candidate.TTFT ||
				perToken(rival) < perToken(candidate) ||
				rival.Price < candidate.Price ||
				quality(rival) > quality(candidate)
			if atLeastAsGood && betterSomewhere {
				beaten = true
				break
			}
		}
		if !beaten {
			survivors = append(survivors, candidate)
		}
	}
	return survivors
}

// ── THE SERVICE FLOOR ───────────────────────────────────────────────────────
//
// Every other bound in this file is RELATIVE — a multiple of the best lane in
// the set, a multiple of the cheapest — and a relative bound cannot say that a
// lane is too slow for a person full stop. On 2026-09-10 Morph answered every
// deepseek-v4.1-flash request it was given, twenty-seven seconds to the first
// token at six tokens a second, and stayed a candidate because it was the
// cheapest lane that answered and nothing here had an absolute opinion. These
// three do.
const (
	// FloorTTFT is the longest believed wait to a first token a lane may carry
	// and still be sent a request on its own merits. Six seconds is the
	// unattended role's patience ceiling (roles.go) with nothing left over.
	FloorTTFT = 6 * time.Second
	// FloorRate is the slowest believed generation a lane may carry. It sits
	// UNDER [ReadRate]: a lane writing faster than a person reads is fast enough
	// for prose whatever a tool loop thinks of it, and the prose objective
	// rightly prefers a 20 tok/s lane with quick first words over a 200 tok/s
	// lane that starts late (internal/provider's workload test). Fifteen is
	// Morph's six and DeepInfra's fourteen, and nothing anybody would keep.
	FloorRate = 15.0
	// FloorServing is the least a lane may be believed to answer. Half: a lane
	// refusing more than it serves costs more than two sends per answer.
	FloorServing = 0.5
	// floorEvidence is how many availability outcomes the serving clause needs
	// before it may refuse, so that one refusal does not empty a set.
	floorEvidence = 3.0
)

// underFloor reports whether a belief is SURELY below the service floor. Surely
// is [ignoreSureVariance], the same sureness a refusal needs in orderOf: a lane
// is refused on a belief the ledger is confident in, never on a wide one — and
// because [Posterior.Predict] widens a belief with every minute it goes
// unobserved, a lane put out by this floor drifts back into the candidate set
// on its own once the belief is no longer sure, which is when it deserves the
// probe that would measure it again.
func underFloor(belief Belief, now time.Time) bool {
	if belief.TTFT.Known() && belief.TTFT.P <= ignoreSureVariance && belief.TTFT.Mean() > float64(FloorTTFT.Milliseconds()) {
		return true
	}
	if belief.Rate.Known() && belief.Rate.P <= ignoreSureVariance && belief.Rate.Mean() < FloorRate {
		return true
	}
	if belief.Availability.Known() && belief.Availability.A+belief.Availability.B >= availabilityPrior.A+floorEvidence && servingAt(belief, now) < FloorServing {
		return true
	}
	return false
}

// aboveServiceFloor drops every candidate surely under the floor — AND NEVER
// ALL OF THEM. A set that is entirely under the floor is a model with no good
// lane, and the honest answer there is the least bad one, not no answer: an
// empty frontier is "no opinion", which sends the request out on the sort word
// to whichever of those same lanes the router picks, blind.
func aboveServiceFloor(candidates []Scored, facts []Facts, beliefs map[ID]Belief, now time.Time) ([]Scored, []Facts) {
	kept := make([]Scored, 0, len(candidates))
	keptFacts := make([]Facts, 0, len(candidates))
	for index, candidate := range candidates {
		if underFloor(beliefs[candidate.ID], now) {
			continue
		}
		kept = append(kept, candidate)
		keptFacts = append(keptFacts, facts[index])
	}
	if len(kept) == 0 {
		return candidates, facts
	}
	return kept, keptFacts
}

// beliefsByID is the beliefs a frontier was built from, by lane, for the
// clauses that read the posterior rather than the score.
func beliefsByID(beliefs []Belief) map[ID]Belief {
	byID := make(map[ID]Belief, len(beliefs))
	for _, belief := range beliefs {
		byID[belief.ID] = belief
	}
	return byID
}
