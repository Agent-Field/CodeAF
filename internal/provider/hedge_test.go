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

// ── THE SCENARIOS THE WATCH EXISTS FOR ──────────────────────────────────────
//
// Every test here is a whole request against the fake router: a real client, a
// real stream, a real cancel. They are scripted in MILLISECONDS and read as
// seconds — the stub's own note says the fast clock is one timeline and
// therefore meaningless where two requests overlap, which is the whole point of
// a hedge, so the scale is 1:100 against the wall clock instead. A first token
// scripted at sixty milliseconds is a lane that takes six seconds; a deadline of
// twelve is one of 1.2 seconds. The arithmetic under test is ratios, so the
// scale changes nothing about what is being proved and takes the suite from
// half a minute to a fifth of a second.

// scriptedLedger believes exactly what a test says it believes and records what
// it is told.
type scriptedLedger struct {
	mu        sync.Mutex
	beliefs   map[lanes.ID]lanes.Belief
	sightings []lanes.Sighting
}

func (l *scriptedLedger) Note(sighting lanes.Sighting) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sightings = append(l.sightings, sighting)
}

func (l *scriptedLedger) NoteOutcome(lanes.Outcome) {}

func (l *scriptedLedger) Prime(lanes.Row, float64) {}

func (l *scriptedLedger) Belief(id lanes.ID) (lanes.Belief, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	belief, ok := l.beliefs[id]
	return belief, ok
}

func (l *scriptedLedger) Beliefs(string) []lanes.Belief { return nil }

func (l *scriptedLedger) noted() []lanes.Sighting {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]lanes.Sighting(nil), l.sightings...)
}

func (l *scriptedLedger) sightingFor(lane string) (lanes.Sighting, bool) {
	for _, sighting := range l.noted() {
		if sighting.ID.Lane == lane {
			return sighting, true
		}
	}
	return lanes.Sighting{}, false
}

// laneRig is one scenario: a fake router, a client pointed at it, and a ledger
// and chooser wired into the registry for the length of the test.
type laneRig struct {
	server *lanestub.Server
	client *Client
	ledger *scriptedLedger
	model  string
}

// newLaneRig starts the router and the client. The model is spelled
// `openrouter/…` because the transport only sends a routing preference to
// something it believes is a router, and a loopback address is not one.
func newLaneRig(t *testing.T, name string, lanesOffered ...lanestub.Lane) *laneRig {
	t.Helper()
	model := "openrouter/" + name
	server := lanestub.New(model, lanesOffered...)
	t.Cleanup(server.Close)

	client, err := NewClient(Config{APIKey: "test-key", BaseURL: server.URL(), Model: model})
	if err != nil {
		t.Fatal(err)
	}
	// THE CHOOSER IS LEFT ALONE ON PURPOSE. Every hedge proved in this file is
	// therefore proved against the chooser a shipped binary runs, which is the
	// case that used to go untested: a feature check that asked whether the
	// registry held the package's own chooser type inverted when the real
	// chooser took that name, and the suite passed anyway because it pinned a
	// stub. Only the ledger is scripted here, because a test has to be able to
	// say what a lane is believed to be.
	ledger := &scriptedLedger{beliefs: map[lanes.ID]lanes.Belief{}}
	registry := lanes.Default()
	registry.SetLedger(ledger)
	SetHedgeBudget(lanes.NewBudget(6, 0))
	t.Cleanup(func() {
		registry.SetLedger(nil)
		SetHedgeBudget(nil)
	})
	return &laneRig{server: server, client: client, ledger: ledger, model: model}
}

// believes states what the ledger thinks of one lane: a median first token in
// milliseconds and a median rate in tokens a second.
func (r *laneRig) believes(lane string, ttft, rate float64) {
	r.ledger.mu.Lock()
	defer r.ledger.mu.Unlock()
	r.ledger.beliefs[lanes.ID{Model: r.model, Lane: lane}] = lanes.Belief{
		ID:   lanes.ID{Model: r.model, Lane: lane},
		TTFT: lanes.Posterior{X: math.Log(ttft), P: 0.04},
		Rate: lanes.Posterior{X: math.Log(rate), P: 0.04},
	}
}

// choice is what the chooser would have handed this request: A first, B as the
// alternative, with the frontier's own numbers so the commitment rule has
// something to compare against.
func choiceFor(model string, deadline time.Duration) lanes.Choice {
	return lanes.Choice{
		Order:    []string{"A", "B"},
		Alt:      "B",
		Deadline: deadline,
		Frontier: []lanes.Scored{
			{ID: lanes.ID{Model: model, Lane: "A"}, TTFT: 2, Rate: 2000, Price: 0.01},
			{ID: lanes.ID{Model: model, Lane: "B"}, TTFT: 5, Rate: 2000, Price: 0.01},
		},
	}
}

