package provider

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
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

// laneRig is one scenario: a fake router, a client pointed at it, a ledger and
// chooser wired into the registry, and the waiting controller installed through
// the seam a shipped build installs it through.
type laneRig struct {
	server *lanestub.Server
	client *Client
	ledger *scriptedLedger
	model  string
	// ceiling is the bound a scenario states for itself, zero when it takes the
	// role's own scaled down to the rig's timeline.
	ceiling atomic.Int64
}

// rigLanes is which machines each scenario's router really offers, so that the
// choice a test hands the transport can name exactly those.
//
// IT IS WHAT "NOWHERE TO GO" NOW MEANS. Routing and waiting are two questions:
// a choice no longer carries the lane a rescue would go to, so a request with
// no alternative is one whose FRONTIER holds nothing else — which is the
// honest shape of a router that offers one machine, of a strict pin, and of a
// ledger that has heard of one lane.
var rigLanes sync.Map

// newLaneRig starts the router and the client. The model is spelled
// `openrouter/…` because the transport only sends a routing preference to
// something it believes is a router, and a loopback address is not one.
func newLaneRig(t *testing.T, name string, lanesOffered ...lanestub.Lane) *laneRig {
	return newLaneRigWithPrice(t, name, nil, lanesOffered...)
}

// newPricedLaneRig is the same shipped-wire rig with the list price that put
// the measured 0.825/2.475 dollars-per-million ceiling on the request.
func newPricedLaneRig(t *testing.T, name string, lanesOffered ...lanestub.Lane) *laneRig {
	t.Helper()
	return newLaneRigWithPrice(t, name, func(string) (float64, float64, bool) {
		return 0.66e-6, 1.98e-6, true
	}, lanesOffered...)
}

func newLaneRigWithPrice(
	t *testing.T,
	name string,
	modelPrice func(string) (prompt, completion float64, known bool),
	lanesOffered ...lanestub.Lane,
) *laneRig {
	t.Helper()
	// NO TEST WRITES THE REAL HOME. The registry's own store is under it, and a
	// suite that saved its scripted beliefs into somebody's ledger would be a
	// suite that cost them their afternoon.
	t.Setenv(home.EnvVar, t.TempDir())
	model := "openrouter/" + name
	server := lanestub.New(model, lanesOffered...)
	t.Cleanup(server.Close)

	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: server.URL(), Model: model, ModelPrice: modelPrice,
	})
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
	// THE CONTROLLER IS INSTALLED THROUGH THE ONE SEAM, exactly as a shipped
	// build installs it, so that what these tests exercise is the wiring and
	// not a second arrangement built for them.
	// AND THE RIG STATES WHAT THIS STUB IS. A shipped build learns that a base
	// carries a routing preference from the base itself — the endpoints page the
	// beat fetches, or the lane an answer names (issue #433) — and neither has
	// happened here: no beat runs in a test, and the belief below is SCRIPTED
	// rather than measured. This stub publishes an endpoints page and names its
	// lane on every answer, so it does carry one; saying so is stating a fact
	// about the fixture, not turning a law off. A rig that left it unsaid would
	// send no `provider` object at all until something was pinned, which is
	// exactly right for a plain endpoint and wrong for a router.
	lanes.HeardPrefsCarried(server.URL())
	rig := &laneRig{server: server, client: client, ledger: ledger, model: model}
	shipped := lanes.SetController(func(plan control.Plan) control.Controller {
		return ridePolicy(rig.scaled(plan))
	})
	names := make([]string, 0, len(lanesOffered))
	for _, offered := range lanesOffered {
		names = append(names, offered.Name)
	}
	rigLanes.Store(model, names)
	t.Cleanup(func() {
		registry.SetLedger(nil)
		// PUT BACK WHAT WAS FOUND, and never nil: the shipped factory is
		// installed at this package's own init, and a rig that cleared it would
		// leave every test after it running a build with no waiting policy at
		// all.
		lanes.SetController(shipped)
		rigLanes.Delete(model)
		SetHedgeBudget(nil)
		// The package's learners go back too, beside the registry and the
		// controller: see resetSharedLearners for why they are a restoration
		// and not an extra.
		resetSharedLearners()
	})
	return rig
}

