package lane

// ── THE ACCEPTANCE TEST FOR THE WHOLE MECHANISM ─────────────────────────────
//
// Everything else in this directory tests one seam. This file tests the CLAIM:
// that a build which believes something about the machines behind a model
// starts faster than one that does not, moves off a lane that has gone bad,
// lets it back when it recovers, refuses one that returns answers nobody can
// use, and never goes to the network to decide any of it.
//
// Those are the five sentences the design is sold on
// (`ideation/provider-routing.md`, "Proof plan"), and they are written here as
// five scenarios rather than as unit tests because every one of them is a
// statement about the seams TOGETHER: a chooser that picks well over a ledger
// that forgets is worth nothing, and so is a ledger that learns for a chooser
// with no opinion.
//
// ── HOW IT IS WIRED, AND WHY IT IS NOT WIRED THROUGH THE TRANSPORT ──────────
//
// The scenarios drive `internal/lane/lanestub` — the fake router — through
// [e2eRouter], which is this file's own forty lines of transport: it asks the
// registry's chooser for a preference, puts it on the wire exactly as the
// router reads it (`provider.order`, `only`, `ignore`), watches the stream that
// comes back, spends at most one hedge on it under a budget, and folds the
// result into the ledger as a sighting. That is the whole of the contract in
// docs/ARCHITECTURE.md Decision 10, and it is written here rather than imported
// from `internal/provider` for one reason: this package may not know that
// package exists (a structural test in this directory says so, and the
// dependency really does point the other way). The transport's own half of the
// same contract is tested in `internal/provider`.
//
// ── THE TWO CLOCKS ─────────────────────────────────────────────────────────
//
// A scenario written in the units of the world — a lane whose first token comes
// in 430 ms, an answer four hundred tokens long — takes half a minute of
// somebody's afternoon to run. So THE WIRE RUNS [e2eSpeedup] TIMES FASTER THAN
// THE WORLD IT DESCRIBES, and nothing else does: every duration that crosses
// into the ledger, every moment a request carries, and every figure asserted on
// below is in the world's own units, and only the timers armed against the
// socket are divided. The fast clock in `lanestub` is not used here, because
// two of these scenarios put two streams in flight at once and that clock is
// one timeline (see the note at the top of `lanestub.go`).
//
// ── WHY MOST OF THIS SKIPS TODAY ────────────────────────────────────────────
//
// This file was written in the same wave as the seams it tests, against the
// contract rather than against an implementation. While the empty chooser has
// no opinion, every scenario that needs one skips with the lane that owes it
// named in the message — [e2eSkipWithoutAChooser]. Nothing here is written
// loosely enough to pass on the empty implementations: a scenario that would
// pass against a chooser that answers nothing is a scenario that is not
// measuring anything.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// e2eSpeedup is how much faster the wire runs than the world it describes. A
// hundred puts a scripted four-second stall at forty milliseconds of somebody's
// afternoon, which is still two orders of magnitude above the loopback round
// trip underneath it — the point at which the scenario would start measuring
// the test's own socket instead of the lane it scripted.
const e2eSpeedup = 100

// e2eModel is the model every scenario here is about. It is the one the design
// was measured on, so the numbers in [e2eSheet] are a real half hour of a real
// router rather than a shape somebody drew.
const e2eModel = "deepseek/deepseek-v4-flash"

// e2ePromptTokens is how long the conversation is in every scenario. It is one
// number in one place because it is the noise term on every first-token
// measurement below, and a prompt that varied between arms would be a
// difference nobody asked for.
const e2ePromptTokens = 4000

// ── THE WORLD, AS IT WAS MEASURED ───────────────────────────────────────────

// e2eLane is one row of the table in `ideation/provider-routing.md`, "The
// world, measured (2026-08-30, one model, 30-minute window)". Six of the
// seventeen lanes are here, chosen because between them they carry every shape
// the design has to get right: the one that is fastest at the median and among
// the worst at the tail, the one with the tight tail at four times the price,
// the fast one that cannot take a tool call, the cheap one that writes at six
// tokens a second, and two ordinary ones in the middle.
//
// Prices are DOLLARS PER TOKEN, which is the unit the router publishes and the
// unit this package stays in; the table quotes dollars per million output
// tokens, so 0.28 there is 2.8e-7 here. The table publishes no input tariff, so
// each lane's is set at two fifths of its output tariff, which is the ratio the
// live sheet showed for this model on the day it was read.
type e2eLane struct {
	name    string
	ttft    [4]float64 // p50, p75, p90, p99, in milliseconds
	rate    [4]float64 // p50, p75, p90, p99, in tokens per second
	uptime  float64
	dollars float64 // per million output tokens, as the table quotes it
	tools   bool
	maxOut  int
	context int
	quant   string
}

// e2eSheet is the table itself. The p75 columns the table does not print are
// filled in at the geometric middle of p50 and p90, which is what a log-normal
// with those two percentiles has there — the same fit `Row` is documented to
// support, done once so that no scenario has to invent one.
var e2eSheet = []e2eLane{
	{name: "CoreWeave", ttft: [4]float64{430, 1397, 4539, 12240}, rate: [4]float64{24, 34, 48, 62},
		uptime: 99.6, dollars: 0.28, tools: true, maxOut: 943718, context: 1048576, quant: "fp8"},
	{name: "Parasail", ttft: [4]float64{758, 1185, 1852, 10679}, rate: [4]float64{41, 53, 68, 84},
		uptime: 99.9, dollars: 0.28, tools: false, maxOut: 943718, context: 1048576, quant: "fp8"},
	{name: "DeepInfra", ttft: [4]float64{760, 1011, 1345, 3234}, rate: [4]float64{27, 32, 37, 44},
		uptime: 99.8, dollars: 0.18, tools: true, maxOut: 65536, context: 163840, quant: "fp8"},
	{name: "Cloudflare", ttft: [4]float64{768, 893, 1037, 1749}, rate: [4]float64{58, 73, 92, 110},
		uptime: 100, dollars: 1.32, tools: false, maxOut: 345000, context: 345000, quant: ""},
	{name: "Baidu", ttft: [4]float64{844, 1155, 1580, 5503}, rate: [4]float64{75, 93, 115, 140},
		uptime: 100, dollars: 0.28, tools: false, maxOut: 131072, context: 131072, quant: "fp8"},
	{name: "DigitalOcean", ttft: [4]float64{1504, 2092, 2913, 64274}, rate: [4]float64{6, 7, 9, 12},
		uptime: 99.8, dollars: 0.17, tools: true, maxOut: 943718, context: 1048576, quant: ""},
}

