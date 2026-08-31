package main

// ── THE WORLD THE SHIPPED CODE IS JUDGED IN ─────────────────────────────────
//
// Everything in this file is the SCENERY: the seventeen lanes of one live
// endpoint sheet, the log-normal-with-a-tail each of them behaves by, the one
// price table every arm is charged through, and the capability gate the
// baseline is given so that it is not a strawman. None of it is a policy and
// none of it is a belief.
//
// It is a line-for-line port of the generative half of `bench/lanelab/sim.py`,
// and it is a port rather than a fresh model on purpose: the two programs are
// worth putting side by side only if the world they disagree about is the same
// world. Where a constant here has a name in that file, it is spelled the same
// way, so the two can be diffed by eye.

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE CONSTANTS, PORTED ───────────────────────────────────────────────────

const (
	// z90 is the standard normal's ninetieth percentile: the sheet gives a p50
	// and a p90, which is a location and a scale.
	z90 = 1.2816

	// sigmaFloor is the least spread any lane is drawn with. A lane whose p90
	// is not above its p50 would fit a zero variance, and a zero-variance draw
	// is a claim of certainty nobody earned.
	sigmaFloor = 0.15

	// THE TAIL IS THE POINT. CoreWeave is the fastest lane on this sheet at the
	// median and one of the worst at the ninety-ninth percentile, and a single
	// log-normal fitted to a p50 and a p90 cannot produce that. So a draw is a
	// MIXTURE: 99% of the time the body, 1% of the time a component whose
	// median is the sheet's own p99. That one per cent is what a person
	// remembers.
	tailP     = 0.01
	tailSigma = 0.35

	// Rate gets the mirror treatment with one asymmetry: a lane that
	// occasionally writes FASTER than its p90 hurts nobody, so there is no fast
	// tail; what hurts is a lane that occasionally crawls.
	rateSlowDiv = 3.0
	rateFloor   = 1.0

	// promptTokens is how long the conversation is in every scenario, in one
	// place, because it is the noise term on every first-token measurement and
	// half of every bill.
	promptTokens = 4000

	// maxAttempts is how many lanes a refused answer is retried down.
	maxAttempts = 3
)

// trueAccept is the share of answers a lane really returns usable, assumed from
// the sheet's quantization word.
//
// IT IS THE SOFTEST ASSUMPTION IN THIS PROGRAM and it is stated rather than
// buried: nobody has measured these, they are a plausible ordering dressed as
// numbers, and they are the same numbers `sim.py` uses so that the two
// programs' quality gates are asked the same question.
var trueAccept = map[string]float64{"fp4": 0.85, "unknown": 0.95}

// trueAcceptDefault is what a lane whose quantization the sheet names precisely
// is assumed to return.
const trueAcceptDefault = 0.985

// ── THE SCENARIOS ───────────────────────────────────────────────────────────

// scenario is one shape of request, with the price of a second that goes with
// it. The three of them exist because λ DIFFERS BY TWO ORDERS OF MAGNITUDE
// between a chat turn and a background node, and a router that used one number
// for both would be wrong twice.
type scenario struct {
	name    string
	why     string
	lambda  float64
	visible int
	hidden  int
	quality float64
	tools   bool
}

// scenarios are exactly the three `sim.py` replays, with its numbers.
var scenarios = []scenario{
	{name: "talk", why: "a person is watching the stream",
		lambda: 90, visible: 400, hidden: 0, quality: 0.90, tools: false},
	{name: "work", why: "critical-path tool loop, nobody reads the tokens",
		lambda: 90, visible: 0, hidden: 2000, quality: 0.97, tools: true},
	{name: "offpath", why: "background, nobody is waiting, price wins outright",
		lambda: 0, visible: 0, hidden: 2000, quality: 0.97, tools: true},
}

// answer is N̂: how many tokens this scenario's answer is thought to be worth.
func (s scenario) answer() int { return s.visible + s.hidden }

