package provider

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
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

	if got := client.laneValueOfTime(callKnobs{intent: IntentInteractive}); got != lanes.AttentionValue {
		t.Fatalf("a turn somebody is watching is worth %v, want %v", got, lanes.AttentionValue)
	}
	if got := client.laneValueOfTime(callKnobs{intent: IntentBackground}); got != 0 {
		t.Fatalf("a call nobody is waiting on is worth %v, want price to win outright", got)
	}
	stated := knobsFrom(WithValueOfTime(context.Background(), 12))
	if got := client.laneValueOfTime(stated); got != 12 {
		t.Fatalf("a call site that stated λ got %v", got)
	}
	// AND A BACKGROUND CALL THAT STATED ONE IS BELIEVED. The default for a call
	// nobody is watching is the price row, and reading that default as "a person
	// said speed is worthless" is what made λ a dead letter on every task turn
	// this build ever ran (bench/lanelab/REPORT.md, "the λ = 0 row").
	background := knobsFrom(WithValueOfTime(context.Background(), 12))
	background.intent = IntentBackground
	if got := client.laneValueOfTime(background); got != 12 {
		t.Fatalf("a background call that stated λ = 12 was routed at %v", got)
	}
	// The row a PERSON wrote still wins outright over any of it.
	priced, _, _ := stubbedRouter(t)
	priced.config.Routing = StaticRouting(RoutingPrice)
	if got := priced.laneValueOfTime(stated); got != 0 {
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

// ── THE FETCHER: THE WHOLE OF THE TRANSPORT BEHIND A SHEET ──────────────────
//
// [sheetFetcher] is the one thing `internal/lane` may not own, so it is the one
// thing that package's own tests cannot reach. Three facts are worth pinning
// and they are the three the sheet depends on: a body it can decode, an error
// rather than a body when the router refuses, and the bearer actually on the
// wire — a fetch that quietly dropped the key would keep working right up until
// the router stopped serving the sheet to strangers.

func TestTheSheetFetcherCarriesTheBearerAndHandsBackTheBody(t *testing.T) {
	var authorization, referer, accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		referer = r.Header.Get("HTTP-Referer")
		accept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	body, err := sheetFetcher{http: server.Client()}.Fetch(context.Background(), server.URL, "  sk-test  ")
	if err != nil {
		t.Fatalf("fetch a sheet the router served: %v", err)
	}
	defer body.Close()
	read, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read the body back: %v", err)
	}
	if string(read) != `{"data":[]}` {
		t.Errorf("the body reached the sheet as %q, not the bytes the router wrote", read)
	}
	if authorization != "Bearer sk-test" {
		t.Errorf("Authorization was %q; the key is trimmed and sent as a bearer", authorization)
	}
	// And the read is attributed like every other read of the router this
	// binary makes (attribution.go) — a sheet fetched under no app is spend
	// nobody can account for.
	if referer != AppURL {
		t.Errorf("HTTP-Referer was %q, want %q", referer, AppURL)
	}
	if accept != "application/json" {
		t.Errorf("Accept was %q, want application/json", accept)
	}
}

func TestTheSheetFetcherSendsNoBearerWhenThereIsNoKey(t *testing.T) {
	held := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, held = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	body, err := sheetFetcher{http: server.Client()}.Fetch(context.Background(), server.URL, "   ")
	if err != nil {
		t.Fatalf("fetch the public sheet with no key: %v", err)
	}
	body.Close()
	// AN EMPTY BEARER IS A REAL STATE. The sheet is public and a client may be
	// built before the person has pasted a key, so `Bearer ` with nothing after
	// it would turn a request that works into a 401.
	if held {
		t.Error("an empty key still sent an Authorization header")
	}
}

func TestTheSheetFetcherTurnsARefusalIntoAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the router is having an afternoon", http.StatusInternalServerError)
	}))
	defer server.Close()

	body, err := sheetFetcher{http: server.Client()}.Fetch(context.Background(), server.URL, "sk-test")
	if err == nil {
		body.Close()
		t.Fatal("a 500 handed back a body; the sheet would have decoded an error page as lanes")
	}
	if body != nil {
		t.Error("a refusal handed back a body as well as an error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("the error was %q and does not say what the router answered", err)
	}
	// AND A 500 IS NOT A VERDICT ABOUT THE BASE. Only a 404 may be read as "no
	// endpoints page here"; a router having an afternoon must stay an ordinary
	// error, or one bad reply would cost a base its lanes for five minutes.
	if errors.Is(err, lanes.ErrNoSheetHere) {
		t.Error("a 500 was read as the base saying it publishes no endpoints page")
	}
}