// priceOut is the lane's output tariff in dollars per token.
func (l e2eLane) priceOut() float64 { return l.dollars / 1e6 }

// priceIn is the lane's input tariff. See the note on [e2eLane].
func (l e2eLane) priceIn() float64 { return 0.4 * l.priceOut() }

// row is the sheet row this lane publishes: the prior the ledger is primed
// from, in the router's own units.
func (l e2eLane) row() Row {
	return Row{
		ID: ID{Model: e2eModel, Lane: l.name},
		Facts: Facts{
			Tools: l.tools, Quant: l.quant, MaxOut: l.maxOut, Context: l.context,
			Uptime5m: l.uptime,
			PriceIn:  l.priceIn(), PriceOut: l.priceOut(), PriceCache: 0.1 * l.priceIn(),
		},
		TTFTp50: l.ttft[0], TTFTp75: l.ttft[1], TTFTp90: l.ttft[2], TTFTp99: l.ttft[3],
		Ratep50: l.rate[0], Ratep75: l.rate[1], Ratep90: l.rate[2], Ratep99: l.rate[3],
	}
}

// stub is the lane as the fake router serves it: it behaves at its own median,
// sped up, and publishes the percentiles the table printed. Behaving at the
// median rather than sampling is deliberate — a scenario that asserts on a p90
// must not also be a test of a random number generator.
func (l e2eLane) stub() lanestub.Lane {
	return lanestub.Lane{
		Name: l.name,
		Profile: lanestub.Profile{
			TTFT: e2eScaled(time.Duration(l.ttft[0]) * time.Millisecond),
			Rate: l.rate[0] * e2eSpeedup,
			// Twenty-four tokens is enough for a stream to have a shape and few
			// enough that a scenario of twenty of them is over in a blink. The
			// answer LENGTH a request is scored on is [Request.Visible] and
			// [Request.Hidden], which is a different number and a belief about
			// the future rather than a fact about this stream.
			Tokens:     lanestub.DefaultTokens,
			Heartbeats: true,
			Tools:      l.tools, Quant: l.quant, Context: l.context, MaxOut: l.maxOut,
			Uptime: l.uptime,
			TTFTms: l.ttft, Rates: l.rate,
			PriceIn: l.priceIn(), PriceOut: l.priceOut(), PriceCache: 0.1 * l.priceIn(),
		},
	}
}

// e2eScaled turns a duration of the world into a duration of the wire.
func e2eScaled(d time.Duration) time.Duration { return d / e2eSpeedup }

// e2eWorld turns a duration measured on the wire back into the world's units,
// which are the only ones anything here asserts on.
func e2eWorld(d time.Duration) time.Duration { return d * e2eSpeedup }

// e2eStubLanes is the whole sheet as the fake router serves it.
func e2eStubLanes() []lanestub.Lane {
	lanes := make([]lanestub.Lane, 0, len(e2eSheet))
	for _, lane := range e2eSheet {
		lanes = append(lanes, lane.stub())
	}
	return lanes
}

// e2eRows is the whole sheet as the ledger is primed from it.
func e2eRows() []Row {
	rows := make([]Row, 0, len(e2eSheet))
	for _, lane := range e2eSheet {
		rows = append(rows, lane.row())
	}
	return rows
}

// ── THE REQUESTS THE SCENARIOS ASK ──────────────────────────────────────────

// e2eTalk is a turn somebody is watching: four hundred visible tokens, no
// tools, and a dollar worth ninety seconds because a person is looking at an
// empty line. It is the shape Part II §1 of the design costs at attention
// value.
func e2eTalk(now time.Time) Request {
	return Request{
		Model: e2eModel, PromptTokens: e2ePromptTokens,
		Visible: 400, Hidden: 0, MaxTokens: 400,
		ValueOfTime: 90, QualityNeed: 0.90, Horizon: 40, Now: now,
	}
}

// e2eWork is a node on the critical path of a piece of work: two thousand
// tokens nobody reads — reasoning and tool-call JSON, which is pure waiting —
// tools on the request, and the higher quality gate a tool loop needs.
func e2eWork(now time.Time) Request {
	return Request{
		Model: e2eModel, PromptTokens: e2ePromptTokens,
		Visible: 0, Hidden: 2000, MaxTokens: 2000, Tools: true,
		ValueOfTime: 90, QualityNeed: 0.97, Horizon: 40, Now: now,
	}
}

// e2ePrice is the same work with nobody waiting on it: λ is zero, which is the
// honest reading of "off the critical path" and the one case in which price
// wins outright.
func e2ePrice(now time.Time) Request {
	request := e2eWork(now)
	request.ValueOfTime = 0
	return request
}

// ── THE GATE THAT KEEPS THIS FILE HONEST WHILE THE SEAMS ARE EMPTY ──────────