// request is the scenario as the chooser is asked about it.
//
// Horizon is how many calls this session has left rather than a constant,
// because that is what [lane.ExplorationHorizon] scales exploration by: a run
// that is nearly over should stop paying to find things out.
func (s scenario) request(model string, now time.Time, horizon int) lane.Request {
	return lane.Request{
		Model:        model,
		PromptTokens: promptTokens,
		Visible:      s.visible,
		Hidden:       s.hidden,
		Tools:        s.tools,
		MaxTokens:    s.answer(),
		ValueOfTime:  s.lambda,
		QualityNeed:  s.quality,
		Horizon:      horizon,
		Now:          now,
	}
}

// ── THE SHEET, DECODED HERE ─────────────────────────────────────────────────
//
// The fixture is decoded in this program and NOT by a reader added to
// `internal/lane`: a bench that needed a new seam in the package it is judging
// would be a bench that changed its own subject.

type sheetPercentiles struct {
	P50 float64 `json:"p50"`
	P75 float64 `json:"p75"`
	P90 float64 `json:"p90"`
	P99 float64 `json:"p99"`
}

type sheetEndpoint struct {
	ProviderName        string `json:"provider_name"`
	Quantization        string `json:"quantization"`
	ContextLength       int    `json:"context_length"`
	MaxCompletionTokens int    `json:"max_completion_tokens"`
	Pricing             struct {
		Prompt         string `json:"prompt"`
		Completion     string `json:"completion"`
		InputCacheRead string `json:"input_cache_read"`
	} `json:"pricing"`
	SupportsToolChoice struct {
		Function bool `json:"function"`
	} `json:"supports_tool_choice"`
	Status                int              `json:"status"`
	UptimeLast5m          float64          `json:"uptime_last_5m"`
	SupportsImplicitCache bool             `json:"supports_implicit_caching"`
	LatencyLast30m        sheetPercentiles `json:"latency_last_30m"`
	ThroughputLast30m     sheetPercentiles `json:"throughput_last_30m"`
}

type sheetDoc struct {
	FetchedAt string `json:"fetched_at"`
	Model     string `json:"model"`
	Data      struct {
		ID        string          `json:"id"`
		Endpoints []sheetEndpoint `json:"endpoints"`
	} `json:"data"`
}

// worldLane is one row of the sheet plus the fit this program draws from.
//
// Nothing is renamed on the way in. The prices are DOLLARS PER TOKEN, which is
// the unit the router publishes and the unit `internal/lane` stays in; the only
// conversion anywhere here is the ParseFloat on the router's decimal strings.
type worldLane struct {
	name     string
	quant    string
	status   int
	uptime5m float64
	tools    bool
	context  int
	maxOut   int
	caches   bool

	priceIn    float64
	priceOut   float64
	priceCache float64

	ttft [4]float64
	rate [4]float64

	ttftMu, ttftSigma, ttftTail float64
	rateMu, rateSigma, rateSlow float64

	accept float64
}

// world is the whole sheet, plus the model it describes.
type world struct {
	model   string
	fetched string
	file    string
	lanes   []worldLane
	total   int
}

