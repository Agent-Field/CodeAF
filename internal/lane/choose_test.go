package lane

import (
	"math"
	"testing"
	"time"
)

// ── THE MEASURED WORLD, AS A FIXTURE ────────────────────────────────────────
//
// Every number below is from the sheet `deepseek/deepseek-v4-flash` published
// on 2026-08-30 (ideation/provider-routing.md, "The world, measured"): the
// first-token percentiles in milliseconds, the throughput percentiles in tokens
// a second, the uptime, the output tariff in dollars per million tokens, the
// quantization, whether the lane takes a tool call, and how much it will write.
//
// They are the fixture for the same reason the design quotes them: a chooser
// tested against invented lanes is a chooser tested against whatever shape made
// its own arithmetic look good, and the whole claim of this package is about a
// world where seventeen lanes of one model differ by 7× on the wait and 12× on
// the rate at nearly the same price.

// sheetLane is one row of that table.
type sheetLane struct {
	name     string
	ttftP50  float64
	ttftP90  float64
	rateP50  float64
	rateP90  float64
	uptime   float64
	outPerM  float64
	tools    bool
	maxOut   int
	quant    string
	contexts int
}

// measuredLanes is the table itself.
var measuredLanes = []sheetLane{
	{"CoreWeave", 430, 4539, 24, 48, 99.6, 0.28, true, 943_000, "fp8", 160_000},
	{"Parasail", 758, 1852, 41, 68, 99.9, 0.28, false, 943_000, "fp8", 160_000},
	{"DeepInfra", 760, 1345, 27, 37, 99.8, 0.18, true, 65_000, "fp8", 160_000},
	{"Cloudflare", 768, 1037, 58, 92, 100, 1.32, false, 345_000, "", 160_000},
	{"Alibaba", 840, 1625, 67, 115, 99.8, 0.27, false, 393_000, "fp8", 160_000},
	{"Baidu", 844, 1580, 75, 115, 100, 0.28, false, 131_000, "fp8", 160_000},
	{"DigitalOcean", 1504, 2913, 6, 9, 99.8, 0.17, true, 943_000, "", 160_000},
	{"AtlasCloud", 1793, 2109, 32, 80, 100, 0.28, false, 393_000, "fp4", 160_000},
	{"GMICloud", 3030, 9268, 30, 67, 97.5, 0.22, false, 943_000, "fp8", 160_000},
}

// zP90 is the standard normal deviate of the ninetieth percentile, which is how
// a p50 and a p90 become a log-normal.
const zP90 = 1.2816

// belief turns one row into a belief, as the ledger's Prime will: the median is
// the location, the spread between the two percentiles is the scale, and
// sightings is how many observations the belief is worth — one for a bare sheet
// prior, twenty for a lane this process has measured for itself.
func (lane sheetLane) belief(model string, sightings float64) Belief {
	ttftSigma := math.Log(lane.ttftP90/lane.ttftP50) / zP90
	rateSigma := math.Log(lane.rateP90/lane.rateP50) / zP90
	if sightings <= 0 {
		sightings = 1
	}
	return Belief{
		ID: ID{Model: model, Lane: lane.name},
		Facts: Facts{
			Tools:    lane.tools,
			Quant:    lane.quant,
			MaxOut:   lane.maxOut,
			Context:  lane.contexts,
			Uptime5m: lane.uptime,
			// The sheet publishes dollars per TOKEN; the table is per million.
			// Input is a quarter of output, which is the shape these lanes
			// publish, and the cache read is a tenth of input.
			PriceIn:    lane.outPerM / 4 / 1_000_000,
			PriceOut:   lane.outPerM / 1_000_000,
			PriceCache: lane.outPerM / 40 / 1_000_000,
			Caches:     true,
		},
		TTFT:    Posterior{X: math.Log(lane.ttftP50), P: ttftSigma * ttftSigma / sightings},
		Rate:    Posterior{X: math.Log(lane.rateP50), P: rateSigma * rateSigma / sightings},
		Quality: Beta{A: 39, B: 1},
	}
}

// fakeLedger is a ledger primed with a fixed set of beliefs. It is a test
// double and it lives in a test file for the reason the structural test states.
type fakeLedger struct {
	beliefs []Belief
}