// e2ePrimed installs a fresh registry for one test, primes the ledger from the
// whole sheet, and hands back the ledger it primed.
//
// It primes rather than fetching because that is the documented path a prior
// takes into a belief ([Ledger.Prime]) and because a scenario about choosing
// must not also be a scenario about HTTP. The two that really are about the
// network reach for [e2eServeSheet] instead.
func e2ePrimed(t *testing.T) Ledger {
	t.Helper()
	// A HOME OF ITS OWN, FIRST. The ledger writes every belief through a store
	// and reads yesterday's back on its first question, and [StorePath] resolves
	// under AFORGE_HOME on every call — so a scenario that did not move the
	// state root would fold the fake lanes of this file into the belief file of
	// whoever ran the tests, and then read them back on the next run. That is
	// two bugs at once: somebody's real router gets an opinion about a lane
	// called "CoreWeave" that this file invented, and these scenarios stop being
	// reproducible, because the ledger they start from is whatever the last run
	// left behind. It was found by scenario 3 choosing a different victim on two
	// runs of the same fixed Tuesday.
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	Default().Reset()
	ledger := Default().Ledger()
	for _, row := range e2eRows() {
		// A quarter of a real sighting: the sheet is a thirty-minute aggregate
		// over everybody's prompts and ours are our own, so it pulls the belief
		// without drowning it. The constant is the design's k ≈ 4.
		ledger.Prime(row, 4)
	}
	return ledger
}

// e2eSkipWithoutAChooser is the one place this file decides whether it is
// measuring anything.
//
// A chooser with no opinion answers the zero [Choice], which is a real and
// correct answer — the transport sends what it would have sent before this
// package existed — and every scenario below would then be asserting on an
// empty slice. So they skip, naming the lane that owes the work, and they bite
// the moment a chooser answers at all.
func e2eSkipWithoutAChooser(t *testing.T, now time.Time) Choice {
	t.Helper()
	choice := Default().Chooser().Choose(e2eTalk(now))
	if choice.Empty() {
		t.Skip("lane chooser not landed yet (wave 1 L-B): a primed ledger still chooses nothing")
	}
	return choice
}

// e2eMoment is the Tuesday every scenario happens on. It is fixed rather than
// read from the wall for the reason [Request.Now] exists at all: a choice that
// depended on the day it ran on would be a choice no test could pin.
var e2eMoment = time.Date(2026, time.August, 30, 11, 0, 0, 0, time.UTC)

// ── SCENARIO 1: COLD START IS NOT BLIND ─────────────────────────────────────

// TestS1ColdStartIsNotBlind is the reason the sheet exists.
//
// A process that has just started has measured nothing, and the strike ledger
// this package replaces had nothing to say about that: it sent the first
// request of every session to whichever endpoint the router felt like, and
// learned only from what came back. The sheet is a free prior for every lane of
// every model, so the FIRST choice of a fresh process is already an informed
// one — and "informed" here has a testable meaning. It means the six lanes are
// not equal:
//
//   - For a turn somebody is watching, the pick is one of the four lanes that
//     start in about three quarters of a second and write faster than a person
//     reads. CoreWeave is excluded from that set on purpose: it is the fastest
//     lane on the table at the median and one of the worst at the ninety-ninth
//     percentile, and a person remembers the twelve-second wait. DigitalOcean
//     is excluded on both counts.
//   - For the same work with nobody waiting on it, λ is zero, price is the
//     whole of the objective, and the pick is the cheapest lane that survived
//     the gate — whichever the frontier says that is.
func TestS1ColdStartIsNotBlind(t *testing.T) {
	e2ePrimed(t)
	e2eSkipWithoutAChooser(t, e2eMoment)

	talk := Default().Chooser().Choose(e2eTalk(e2eMoment))
	if len(talk.Order) == 0 {
		t.Fatalf("a primed ledger asked about a talk turn ordered nothing: %+v", talk)
	}
	quick := map[string]bool{"Cloudflare": true, "Baidu": true, "Parasail": true, "DeepInfra": true}
	if !quick[talk.Order[0]] {
		t.Fatalf("the first choice of a fresh process is %q; a turn somebody is watching "+
			"belongs on one of %v, whose tails a person can live with (order %v)",
			talk.Order[0], e2eSortedNames(quick), talk.Order)
	}
	if talk.Order[0] == "DigitalOcean" {
		t.Fatal("a lane that writes at six tokens a second was chosen for a person to watch")
	}

	// λ = 0 is the honest reading of "nobody is waiting", and it is the one
	// case where the answer is simply the cheapest lane still standing. It is
	// asserted against the frontier rather than against a lane name because the
	// frontier is what the choice was made from: a test that named the lane
	// would be asserting on the gate's answer as well, and the gate is
	// scenario 4's business.
	price := Default().Chooser().Choose(e2ePrice(e2eMoment))
	if len(price.Order) == 0 {
		t.Fatalf("with nobody waiting, the chooser ordered nothing: %+v", price)
	}
	if len(price.Frontier) == 0 {
		t.Fatalf("the choice carries no frontier, so it cannot explain itself: %+v", price)
	}
	cheapest := price.Frontier[0]
	for _, scored := range price.Frontier[1:] {
		if scored.Price < cheapest.Price {
			cheapest = scored
		}
	}
	if price.Order[0] != cheapest.ID.Lane {
		t.Fatalf("with λ = 0 the pick is %q at $%.6g, but %q on the same frontier costs $%.6g — "+
			"price is the whole objective when nobody is waiting",
			price.Order[0], e2ePriceOf(price.Frontier, price.Order[0]), cheapest.ID.Lane, cheapest.Price)
	}
}

// ── SCENARIO 2: THE DEFAULT GOES SLOW AND THE ROUTER MOVES ──────────────────