// loadWorld reads the fixture and fits every lane the sheet published timing
// for. A lane with no p50 cannot be simulated and inventing one would be this
// program preferring or refusing a lane on a number nobody measured.
func loadWorld(path string) (*world, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc sheetDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	model := doc.Model
	if model == "" {
		model = doc.Data.ID
	}
	out := &world{model: model, fetched: doc.FetchedAt, file: path, total: len(doc.Data.Endpoints)}
	for _, e := range doc.Data.Endpoints {
		quant := strings.ToLower(strings.TrimSpace(e.Quantization))
		if quant == "" {
			quant = "unknown"
		}
		l := worldLane{
			name:     e.ProviderName,
			quant:    quant,
			status:   e.Status,
			uptime5m: e.UptimeLast5m,
			tools:    e.SupportsToolChoice.Function,
			context:  e.ContextLength,
			maxOut:   e.MaxCompletionTokens,
			caches:   e.SupportsImplicitCache,
			priceIn:  money(e.Pricing.Prompt),
			priceOut: money(e.Pricing.Completion),
			ttft: [4]float64{e.LatencyLast30m.P50, e.LatencyLast30m.P75,
				e.LatencyLast30m.P90, e.LatencyLast30m.P99},
			rate: [4]float64{e.ThroughputLast30m.P50, e.ThroughputLast30m.P75,
				e.ThroughputLast30m.P90, e.ThroughputLast30m.P99},
		}
		l.priceCache = money(e.Pricing.InputCacheRead)
		if strings.TrimSpace(e.Pricing.InputCacheRead) == "" {
			l.priceCache = l.priceIn
		}
		if l.ttft[0] <= 0 || l.rate[0] <= 0 {
			continue
		}
		l.ttftMu, l.ttftSigma = fit(l.ttft[0], l.ttft[2])
		l.rateMu, l.rateSigma = fit(l.rate[0], l.rate[2])
		l.ttftTail = math.Log(math.Max(l.ttft[3], math.Max(l.ttft[0], 1)))
		l.rateSlow = math.Log(math.Max(l.rate[0]/rateSlowDiv, rateFloor))
		l.accept = trueAcceptDefault
		if given, ok := trueAccept[l.quant]; ok {
			l.accept = given
		}
		out.lanes = append(out.lanes, l)
	}
	if len(out.lanes) < 2 {
		return nil, fmt.Errorf("gosim: %s named %d lanes with published timing; "+
			"a router with one lane is not choosing", path, len(out.lanes))
	}
	return out, nil
}

// money reads the router's per-token price, which travels as a decimal string
// with more precision than a JSON float survives being read back.
func money(text string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0
	}
	return value
}

// fit turns a published p50 and p90 into a log-normal's location and scale.
func fit(p50, p90 float64) (mu, sigma float64) {
	if p50 <= 0 {
		return 0, sigmaFloor
	}
	mu = math.Log(p50)
	if p90 <= p50 {
		return mu, sigmaFloor
	}
	return mu, math.Max(sigmaFloor, (math.Log(p90)-mu)/z90)
}

// row is the sheet row the real ledger is primed from — the prior, in the
// router's own units, with milliseconds for the first token and tokens per
// second for the rate.
func (l worldLane) row(model string) lane.Row {
	return lane.Row{
		ID: lane.ID{Model: model, Lane: l.name},
		Facts: lane.Facts{
			Tools: l.tools, Quant: l.quant, MaxOut: l.maxOut, Context: l.context,
			Uptime5m:   l.uptime5m,
			PriceIn:    l.priceIn,
			PriceOut:   l.priceOut,
			PriceCache: l.priceCache,
			Caches:     l.caches,
		},
		TTFTp50: l.ttft[0], TTFTp75: l.ttft[1], TTFTp90: l.ttft[2], TTFTp99: l.ttft[3],
		Ratep50: l.rate[0], Ratep75: l.rate[1], Ratep90: l.rate[2], Ratep99: l.rate[3],
	}
}

// price is what one request costs on one lane, in dollars.
//
// THE ONLY PRICE FUNCTION IN THIS PROGRAM, and every arm is charged through it.
// A benchmark where the arms price themselves measures its own bookkeeping. The
// cache term of Part II §3 is absent for the reason `sim.py` gives: exactly one
// lane on this sheet advertises implicit caching and no policy here ever holds
// a prefix, so the branch would never fire.
func (l worldLane) price(s scenario) float64 {
	return l.priceIn*promptTokens + l.priceOut*float64(s.answer())
}

// ── THE DRAWS ───────────────────────────────────────────────────────────────