// patience shortens the ceiling every controller in this scenario is built
// with.
//
// THE CEILING IS THE ROLE'S AND ITS VALUE IS `control`'s, not this file's: ten
// seconds is a statement about how long a person is asked to watch an empty
// line, and a wire test that really waited it would take ten seconds to prove
// something about a channel. So the scenarios below state the ceiling they are
// about and prove what this lane owns — that the ceiling is what acts when
// nothing at all is believed, and that it acts through the wire.
func (r *laneRig) patience(_ *testing.T, ceiling time.Duration) {
	r.ceiling.Store(int64(ceiling))
}

// rigScale is how much shorter every bound a scenario is judged against is than
// the one a person is really given.
//
// THE RIG IS A TIMELINE AT 1:100 and its bounds are at 1:20, and the difference
// is deliberate. A lane's own numbers are scripted in milliseconds and read as
// seconds; the ceiling, the action floor and the hysteresis are scaled less
// hard so that BOTH kinds of bound are reachable inside one scenario — the
// belief-derived crossing, which is what almost every test here is about, and
// the ceiling over it, which is what the two cold-store scenarios are about. At
// a hundred they would be a fifth of a millisecond apart and every test would
// be about whichever fired first.
const rigScale = 20

// rigTimeline is how many times faster this rig's wire runs than the world the
// design's figures are written about. It is the same hundred `internal/lane`'s
// own e2e scenarios and `bench/lanelab/gosim` use.
//
// IT IS WHAT λ IS BROUGHT ONTO, AND THE REASON IS THAT MONEY DOES NOT COMPRESS.
// λ is SECONDS PER DOLLAR, so on a timeline a hundred times shorter one dollar
// buys a hundred times fewer of these seconds; a rig that left it alone would
// price every rescue at nearly a whole scenario and nothing would ever be worth
// acting on. The lane's own beliefs need no such treatment — they are stated in
// milliseconds and read as the wall-clock milliseconds this rig really waits.
const rigTimeline = 100

// ridePolicy is the controller these wire tests run against, AND IT IS THE
// SHIPPED ONE.
//
// It stayed a variable while the controller and the wire were built in parallel
// lanes and the double below stood in for it; now that both have landed, a test
// double here would be a second idea of when to act, proved against a wire that
// obeys a different one. The arithmetic is proved in `internal/lane/control`;
// what is proved here is that the wire drives it.
var ridePolicy control.Factory = control.New