// TestS2TheDefaultGoesSlowAndTheRouterMoves is the claim the whole design is
// bought for.
//
// Twenty requests in a row. From the sixth, whichever lane the router picked
// first takes four seconds to say its first word — not a failure, not a refusal,
// just the ordinary way an endpoint goes bad. The question is whether a build
// that believes something notices, and the control is a build that cannot: the
// same twenty requests pinned to that lane, which is what a session with a
// `/model @lane` pin does and what every session did before this package.
//
// The gate is the ninetieth percentile and not the median, because the median
// is exactly what a slow lane keeps looking fine on. Half the improvement would
// be a story; the design ships on p90.
func TestS2TheDefaultGoesSlowAndTheRouterMoves(t *testing.T) {
	ledger := e2ePrimed(t)
	e2eSkipWithoutAChooser(t, e2eMoment)

	stub := lanestub.New(e2eModel, e2eStubLanes()...)
	defer stub.Close()
	router := newE2ERouter(stub, ledger)

	// Who the router would pick with everything healthy. That is the lane the
	// scenario then breaks, because breaking a lane nobody was using would
	// prove nothing.
	first := Default().Chooser().Choose(e2eTalk(e2eMoment))
	if len(first.Order) == 0 {
		t.Fatalf("nothing was ordered to break: %+v", first)
	}
	victim := first.Order[0]

	routed := router.run(t, 20, victim, false)
	// The control runs against its own ledger and its own router so that what
	// it learns cannot leak into the arm it is the control for.
	Default().Reset()
	control := Default().Ledger()
	for _, row := range e2eRows() {
		control.Prime(row, 4)
	}
	pinnedStub := lanestub.New(e2eModel, e2eStubLanes()...)
	defer pinnedStub.Close()
	pinned := newE2ERouter(pinnedStub, control).run(t, 20, victim, true)

	routedP90, controlP90 := e2eP90(routed.answers), e2eP90(pinned.answers)
	if controlP90 <= 0 {
		t.Fatalf("the control measured nothing")
	}
	t.Logf("victim %s — routed p90 %v over %d requests, pinned p90 %v over %d",
		victim, routedP90.Round(time.Millisecond), stub.Requests(victim),
		controlP90.Round(time.Millisecond), pinnedStub.Requests(victim))

	if routedP90*2 > controlP90 {
		t.Fatalf("the router's p90 is %v against the pin's %v: a build that believes something "+
			"about its lanes has to halve the ninetieth percentile of a lane going bad, not shave it",
			routedP90.Round(time.Millisecond), controlP90.Round(time.Millisecond))
	}
	// A rescue mechanism that pays for itself has to be rare. Three in twenty
	// is already generous against the design's budget of six a minute.
	if routed.hedges > 3 {
		t.Fatalf("twenty requests spent %d hedges; a hedge is a second bill", routed.hedges)
	}
	if total := routed.requests(); total > 23 {
		t.Fatalf("twenty requests put %d asks on the wire", total)
	}
}

// ── SCENARIO 3: IT COMES BACK ───────────────────────────────────────────────

// TestS3ItComesBack is the half of scenario 2 that a strike table could not do
// at all.
//
// The lane that went slow gets better. There is no penalty box in this design
// and no cooldown timer: what happened to the lane is that its belief moved and
// its variance is widening, so it earns its way back by being sampled once its
// spread has grown enough, by a sheet refresh saying it is healthy, or by a
// hedge landing on it. Within ten more requests and two refreshes, it must be
// chosen at least once — not preferred, just tried, because a router that can
// never revisit a judgement is a router that gets one bad minute wrong for the
// rest of the session.
func TestS3ItComesBack(t *testing.T) {
	ledger := e2ePrimed(t)
	e2eSkipWithoutAChooser(t, e2eMoment)

	stub := lanestub.New(e2eModel, e2eStubLanes()...)
	defer stub.Close()
	Default().SetSheet(e2eServeSheet(stub))
	router := newE2ERouter(stub, ledger)

	first := Default().Chooser().Choose(e2eTalk(e2eMoment))
	if len(first.Order) == 0 {
		t.Fatalf("nothing was ordered to break: %+v", first)
	}
	victim := first.Order[0]
	run := router.run(t, 20, victim, false)

	// The lane recovers. The stub is re-scripted whole, which is what a router
	// whose endpoint came back looks like from out here.
	stub.Model(e2eModel, e2eStubLanes()...)
	before := len(stub.Served())
	for beat := 0; beat < 2; beat++ {
		// The refresh is a BEAT and not part of a send: it happens between
		// requests, exactly where the law puts it.
		if err := Default().Sheet().Refresh(context.Background(), e2eModel); err != nil {
			t.Fatalf("refresh the sheet: %v", err)
		}
		for _, row := range Default().Sheet().Rows(e2eModel) {
			ledger.Prime(row, 4)
		}
		router.at = router.at.Add(2 * time.Minute)
		router.more(t, 5, "", false)
	}

	returned := 0
	for _, served := range stub.Served()[before:] {
		if served == victim {
			returned++
		}
	}
	if returned == 0 {
		t.Fatalf("%s recovered and was never tried again in ten requests and two refreshes "+
			"(it served %d of the first twenty); a belief that cannot be revisited is a penalty box",
			victim, run.servedBy(victim))
	}
}

// ── SCENARIO 4: QUALITY IS A GATE ───────────────────────────────────────────