// shot is what one lane would do on one request, drawn before anybody has
// chosen anything.
type shot struct {
	ttftMs float64
	rate   float64
	// roll is the uniform this lane's answer is accepted against. It is drawn
	// here, with the timings, so that WHETHER AN ANSWER IS USABLE IS A FACT OF
	// THE WORLD and not something an arm draws for itself.
	roll float64
}

// shots is the whole sheet's behaviour on one request, in the sheet's own
// order.
//
// EVERY ARM SEES THE SAME WORLD. The draw is keyed by the seed, the scenario,
// the request and the attempt — and never by the policy — so lane seven behaves
// identically on request 412 of the belief arm and request 412 of the baseline.
// `sim.py` keys its stream by the policy instead, because a policy that draws
// more numbers there would shift the stream another policy sees; drawing the
// world once and handing it to everybody is the stronger form of the same law
// and it is one of the few places this program is deliberately not a port.
func (w *world) shots(seed int, scen string, request, attempt int) []shot {
	draws := rand.New(rand.NewSource(keyed(seed, scen, request, attempt)))
	out := make([]shot, len(w.lanes))
	for index, l := range w.lanes {
		out[index] = shot{
			ttftMs: l.drawTTFT(draws),
			rate:   l.drawRate(draws),
			roll:   draws.Float64(),
		}
	}
	return out
}

// keyed turns the coordinates of one draw into a seed. It is a hash rather than
// an arithmetic mix so that two coordinates which happen to sum alike do not
// share a stream.
func keyed(seed int, scen string, request, attempt int) int64 {
	sum := fnv.New64a()
	fmt.Fprintf(sum, "%d|%s|%d|%d", seed, scen, request, attempt)
	return int64(sum.Sum64() &^ (1 << 63))
}

func (l worldLane) drawTTFT(draws *rand.Rand) float64 {
	if draws.Float64() < tailP {
		return math.Exp(l.ttftTail + tailSigma*draws.NormFloat64())
	}
	return math.Exp(l.ttftMu + l.ttftSigma*draws.NormFloat64())
}

func (l worldLane) drawRate(draws *rand.Rand) float64 {
	var r float64
	if draws.Float64() < tailP {
		r = math.Exp(l.rateSlow + tailSigma*draws.NormFloat64())
	} else {
		r = math.Exp(l.rateMu + l.rateSigma*draws.NormFloat64())
	}
	return math.Max(rateFloor, r)
}

// ── THE CAPABILITY GATE THE BASELINE IS GIVEN ───────────────────────────────

// capable reports whether a lane could serve this request at all.
//
// IT IS A PORT OF THE SHIPPED GATE (`internal/lane/frontier.go`, `capable`) and
// not of `sim.py`'s, because the arms it is applied to have to be refused for
// the same reasons the belief arms are: giving the baseline a laxer gate would
// credit the design with a refusal the shipped code makes for every arm alike.
// The one clause that cannot be ported is the quality bound, which is a belief
// rather than a fact; the baseline holds no beliefs, so it gets the whole of
// the capability half and none of the quality half.
//
// Two clauses of `sim.py`'s gate are missing here BECAUSE THE SHIPPED GATE DOES
// NOT HAVE THEM, and that is a finding rather than an omission: the shipped
// [lane.Facts] carries no `status` field at all, so a lane the router itself
// marks unhealthy is invisible to this build, and the shipped gate reads
// [lane.Request.MaxTokens] where `sim.py` reads the answer length. See
// REPORT.md.
func capable(l worldLane, s scenario) bool {
	if s.tools && !l.tools {
		return false
	}
	if want := s.answer(); want > 0 && l.maxOut > 0 && l.maxOut < want {
		return false
	}
	if need := promptTokens + s.answer(); need > 0 && l.context > 0 && l.context < need {
		return false
	}
	if lowQuantization(l.quant) {
		return false
	}
	if l.uptime5m > 0 && l.uptime5m < lane.UptimeFloor {
		return false
	}
	return true
}

