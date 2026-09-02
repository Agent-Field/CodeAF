package provider

import (
	"strings"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE REFUSAL THAT COULD NEVER ACT ────────────────────────────────────────
//
// Every test in this file is one law of issue #266, and all of them are proved
// against a REAL refusal on a real wire: the stub publishes a lane on its
// endpoints page and will not serve it, which is the disagreement the reported
// run was made of, and the body it answers with is the router's own
// `…but your request's provider.only preference permits only: …`.
//
// Nothing here synthesises an error value. The whole defect was that a refusal
// arriving through the transport was classified three different ways by three
// pieces of code, so a test that handed one of them a hand-built [APIError]
// would be testing the piece rather than the path.

// refusalLanes is the scenario every test below is staged on: five tool-capable
// machines on the sheet, two of which the router will not serve.
//
// Ghost and Phantom are the two the sheet publishes and the wire refuses; Haven
// is the machine that finally answers. The two behind it are the rest of the
// model's endpoints — real, servable, and out of the frontier this scenario
// hands the race, which is what keeps the walk's order a fact rather than a
// draw from the chooser.
func refusalLanes() []lanestub.Lane {
	fast := lanestub.Profile{TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 6, Tools: true, Quant: "fp8"}
	return []lanestub.Lane{
		{Name: "Ghost", SheetOnly: true, Profile: fast},
		{Name: "Phantom", SheetOnly: true, Profile: fast},
		{Name: "Haven", Profile: fast},
		{Name: "Harbor", Profile: fast},
		{Name: "Hollow", Profile: fast},
	}
}

// demanding is the choice a request goes out on when something has named one
// machine: the person's own pin, or a lane picker's row. The frontier behind it
// is what the race may walk to, in order.
func demanding(model, only string, frontier ...string) lanes.Choice {
	choice := lanes.Choice{Only: []string{only}}
	for _, lane := range frontier {
		choice.Frontier = append(choice.Frontier, lanes.Scored{
			ID: lanes.ID{Model: model, Lane: lane}, TTFT: 3, Rate: 4000, Price: 0.01,
		})
	}
	return choice
}

// rescueLog collects the rescue news a race posts while it is still running,
// which is the only moment any of it is true.
type rescueLog struct {
	mu   sync.Mutex
	news []RescueNews
}

func (l *rescueLog) watch(report *HedgeReport) {
	report.OnHedgeStart(func(news RescueNews) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.news = append(l.news, news)
	})
}

func (l *rescueLog) all() []RescueNews {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]RescueNews(nil), l.news...)
}