// scaled is the plan as this rig hands it over: the role's own ceiling, floor
// and hysteresis brought onto the rig's timeline, and an explicit ceiling where
// a scenario states one.
func (r *laneRig) scaled(plan control.Plan) control.Plan {
	plan.Ceiling /= rigScale
	plan.Floor /= rigScale
	plan.Margin /= rigScale
	plan.Lambda /= rigTimeline
	if stated := r.ceiling.Load(); stated > 0 {
		plan.Ceiling = time.Duration(stated)
	}
	return plan
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

// choiceFor is what the chooser would have handed this request: the router's
// own machines in order, with the frontier's numbers so that the commitment
// rule and the purse have something to compare against.
//
// IT CARRIES NOTHING ABOUT TIME. The deadline argument is what a choice used to
// hold, and it is kept so that every scenario still reads as the sentence it
// was written as; the controller derives its own bound from what the ledger
// believes about the lane expected to serve, which is the whole point of
// separating the two questions.
func choiceFor(model string, deadline time.Duration) lanes.Choice {
	names, _ := rigLanes.Load(model)
	offered, _ := names.([]string)
	if len(offered) == 0 {
		offered = []string{"A", "B"}
	}
	choice := lanes.Choice{Order: append([]string(nil), offered...)}
	for index, lane := range offered {
		ttft := 5.0
		if index == 0 {
			ttft = 2
		}
		choice.Frontier = append(choice.Frontier, lanes.Scored{
			ID: lanes.ID{Model: model, Lane: lane}, TTFT: ttft, Rate: 2000, Price: 0.01,
		})
	}
	return choice
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

// TestTwoArmsRecordingAtOnceDoNotDeadlock is the call-log race from issue
// #330 without a model or a wire. Each arm's row asks its own watch for local
// timing and the race for shared lane and spend facts. If facts ever holds a
// watch lock while asking the race, one arm can meet spend walking the watches
// in the opposite order and neither row — nor the turn — can finish.
//
// The barrier puts both arms at that seam together on every round. Repeating it
// turns the narrow scheduling window that happened on the wire into a stable
// regression under both the ordinary scheduler and the race detector.
func TestTwoArmsRecordingAtOnceDoNotDeadlock(t *testing.T) {
	race := &hedgeRace{plan: control.Plan{Lane: "A"}}
	race.arms = []*hedgeArm{
		{index: 0, lane: "A"},
		{index: 1, lane: "B"},
	}
	for _, arm := range race.arms {
		arm.watch = &streamWatch{race: race, arm: arm.index}
	}

	const rounds = 500
	ready := make(chan struct{}, len(race.arms))
	release := make([]chan struct{}, len(race.arms))
	done := make(chan struct{}, len(race.arms))
	for index, arm := range race.arms {
		watch := arm.watch
		release[index] = make(chan struct{})
		gate := release[index]
		go func() {
			for range rounds {
				ready <- struct{}{}
				<-gate
				watch.facts()
				done <- struct{}{}
			}
		}()
	}

	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	wait := func(stage string) {
		t.Helper()
		select {
		case <-done:
		case <-timer.C:
			t.Fatalf("two arms recording their rows deadlocked while %s", stage)
		}
	}
	for round := range rounds {
		for range race.arms {
			<-ready
		}
		for _, gate := range release {
			gate <- struct{}{}
		}
		for range race.arms {
			wait("finishing round " + strconv.Itoa(round+1))
		}
	}
}

func TestALateFirstTokenIsRescuedByTheAlternativeAndTheLoserIsCancelled(t *testing.T) {
	rig := newLaneRig(t, "late/first-token",
		// Thirty virtual seconds to the first token, against half a second.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}

	if !report.Hedged() || report.Reason() != "first token late" {
		t.Fatalf("report = hedged %v, reason %q; want one hedge for a late first token", report.Hedged(), report.Reason())
	}
	// The winner is named because its stream said so on every chunk. THE LOSER
	// IS NOT, and that is the attribution law rather than an omission: A was
	// cancelled before it had said a word, and the request it answered named an
	// order rather than a lane, so nobody here may put a name to it.
	//
	// AND THE EMPTY LOSER IS ALSO THE TIMING CLAIM. That the whole rescue lands
	// before A would have said its first word used to be spelled as an elapsed
	// bound — `took < 300ms`, against A's own 300 ms first token — which left
	// about eighty milliseconds of slack for a starved machine to eat. A lane
	// names itself on every chunk it writes, so an A that had got one word out
	// would be sitting right here as the loser. The ordering the transport
	// guarantees says it at any speed; a wall clock only ever said it by luck.
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
	// A never named a lane, so there is nothing honest to write about it.
	if _, ok := rig.ledger.sightingFor("B"); !ok {
		t.Fatalf("the hedge was not folded back as a sighting: %+v", rig.ledger.noted())
	}
}

func TestALaneThatStallsMidAnswerIsHedgedAndTheAnswerArrivesWhole(t *testing.T) {
	// A IS HELD BY A SIGNAL AND NOT BY A DURATION. What this test is about is
	// the rule that a rescue which lands while the primary is still quiet takes
	// the answer — and the rule the other way round is just as real: a primary
	// that comes back and finishes first KEEPS the answer, which is what
	// TestAnAlmostFinishedAnswerIsNeverAbandoned demands. So an A told to resume
	// after some number of milliseconds is asserting nothing but the slack
	// between two wall-clock figures, and a starved machine that eats the slack
	// makes a correct build look broken. Held open until this channel closes —
	// and it closes only at teardown, never while the call is out — A cannot
	// finish first, so B winning is the rule and not the luck.
	resume := make(chan struct{})
	rig := newLaneRig(t, "stall/mid-answer",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000, Tokens: 60,
			StallAfter: 30, StallUntil: resume,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	// Released after the assertions and BEFORE the rig closes its server, so a
	// run that never reached the cancel still lets the handler go.
	t.Cleanup(func() { close(resume) })
	// Believed at a quarter of what it really writes at, which is the honest
	// shape of a belief: ordinary jitter is never a surprise and a twenty-second
	// silence is nothing else.
	rig.believes("A", 2, 250)

	watched := &notices{}
	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
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
	ctx := WithHedgeReport(talking(), report)
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
	ctx := WithHedgeReport(talking(), report)
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

	// THE SCENARIO IS ABOUT THE DEAD-PATH BOUND, so the clock has to be allowed
	// to reach it: the role's own ceiling would act first and say, correctly,
	// that the wait was over — which is a different sentence from "nothing ever
	// came back", and this test is about the second one.
	rig.patience(t, lanes.DeadPathFloor+500*time.Millisecond)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
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
	ctx := WithHedgeReport(talking(), report)

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
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)

	ctx := WithLaneChoice(talking(), choiceFor(rig.model, 12*time.Millisecond))
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

// A PROBE IS REFUSED ON A BASE THAT HAS SAID IT WILL NOT CARRY A DEMAND.
//
// WHAT WAS TRUE: the refusal was a hostname test, so a prober was refused to
// every proxy, mirror and self-hosted router as well (issue #433). WHAT IS TRUE
// NOW: a base nobody has asked is wired — the asking is the sending, and a
// frontier with no lane in it buys nothing anyway — and only a base that has
// answered is refused.
func TestAProbeRefusesAClientOnABaseThatWillNotCarryADemand(t *testing.T) {
	client, err := NewClient(Config{APIKey: "test-key", BaseURL: "http://provider.test", Model: "sim/model"})
	if err != nil {
		t.Fatal(err)
	}
	if !InstallLaneProber(client, nil) {
		t.Fatal("a base nobody has asked was refused a prober, so it can never be asked")
	}
	if !lanes.HeardPrefsSilent(client.config.BaseURL) {
		t.Fatal("the answer was not filed against the base the client is talking to")
	}
	if InstallLaneProber(client, nil) {
		t.Fatal("a probe was wired to an endpoint that has said it cannot honour `only`")
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
	// A IS HELD BY A SIGNAL AND NOT BY A DURATION, for the reason spelled out
	// over TestALaneThatStallsMidAnswerIsHedgedAndTheAnswerArrivesWhole: the
	// last assertion here names B as the winner, and a primary told to resume
	// after a couple of hundred milliseconds can honestly beat the rescue home
	// on a loaded machine. The channel closes only at teardown.
	resume := make(chan struct{})
	rig := newLaneRig(t, "choice/once",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000, Tokens: 60,
			StallAfter: 30, StallUntil: resume,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	t.Cleanup(func() { close(resume) })
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
	if _, err := rig.client.CompleteWithMessages(WithHedgeReport(talking(), report), userMessages("hello")); err != nil {
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
	// A IS GENUINELY ABNORMAL FOR A, and it has to be: since §B's abnormality
	// gate, a lane doing something its own belief calls ordinary is not rescued
	// at all, so a hundred milliseconds against a believed twenty — well inside
	// one nat — would raise nothing and this test would be about silence.
	rig := newLaneRig(t, "rescue/announced",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 900 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)

	var mu sync.Mutex
	var announced []string
	var announcedBefore bool
	report := &HedgeReport{}
	report.OnHedgeStart(func(news RescueNews) {
		mu.Lock()
		defer mu.Unlock()
		// The answer has not arrived yet — that is the whole claim.
		announcedBefore = !report.Hedged() || report.Primary() == ""
		announced = append(announced, news.Alt)
	})
	ctx := WithHedgeReport(talking(), report)
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

// ── THE CASE THAT USED TO HAVE NO CLOCK AT ALL ──────────────────────────────

// TestAColdStoreStillActsAtTheCeiling is the reported defect, run rather than
// read.
//
// Nothing is believed about any lane here — no sighting, no sheet, the state of
// every model somebody picks after launch — so there is no derived deadline to
// wait against. Until this wave that meant no watch at all: the request went
// out with no deadline, no silence beat and nothing to hedge to, and the first
// thing that acted on the silence was a transport bound two and a half minutes
// away. The ceiling is what makes the invariant hold from a cold store, and it
// exists whether or not a belief does.
func TestAColdStoreStillActsAtTheCeiling(t *testing.T) {
	rig := newLaneRig(t, "cold/store",
		// Thirty virtual seconds to a first word, with the router's own comment
		// lines on the way — so the path is demonstrably alive and the only
		// thing left to act on the wait is the ceiling.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	// AND THE LEDGER IS LEFT EMPTY. That is the whole scenario.
	rig.patience(t, 150*time.Millisecond)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}

	// THE REASON WORD IS THE CLAIM, and it is the only thing here that can
	// carry it. This used to also assert `took < 500ms`, "acted on at the
	// ceiling rather than at A's own pace" — but A's own pace is a 300 ms first
	// token and twelve more for the answer, comfortably inside that bound, so
	// the figure never separated the two cases it named. What separates them is
	// that the controller said "ceiling" and that B answered: had the ceiling
	// not acted, A would have finished and A would be the winner. Those hold at
	// any speed, and the elapsed figure only ever held on a quiet machine.
	if !report.Hedged() || report.Reason() != "ceiling" {
		t.Fatalf("report = hedged %v, reason %q; want the ceiling to have acted with nothing believed",
			report.Hedged(), report.Reason())
	}
	if winner, _ := report.Lanes(); winner != "B" {
		t.Fatalf("winner = %q, want the answer from the lane the rescue went to", winner)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want B's 24", tokens)
	}
	// AND THE LOSER IS CANCELLED, which is what stops the bill on the lanes
	// that honour it.
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
}

// TestAHeartbeatNeverResetsTheSilence is the difference between a claim about
// the PATH and a claim about the endpoint.
//
// `: OPENROUTER PROCESSING` proves the connection is alive and proves nothing
// whatsoever about the model, so it buys the dead-path bound patience and it
// buys the wait none. A clock a comment line reset would be a clock a router
// could hold open forever by saying nothing in a well-formed way — which is
// exactly what a stalled lane emitting keepalives does.
func TestAHeartbeatNeverResetsTheSilence(t *testing.T) {
	rig := newLaneRig(t, "beat/only",
		// Three comment lines on the way to a first token six hundred
		// milliseconds away: one of them lands before the ceiling does.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 600 * time.Millisecond, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.patience(t, 300*time.Millisecond)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}

	// THAT IT WAS HEDGED AT ALL IS THE WHOLE CLAIM, and the fixture is built so
	// that it can be.
	//
	// The three comment lines are two hundred milliseconds apart and the ceiling
	// is three hundred, so they come in FASTER than the clock they are alleged to
	// reset: a build that reset on a keepalive would push its deadline out at
	// every beat and never reach one before A's own first token at six hundred
	// milliseconds, after which A is writing and there is no silence left to act
	// on. So a reset build does not hedge here, ever, and this build does — which
	// is the difference, said without reading a clock.
	//
	// It used to be said with one: `took < 500ms`, "acted on at the ceiling and
	// not at the beat after it". That reads about a hundred and eighty
	// milliseconds of slack on a wall clock wrapped around a live round trip, and
	// a starved machine eats slack. The ceiling is three hundred milliseconds of
	// the controller's own silence and A cannot finish before six hundred no
	// matter how the machine is loaded, so the hedge is there at any speed.
	if !report.Hedged() {
		t.Fatal("a stream that said nothing but keepalives for twice its ceiling was never acted on")
	}
	// A BEAT WAS SEEN, so this is a slow lane and not a dead path — and the
	// belief is charged accordingly rather than being let off.
	if report.PathFault() {
		t.Fatal("a stream that was sending keepalives was called a dead path")
	}
	// AND THE RESCUE IS WHAT ANSWERED. A had written nothing but comment lines,
	// so it never named itself a lane and there is no honest loser to name.
	if winner, loser := report.Lanes(); winner != "B" || loser != "" {
		t.Fatalf("winner %q, loser %q; want the answer from B and nobody named as the loser", winner, loser)
	}
}

// TestALongHealthyThinkIsLeftAlone is the failure mode this design most risks
// introducing.
//
// A model asked at the top rung may deliberate for a long time on purpose, and
// hedging that fires a second long deliberation and buys nothing. A think is
// judged against what a think costs — not against the first-token belief, and
// not against a fixed gap — so a run of thought arriving at the believed rate
// is a stream that is working.
func TestALongHealthyThinkIsLeftAlone(t *testing.T) {
	rig := newLaneRig(t, "think/healthy",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 5 * time.Millisecond, Rate: 1000, Reasoning: 300, Tokens: 24,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 5, 250)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Hedged() {
		t.Fatalf("hedged a model that was thinking at exactly the rate it is believed to write at; reason %q",
			report.Reason())
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want none", got)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want A's own 24", tokens)
	}
}

// TestTheRowSaysWhyItWaitedAndWhatWasDone is the autopsy this design was
// written for: one line that answers when it acted, what it believed, what it
// did and what it cost.
func TestTheRowSaysWhyItWaitedAndWhatWasDone(t *testing.T) {
	read := loggingTo(t)
	rig := newLaneRig(t, "row/why",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000, Tokens: 60,
			StallAfter: 30, StallFor: 300 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 250)

	ctx := WithLaneChoice(talking(), choiceFor(rig.model, 0))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}

	// THE LOSER'S ROW LANDS A MOMENT AFTER THE CALLER HAS ITS ANSWER, which is
	// the whole point of a rescue: nobody waits for the arm that was cancelled.
	waitFor(t, func() bool { return len(ended(read())) == 2 })

	var acted, rescue bool
	for _, row := range ended(read()) {
		if row.Lane == "B" {
			rescue = true
			if !row.Hedged || row.Arms < 2 {
				t.Fatalf("the rescue's own row says hedged %v over %d arms; want it to name the pair it was half of", row.Hedged, row.Arms)
			}
		}
		if row.Action == "" {
			continue
		}
		acted = true
		if row.Action != "hedge" {
			t.Fatalf("the row says the wait was answered with %q, want the rescue that really went out", row.Action)
		}
		if row.Lane != "A" {
			t.Fatalf("the acted row names lane %q, want the machine the preference asked for", row.Lane)
		}
		if row.SilenceMs <= 0 {
			t.Fatal("the row records an action and no silence, so nobody can say when it was taken")
		}
		if row.DeadlineMs <= 0 {
			t.Fatal("the row records no deadline, so nobody can say whether it was set in the right place")
		}
		if row.WaitS <= 0 {
			t.Fatal("the row records the action without the wait it was decided on")
		}
		if row.Arms != 2 {
			t.Fatalf("the row says %d arms, want the original and its rescue", row.Arms)
		}
		if row.WasteUSD <= 0 {
			t.Fatal("two arms went out and the row says the second one was free")
		}
	}
	if !acted || !rescue {
		t.Fatalf("no row said what was done about the wait (acted %v, rescue %v)", acted, rescue)
	}
}

// TestAtTheCeilingAnErrandNobodyIsWatchingIsStillRescued is the owner's order,
// stated as a scenario: ALL of this build's calls answer promptly, not only the
// ones a person is reading.
//
// λ is what a second of a wait is worth, and for a background errand it is
// zero: no amount of money buys speed for an answer nobody is waiting on, so
// the arithmetic under the ceiling never crosses and the honest verdict there
// is to say the wait is real. THE CEILING IS NOT PART OF THAT ARITHMETIC. It is
// the promise that nothing waits longer than the role's own bound, and a
// promise kept by reporting is a promise to keep waiting. So a report raised at
// the ceiling with somewhere to go becomes the rescue it would have been for a
// person.
func TestAtTheCeilingAnErrandNobodyIsWatchingIsStillRescued(t *testing.T) {
	rig := newLaneRig(t, "ceiling/reported",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 400 * time.Millisecond, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	// THE SHIPPED POLICY, ASKED IN THE ROLE THAT NEVER BUYS SPEED. Nothing is
	// scripted here beyond how long the ceiling is: the arithmetic under it can
	// never cross with λ at zero, so whatever acts is the bound acting.
	rig.patience(t, 100*time.Millisecond)

	report := &HedgeReport{}
	ctx := WithRole(context.Background(), lanes.RoleStanding)
	ctx = WithHedgeReport(ctx, report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Action() != "hedge" {
		t.Fatalf("the row says %q; a wait reported at the ceiling with a lane to go to is a rescue", report.Action())
	}
	if got := rig.server.Requests("B"); got != 1 {
		t.Fatalf("Requests(B) = %d, want the rescue the ceiling owed", got)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want the rescuer's 24", tokens)
	}
}

// TestAFencedRunOfThoughtIsHiddenToTheController is the seam between #234's
// answer split and this build's waiting policy, and it is the one place the two
// have to agree.
//
// A gateway that does not strip its model's working delivers it on the CONTENT
// channel inside `<think>` tags. `answer.go` carves it back out so it never
// reaches the transcript — and the SAME carving has to reach the controller,
// because the silence clock is about what a person can read. If fenced working
// counted as visible progress it would reset the clock on every delta, and a
// model that fenced its thoughts could hold a turn open forever by thinking out
// loud: the very defect this design was written from, wearing a different hat.
//
// So: a lane that streams a long fenced thought and never a word of answer must
// still be acted on inside the role's ceiling. Nothing else in this file can
// tell that apart from a lane that was writing all along.
func TestAFencedRunOfThoughtIsHiddenToTheController(t *testing.T) {
	rig := newLaneRig(t, "fenced/think",
		// A whole run of thought on the content channel, slowly, and nothing
		// else: no visible word ever arrives from A.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 40, Tokens: 0,
			Reasoning: 40, Fenced: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 250)

	watched := &notices{}
	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))
	ctx = WithStreamObserver(ctx, watched.observe)

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}

	// THE CLOCK RAN. Had the fenced deltas been read as visible progress, every
	// one of them would have reset the silence and nothing would ever have been
	// acted on.
	if !report.Hedged() {
		t.Fatal("a lane that only ever wrote fenced working was never acted on — " +
			"its thinking is being counted as a person's reading")
	}

	// AND THE WORKING NEVER BECAME THE ANSWER, which is answer.go's half of the
	// same seam: it rides the reasoning events, marked as carved out of the
	// answer channel, and no delta of it is offered as text.
	for _, event := range watched.kinds(StreamDelta) {
		if strings.Contains(event.Delta, "<think>") || strings.Contains(event.Delta, "r0 ") {
			t.Fatalf("fenced working reached the answer channel as a delta: %q", event.Delta)
		}
	}
	fenced := 0
	for _, event := range watched.kinds(StreamReasoning) {
		if event.FromAnswer {
			fenced++
		}
	}
	if fenced == 0 {
		t.Fatal("no reasoning event was marked FromAnswer, so the split never carved the fence out " +
			"and this test proved nothing about the seam")
	}
}