// The live router answers 404 twice over, and the two mean opposite things about
// the base (measured with curl on 2026-09-02). These two bodies are quoted
// exactly, because the reading is made on the body and a paraphrase would be
// testing a router that does not exist.
const (
	// routerModel404 is GET /api/v1/models/nonexistent/model-xyz/endpoints: the
	// router's own error envelope, the route answering about one model.
	routerModel404 = `{"error":{"message":"Not Found","code":404}}`
	// routerRoute404 is GET /api/v1/nonexistent-route/x/endpoints, cut to its
	// head: a page, not an envelope, for an address the router does not serve.
	routerRoute404 = "<!DOCTYPE html><html><head><title>Not Found | OpenRouter</title></head><body>Not Found</body></html>"
)

// TestTheSheetFetcherReadsA404AsNoPageHere is the one answer the sheet may
// remember a base by, made where only the transport can see a status and a
// body: a 404 whose body is NOT the router's envelope is "no such route".
func TestTheSheetFetcherReadsA404AsNoPageHere(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(routerRoute404))
	}))
	defer server.Close()

	body, err := sheetFetcher{http: server.Client()}.Fetch(context.Background(), server.URL, "sk-test")
	if err == nil {
		body.Close()
		t.Fatal("a 404 handed back a body")
	}
	if !errors.Is(err, lanes.ErrNoSheetHere) {
		t.Fatalf("a route 404 came back as %v, want one wrapping lanes.ErrNoSheetHere", err)
	}
	if errors.Is(err, errNoSheetForModel) {
		t.Fatalf("a route 404 was also read as a verdict about a model: %v", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("the error was %q and does not say what the router answered", err)
	}
}

// TestTheSheetFetcherReadsTheRoutersOwn404AsNoSheetForThatModel is the other
// 404: the router's own error envelope, which is the route answering about ONE
// MODEL it does not publish. It marks nothing about the base — reading it as
// "no page here" would hold a whole proxy or mirror sheetless for five minutes
// on the strength of one model the beat happened to ask about first, which is
// the defect class of #373 over again.
func TestTheSheetFetcherReadsTheRoutersOwn404AsNoSheetForThatModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(routerModel404))
	}))
	defer server.Close()

	body, err := sheetFetcher{http: server.Client()}.Fetch(context.Background(), server.URL, "sk-test")
	if err == nil {
		body.Close()
		t.Fatal("a 404 handed back a body")
	}
	if errors.Is(err, lanes.ErrNoSheetHere) {
		t.Fatalf("the router's own envelope 404 was read as the base having no endpoints route: %v", err)
	}
	if !errors.Is(err, errNoSheetForModel) {
		t.Fatalf("the envelope 404 came back as %v, want one wrapping errNoSheetForModel", err)
	}
	// AND IT IS DECODED THROUGH THE ONE REFUSAL OBJECT this package has, not a
	// second reader of the same shape: the router's sentence is recoverable
	// from the chain exactly as it is from a refused completion.
	refused, ok := RefusalFrom(err)
	if !ok || refused.Status != http.StatusNotFound || refused.Message != "Not Found" {
		t.Fatalf("the router's own refusal did not survive the read: %v", err)
	}
}

// TestTheShippedRouterIsCertainAndEveryOtherBaseIsAsked pins what the hostname
// still decides, which is only whether the asking can be skipped. A loopback
// base, a gateway, an empty base: none of them is refused, all of them are
// wired and asked ([WireLaneSheet]).
func TestTheShippedRouterIsCertainAndEveryOtherBaseIsAsked(t *testing.T) {
	for base, want := range map[string]bool{
		"https://openrouter.ai/api/v1":   true,
		" https://OpenRouter.ai/api/v1 ": true,
		"http://localhost:8080/v1":       false,
		"http://127.0.0.1:43121/api/v1":  false,
		"https://api.openai.com/v1":      false,
		"":                               false,
	} {
		if got := LaneSheetCertain(base); got != want {
			t.Errorf("LaneSheetCertain(%q) = %v, want %v", base, got, want)
		}
	}
}