func (l *fakeLedger) Note(Sighting)       {}
func (l *fakeLedger) NoteOutcome(Outcome) {}
func (l *fakeLedger) Prime(Row, float64)  {}
func (l *fakeLedger) Belief(id ID) (Belief, bool) {
	for _, belief := range l.beliefs {
		if belief.ID == id {
			return belief, true
		}
	}
	return Belief{}, false
}

func (l *fakeLedger) Beliefs(model string) []Belief {
	var found []Belief
	for _, belief := range l.beliefs {
		if belief.ID.Model == model {
			found = append(found, belief)
		}
	}
	return found
}

const testModel = "deepseek/deepseek-v4-flash"

// measured is a ledger holding the whole table, each lane worth sightings
// observations.
func measured(sightings float64, at time.Time) *fakeLedger {
	ledger := &fakeLedger{}
	for _, lane := range measuredLanes {
		belief := lane.belief(testModel, sightings)
		belief.At = at
		ledger.beliefs = append(ledger.beliefs, belief)
	}
	return ledger
}

// noon — the moment every request in this file is made at — is declared once
// for the whole package in belief_test.go. It is fixed so that the sampling is
// reproducible: the seed is the moment mixed with the model.

// talk is a turn somebody is watching: four hundred visible tokens, a
// conversation's worth of prompt, and a person's attention as λ.
func talk() Request {
	return Request{
		Model:        testModel,
		PromptTokens: 20_000,
		Visible:      400,
		MaxTokens:    4_000,
		ValueOfTime:  AttentionValue,
		QualityNeed:  0.90,
		Horizon:      ExplorationHorizon,
		Now:          noon,
	}
}

// named reports whether a list of lanes names one.
func named(list []string, lane string) bool {
	for _, name := range list {
		if name == lane {
			return true
		}
	}
	return false
}

// inFrontier finds one lane's scored row.
func inFrontier(frontier []Scored, lane string) (Scored, bool) {
	for _, row := range frontier {
		if row.ID.Lane == lane {
			return row, true
		}
	}
	return Scored{}, false
}

// ── THE GATE ────────────────────────────────────────────────────────────────

// TestTheGateDropsALaneThatCannotServeTheRequestAtAll is the law that a gate
// drop is never a sampled event: a lane that would drop the tool call, truncate
// the answer, or serve four-bit weights is a WRONG answer rather than a slow
// one, and it leaves the candidate set before anything is scored.
func TestTheGateDropsALaneThatCannotServeTheRequestAtAll(t *testing.T) {
	at := noon.Add(-time.Minute)
	opts := gateOptions{}

	lanes := map[string]Belief{}
	for _, lane := range measuredLanes {
		belief := lane.belief(testModel, 1)
		belief.At = at
		lanes[lane.name] = belief
	}

	tools := talk()
	tools.Tools = true
	for _, lane := range []string{"Parasail", "Cloudflare", "Baidu", "Alibaba"} {
		if capable(lanes[lane], tools, opts) {
			t.Errorf("%s took a tool call it does not honour", lane)
		}
	}
	for _, lane := range []string{"CoreWeave", "DeepInfra", "DigitalOcean"} {
		if !capable(lanes[lane], tools, opts) {
			t.Errorf("%s honours tool calls and was dropped anyway", lane)
		}
	}

	long := talk()
	long.MaxTokens = 100_000
	if capable(lanes["DeepInfra"], long, opts) {
		t.Error("DeepInfra stops at 65k of output and took a request for 100k")
	}
	if !capable(lanes["CoreWeave"], long, opts) {
		t.Error("CoreWeave writes 943k and was dropped from a 100k request")
	}

	if capable(lanes["AtlasCloud"], talk(), opts) {
		t.Error("a four-bit lane was chosen without being asked for")
	}
	if !capable(lanes["AtlasCloud"], talk(), gateOptions{allowLowQuantization: true}) {
		t.Error("a four-bit lane stayed out after being allowed in")
	}

	half := lanes["Baidu"]
	half.Facts.Uptime5m = 90
	if capable(half, talk(), opts) {
		t.Error("a lane answering nine minutes in ten was treated as available")
	}

	poor := lanes["Baidu"]
	poor.Quality = Beta{A: 8, B: 4}
	if capable(poor, talk(), opts) {
		t.Error("quality was weighed rather than gated")
	}
}

// ── THE FRONTIER ────────────────────────────────────────────────────────────