// notices collects what the person was told, in order.
type notices struct {
	mu     sync.Mutex
	events []StreamEvent
}

func (n *notices) observe(event StreamEvent) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, event)
}

func (n *notices) kinds(kind StreamEventKind) []StreamEvent {
	n.mu.Lock()
	defer n.mu.Unlock()
	var found []StreamEvent
	for _, event := range n.events {
		if event.Kind == kind {
			found = append(found, event)
		}
	}
	return found
}

func TestALateFirstTokenIsRescuedByTheAlternativeAndTheLoserIsCancelled(t *testing.T) {
	rig := newLaneRig(t, "late/first-token",
		// Ten virtual seconds to the first token, against half a second.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 100 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)

	report := &HedgeReport{}
	ctx := WithHedgeReport(context.Background(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	began := time.Now()
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(began)

	if !report.Hedged() || report.Reason() != "first token late" {
		t.Fatalf("report = hedged %v, reason %q; want one hedge for a late first token", report.Hedged(), report.Reason())
	}
	// The winner is named because its stream said so on every chunk. THE LOSER
	// IS NOT, and that is the attribution law rather than an omission: A was
	// cancelled before it had said a word, and the request it answered named an
	// order rather than a lane, so nobody here may put a name to it.
	winner, loser := report.Lanes()
	if winner != "B" || loser != "" {
		t.Fatalf("winner %q, loser %q; want the answer from B and nobody named as the loser", winner, loser)
	}
	// The cancel reaches the router a moment after the caller has its answer,
	// which is the whole point of it: nobody waits for the loser.
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
	if got := rig.server.Requests("B"); got != 1 {
		t.Fatalf("Requests(B) = %d, want one hedge and not a race", got)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want B's whole answer of 24", tokens)
	}
	// THE WHOLE RESCUE LANDS BEFORE A WOULD HAVE SAID ITS FIRST WORD: about 1.5
	// virtual seconds to decide, and the answer written by the alternative, all
	// inside the ten seconds the first lane was going to spend thinking.
	if took >= 100*time.Millisecond {
		t.Fatalf("the answer took %s (×100 virtual), want it inside A's own first token", took)
	}
	// A never named a lane, so there is nothing honest to write about it.
	if _, ok := rig.ledger.sightingFor("B"); !ok {
		t.Fatalf("the hedge was not folded back as a sighting: %+v", rig.ledger.noted())
	}
}

func TestALaneThatStallsMidAnswerIsHedgedAndTheAnswerArrivesWhole(t *testing.T) {
	rig := newLaneRig(t, "stall/mid-answer",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000, Tokens: 60,
			StallAfter: 30, StallFor: 200 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	// Believed at a quarter of what it really writes at, which is the honest
	// shape of a belief: ordinary jitter is never a surprise and a twenty-second
	// silence is nothing else.
	rig.believes("A", 2, 250)

	watched := &notices{}
	report := &HedgeReport{}
	ctx := WithHedgeReport(context.Background(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))
	ctx = WithStreamObserver(ctx, watched.observe)

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatalf("a lane that went quiet for twenty virtual seconds was not hedged")
	}
	if reason := report.Reason(); reason != "drift" && reason != "gap" {
		t.Fatalf("reason = %q, want the drift test to have said so", reason)
	}
	winner, loser := report.Lanes()
	if winner != "B" || loser != "A" {
		t.Fatalf("winner %q, loser %q; want B to have taken the answer", winner, loser)
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want B's whole answer of 24", tokens)
	}
	// THE PERSON WAS TOLD. Thirty tokens of A were already on the screen, so
	// the change of lane is a notice and not a silent swap.
	told := watched.kinds(StreamNotice)
	if len(told) != 1 || told[0].Delta != hedgeNotice {
		t.Fatalf("notices = %+v, want the one line about the answer changing lanes", told)
	}
	// And both lanes were measured, because a hedge is a measurement.
	if _, ok := rig.ledger.sightingFor("A"); !ok {
		t.Fatalf("the stalled lane taught the ledger nothing: %+v", rig.ledger.noted())
	}
	if _, ok := rig.ledger.sightingFor("B"); !ok {
		t.Fatalf("the rescuing lane taught the ledger nothing: %+v", rig.ledger.noted())
	}
	// EACH OF THEM EXACTLY ONCE. The loser is noted by the race, which is the
	// only thing that can see it; the winner is noted by the ordinary path every
	// finished stream takes. Both wrote it until the wave that merged them, and
	// the symptom of that is invisible in any one assertion: a lane that had
	// been raced was believed on twice the evidence it had earned.
	waitFor(t, func() bool { return len(rig.ledger.noted()) == 2 })
	seen := map[string]int{}
	for _, sighting := range rig.ledger.noted() {
		seen[sighting.ID.Lane]++
	}
	if seen["A"] != 1 || seen["B"] != 1 {
		t.Fatalf("sightings per lane = %+v, want one each: %+v", seen, rig.ledger.noted())
	}
}

func TestAnAlmostFinishedAnswerIsNeverAbandoned(t *testing.T) {
	rig := newLaneRig(t, "stall/almost-done",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 220,
			StallAfter: 200, StallFor: 300 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 200)

	report := &HedgeReport{}
	ctx := WithHedgeReport(context.Background(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))
	// Two hundred and twenty tokens expected, and two hundred have arrived.
	ctx = WithExpectedAnswer(ctx, 220)

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Hedged() {
		t.Fatalf("abandoned an answer with twenty tokens to go; reason %q", report.Reason())
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want none: finishing here was cheaper than starting again", got)
	}
	if tokens := answerTokens(response); tokens != 220 {
		t.Fatalf("the answer is %d tokens, want all 220 of A's", tokens)
	}
}

func TestAnExhaustedBudgetRefusesTheRescueAndTheAnswerArrivesLate(t *testing.T) {
	rig := newLaneRig(t, "budget/exhausted",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 60 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	// A budget with no bucket is how hedging is switched off.
	SetHedgeBudget(lanes.NewBudget(0, 0))

	report := &HedgeReport{}
	ctx := WithHedgeReport(context.Background(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	began := time.Now()
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(began)

	if report.Hedged() {
		t.Fatalf("a hedge went out on an exhausted budget")
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want none", got)
	}
	if got := rig.server.Cancels("A"); got != 0 {
		t.Fatalf("Cancels(A) = %d, want none: with no rescue, the slow answer is the answer", got)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want A's own 24", tokens)
	}
	if took < 55*time.Millisecond {
		t.Fatalf("the answer took %s, want the whole of A's six virtual seconds", took)
	}
}

func TestAPathWithNoHeartbeatAndNoByteIsHedgedWithoutChargingTheLane(t *testing.T) {
	rig := newLaneRig(t, "path/dead",
		// No heartbeats and nothing at all for longer than the dead-path bound.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 4 * time.Second, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)

	report := &HedgeReport{}
	ctx := WithHedgeReport(context.Background(), report)
	// No deadline and nothing believed about A: the only bound left is the
	// dead-path one, which is three seconds of no sign of life whatsoever.
	choice := choiceFor(rig.model, 0)
	ctx = WithLaneChoice(ctx, choice)

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() || report.Reason() != "no heartbeat" {
		t.Fatalf("report = hedged %v, reason %q; want a dead path", report.Hedged(), report.Reason())
	}
	if !report.PathFault() {
		t.Fatalf("PathFault = false; a stream with no sign of life is not the lane's fault")
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want B's 24", tokens)
	}
	// AND THE LANE'S BELIEF IS UNTOUCHED. Nothing about A was observed, so
	// nothing about A is written down.
	if sighting, ok := rig.ledger.sightingFor("A"); ok {
		t.Fatalf("a dead path was charged to the lane: %+v", sighting)
	}
	if _, ok := rig.ledger.sightingFor("B"); !ok {
		t.Fatalf("the rescuing lane taught the ledger nothing: %+v", rig.ledger.noted())
	}
}

func TestWithNoLaneChoiceTheStreamIsExactlyWhatItAlwaysWas(t *testing.T) {
	rig := newLaneRig(t, "plain/unwatched",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 60 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)

	report := &HedgeReport{}
	// The slot is open and the router is wired in; the one thing missing is a
	// choice for this request, which is every request in a build where nothing
	// asks the chooser.
	ctx := WithHedgeReport(context.Background(), report)

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Hedged() {
		t.Fatalf("an unwatched request hedged")
	}
	if got := rig.server.Requests("A"); got != 1 {
		t.Fatalf("Requests(A) = %d, want the one request the caller made", got)
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want none", got)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want A's 24", tokens)
	}
	// IT STILL TAUGHT THE LEDGER, ONCE. A stream with no choice behind it is
	// still a timed answer from a named lane, and folding it in is how a machine
	// with no sheet ever learns anything (lanes.go's noteLane, on the ordinary
	// path). What an unwatched request must not do is write a SECOND one: that
	// was the seam where the watch's own bookkeeping and the transport's
	// overlapped, and a lane noted twice for one answer is a lane whose belief
	// moves twice as fast for having been looked at.
	noted := rig.ledger.noted()
	if len(noted) != 1 {
		t.Fatalf("an unwatched request wrote %d lane sightings, want the one the ordinary path writes: %+v",
			len(noted), noted)
	}
	if noted[0].ID.Lane != "A" {
		t.Fatalf("the sighting was credited to %q, want the lane that served", noted[0].ID.Lane)
	}
	asks := rig.server.Asks()
	if len(asks) != 1 {
		t.Fatalf("%d requests went out, want one", len(asks))
	}
	if len(asks[0].Only) != 0 {
		t.Fatalf("the request demanded a lane: %+v", asks[0].Only)
	}
}

func TestAHedgeDemandsItsOwnLaneAndTakesNoFallback(t *testing.T) {
	rig := newLaneRig(t, "hedge/wire",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 60 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)

	ctx := WithLaneChoice(context.Background(), choiceFor(rig.model, 12*time.Millisecond))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	asks := rig.server.Asks()
	if len(asks) != 2 {
		t.Fatalf("%d requests went out, want the original and one hedge", len(asks))
	}
	if len(asks[0].Only) != 0 {
		t.Fatalf("the first request demanded a lane: %+v", asks[0].Only)
	}
	if len(asks[1].Only) != 1 || asks[1].Only[0] != "B" {
		t.Fatalf("the hedge asked for %v, want only B", asks[1].Only)
	}
}

// ── THE PROBE ───────────────────────────────────────────────────────────────

func TestAProbePairAsksTwoLanesForOneTokenEach(t *testing.T) {
	rig := newLaneRig(t, "probe/pair",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 3 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	// The prober is built around this client's transport directly rather than
	// through [InstallLaneProber], whose gate also asks the process-wide
	// connection pool whether it is pacing — a fact the rest of this package's
	// suite moves, and one that has nothing to do with the request under test.
	prober := lanes.NewProber(lanes.ProberConfig{Send: rig.client.probeLane})
	prober.Probe(context.Background(), rig.model, []string{"A", "B", "C"})
	waitFor(t, func() bool { return len(rig.ledger.noted()) == 2 })

	asks := rig.server.Asks()
	if len(asks) != 2 {
		t.Fatalf("%d probes went out, want a pair", len(asks))
	}
	for index, ask := range asks {
		if ask.MaxTokens != 1 {
			t.Fatalf("probe %d asked for %d tokens, want one", index, ask.MaxTokens)
		}
		if !ask.Stream {
			t.Fatalf("probe %d did not stream, so it could not have timed a first token", index)
		}
		if len(ask.Only) != 1 {
			t.Fatalf("probe %d asked for %v, want exactly one lane", index, ask.Only)
		}
		if ask.PromptTokens > 20 {
			t.Fatalf("probe %d carried %d prompt tokens; the prompt is meant to be about ten", index, ask.PromptTokens)
		}
	}
	for _, sighting := range rig.ledger.noted() {
		if !sighting.Probe {
			t.Fatalf("sighting %+v is not marked as a probe", sighting)
		}
		if sighting.TTFT <= 0 || sighting.At.IsZero() {
			t.Fatalf("sighting %+v measured nothing", sighting)
		}
	}

	// And the second pair inside the window is refused outright.
	prober.Probe(context.Background(), rig.model, []string{"A", "B"})
	time.Sleep(50 * time.Millisecond)
	if got := len(rig.server.Asks()); got != 2 {
		t.Fatalf("%d probes went out in the same twenty seconds, want the pair and no more", got)
	}
}

func TestAProbeRefusesAClientThatIsNotTalkingToARouter(t *testing.T) {
	client, err := NewClient(Config{APIKey: "test-key", BaseURL: "http://provider.test", Model: "sim/model"})
	if err != nil {
		t.Fatal(err)
	}
	if InstallLaneProber(client, nil) {
		t.Fatal("a probe was wired to an endpoint that cannot honour `only`")
	}
}

// answerTokens is how long an answer was, by the provider's own count. It is
// the one thing that tells the two arms apart in these tests: both lanes write
// the same words and only the length of what they were scripted to write
// differs.
func answerTokens(response *ai.Response) int {
	if response == nil || response.Usage == nil {
		return 0
	}
	return response.Usage.CompletionTokens
}

// waitFor spins until the condition holds, and fails rather than hanging. It is
// how a fire-and-forget probe is waited on without the test knowing anything
// about its goroutines.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the thing being waited for never happened")
}

// ── THE WIRE AND THE WATCH SEE ONE CHOICE ───────────────────────────────────

// countingChooser answers a fixed choice and says how often it was asked.
type countingChooser struct {
	mu     sync.Mutex
	choice lanes.Choice
	asked  int
}

func (c *countingChooser) Choose(lanes.Request) lanes.Choice {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked++
	return c.choice
}

func (c *countingChooser) times() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.asked
}

// TestOneCallMakesOneChoiceAndBothHalvesUseIt is the seam between the two lanes
// that built this: the encoder that writes `provider.order`, and the watch that
// decides whether to hedge and where to.
//
// A CHOICE IS A SAMPLED DECISION, so asking for it twice gives two answers. The
// two halves used to ask separately — the watch on the way in, the encoder on
// the way out — and a watch armed on a lane the wire never asked for is a hedge
// fired at the wrong moment toward the wrong alternative, with neither half
// looking wrong on its own. So the call decides once and both halves read it,
// and this test holds all three facts at the same time: asked once, on the
// wire, and in force at the watch.
func TestOneCallMakesOneChoiceAndBothHalvesUseIt(t *testing.T) {
	rig := newLaneRig(t, "choice/once",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000, Tokens: 60,
			StallAfter: 30, StallFor: 200 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 250)
	chooser := &countingChooser{choice: choiceFor(rig.model, 12*time.Millisecond)}
	lanes.Default().SetChooser(chooser)
	// PUT THE REAL ONE BACK. This is the only test in the package that swaps the
	// chooser, so it is the only one that has to restore it; a stub left in the
	// registry answers for every test that runs after this one.
	t.Cleanup(func() { lanes.Default().SetChooser(nil) })

	// NOTHING IS PUT ON THE CONTEXT HERE. Every other test in this file hands
	// the transport a choice by hand; this one is about the transport making it.
	report := &HedgeReport{}
	if _, err := rig.client.CompleteWithMessages(WithHedgeReport(context.Background(), report), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if got := chooser.times(); got != 1 {
		t.Fatalf("the chooser was asked %d times for one call; a sampled decision asked twice is two decisions", got)
	}
	asks := rig.server.Asks()
	if len(asks) == 0 || len(asks[0].Order) == 0 || asks[0].Order[0] != "A" {
		t.Fatalf("the wire asked for %+v, want the chosen lane at the head", asks)
	}
	if !report.Hedged() {
		t.Fatalf("the watch was never armed, so the choice reached the wire and not the watch")
	}
	if winner, loser := report.Lanes(); winner != "B" || loser != "A" {
		t.Fatalf("winner %q, loser %q; the watch hedged somewhere the choice did not name", winner, loser)
	}
}

// ── WHAT A PERSON IS TOLD WHILE A RESCUE IS OUT ─────────────────────────────

// THE MIDDLE STATE IS REPORTED AS IT HAPPENS. A rescue that was only reported
// once it had landed is a rescue somebody watched as an unexplained pause; the
// one sentence this build says about a slow answer is said while something is
// already being done about it. The callback is the seam internal/session posts
// `slow · trying …` from.
func TestARescueTellsItsCallerTheMomentItGoesOut(t *testing.T) {
	rig := newLaneRig(t, "rescue/announced",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 100 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)

	var mu sync.Mutex
	var announced []string
	var announcedBefore bool
	report := &HedgeReport{}
	report.OnHedgeStart(func(alt string) {
		mu.Lock()
		defer mu.Unlock()
		// The answer has not arrived yet — that is the whole claim.
		announcedBefore = !report.Hedged() || report.Primary() == ""
		announced = append(announced, alt)
	})
	ctx := WithHedgeReport(context.Background(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(announced) != 1 || announced[0] != "B" {
		t.Fatalf("the rescue announced %v, want the one lane it went to", announced)
	}
	if !announcedBefore {
		t.Fatal("the rescue was announced after it had already settled")
	}
	if winner, _ := report.Lanes(); winner != "B" {
		t.Fatalf("winner %q, want the lane that answered", winner)
	}
}