// TestAPlainLoopbackBaseGetsASheetFromItsOwnAnswer is acceptance 1 of #373,
// end to end: a client built against a stub at its plain loopback address —
// no hostname trick anywhere — has a sheet after one refresh, and the only
// request it ever made was that refresh. THE PROBE IS THE FETCH: construction
// costs nothing, and neither a known base nor an unknown one pays a second
// round trip to find out what it is.
func TestAPlainLoopbackBaseGetsASheetFromItsOwnAnswer(t *testing.T) {
	forgetLanes(t)
	const model = "openrouter/plain-base"
	server := lanestub.New(model,
		lanestub.Lane{Name: "quicksilver", Profile: lanestub.Profile{TTFT: 20 * time.Millisecond, Rate: 400, Tokens: 8, Tools: true}},
		lanestub.Lane{Name: "brass", Profile: lanestub.Profile{TTFT: 30 * time.Millisecond, Rate: 300, Tokens: 8, Tools: true}},
	)
	t.Cleanup(server.Close)
	if LaneSheetCertain(server.URL()) {
		t.Fatalf("the stub's address %q reads as the shipped router, so this test would not be asking anything", server.URL())
	}

	if _, err := NewClient(Config{APIKey: "test-key", BaseURL: server.URL(), Model: model, Routing: StaticRouting(RoutingLatency)}); err != nil {
		t.Fatal(err)
	}
	if got := server.Sheets(lanes.LedgerModel(model)); got != 0 {
		t.Fatalf("building a client made %d endpoints requests; the probe is the beat's fetch and never construction's", got)
	}

	if err := lanes.Default().Sheet().Refresh(context.Background(), model); err != nil {
		t.Fatalf("one refresh against a plain loopback base: %v", err)
	}
	rows := lanes.Default().Sheet().Rows(model)
	if len(rows) != 2 {
		t.Fatalf("a plain loopback base gave %d rows after one refresh, want 2", len(rows))
	}
	if got := server.Sheets(lanes.LedgerModel(model)); got != 1 {
		t.Fatalf("one refresh cost %d endpoints requests, want exactly the fetch itself", got)
	}
}

// TestABaseWithNoEndpointsPageIsAskedOnce is acceptance 2 through the real
// transport: the stub answers completions and 404s every endpoints page, the
// client is wired to it regardless, and after the base's one answer nothing
// asks it again.
func TestABaseWithNoEndpointsPageIsAskedOnce(t *testing.T) {
	forgetLanes(t)
	const model = "openrouter/no-sheet"
	server := lanestub.New(model,
		lanestub.Lane{Name: "quicksilver", Profile: lanestub.Profile{TTFT: 20 * time.Millisecond, Rate: 400, Tokens: 8, Tools: true}},
	)
	t.Cleanup(server.Close)
	server.Sheetless()

	if _, err := NewClient(Config{APIKey: "test-key", BaseURL: server.URL(), Model: model, Routing: StaticRouting(RoutingLatency)}); err != nil {
		t.Fatal(err)
	}
	sheet := lanes.Default().Sheet()
	if err := sheet.Refresh(context.Background(), model); !errors.Is(err, lanes.ErrNoSheetHere) {
		t.Fatalf("the base's 404 came back as %v, want lanes.ErrNoSheetHere", err)
	}
	for _, again := range []string{model, "openrouter/another-model"} {
		if err := sheet.Refresh(context.Background(), again); !errors.Is(err, lanes.ErrNoSheetHere) {
			t.Fatalf("a refresh of %s after the base had answered came back as %v", again, err)
		}
	}
	if got := server.Sheets(lanes.LedgerModel(model)) + server.Sheets("openrouter/another-model"); got != 1 {
		t.Fatalf("a base that said it has no endpoints page was asked %d times, want once", got)
	}
	if rows := sheet.Rows(model); rows != nil {
		t.Fatalf("a sheetless base produced rows: %v", rows)
	}
}