// A REFUSAL IS TERMINAL FOR THAT LANE AT ONCE. This is the whole of issue #266
// in one run: the strike fires, the walk moves the same second, the lane is
// never asked again, and what the surface is told is the word the wire said.
//
// The measured failure it replaces: six 404s of this exact shape in one chat
// run, the same machine chosen three separate times, and the whole thing drawn
// as `· slow · trying nextbit…` until it aged off the screen ten minutes later.
func TestARefusedLaneIsStruckTheWalkMovesAndTheScreenIsToldRefused(t *testing.T) {
	rig := newLaneRig(t, "refusal/permits-only", refusalLanes()...)
	log := &rescueLog{}
	report := &HedgeReport{}
	log.watch(report)

	// The request demands Ghost, and the frontier behind it offers Phantom and
	// then Haven — so the walk's order is stated rather than sampled, and what
	// this test proves is what the transport does with a refusal rather than
	// which machine a chooser happened to draw.
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, demanding(rig.model, "Ghost", "Ghost", "Phantom", "Haven"))

	began := time.Now()
	answer, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatalf("the turn died on a refusal the walk was supposed to absorb: %v", err)
	}
	if answer == nil {
		t.Fatal("no answer came back from the machine the walk reached")
	}

	// 1. THE REFUSED LANE IS NEVER ASKED AGAIN. One ask each, and the run kept
	//    going: this is the count that used to read three.
	for _, refused := range []string{"Ghost", "Phantom"} {
		if asked := rig.server.Requests(refused); asked != 1 {
			t.Fatalf("%s was asked %d times, want exactly one — a refusal is terminal", refused, asked)
		}
	}
	if served := rig.server.Requests("Haven"); served != 1 {
		t.Fatalf("Haven answered %d requests, want the one the walk sent it", served)
	}

	// 2. THE NEXT REQUEST LEFT AT ONCE AND DEMANDED SOMEBODY ELSE. The ladder
	//    used to climb six rungs still pinned to the machine that had said no.
	asks := rig.server.Asks()
	if len(asks) < 3 {
		t.Fatalf("%d requests went out, want the demand, the walk and the answer", len(asks))
	}
	if !demandedOnly(asks[0], "Ghost") {
		t.Fatalf("the first ask demanded %v, want Ghost", asks[0].Only)
	}
	for _, later := range asks[1:] {
		if namesLane(later.Only, "Ghost") {
			t.Fatalf("a request after the refusal still demanded Ghost: %v", later.Only)
		}
	}
	if gap := asks[1].At.Sub(asks[0].At); gap > 2*time.Second {
		t.Fatalf("the walk took %v to leave, want under two seconds", gap)
	}
	// AND NOTHING SAT ANYWHERE. The reported run had a wait clock counting
	// twenty-six minutes; a walk that acts on the refusal has no gap in it at
	// all.
	for index := 1; index < len(asks); index++ {
		if gap := asks[index].At.Sub(asks[index-1].At); gap > 5*time.Second {
			t.Fatalf("a %v gap opened between request %d and %d while the turn was working", gap, index-1, index)
		}
	}
	if spent := time.Since(began); spent > 5*time.Second {
		t.Fatalf("the whole turn took %v, want a run with no waiting in it", spent)
	}

	// 3. THE SERVING SET LEARNED. This is what stops the lane coming back on
	//    the next turn, which no amount of per-race bookkeeping could.
	for _, refused := range []string{"Ghost", "Phantom"} {
		if lanes.Serves(rig.model, refused) {
			t.Fatalf("%s is still believed to serve %s after the router refused it", refused, rig.model)
		}
	}
	if !lanes.Serves(rig.model, "Haven") {
		t.Fatal("Haven was written out of the serving set, and it answered")
	}

	// 4. THE SCREEN WAS TOLD WHAT THE WIRE SAID. Two rescues, both refused
	//    rather than slow, and the claim about Phantom withdrawn when Phantom
	//    itself died.
	news := log.all()
	if len(news) != 3 {
		t.Fatalf("the surface heard %+v, want trying-phantom, phantom-failed, trying-haven", news)
	}
	if news[0].Alt != "Phantom" || news[0].Reason != RescueRefused || news[0].Failed {
		t.Fatalf("the first rescue reads %+v, want a refusal walking to Phantom", news[0])
	}
	if news[1].Alt != "Phantom" || !news[1].Failed {
		t.Fatalf("the retraction reads %+v, want the Phantom claim withdrawn", news[1])
	}
	if news[2].Alt != "Haven" || news[2].Reason != RescueRefused || news[2].Failed {
		t.Fatalf("the second rescue reads %+v, want a refusal walking to Haven", news[2])
	}
}

// AND THE LADDER DROPS THE PIN ON ITS FIRST RUNG. When the race has nowhere
// left to walk, the demand itself is the field that emptied the endpoint set,
// and rung one is what takes it off — which it could not do, because `only` was
// on neither the list that offered the rung nor the list that cleared it.
func TestRungOneDropsTheDemandWhenThereIsNowhereLeftToWalk(t *testing.T) {
	rig := newLaneRig(t, "refusal/last-arm",
		lanestub.Lane{Name: "Ghost", SheetOnly: true, Profile: lanestub.Profile{
			TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 6, Tools: true, Quant: "fp8"}},
		lanestub.Lane{Name: "Haven", Profile: lanestub.Profile{
			TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 6, Tools: true, Quant: "fp8"}},
	)
	// A frontier of one is a race with nowhere to walk, which is the state the
	// ladder is for: the evidence is now about the request rather than about
	// the endpoints.
	ctx := WithLaneChoice(talking(), demanding(rig.model, "Ghost", "Ghost"))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the ladder did not recover the refusal: %v", err)
	}
	asks := rig.server.Asks()
	if len(asks) != 2 {
		t.Fatalf("%d requests went out, want the demand and the relaxed retry", len(asks))
	}
	if !demandedOnly(asks[0], "Ghost") {
		t.Fatalf("the first ask demanded %v, want Ghost", asks[0].Only)
	}
	if len(asks[1].Only) != 0 {
		t.Fatalf("the retry still demanded %v — rung one is supposed to take the whole provider object off", asks[1].Only)
	}
	if rig.server.Requests("Haven") != 1 {
		t.Fatal("the relaxed retry did not reach the machine that serves the model")
	}
}

// THE LANE A REFUSAL IS ABOUT IS THE ONE OUR REQUEST DEMANDED. The router's
// sentence names two sets of machines in English and this process reads neither
// of them: it reads the `provider.only` it wrote itself.
//
// The proof is a refusal whose prose names a lane the request never asked for.
// A build that parsed the words would strike that one; this one strikes the
// machine it actually demanded, and leaves the innocent lane alone.
func TestTheStruckLaneComesFromOurOwnDemandAndNotFromTheRoutersProse(t *testing.T) {
	rig := newLaneRig(t, "refusal/prose", refusalLanes()...)
	ctx := WithLaneChoice(talking(), demanding(rig.model, "Ghost", "Ghost", "Haven"))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the walk did not absorb the refusal: %v", err)
	}
	// The stub's refusal body names Haven, Harbor and Hollow as the machines it
	// DOES serve — the same sentence shape the live router sends — and Haven is
	// the one that went on to answer this very turn.
	if !lanes.Serves(rig.model, "Haven") {
		t.Fatal("a lane the refusal's prose merely mentioned was struck")
	}
	if lanes.Serves(rig.model, "Ghost") {
		t.Fatal("the machine the request demanded was not struck")
	}
}