// lowQuantization is the shipped list, copied. Four-bit weights are a different
// model wearing the same name, so they are opt-in and no arm here opts in.
func lowQuantization(quant string) bool {
	switch strings.ToLower(strings.TrimSpace(quant)) {
	case "fp4", "int4", "nf4", "q4", "int4_w4a16", "fp4_e2m1":
		return true
	}
	return false
}

// gated is the candidate set for one scenario, in the sheet's own order.
func (w *world) gated(s scenario) []int {
	var out []int
	for index, l := range w.lanes {
		if capable(l, s) {
			out = append(out, index)
		}
	}
	return out
}

// ── THE BASELINE'S OWN DRAW ─────────────────────────────────────────────────

// defaultOrder is what a request gets when nothing in the harness has an
// opinion: the router's own documented default, a lane sampled with weight
// 1/price² over the STABLE lanes — status zero and five-minute uptime at or
// above the floor — inside the shared capability gate.
//
// It is drawn here rather than left to `lanestub`, which answers a request with
// no preference from the first lane declared, because "the first lane declared"
// is a property of this program's own bookkeeping and not of any router. The
// wire still carries NO `provider` block for this arm; what this function
// decides is only which lane the fake router finds at the head of its list.
//
// Sampled WITHOUT REPLACEMENT so that a refused answer's retry is a second draw
// from the same weights rather than the same lane three times.
func (w *world) defaultOrder(s scenario, seed int, request int) []string {
	pool := make([]int, 0, len(w.lanes))
	weights := make([]float64, 0, len(w.lanes))
	for _, index := range w.gated(s) {
		l := w.lanes[index]
		if l.status != 0 || l.uptime5m < lane.UptimeFloor {
			continue
		}
		cost := l.price(s)
		if cost <= 0 {
			continue
		}
		pool = append(pool, index)
		weights = append(weights, 1/(cost*cost))
	}
	draws := rand.New(rand.NewSource(keyed(seed, s.name+"|default", request, 0)))
	out := make([]string, 0, maxAttempts)
	for len(out) < maxAttempts && len(pool) > 0 {
		k := weightedIndex(draws, weights)
		out = append(out, w.lanes[pool[k]].name)
		pool = append(pool[:k], pool[k+1:]...)
		weights = append(weights[:k], weights[k+1:]...)
	}
	return out
}

func weightedIndex(draws *rand.Rand, weights []float64) int {
	total := 0.0
	for _, one := range weights {
		total += one
	}
	x := draws.Float64() * total
	acc := 0.0
	for index, one := range weights {
		acc += one
		if x <= acc {
			return index
		}
	}
	return len(weights) - 1
}

// ── THE SHEET AS THE FAKE ROUTER SERVES IT ──────────────────────────────────

// stubLanes is the whole sheet scripted for one request: every lane behaving as
// this request's draw says it will, sped up, with head declared first so that
// an arm sending no preference at all lands somewhere a router would have sent
// it.
//
// broken names a lane that has gone bad, which takes four seconds to say its
// first word whatever it drew. Everything else about it — its price, its rate,
// its published percentiles — is unchanged, because that is what makes it the
// interesting case: the sheet still says the lane is quick.
func (w *world) stubLanes(shots []shot, head, broken string, speedup int) []lanestub.Lane {
	out := make([]lanestub.Lane, 0, len(w.lanes))
	for index, l := range w.lanes {
		ttft := time.Duration(shots[index].ttftMs * float64(time.Millisecond))
		if l.name == broken {
			ttft = brokenTTFT
		}
		out = append(out, lanestub.Lane{
			Name: l.name,
			Profile: lanestub.Profile{
				TTFT: ttft / time.Duration(speedup),
				// THE GAPS BETWEEN TOKENS ARE ZERO ON THE WIRE, on purpose, and
				// the rate this lane really writes at is carried beside the
				// stream rather than through it. See [instantRate].
				Rate:       instantRate,
				Tokens:     streamTokens,
				Heartbeats: false,
				Tools:      l.tools, Quant: l.quant,
				Context: l.context, MaxOut: l.maxOut, Uptime: l.uptime5m,
				Status:  l.status,
				Caches:  l.caches,
				PriceIn: l.priceIn, PriceOut: l.priceOut, PriceCache: l.priceCache,
				TTFTms: l.ttft, Rates: l.rate,
			},
		})
	}
	if head != "" {
		for index := range out {
			if out[index].Name == head {
				out[0], out[index] = out[index], out[0]
				break
			}
		}
	}
	return out
}