// TestAModelTheRouterDoesNotPublishHoldsNothingAgainstTheBase is the body rule
// through the real transport against the stub, which answers its two 404s the
// way the live router does: the first model the beat asks about is one the
// router does not publish, and that must cost the base nothing — the very next
// refresh, for a model it does publish, gets its sheet with no wait and no
// second answer needed.
func TestAModelTheRouterDoesNotPublishHoldsNothingAgainstTheBase(t *testing.T) {
	forgetLanes(t)
	const published = "openrouter/published-model"
	const unknown = "openrouter/unknown-model"
	server := lanestub.New(published,
		lanestub.Lane{Name: "quicksilver", Profile: lanestub.Profile{TTFT: 20 * time.Millisecond, Rate: 400, Tokens: 8, Tools: true}},
	)
	t.Cleanup(server.Close)

	if _, err := NewClient(Config{APIKey: "test-key", BaseURL: server.URL(), Model: unknown, Routing: StaticRouting(RoutingLatency)}); err != nil {
		t.Fatal(err)
	}
	sheet := lanes.Default().Sheet()
	err := sheet.Refresh(context.Background(), unknown)
	if err == nil {
		t.Fatal("a model the router does not publish came back with a sheet")
	}
	if errors.Is(err, lanes.ErrNoSheetHere) {
		t.Fatalf("the router's 404 about one model was read as the base having no endpoints route: %v", err)
	}
	if !errors.Is(err, errNoSheetForModel) {
		t.Fatalf("the router's 404 about one model came back as %v", err)
	}
	if rows := sheet.Rows(unknown); rows != nil {
		t.Fatalf("an unpublished model produced rows: %v", rows)
	}

	// The base was not held: the published model is fetched at once.
	if err := sheet.Refresh(context.Background(), published); err != nil {
		t.Fatalf("a refresh for a published model after an unpublished one: %v", err)
	}
	if rows := sheet.Rows(published); len(rows) != 1 {
		t.Fatalf("the published model has %d rows after one refresh, want 1", len(rows))
	}
	if got := server.Sheets(lanes.LedgerModel(published)); got != 1 {
		t.Fatalf("the published model's refresh cost %d endpoints requests, want exactly the fetch itself", got)
	}
	if got := server.Sheets(lanes.LedgerModel(unknown)); got != 1 {
		t.Fatalf("the unpublished model was asked %d times, want once", got)
	}
}

// TestConstructionWiresTheSheetAtARouter is the wire point itself: the live
// sheet cannot fetch until a client that talks to a router hands it a base, a
// bearer and a transport, and [NewClient] is the one place that happens.
//
// It asserts through a CANCELLED context, which is what keeps this test off the
// network. An unwired sheet refuses with [lanes.ErrNoSheet] whatever the caller
// passes, because it has nothing to fetch with; a wired one gets as far as the
// transport and comes back with the context's own error, and the only way to
// tell those two apart is to have been wired.
func TestConstructionWiresTheSheetAtARouter(t *testing.T) {
	defer lanes.Default().Reset()

	lanes.Default().Reset()
	dead, stop := context.WithCancel(context.Background())
	stop()
	if err := lanes.Default().Sheet().Refresh(dead, "deepseek/deepseek-v4-flash"); !errors.Is(err, lanes.ErrNoSheet) {
		t.Fatalf("a fresh registry's sheet refused with %v, want ErrNoSheet", err)
	}

	// A BASE NOBODY HAS VOUCHED FOR IS WIRED TOO. It used to wire nothing on
	// the strength of its hostname (issue #373); now every base is wired, and
	// whether there is a page there is the base's own to answer on the first
	// refresh. Through a cancelled context that answer is the context's own
	// error, which is exactly what proves the wiring reached the transport.
	if _, err := NewClient(Config{BaseURL: "http://localhost:8080/v1", Model: "local/model", APIKey: "sk-test"}); err != nil {
		t.Fatalf("build a client against a local base: %v", err)
	}
	if err := lanes.Default().Sheet().Refresh(dead, "deepseek/deepseek-v4-flash"); !errors.Is(err, context.Canceled) {
		t.Fatalf("a local base left the sheet unwired; it refused with %v, want the cancelled context's own error", err)
	}

	// AND AN EMPTY BASE WIRES NOTHING, because there is nowhere to ask. The
	// constructor refuses an empty base before it gets this far, so the door
	// the process beat uses is asked directly.
	lanes.Default().Reset()
	WireLaneSheet("   ", "sk-test")
	if err := lanes.Default().Sheet().Refresh(dead, "deepseek/deepseek-v4-flash"); !errors.Is(err, lanes.ErrNoSheet) {
		t.Fatalf("an empty base wired the sheet; it refused with %v, want ErrNoSheet", err)
	}

	if _, err := NewClient(Config{BaseURL: "https://openrouter.ai/api/v1", Model: "deepseek/deepseek-v4-flash", APIKey: "sk-test"}); err != nil {
		t.Fatalf("build a client against the router: %v", err)
	}
	err := lanes.Default().Sheet().Refresh(dead, "deepseek/deepseek-v4-flash")
	if errors.Is(err, lanes.ErrNoSheet) {
		t.Fatal("construction against the router left the sheet unwired")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the wired sheet refused with %v, want the cancelled context's own error", err)
	}
}