// TestS4QualityIsAGate is the law that stops this whole mechanism from becoming
// a machine for shipping wrong answers cheaply.
//
// A lane returns tool-call JSON the decoder refuses, three times. Nothing about
// its speed changed and nothing about its price did; what changed is that its
// answers cannot be used. A router that weighed that against price would learn
// to prefer it. A gate cannot: the lane leaves the candidate set for work that
// needs a tool loop, and it stays out of a talk turn too unless its believed
// share of usable answers is still above what a talk turn needs.
//
// Both halves are asserted against the belief itself rather than against a
// number written here, so the scenario stays true whatever prior the ledger
// starts a lane's quality at.
func TestS4QualityIsAGate(t *testing.T) {
	ledger := e2ePrimed(t)
	e2eSkipWithoutAChooser(t, e2eMoment)

	// A tool-capable lane, because a lane that never survived the tool gate
	// could not demonstrate a quality one.
	const suspect = "DeepInfra"
	work := e2eWork(e2eMoment)
	if !e2eOrdered(Default().Chooser().Choose(work), suspect) {
		t.Skipf("%s is not in the order for work before anything went wrong, "+
			"so this scenario has nothing to remove", suspect)
	}

	for refusal := 0; refusal < 3; refusal++ {
		ledger.NoteOutcome(Outcome{
			ID:       ID{Model: e2eModel, Lane: suspect},
			Accepted: false,
			Reason:   "tool_json",
			At:       e2eMoment.Add(time.Duration(refusal) * time.Second),
		})
	}

	belief, known := ledger.Belief(ID{Model: e2eModel, Lane: suspect})
	if !known {
		t.Fatalf("three refused answers taught the ledger nothing about %s", suspect)
	}
	usable := belief.Quality.Mean()
	t.Logf("%s is believed to return a usable answer %.3f of the time after three refusals", suspect, usable)

	after := Default().Chooser().Choose(work)
	if e2eOrdered(after, suspect) {
		t.Fatalf("%s still leads work needing %.2f usable answers at a believed %.3f: "+
			"quality is a gate and never a weight (order %v)", suspect, work.QualityNeed, usable, after.Order)
	}

	// The talk gate is lower, so the same lane may legitimately survive it. What
	// may not happen is the two disagreeing with the belief they were both
	// asked about.
	talk := e2eTalk(e2eMoment)
	stillTalking := e2eOrdered(Default().Chooser().Choose(talk), suspect)
	if stillTalking != (usable >= talk.QualityNeed) {
		t.Fatalf("%s is believed usable %.3f of the time and a talk turn needs %.2f, "+
			"but the chooser %s it", suspect, usable, talk.QualityNeed,
			map[bool]string{true: "kept", false: "dropped"}[stillTalking])
	}
}

// ── SCENARIO 5: NO FETCH ON THE SEND PATH ───────────────────────────────────

// TestS5NoFetchOnTheSendPath is the law that makes every other scenario safe to
// ship.
//
// The sheet is worth having because it is a free prior. It stops being free the
// instant a request that is about to be sent goes and reads it: that is an HTTP
// round trip in front of a person's first token, and it is INVISIBLE — the call
// succeeds, the answer is right, and the surface has simply felt slower since
// some Tuesday. `internal/provider/lane_law_test.go` holds the same law by
// reading the transport's sources; this one holds it by counting, which is the
// version that survives somebody writing a wrapper with a different name.
//
// It is asserted while the router is doing everything it does — choosing,
// watching, folding sightings back in — because the interesting failure is a
// refresh hidden inside one of those, not one written at the top of a send.
func TestS5NoFetchOnTheSendPath(t *testing.T) {
	ledger := e2ePrimed(t)
	e2eSkipWithoutAChooser(t, e2eMoment)

	stub := lanestub.New(e2eModel, e2eStubLanes()...)
	defer stub.Close()
	Default().SetSheet(e2eServeSheet(stub))

	// One reading, on a beat, before anything is sent. That is the only fetch
	// this scenario permits and it is the only one it makes.
	if err := Default().Sheet().Refresh(context.Background(), e2eModel); err != nil {
		t.Fatalf("the beat could not read the sheet: %v", err)
	}
	fetched := stub.Sheets(e2eModel)
	if fetched != 1 {
		t.Fatalf("one beat read the sheet %d times", fetched)
	}

	router := newE2ERouter(stub, ledger)
	router.more(t, 20, "", false)

	if now := stub.Sheets(e2eModel); now != fetched {
		t.Fatalf("twenty sends read the sheet %d more times; a fetch on the send path is an "+
			"HTTP round trip in front of somebody's first token", now-fetched)
	}
	// And the choice was still informed, so the law was kept by reading memory
	// rather than by having nothing to read.
	if Default().Chooser().Choose(e2eTalk(router.at)).Empty() {
		t.Fatal("twenty sends later the chooser has no opinion, so the law above held vacuously")
	}
}

// ── THIS FILE'S FORTY LINES OF TRANSPORT ────────────────────────────────────

// e2eRouter is the contract in docs/ARCHITECTURE.md Decision 10, written out:
// ask, send, watch, fold back.
//
// It is not a reimplementation of `internal/provider` and it does not try to
// be. It is the smallest thing that can put a [Choice] on a wire the way the
// router reads it and turn what comes back into a [Sighting] — which is exactly
// the surface the scenarios above are about.
type e2eRouter struct {
	stub   *lanestub.Server
	ledger Ledger
	budget *Budget
	client *http.Client
	// at is the moment in the WORLD, which advances by what each request
	// actually took plus the time somebody spends reading the answer. The
	// scenarios need it because a belief ages, and ten minutes of ageing is
	// what lets a lane come back.
	at time.Time
	// answers is the world-time to a finished answer, per request, and hedges
	// is how many of them needed a second one.
	answers []time.Duration
	hedges  int
}

func newE2ERouter(stub *lanestub.Server, ledger Ledger) *e2eRouter {
	return &e2eRouter{
		stub:   stub,
		ledger: ledger,
		// The design's own budget: six hedges a minute, and a tenth of recent
		// spend. While lane L-C has not built the bucket this refuses
		// everything, which is the right empty answer — an un-budgeted hedge is
		// the one failure mode here that costs real money.
		budget: NewBudget(6, 0.1),
		client: &http.Client{},
		at:     e2eMoment,
	}
}