// brokenTTFT is what "the lane went slow" means here. Four seconds to the first
// word is not a failure and not a refusal — it is the ordinary way an endpoint
// goes bad, and it is the case a strike table's fixed two seconds was invented
// for and gets wrong for every other lane. It is the same figure
// `internal/lane/e2e_test.go` breaks a lane with.
const brokenTTFT = 4 * time.Second

// instantRate is the tokens-per-second the stub is scripted at so that the gaps
// between its tokens round to nothing.
//
// ── THE WIRE CANNOT DELIVER A RATE, AND THIS IS THE MEASUREMENT THAT SAYS SO ─
//
// A rate of thirty tokens a second at a speedup of two hundred is a gap of a
// sixth of a millisecond, and a Go timer armed for a sixth of a millisecond on
// the machine this was written on returns after about six tenths of one — the
// overshoot is roughly 0.5–0.65 ms for every sleep under two milliseconds, and
// it does not shrink with the sleep. So a forty-token stream costs
// thirty-nine such sleeps whatever it was scripted for, its measured rate is a
// measurement of this machine's scheduler, and the wall time of the whole run
// is set by the timer rather than by the world.
//
// (That is also why `internal/lane/e2e_test.go`'s note — that a hundredfold
// speedup leaves a scripted duration "two orders of magnitude above the
// loopback round trip" — names the wrong constant. The loopback round trip is
// not what binds; the timer's own floor is, and at a hundredfold it is fifty
// milliseconds of world time on every scripted wait.)
//
// So the split is made explicitly rather than left to be discovered in a
// number: THE FIRST TOKEN IS MEASURED OFF THE WIRE — one sleep, long enough to
// survive the floor, and what it cost is reported with the table — and THE RATE
// IS THE WORLD'S OWN DRAW, carried beside the stream and never read off it. The
// rate that reaches [lane.PerceivedSeconds], the rate that reaches the ledger's
// own filter and the rate `sim.py` uses are then the same number, which is the
// only way the two tables mean the same thing.
//
// Four billion is chosen so that `time.Second / Rate` truncates to zero and
// `lanestub`'s clock is never armed at all between tokens.
const instantRate = 4e9

// streamTokens is how many tokens the fake router actually writes.
//
// IT IS A SAMPLE OF THE ANSWER AND NOT THE ANSWER. A `work` request is scored
// on two thousand hidden tokens; writing two thousand chunks through a socket a
// hundred and forty thousand times would take a day, and the reference model in
// `sim.py` does not write them either — it draws a first token and a rate and
// does arithmetic. What this program buys by writing any of them at all is that
// the first token and the rate are MEASURED off a real wire by real code rather
// than asserted, and the wait and the bill for the whole answer are then the
// same arithmetic `sim.py` does, on numbers that were measured.
//
// FORTY AND NOT TWENTY-FOUR. `lanestub.DefaultTokens` is 24 and
// `internal/lane/belief.go`'s `ratedFloor` is 32: an answer under the floor
// teaches the ledger its first token and never its rate. So a run scripted at
// the stub's default would drive the shipped ledger with the rate filter
// switched off for its whole length — see REPORT.md, which reports that
// `internal/lane/e2e_test.go` has exactly that hole.
const streamTokens = 40
