package provider

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE JOIN, END TO END ────────────────────────────────────────────────────
//
// The unit tests of the choice live in `internal/lane`, where they belong: the
// arithmetic is pure and none of it needs a socket. What cannot be tested there
// is the JOIN — that the preference the chooser formed reaches the wire as
// `provider.order`, and that the answer which comes back reaches the ledger as
// a sighting. Both directions cross the boundary exactly once and both are
// tested here against `internal/lane/lanestub`, a real HTTP router with lanes
// of a scripted speed.

// recordingLedger is a ledger primed with fixed beliefs that also keeps what it
// is told. It is the instrument for both directions at once.
type recordingLedger struct {
	mu       sync.Mutex
	beliefs  []lanes.Belief
	sighted  []lanes.Sighting
	outcomes []lanes.Outcome
}

func (l *recordingLedger) Note(sighting lanes.Sighting) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sighted = append(l.sighted, sighting)
}

func (l *recordingLedger) NoteOutcome(outcome lanes.Outcome) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.outcomes = append(l.outcomes, outcome)
}

func (l *recordingLedger) Prime(lanes.Row, float64) {}

func (l *recordingLedger) Belief(id lanes.ID) (lanes.Belief, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, belief := range l.beliefs {
		if belief.ID == id {
			return belief, true
		}
	}
	return lanes.Belief{}, false
}

func (l *recordingLedger) Beliefs(model string) []lanes.Belief {
	l.mu.Lock()
	defer l.mu.Unlock()
	var found []lanes.Belief
	for _, belief := range l.beliefs {
		if belief.ID.Model == model {
			found = append(found, belief)
		}
	}
	return found
}

func (l *recordingLedger) sightings() []lanes.Sighting {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]lanes.Sighting(nil), l.sighted...)
}

// laneBelief is one sharply-measured lane: a median first-token wait in
// milliseconds, a median rate, and an output tariff in dollars per million
// tokens. The variances are small on purpose — this test is about the wiring,
// and a wide belief would let the sampling reorder what it is asserting.
func laneBelief(model, lane string, ttft, rate, perMillion float64) lanes.Belief {
	return lanes.Belief{
		ID: lanes.ID{Model: model, Lane: lane},
		Facts: lanes.Facts{
			Tools:      true,
			Quant:      "fp8",
			MaxOut:     128_000,
			Context:    256_000,
			Uptime5m:   100,
			PriceIn:    perMillion / 4 / 1_000_000,
			PriceOut:   perMillion / 1_000_000,
			PriceCache: perMillion / 40 / 1_000_000,
			Caches:     true,
		},
		TTFT:    lanes.Posterior{X: math.Log(ttft), P: 0.004},
		Rate:    lanes.Posterior{X: math.Log(rate), P: 0.004},
		Quality: lanes.Beta{A: 39, B: 1},
		At:      time.Now(),
	}
}

// stubbedRouter starts a fake router with three lanes and a client pointed at
// it. The client's own model is spelled `openrouter/…` because that is how the
// transport decides it is talking to a router at all.
func stubbedRouter(t *testing.T) (*Client, *lanestub.Server, string) {
	t.Helper()
	const model = "openrouter/scripted-model"
	server := lanestub.New(model,
		lanestub.Lane{Name: "quicksilver", Profile: lanestub.Profile{
			TTFT: 20 * time.Millisecond, Rate: 400, Tokens: 40, Tools: true}},
		lanestub.Lane{Name: "brass", Profile: lanestub.Profile{
			TTFT: 30 * time.Millisecond, Rate: 300, Tokens: 40, Tools: true}},
		lanestub.Lane{Name: "molasses", Profile: lanestub.Profile{
			TTFT: 40 * time.Millisecond, Rate: 200, Tokens: 40, Tools: true}},
	)
	t.Cleanup(server.Close)
	client, err := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: server.URL(),
		Model:   model,
		Routing: StaticRouting(RoutingLatency),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Its own strike ledger: the shared one is process-wide, and a lane another
	// test in this package taught it about would arrive here as an order nobody
	// in this test asked for.
	client.velocity = newVelocityLedger()
	return client, server, model
}