// run sends count requests, breaking victim from the sixth onward, and hands
// back the router itself so a scenario can read what it measured.
func (r *e2eRouter) run(t *testing.T, count int, victim string, pin bool) *e2eRouter {
	t.Helper()
	for sent := 0; sent < count; sent++ {
		if sent == 5 && victim != "" {
			// The lane goes slow. Four seconds to the first token is not a
			// failure and not a refusal — it is the ordinary way an endpoint
			// goes bad, and it is the case a strike table's fixed two seconds
			// was invented for and gets wrong for every other lane.
			r.stub.Model(e2eModel, e2eBrokenLanes(victim)...)
		}
		r.send(t, victim, pin)
	}
	return r
}

// more sends count further requests with the sheet as it now stands.
func (r *e2eRouter) more(t *testing.T, count int, pinned string, pin bool) {
	t.Helper()
	for sent := 0; sent < count; sent++ {
		r.send(t, pinned, pin)
	}
}

// send is one request, all the way through.
func (r *e2eRouter) send(t *testing.T, pinned string, pin bool) {
	t.Helper()
	request := e2eTalk(r.at)
	choice := Default().Chooser().Choose(request)
	if pin {
		// The control arm: what a session with a lane pinned does, and what
		// every session did before this package. Only is a demand.
		choice = Choice{Only: []string{pinned}}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	answer, err := r.race(ctx, choice, request)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	r.answers = append(r.answers, answer.total)
	if answer.hedged {
		r.hedges++
	}
	// A HEDGE IS A MEASUREMENT: both halves of a pair taught us something about
	// the lane they went to, so both are folded in.
	for _, sighting := range answer.sightings {
		r.ledger.Note(sighting)
	}
	// The world moves on by what this took plus the seconds somebody spends
	// reading four hundred tokens at reading speed.
	reading := float64(time.Second) * 400 / ReadRate
	r.at = r.at.Add(answer.total + time.Duration(reading))
}

// e2eAnswer is what one request came back with, in the world's units.
type e2eAnswer struct {
	total     time.Duration
	hedged    bool
	sightings []Sighting
}

// race sends the choice, watches the stream, and spends at most one hedge on
// it. The deadline comes from the choice, which computed it from the belief
// about the lane it expects to serve rather than from any constant here.
func (r *e2eRouter) race(ctx context.Context, choice Choice, request Request) (e2eAnswer, error) {
	primary := r.start(ctx, choice.Order, choice.Only, choice.Ignore, request)
	var alternate *e2eStream
	var hedgeTimer *time.Timer
	if choice.Deadline > 0 && choice.Alt != "" {
		hedgeTimer = time.NewTimer(e2eScaled(choice.Deadline))
		defer hedgeTimer.Stop()
	}

	answer := e2eAnswer{}
	var hedgeChannel <-chan time.Time
	if hedgeTimer != nil {
		hedgeChannel = hedgeTimer.C
	}
	for {
		var alternateDone <-chan e2eResult
		if alternate != nil {
			alternateDone = alternate.done
		}
		select {
		case <-primary.first:
			// The stream started talking before its deadline, so there is
			// nothing to rescue.
			hedgeChannel = nil

		case <-hedgeChannel:
			hedgeChannel = nil
			// The estimate a hedge is judged against: what a second whole
			// answer on the alternative would cost.
			estimate := e2eEstimate(choice.Alt, request)
			if !r.budget.Allow(r.at, estimate) {
				continue
			}
			answer.hedged = true
			alternate = r.start(ctx, nil, []string{choice.Alt}, nil, request)

		case result := <-primary.done:
			if result.err != nil && alternate != nil {
				continue
			}
			if alternate != nil {
				alternate.cancel()
			}
			return r.finish(answer, result, request)

		case result := <-alternateDone:
			if result.err != nil {
				continue
			}
			primary.cancel()
			return r.finish(answer, result, request)
		}
	}
}

// finish turns a finished stream into the answer and the sighting it earns.
func (r *e2eRouter) finish(answer e2eAnswer, result e2eResult, request Request) (e2eAnswer, error) {
	if result.err != nil {
		return answer, result.err
	}
	answer.total = e2eWorld(result.elapsed)
	answer.sightings = append(answer.sightings, Sighting{
		ID:           ID{Model: request.Model, Lane: result.lane},
		TTFT:         e2eWorld(result.ttft),
		Gen:          e2eWorld(result.gen),
		Tokens:       result.tokens,
		PromptTokens: request.PromptTokens,
		At:           r.at,
	})
	return answer, nil
}

// requests is how many asks this router put on the wire, over every lane.
func (r *e2eRouter) requests() int { return len(r.stub.Asks()) }

// servedBy is how many of this router's answers a lane wrote.
func (r *e2eRouter) servedBy(lane string) int {
	count := 0
	for _, served := range r.stub.Served() {
		if served == lane {
			count++
		}
	}
	return count
}

// e2eResult is one finished stream.
type e2eResult struct {
	lane                string
	ttft, gen, elapsed  time.Duration
	tokens, promptCount int
	cost                float64
	err                 error
}

// e2eStream is one stream in flight.
type e2eStream struct {
	first  chan string
	done   chan e2eResult
	cancel context.CancelFunc
}

// start puts one request on the wire and reads it in the background.
func (r *e2eRouter) start(ctx context.Context, order, only, ignore []string, request Request) *e2eStream {
	streamCtx, cancel := context.WithCancel(ctx)
	stream := &e2eStream{first: make(chan string, 1), done: make(chan e2eResult, 1), cancel: cancel}
	go func() {
		stream.done <- r.read(streamCtx, stream.first, order, only, ignore, request)
	}()
	return stream
}

// read is the wire itself: the preference in the router's own field names, and
// the stream back with the serving lane named on every chunk.
func (r *e2eRouter) read(ctx context.Context, first chan string, order, only, ignore []string, request Request) e2eResult {
	body := map[string]any{
		"model":      request.Model,
		"stream":     true,
		"max_tokens": request.MaxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": strings.Repeat("x", 4*request.PromptTokens)},
		},
	}
	preference := map[string]any{}
	if len(order) > 0 {
		preference["order"] = order
	}
	if len(only) > 0 {
		preference["only"] = only
	}
	if len(ignore) > 0 {
		preference["ignore"] = ignore
	}
	if len(preference) > 0 {
		body["provider"] = preference
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return e2eResult{err: err}
	}
	post, err := http.NewRequestWithContext(ctx, http.MethodPost, r.stub.URL()+"/chat/completions", strings.NewReader(string(encoded)))
	if err != nil {
		return e2eResult{err: err}
	}
	post.Header.Set("Content-Type", "application/json")

	began := time.Now()
	response, err := r.client.Do(post)
	if err != nil {
		return e2eResult{err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		drained, _ := io.ReadAll(response.Body)
		return e2eResult{err: fmt.Errorf("the router answered %d: %s", response.StatusCode, strings.TrimSpace(string(drained)))}
	}

	result := e2eResult{}
	var firstToken, lastToken time.Time
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			// A comment line is the router's own heartbeat: proof about the
			// PATH and never about the endpoint.
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Provider string `json:"provider"`
			Choices  []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int     `json:"prompt_tokens"`
				CompletionTokens int     `json:"completion_tokens"`
				Cost             float64 `json:"cost"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return e2eResult{err: err}
		}
		if chunk.Provider != "" {
			result.lane = chunk.Provider
		}
		if chunk.Usage != nil {
			result.tokens = chunk.Usage.CompletionTokens
			result.promptCount = chunk.Usage.PromptTokens
			result.cost = chunk.Usage.Cost
			continue
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}
		if firstToken.IsZero() {
			firstToken = time.Now()
			select {
			case first <- result.lane:
			default:
			}
		}
		lastToken = time.Now()
	}
	if err := scanner.Err(); err != nil {
		return e2eResult{err: err}
	}
	result.elapsed = time.Since(began)
	if !firstToken.IsZero() {
		result.ttft = firstToken.Sub(began)
		result.gen = lastToken.Sub(firstToken)
	}
	return result
}

// ── THE SHEET, SERVED OVER THE WIRE ─────────────────────────────────────────

// e2eWireSheet is a sheet that really does go to the network on a refresh and
// really does answer from memory otherwise. Two scenarios need that: one to
// count the fetches, one to let a recovered lane's row reach the belief.
type e2eWireSheet struct {
	base string
	rows map[string][]Row
}

// e2eServeSheet builds a sheet pointed at the fake router.
func e2eServeSheet(stub *lanestub.Server) Sheet {
	return &e2eWireSheet{base: stub.URL(), rows: map[string][]Row{}}
}

// Rows answers from memory, with no clock and no connection. That is the half
// the send path is allowed to call.
func (s *e2eWireSheet) Rows(model string) []Row { return s.rows[model] }

// Refresh goes to the network. It belongs to a beat and to nothing else.
func (s *e2eWireSheet) Refresh(ctx context.Context, model string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/models/"+model+"/endpoints", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the sheet answered %d", response.StatusCode)
	}
	var body struct {
		Data struct {
			Endpoints []struct {
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
				UptimeLast5m   float64 `json:"uptime_last_5m"`
				LatencyLast30m struct {
					P50, P75, P90, P99 float64
				} `json:"latency_last_30m"`
				ThroughputLast30m struct {
					P50, P75, P90, P99 float64
				} `json:"throughput_last_30m"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return err
	}
	rows := make([]Row, 0, len(body.Data.Endpoints))
	for _, endpoint := range body.Data.Endpoints {
		rows = append(rows, Row{
			ID: ID{Model: model, Lane: endpoint.ProviderName},
			Facts: Facts{
				Tools: endpoint.SupportsToolChoice.Function, Quant: endpoint.Quantization,
				MaxOut: endpoint.MaxCompletionTokens, Context: endpoint.ContextLength,
				Uptime5m:   endpoint.UptimeLast5m,
				PriceIn:    e2eMoney(endpoint.Pricing.Prompt),
				PriceOut:   e2eMoney(endpoint.Pricing.Completion),
				PriceCache: e2eMoney(endpoint.Pricing.InputCacheRead),
			},
			TTFTp50: endpoint.LatencyLast30m.P50, TTFTp75: endpoint.LatencyLast30m.P75,
			TTFTp90: endpoint.LatencyLast30m.P90, TTFTp99: endpoint.LatencyLast30m.P99,
			Ratep50: endpoint.ThroughputLast30m.P50, Ratep75: endpoint.ThroughputLast30m.P75,
			Ratep90: endpoint.ThroughputLast30m.P90, Ratep99: endpoint.ThroughputLast30m.P99,
		})
	}
	s.rows[model] = rows
	return nil
}