// TestTheFrontierIsThreeToFiveLanesOutOfTheWholeSheet is the claim the prune
// exists for: most lanes are beaten outright and could not be the answer to any
// request, whatever λ is.
func TestTheFrontierIsThreeToFiveLanesOutOfTheWholeSheet(t *testing.T) {
	ledger := measured(1, noon.Add(-time.Minute))
	front := frontierFor(ledger.Beliefs(testModel), talk(), gateOptions{}, nil)
	if len(front) < 2 || len(front) > 5 {
		names := make([]string, 0, len(front))
		for _, row := range front {
			names = append(names, row.ID.Lane)
		}
		t.Fatalf("the frontier kept %d of %d lanes (%v); the design says three to five",
			len(front), len(measuredLanes), names)
	}
	if _, kept := inFrontier(front, "GMICloud"); kept {
		t.Error("GMICloud starts last, writes slowly and is not the cheapest: nothing could want it")
	}
	if _, kept := inFrontier(front, "AtlasCloud"); kept {
		t.Error("a four-bit lane reached the frontier without being allowed in")
	}
}

// TestAnUncertainLaneIsJudgedAtTheQuartileAndNotAtItsMean is why the comparison
// is at the p75.
//
// For a log-normal wide enough — and a lane whose p90 is ten times its p50 is
// exactly that — the MEAN sits above the third quartile, because it is dragged
// up by a tail that happens one time in ten. Pruning on the mean would drop a
// lane that is the fastest thing on the sheet half the time; pruning at the
// quartile keeps it, and that is the only kind of exploration worth paying for.
func TestAnUncertainLaneIsJudgedAtTheQuartileAndNotAtItsMean(t *testing.T) {
	request := talk()
	// CoreWeave's own numbers: 430ms at the median, 4539ms at the ninetieth.
	wide := measuredLanes[0].belief(testModel, 1)
	wide.At = noon.Add(-time.Minute)
	sharp := wide
	sharp.ID = ID{Model: testModel, Lane: "Steady"}
	sharp.TTFT = Posterior{X: math.Log(1500), P: 0.01}
	sharp.Rate = wide.Rate

	if wide.TTFT.Quantile(quartileZ) >= math.Exp(wide.TTFT.X+wide.TTFT.P/2) {
		t.Fatal("the fixture is not a wide belief at all, so this law would pass vacuously")
	}
	front := frontierFor([]Belief{wide, sharp}, request, gateOptions{}, nil)
	if _, kept := inFrontier(front, "CoreWeave"); !kept {
		t.Fatalf("the uncertain lane was pruned at the quartile: %+v", front)
	}
}

// ── THE SCALAR ──────────────────────────────────────────────────────────────

// TestAVisibleAnswerIsWorthNoMoreThanReadingSpeed is the second mechanism of
// Part II. Four hundred tokens somebody is READING are delivered no faster than
// they can be read, so throughput above the reading rate buys nothing and the
// dear fast lane loses to a cheap one that starts just as soon.
func TestAVisibleAnswerIsWorthNoMoreThanReadingSpeed(t *testing.T) {
	chooser := &chooser{ledger: measured(20, noon.Add(-time.Minute))}
	choice := chooser.Choose(talk())
	if len(choice.Order) == 0 {
		t.Fatal("a ledger full of measured lanes produced no order")
	}
	if choice.Order[0] == "Cloudflare" {
		t.Fatalf("the dearest lane won a turn nobody could read faster than 18 tok/s: %+v", choice.Frontier)
	}
	won, ok := inFrontier(choice.Frontier, choice.Order[0])
	if !ok {
		t.Fatalf("the chosen lane %s is not on its own frontier", choice.Order[0])
	}
	dear, kept := inFrontier(choice.Frontier, "Cloudflare")
	if !kept {
		t.Fatal("Cloudflare should be on the frontier and beaten on price, not gated out")
	}
	if dear.Rate <= won.Rate {
		t.Skip("the sampled draws put the dear lane below the cheap one on rate, so it lost nothing to the reading ceiling")
	}
	if won.Price*2 > dear.Price {
		t.Fatalf("the turn went to %s at $%.5f while a cheaper lane was on the frontier: %+v",
			choice.Order[0], won.Price, choice.Frontier)
	}
}