// ── ONE TEST'S BELIEFS ARE NOT ANOTHER'S ────────────────────────────────────

// forgetLanes gives one test a registry of its own.
//
// The velocity ledger this package's tests were written around is PER CLIENT
// (`client.velocity = newVelocityLedger()`), so every test that built a client
// got a blank one. The lane registry is per PROCESS, by design — a belief about
// a machine is a fact about this build's afternoon and not about one caller —
// and a test binary is one process. So without this, a test that streamed an
// answer taught every test after it, and the ones that assert on what a FIRST
// request carries failed on an order they could not have learned.
//
// It is called from the builders rather than from each test, because the thing
// that needs the clean registry is the thing that is about to send.
func forgetLanes(t *testing.T) {
	t.Helper()
	// A HOME OF ITS OWN AS WELL AS A REGISTRY OF ITS OWN, and the second alone
	// is not enough: the ledger writes through a store on the way in and reads
	// it back on its first question, so a fresh registry pointed at the same
	// file inherits the previous test's beliefs from disk a moment after being
	// emptied. [TestMain] moves the state root off the developer's machine;
	// this moves it again, per test.
	t.Setenv(home.EnvVar, t.TempDir())
	lanes.Default().Reset()
	lanes.ForgetPrefixes()
	t.Cleanup(func() {
		lanes.Default().Reset()
		lanes.ForgetPrefixes()
	})
}

// TestTheUsageFrameTeachesTheLedgerWhatWasCached is the last of the four things
// an answer teaches, and the only one that comes from the router rather than
// from a clock.
//
// A LANE HOLDING OUR PREFIX IS WHAT MAKES PRICE PATH-DEPENDENT: the cheapest
// lane on the sheet is not the cheapest lane for a request whose prompt another
// lane already has. Everything else this process knows about that is its own
// memory of where it sent the last request; `cached_tokens` on the usage frame
// is the only DIRECT evidence, so it is passed through and never estimated —
// a cache hit derived from the adapter's own four-characters-a-token guess
// would be a discount nobody granted.
func TestTheUsageFrameTeachesTheLedgerWhatWasCached(t *testing.T) {
	client, _, model := stubbedRouter(t)
	ledger := primed(t, model, laneBelief(model, "quicksilver", 400, 70, 0.25))

	client.noteLane(model, "quicksilver", 300*time.Millisecond, 40, time.Second, 0, 1536)
	sightings := ledger.sightings()
	if len(sightings) != 1 {
		t.Fatalf("%d sightings for one answer", len(sightings))
	}
	if got := sightings[0].CachedTokens; got != 1536 {
		t.Fatalf("the sighting carries %d cached tokens, want the frame's own 1536", got)
	}

	// And a frame that said nothing carries nothing, which reads the same as a
	// cold prefix — because neither is evidence of a cache.
	client.noteLane(model, "quicksilver", 300*time.Millisecond, 40, time.Second, 0, 0)
	if got := ledger.sightings()[1].CachedTokens; got != 0 {
		t.Fatalf("a frame that said nothing about caching taught %d cached tokens", got)
	}
}

// judged is the outcomes half of [recordingLedger.sightings]: what the transport
// told the belief about whether its answers could be used.
func (l *recordingLedger) judged() []lanes.Outcome {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]lanes.Outcome(nil), l.outcomes...)
}