// primed installs a ledger holding the three lanes and puts the registry back
// afterwards, which every test that touches a seam must do.
func primed(t *testing.T, model string, beliefs ...lanes.Belief) *recordingLedger {
	t.Helper()
	ledger := &recordingLedger{beliefs: beliefs}
	lanes.Default().SetLedger(ledger)
	lanes.ForgetPrefixes()
	t.Cleanup(func() {
		lanes.Default().Reset()
		lanes.ForgetPrefixes()
	})
	return ledger
}

// TestTheBeliefsOrderIsWhatGoesOnTheWire is the first direction of the join.
//
// The chooser's answer is a preference and this is where it becomes one: the
// lanes it ranked arrive as `provider.order`, the sort word comes off because
// the order already says what to try first, fallbacks stay on so that a slow
// answer still beats no answer, and the router serves the lane at the head of
// the list.
func TestTheBeliefsOrderIsWhatGoesOnTheWire(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "molasses", 1500, 40, 0.20),
		laneBelief(model, "brass", 900, 60, 0.30),
		laneBelief(model, "quicksilver", 400, 70, 0.25),
	)

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	asks := server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	ask := asks[0]
	if len(ask.Order) == 0 {
		t.Fatalf("no order on the wire: %+v", ask)
	}
	if ask.Order[0] != "quicksilver" {
		t.Fatalf("order = %v, want the lane believed quickest and cheap in front", ask.Order)
	}
	if ask.Sort != "" {
		t.Fatalf("sort = %q rode beside an order, which the router reads as a second opinion", ask.Sort)
	}
	if served := server.Served(); len(served) == 0 || served[0] != "quicksilver" {
		t.Fatalf("%v answered, want the head of the order", served)
	}
	// The same preference, asked for directly: the wire carries the choice
	// rather than a second ranking computed somewhere else.
	choice := lanes.Default().Chooser().Choose(lanes.Request{
		Model:       model,
		Visible:     talkTokens,
		QualityNeed: talkQuality,
		ValueOfTime: lanes.AttentionValue,
		Horizon:     defaultHorizon,
		Now:         time.Now(),
	})
	if len(choice.Order) == 0 || choice.Order[0] != ask.Order[0] {
		t.Fatalf("the wire asked for %v and the chooser wanted %v", ask.Order, choice.Order)
	}
}

// TestAnAnsweredRequestTeachesTheLedger is the other direction: the stream that
// came back is a measurement, and it reaches the belief with the lane that
// served it, the wait before its first token, and how long its prompt was.
func TestAnAnsweredRequestTeachesTheLedger(t *testing.T) {
	client, _, model := stubbedRouter(t)
	ledger := primed(t, model,
		laneBelief(model, "quicksilver", 400, 70, 0.25),
		laneBelief(model, "brass", 900, 60, 0.30),
	)

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	sightings := ledger.sightings()
	if len(sightings) == 0 {
		t.Fatal("an answered request taught the belief nothing")
	}
	sighting := sightings[len(sightings)-1]
	if sighting.ID.Lane != "quicksilver" || sighting.ID.Model != model {
		t.Fatalf("the sighting was credited to %+v", sighting.ID)
	}
	if sighting.TTFT <= 0 {
		t.Fatalf("the sighting carried no first-token wait: %+v", sighting)
	}
	if sighting.Tokens <= 0 {
		t.Fatalf("the sighting carried no answer length: %+v", sighting)
	}
	if sighting.PromptTokens <= 0 {
		t.Fatalf("the sighting carried no prompt length, so a long prefill cannot be told from a slow lane: %+v", sighting)
	}
	if sighting.At.IsZero() {
		t.Fatal("the sighting was not stamped with a moment")
	}
}