// A REFUSAL WITH NO DEMAND ON IT STRIKES NOBODY. It is the other half of the
// same law, and it is what keeps the strike from emptying the ledger one
// malformed request at a time: with no `provider.only`, an empty endpoint set
// is a fact about the request's shape and there is no machine to blame for it.
func TestARefusalThatDemandedNoMachineStrikesNothing(t *testing.T) {
	rig := newLaneRig(t, "refusal/undemanded",
		lanestub.Lane{Name: "Haven", Profile: lanestub.Profile{
			TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 6, Tools: true, Quant: "fp8"}},
	)
	if _, err := rig.client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("an ordinary answer failed: %v", err)
	}
	if !lanes.Serves(rig.model, "Haven") {
		t.Fatal("a request that demanded nothing struck a lane anyway")
	}
}

// demandedOnly reports whether an ask demanded exactly this one machine.
func demandedOnly(ask lanestub.Ask, lane string) bool {
	return len(ask.Only) == 1 && strings.EqualFold(ask.Only[0], lane)
}

// namesLane reports whether a preference list names this machine.
func namesLane(list []string, lane string) bool {
	for _, name := range list {
		if strings.EqualFold(name, lane) {
			return true
		}
	}
	return false
}

// ── THE LADDER'S FIRST RUNG, AS A RULE RATHER THAN AS AN ACCIDENT ───────────

// EVERY FIELD THAT CAN EMPTY THE ENDPOINT SET COMES OFF ON RUNG ONE, and the
// two that could not were the two that made the reported outage: `only` names
// the machines a request may go to and `allow_fallbacks: false` forbids every
// other, which together are the narrowest filter this process ever sends.
//
// It is asserted with a sort word and an order still on the object, because
// those are what make the difference visible: a preference with nothing left in
// it is dropped whole, and a rung that only worked through THAT path would go
// on working right up until somebody left a sort word on a pinned request.
func TestRungOneTakesOffEveryFieldThatCanEmptyTheEndpointSet(t *testing.T) {
	no, yes := false, true
	prefs := &providerPrefs{
		Sort:              "latency",
		Order:             []string{"Haven"},
		Only:              []string{"Ghost"},
		Ignore:            []string{"Hollow"},
		AllowFallbacks:    &no,
		RequireParameters: &yes,
		MaxPrice:          &maxPrice{Prompt: 1, Completion: 2},
	}
	if !prefs.narrowing() {
		t.Fatal("an object carrying every filter there is says it narrows nothing")
	}
	relaxed := relaxedPreferences(prefs)
	if relaxed == nil {
		t.Fatal("a preference that still ranks was dropped whole")
	}
	if relaxed.narrowing() {
		t.Fatalf("rung one left %+v behind, and every field on it can empty the set", relaxed)
	}
	if relaxed.Sort != "latency" || len(relaxed.Order) != 1 {
		t.Fatalf("rung one took the ranking off too: %+v", relaxed)
	}
	// AND THE ORIGINAL IS UNTOUCHED, because the ladder retries a copy and the
	// caller's own object is still describing the request that was refused.
	if len(prefs.Only) != 1 {
		t.Fatal("relaxing one request edited the preference it was relaxing")
	}
}

// AND THE RUNG IS OFFERED FOR A DEMAND THE LEDGER DID NOT MAKE. A rescue's lane
// is added after the ledger's own preferences are assembled, so a plan built
// from the ledger's half alone could not see it — which is how a pinned request
// came to have no first rung at all.
func TestTheFirstRungIsOfferedForARescuesOwnDemand(t *testing.T) {
	rig := newLaneRig(t, "refusal/rung-offered",
		lanestub.Lane{Name: "Haven", Profile: lanestub.Profile{
			TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 6, Tools: true, Quant: "fp8"}},
	)
	// Routing off is a build that sends no preference object of its own, so the
	// demand below is the ONLY thing on the wire that can narrow anything.
	rig.client.config.Routing = StaticRouting(RoutingOff)
	plan := rig.client.relaxationPlan(&ai.Request{Messages: userMessages("hi")},
		callKnobs{hedgeLane: "Ghost"}, rig.model)
	if len(plan) == 0 || plan[0].bit != relaxEndpointFilter {
		t.Fatalf("the plan for a pinned request is %+v, want the endpoint filter first", plan)
	}
}