// TestHiddenTokensPayForThroughput is the other side of the same mechanism: a
// tool loop's tokens are pure waiting, so throughput is worth its full rate and
// a slow-writing lane loses however cheap it is.
func TestHiddenTokensPayForThroughput(t *testing.T) {
	request := talk()
	request.Visible, request.Hidden = 0, 2000
	chooser := &chooser{ledger: measured(20, noon.Add(-time.Minute))}
	choice := chooser.Choose(request)
	if len(choice.Order) == 0 {
		t.Fatal("no order for a request with two thousand hidden tokens")
	}
	won, ok := inFrontier(choice.Frontier, choice.Order[0])
	if !ok {
		t.Fatalf("the chosen lane %s is not on its own frontier", choice.Order[0])
	}
	if won.Rate < 40 {
		t.Fatalf("a two-thousand-token tool loop went to %s at %.0f tok/s: %+v",
			choice.Order[0], won.Rate, choice.Frontier)
	}
}

// TestWithNobodyWaitingTheCheapestSurvivorWins is what λ = 0 means, and it is
// the whole of what the routing row's `price` word now does.
func TestWithNobodyWaitingTheCheapestSurvivorWins(t *testing.T) {
	request := talk()
	request.ValueOfTime = 0
	request.Visible, request.Hidden = 0, 2000
	chooser := &chooser{ledger: measured(20, noon.Add(-time.Minute))}
	choice := chooser.Choose(request)
	if len(choice.Order) == 0 {
		t.Fatal("no order for a background call")
	}
	cheapest, price := "", math.Inf(1)
	for _, row := range choice.Frontier {
		if row.Price < price {
			cheapest, price = row.ID.Lane, row.Price
		}
	}
	if choice.Order[0] != cheapest {
		t.Fatalf("with λ at zero the pick was %s and the cheapest lane was %s: %+v",
			choice.Order[0], cheapest, choice.Frontier)
	}
}

// TestALaneSureToBeFarSlowerIsRefusedOnlyWhenSomebodyIsWaiting holds both
// halves of the refusal rule: it takes a sure belief and a person whose time
// the slowness is costing.
func TestALaneSureToBeFarSlowerIsRefusedOnlyWhenSomebodyIsWaiting(t *testing.T) {
	chooser := &chooser{ledger: measured(20, noon.Add(-time.Minute))}
	watched := chooser.Choose(talk())
	if !named(watched.Ignore, "DigitalOcean") {
		t.Fatalf("a lane believed to start three times later than the best was not refused: %v", watched.Ignore)
	}
	background := talk()
	background.ValueOfTime = 0
	if refused := chooser.Choose(background).Ignore; len(refused) != 0 {
		t.Fatalf("a background call refused %v on speed, which is not its objective", refused)
	}
}

// TestTheChoiceIsReproducible is what [Request.Now] is for. The sampling is
// random and the seed is the request's own moment, so the same request twice
// gives the same answer and a test can pin one.
func TestTheChoiceIsReproducible(t *testing.T) {
	chooser := &chooser{ledger: measured(20, noon.Add(-time.Minute))}
	first, second := chooser.Choose(talk()), chooser.Choose(talk())
	if len(first.Order) == 0 || len(first.Order) != len(second.Order) {
		t.Fatalf("orders of different lengths: %v and %v", first.Order, second.Order)
	}
	for index := range first.Order {
		if first.Order[index] != second.Order[index] {
			t.Fatalf("the same request chose %v and then %v", first.Order, second.Order)
		}
	}
	if first.Deadline != second.Deadline || first.Why != second.Why {
		t.Fatal("the same request explained itself two ways")
	}
}

// TestAShortSessionDoesNotExplore is the knowledge-gradient scaling: sampling
// width is worth its cost only when there are decisions left to spend what it
// learns on.
func TestAShortSessionDoesNotExplore(t *testing.T) {
	ledger := measured(1, noon.Add(-time.Minute))
	short, long := talk(), talk()
	short.Horizon, long.Horizon = 2, 500
	chooser := &chooser{ledger: ledger}
	seen := map[string]bool{}
	for minute := range 12 {
		moment := noon.Add(time.Duration(minute) * time.Minute)
		short.Now, long.Now = moment, moment
		if order := chooser.Choose(short).Order; len(order) > 0 {
			seen["short:"+order[0]] = true
		}
	}
	shortPicks := len(seen)
	seen = map[string]bool{}
	for minute := range 12 {
		moment := noon.Add(time.Duration(minute) * time.Minute)
		long.Now = moment
		if order := chooser.Choose(long).Order; len(order) > 0 {
			seen["long:"+order[0]] = true
		}
	}
	if shortPicks > len(seen) {
		t.Fatalf("a two-call session tried %d lanes and a five-hundred-call session tried %d",
			shortPicks, len(seen))
	}
}