// TestAnEmptyLedgerLeavesTheRequestExactlyAsItWas is the law this whole file is
// written under. On a machine that has never used a model there is no belief,
// the chooser says nothing, and every part of the request is what it was before
// this package existed — the sort word above all, because that is what the
// router falls back on.
func TestAnEmptyLedgerLeavesTheRequestExactlyAsItWas(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model)

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	asks := server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	if asks[0].Sort != "latency" {
		t.Fatalf("sort = %q, want the ask a person waiting has always had", asks[0].Sort)
	}
	if len(asks[0].Order) != 0 {
		t.Fatalf("order = %v was invented from no belief at all", asks[0].Order)
	}
}

// TestWhoIsWaitingDecidesWhatASecondIsWorth pins the λ plumbing at its own
// seam: the option states it, the routing row overrides it, and a call that
// says nothing follows who is waiting.
func TestWhoIsWaitingDecidesWhatASecondIsWorth(t *testing.T) {
	client, _, _ := stubbedRouter(t)

	if got := client.laneValueOfTime(RoutingLatency, callKnobs{intent: IntentInteractive}); got != lanes.AttentionValue {
		t.Fatalf("a turn somebody is watching is worth %v, want %v", got, lanes.AttentionValue)
	}
	if got := client.laneValueOfTime(RoutingLatency, callKnobs{intent: IntentBackground}); got != 0 {
		t.Fatalf("a call nobody is waiting on is worth %v, want price to win outright", got)
	}
	stated := knobsFrom(WithValueOfTime(context.Background(), 12))
	if got := client.laneValueOfTime(RoutingLatency, stated); got != 12 {
		t.Fatalf("a call site that stated λ got %v", got)
	}
	if got := client.laneValueOfTime(RoutingPrice, stated); got != 0 {
		t.Fatalf("the price row bought speed at %v seconds to the dollar", got)
	}
	silent := knobsFrom(context.Background())
	if seconds, said := ValueOfTimeFrom(context.Background()); said || seconds != 0 {
		t.Fatalf("a bare context claimed λ = %v (said: %v)", seconds, said)
	}
	if silent.lambda.said {
		t.Fatal("a call that said nothing about λ was recorded as having said something")
	}
	if calls := knobsFrom(WithCallHorizon(context.Background(), 7)).horizon; calls != 7 {
		t.Fatalf("the horizon arrived as %d, want the seven calls the caller expects", calls)
	}
}

// TestTheRequestTheChooserSeesIsTheRequestBeingSent keeps the two shapes of
// call apart, because it is the whole of what makes a tool loop pay for
// throughput and a talk turn not.
func TestTheRequestTheChooserSeesIsTheRequestBeingSent(t *testing.T) {
	client, _, model := stubbedRouter(t)
	ceiling := 4096
	request := &ai.Request{
		Model:     model,
		MaxTokens: &ceiling,
		Messages:  userMessages("a question worth about ten tokens of prompt"),
		Tools:     []ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}},
	}

	talk := client.laneRequest(model, callKnobs{intent: IntentInteractive}, request, lanes.AttentionValue)
	if talk.Visible != talkTokens || talk.Hidden != 0 {
		t.Fatalf("a turn somebody is watching was shaped as %d visible and %d hidden", talk.Visible, talk.Hidden)
	}
	if talk.QualityNeed != talkQuality || !talk.Tools || talk.MaxTokens != ceiling {
		t.Fatalf("the ask lost part of the request: %+v", talk)
	}
	if talk.PromptTokens <= 0 {
		t.Fatalf("the prompt was estimated at %d tokens", talk.PromptTokens)
	}
	work := client.laneRequest(model, callKnobs{intent: IntentBackground}, request, 0)
	if work.Visible != 0 || work.Hidden != workTokens || work.QualityNeed != workQuality {
		t.Fatalf("a call nobody is waiting on was shaped as %+v", work)
	}
}