// e2eMoney reads the router's per-token price, which travels as a string with
// more precision than a JSON float survives.
func e2eMoney(price string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(price), 64)
	if err != nil {
		return 0
	}
	return value
}

// ── SMALL THINGS THE SCENARIOS SAY OUT LOUD ─────────────────────────────────

// e2eBrokenLanes is the sheet with one lane taking four seconds to say its
// first word. Everything else about it — its price, its rate, its published
// percentiles — is unchanged, because that is what makes it the interesting
// case: the sheet still says the lane is quick.
func e2eBrokenLanes(victim string) []lanestub.Lane {
	lanes := e2eStubLanes()
	for index := range lanes {
		if lanes[index].Name == victim {
			lanes[index].TTFT = e2eScaled(4 * time.Second)
		}
	}
	return lanes
}

// e2eEstimate is roughly what a whole answer on one lane would cost, which is
// what a hedge is weighed against.
func e2eEstimate(lane string, request Request) float64 {
	for _, candidate := range e2eSheet {
		if candidate.name != lane {
			continue
		}
		return candidate.priceIn()*float64(request.PromptTokens) +
			candidate.priceOut()*float64(request.Visible+request.Hidden)
	}
	return 0
}

// e2eP90 is the ninetieth percentile of a set of durations, by the nearest-rank
// method: the smallest value at or above which nine tenths of the answers sit.
// Nearest rank rather than an interpolation because twenty samples is few
// enough that an interpolated percentile is mostly an opinion about two of them.
func e2eP90(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	rank := int(math.Ceil(0.9*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	return sorted[rank]
}

// e2eOrdered reports whether a choice would send a request to lane at all.
func e2eOrdered(choice Choice, lane string) bool {
	for _, name := range choice.Order {
		if name == lane {
			return true
		}
	}
	for _, name := range choice.Only {
		if name == lane {
			return true
		}
	}
	return false
}

// e2ePriceOf is what the frontier said one lane would cost, zero when it is not on
// the frontier at all.
func e2ePriceOf(frontier []Scored, lane string) float64 {
	for _, scored := range frontier {
		if scored.ID.Lane == lane {
			return scored.Price
		}
	}
	return 0
}

// e2eSortedNames is a set of lane names in a stable order, so a failure message
// reads the same twice.
func e2eSortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ── PROVING THE INSTRUMENT BEFORE THERE IS ANYTHING TO MEASURE ──────────────

// e2ePinChooser is a chooser with exactly one opinion, held forever.
//
// It exists so that the five scenarios above are not the only thing standing
// between this file and a silent failure. Every one of them skips while the
// real chooser has no opinion, and a file that only ever skips is a file whose
// own wiring nobody has checked — so when it finally stops skipping, in a wave
// where four other lanes changed at once, a broken assertion here would read as
// a broken chooser there. This double takes the skip away from one test.
type e2ePinChooser struct {
	order    []string
	deadline time.Duration
	alt      string
}

func (c *e2ePinChooser) Choose(Request) Choice {
	return Choice{Order: c.order, Deadline: c.deadline, Alt: c.alt}
}

// e2eRecordingLedger keeps what it was told and nothing else. It is the empty
// ledger with a memory, which is all this check needs and deliberately less
// than a belief.
type e2eRecordingLedger struct {
	Ledger
	sightings []Sighting
}

func (l *e2eRecordingLedger) Note(sighting Sighting) { l.sightings = append(l.sightings, sighting) }

// TestTheInstrumentInThisFileWorksBeforeAnySeamDoes drives the whole of
// [e2eRouter] against a chooser that cannot be wrong, so that everything the
// scenarios rest on — the preference reaching the wire in the router's own
// field names, the serving lane read off the chunks, the first token timed
// apart from the writing, the scaling between the wire's clock and the world's,
// the sighting folded back, and the sheet decoded over HTTP — is known to work
// on the day it was written rather than in the wave it is first needed.
func TestTheInstrumentInThisFileWorksBeforeAnySeamDoes(t *testing.T) {
	t.Cleanup(Default().Reset)
	Default().Reset()
	ledger := &e2eRecordingLedger{Ledger: Default().Ledger()}
	Default().SetLedger(ledger)
	Default().SetChooser(&e2ePinChooser{order: []string{"Cloudflare", "Baidu"}})

	stub := lanestub.New(e2eModel, e2eStubLanes()...)
	defer stub.Close()
	Default().SetSheet(e2eServeSheet(stub))

	router := newE2ERouter(stub, ledger)
	router.run(t, 8, "Cloudflare", false)

	// The preference really did travel, and the stub really did honour it.
	asks := stub.Asks()
	if len(asks) != 8 {
		t.Fatalf("eight requests put %d asks on the wire", len(asks))
	}
	if len(asks[0].Order) != 2 || asks[0].Order[0] != "Cloudflare" {
		t.Fatalf("the preference on the wire is %+v", asks[0])
	}
	for index, served := range stub.Served() {
		if served != "Cloudflare" {
			t.Fatalf("ask %d was ordered to Cloudflare and %s answered", index, served)
		}
	}

	// The lane went slow from the sixth, and the measurements say so in the
	// world's own units rather than the wire's.
	if len(ledger.sightings) != 8 {
		t.Fatalf("eight answers left %d sightings", len(ledger.sightings))
	}
	early, late := ledger.sightings[0], ledger.sightings[7]
	if early.ID.Lane != "Cloudflare" || late.ID.Lane != "Cloudflare" {
		t.Fatalf("a sighting was credited to %q and %q", early.ID.Lane, late.ID.Lane)
	}
	if early.TTFT < 400*time.Millisecond || early.TTFT > 2*time.Second {
		t.Fatalf("a lane whose median first token is 768 ms was timed at %v", early.TTFT)
	}
	if late.TTFT < 3*time.Second {
		t.Fatalf("a lane scripted to take four seconds was timed at %v", late.TTFT)
	}
	if early.Rate() <= 0 {
		t.Fatalf("an answer of %d tokens over %v rated at nothing", early.Tokens, early.Gen)
	}
	// The ninetieth percentile really does move when a lane goes bad, which is
	// the statistic every claim in this file is made on.
	if e2eP90(router.answers) < 3*time.Second {
		t.Fatalf("three of eight answers took four seconds and the p90 is %v", e2eP90(router.answers))
	}

	// And the sheet decodes over the wire, on a beat.
	if err := Default().Sheet().Refresh(context.Background(), e2eModel); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	rows := Default().Sheet().Rows(e2eModel)
	if len(rows) != len(e2eSheet) {
		t.Fatalf("the sheet came back with %d rows, want %d", len(rows), len(e2eSheet))
	}
	for _, row := range rows {
		if !row.Known() {
			t.Fatalf("%s came back with no percentiles to fit a prior from: %+v", row.ID.Lane, row)
		}
		if row.Facts.PriceOut <= 0 {
			t.Fatalf("%s came back with no tariff: %+v", row.ID.Lane, row.Facts)
		}
	}
}