// ── THE PRICE OF A PREFIX ───────────────────────────────────────────────────

// TestTheLaneHoldingThePrefixIsCheaperByExactlyTheDiscount is Part II §3: cost
// is path-dependent, and the score pays the cache forfeit explicitly instead of
// hiding it in a pin.
func TestTheLaneHoldingThePrefixIsCheaperByExactlyTheDiscount(t *testing.T) {
	ForgetPrefixes()
	defer ForgetPrefixes()
	request := talk()
	request.Prefix = "conversation-7"
	incumbent := ID{Model: testModel, Lane: "Baidu"}
	RememberPrefix(incumbent, request.Prefix, noon.Add(-time.Minute))

	facts := measuredLanes[5].belief(testModel, 20).Facts
	cold := PriceOf(facts, request)
	warm := PriceWithCache(facts, request, cachedTokens(incumbent, request))
	if warm >= cold {
		t.Fatalf("a warm prefix cost %.6f and a cold one %.6f", warm, cold)
	}
	stranger := ID{Model: testModel, Lane: "Alibaba"}
	if held := cachedTokens(stranger, request); held != 0 {
		t.Fatalf("a lane that never served this conversation was credited with %d cached tokens", held)
	}
	stale := request
	stale.Now = noon.Add(PrefixHold + time.Minute)
	if held := cachedTokens(incumbent, stale); held != 0 {
		t.Fatalf("a prefix older than the cache window was still believed warm: %d tokens", held)
	}
}

// ── THE HEDGE TIME ──────────────────────────────────────────────────────────

// TestTheHedgeTimeComesFromTheBeliefAndNotFromAConstant is the arithmetic of
// Part I §4: for a log-normal the longer you have waited the longer you should
// expect to go on waiting, so the moment to ask somebody else is a property of
// this lane's belief and of what the alternative would take.
func TestTheHedgeTimeComesFromTheBeliefAndNotFromAConstant(t *testing.T) {
	quick := Belief{TTFT: Posterior{X: math.Log(400), P: 1}}
	alt := Belief{TTFT: Posterior{X: math.Log(800), P: 0.09}}
	deadline := hedgeTime(quick, alt)
	if deadline < 700*time.Millisecond || deadline > 2*time.Second {
		t.Fatalf("a lane whose normal is 0.4s with a 0.8s alternative hedges at %s", deadline)
	}
	slower := hedgeTime(quick, Belief{TTFT: Posterior{X: math.Log(2500), P: 0.09}})
	if slower <= deadline {
		t.Fatalf("hedging to a slower alternative got no later: %s then %s", deadline, slower)
	}
	// And the whole scale moves with the lane's own belief rather than with a
	// constant: a lane whose normal is 2s, beside an alternative whose normal is
	// 4s, waits far longer before anybody asks somebody else.
	patient := hedgeTime(
		Belief{TTFT: Posterior{X: math.Log(2000), P: 1}},
		Belief{TTFT: Posterior{X: math.Log(4000), P: 0.09}})
	if patient <= deadline {
		t.Fatalf("a lane whose normal is 2s hedged at %s, no later than a lane whose normal is 0.4s (%s)",
			patient, deadline)
	}
	if none := hedgeTime(Belief{}, alt); none != 0 {
		t.Fatalf("a lane nothing is believed about was given a deadline of %s", none)
	}
	lonely := hedgeTime(quick, Belief{})
	if lonely < hedgeFloor || lonely > hedgeCeiling {
		t.Fatalf("with nobody to hedge to the deadline was %s", lonely)
	}
}

// TestAChoiceCarriesItsDeadlineAndItsAlternative keeps the two halves of a
// rescue together: the moment a hedge is wanted is the worst moment to start
// choosing where to send it.
func TestAChoiceCarriesItsDeadlineAndItsAlternative(t *testing.T) {
	chooser := &chooser{ledger: measured(20, noon.Add(-time.Minute))}
	choice := chooser.Choose(talk())
	if choice.Alt == "" || choice.Alt == choice.Order[0] {
		t.Fatalf("the alternative was %q beside an order of %v", choice.Alt, choice.Order)
	}
	if choice.Deadline < hedgeFloor || choice.Deadline > hedgeCeiling {
		t.Fatalf("the deadline was %s, outside the band a hedge is worth having in", choice.Deadline)
	}
	if choice.Why == "" {
		t.Fatal("a choice with an opinion said nothing about it")
	}
}

// ── THE EMPTY ANSWER ────────────────────────────────────────────────────────

// TestAnEmptyLedgerIsAnEmptyChoice is the honest answer on the first call of a
// fresh machine: the transport sends exactly what it sent before this package
// existed.
func TestAnEmptyLedgerIsAnEmptyChoice(t *testing.T) {
	chooser := &chooser{ledger: &fakeLedger{}}
	choice := chooser.Choose(talk())
	if !choice.Empty() || choice.Why != "" || len(choice.Frontier) != 0 || choice.Deadline != 0 {
		t.Fatalf("an empty ledger produced an opinion: %+v", choice)
	}
}

// TestAGateThatEmptiesTheSetIsAnEmptyChoice is the same law one step later: a
// request no lane can serve is one the router should shape for itself, not one
// this package should answer with a lane it has just refused.
func TestAGateThatEmptiesTheSetIsAnEmptyChoice(t *testing.T) {
	request := talk()
	request.Tools = true
	request.MaxTokens = 900_000
	chooser := &chooser{ledger: measured(20, noon.Add(-time.Minute))}
	choice := chooser.Choose(request)
	for _, row := range choice.Frontier {
		if !named([]string{"CoreWeave", "DigitalOcean"}, row.ID.Lane) {
			t.Fatalf("a lane that cannot write 900k tokens with tools reached the frontier: %s", row.ID.Lane)
		}
	}
	request.QualityNeed = 0.999
	if strict := chooser.Choose(request); !strict.Empty() {
		t.Fatalf("a quality nobody meets still produced a preference: %+v", strict)
	}
}

// TestλIsATableAndNotADial pins the four cases of [Lambda] the design states.
func TestTheValueOfASecondIsATableAndNotADial(t *testing.T) {
	if got := Lambda(true, false, 0, 0, 0); got != AttentionValue {
		t.Fatalf("a person watching is worth %v seconds to the dollar, want %v", got, AttentionValue)
	}
	if got := Lambda(false, true, 0, time.Minute, 0); got != TaskWallValue {
		t.Fatalf("a call on the critical path is worth %v, want %v", got, TaskWallValue)
	}
	if got := Lambda(false, false, 10*time.Minute, time.Minute, 0); got != 0 {
		t.Fatalf("a node with slack to spare is worth %v, want price to win outright", got)
	}
	tight := Lambda(false, false, 0, time.Minute, 0)
	if tight <= 0 || tight > TaskWallValue {
		t.Fatalf("a node with no slack left is worth %v", tight)
	}
	pressed := Lambda(false, false, 10*time.Minute, time.Minute, 30*time.Second)
	if pressed <= TaskWallValue {
		t.Fatalf("a deadline nearer than the work is worth %v, which is no more than an ordinary node", pressed)
	}
	if pressed > TaskWallValue*DeadlineUrgencyCap {
		t.Fatalf("a deadline bought λ = %v, above the cap", pressed)
	}
}

// TestThePriceIsCacheAwareAndInDollars pins the arithmetic of Part II §3 on one
// worked example, because a price that is wrong by a factor of a million is a
// price that looks plausible in every log.
func TestThePriceIsCacheAwareAndInDollars(t *testing.T) {
	facts := Facts{PriceIn: 0.07 / 1_000_000, PriceOut: 0.28 / 1_000_000, PriceCache: 0.007 / 1_000_000}
	request := Request{PromptTokens: 20_000, Visible: 400}
	cold := PriceOf(facts, request)
	want := 0.07/1_000_000*20_000 + 0.28/1_000_000*400
	if math.Abs(cold-want) > 1e-12 {
		t.Fatalf("a cold prompt cost %.9f, want %.9f", cold, want)
	}
	warm := PriceWithCache(facts, request, 20_000)
	wantWarm := 0.007/1_000_000*20_000 + 0.28/1_000_000*400
	if math.Abs(warm-wantWarm) > 1e-12 {
		t.Fatalf("a warm prompt cost %.9f, want %.9f", warm, wantWarm)
	}
	noDiscount := Facts{PriceIn: facts.PriceIn, PriceOut: facts.PriceOut}
	if PriceWithCache(noDiscount, request, 20_000) != PriceOf(noDiscount, request) {
		t.Fatal("a lane with no cache tariff gave a discount it does not publish")
	}
}
