package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── THE LEDGER ──────────────────────────────────────────────────────────────
//
// Every test here scripts answers from NAMED endpoints and asserts what the
// next request would carry as a result. No vendor is named: the names below are
// invented, which is the point — the law reads whatever the wire said.

// fakeClock is the ledger's time, so a five-minute cooldown is asserted in
// microseconds.
type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func newClock() *fakeClock {
	return &fakeClock{at: time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func testLedger() (*velocityLedger, *fakeClock) {
	clock := newClock()
	ledger := newVelocityLedger()
	ledger.now = clock.now
	return ledger, clock
}

// laggy is one slow answer: a first token well past the threshold.
func (l *velocityLedger) laggy(model, served string) Sighting {
	return l.observe(model, served, LagTTFT+500*time.Millisecond, 200, 2*time.Second)
}

// brisk is one fast answer: prompt first token, a healthy sustained rate.
func (l *velocityLedger) brisk(model, served string) Sighting {
	return l.observe(model, served, 200*time.Millisecond, 400, time.Second)
}

func TestVelocityDemotesAfterTwoStrikesAndIgnoresAfterThree(t *testing.T) {
	ledger, _ := testLedger()
	const model = "vendor/fast-model"

	ledger.brisk(model, "quicksilver")
	ledger.laggy(model, "molasses")

	// ONE strike is not a verdict: a single slow answer is as likely to be a
	// cold cache as a bad endpoint, so nothing about the next request changes.
	order, ignore := ledger.preferences(model)
	if want := []string{"quicksilver", "molasses"}; !equalStrings(order, want) {
		t.Fatalf("after one strike order = %v, want %v (first seen, nothing demoted)", order, want)
	}
	if len(ignore) != 0 {
		t.Fatalf("after one strike ignore = %v, want none", ignore)
	}

	ledger.laggy(model, "molasses")
	order, ignore = ledger.preferences(model)
	if want := []string{"quicksilver", "molasses"}; !equalStrings(order, want) {
		t.Fatalf("after two strikes order = %v, want the slow lane last", order)
	}
	if len(ignore) != 0 {
		t.Fatalf("after two strikes ignore = %v, want none — demotion first, refusal later", ignore)
	}
	// The demotion is only visible as an ORDER when there is something to be
	// demoted behind, so it is asserted against a third, healthy lane too.
	ledger.brisk(model, "zephyr")
	order, _ = ledger.preferences(model)
	if want := []string{"quicksilver", "zephyr", "molasses"}; !equalStrings(order, want) {
		t.Fatalf("order = %v, want both healthy lanes ahead of the demoted one (%v)", order, want)
	}

	ledger.laggy(model, "molasses")
	order, ignore = ledger.preferences(model)
	if want := []string{"quicksilver", "zephyr"}; !equalStrings(order, want) {
		t.Fatalf("after three strikes order = %v, want only the healthy lanes", order)
	}
	if want := []string{"molasses"}; !equalStrings(ignore, want) {
		t.Fatalf("after three strikes ignore = %v, want %v", ignore, want)
	}
}

func TestVelocityRefusalExpiresIntoProbation(t *testing.T) {
	ledger, clock := testLedger()
	const model = "vendor/fast-model"
	ledger.brisk(model, "quicksilver")
	for i := 0; i < ignoreAfter; i++ {
		ledger.laggy(model, "molasses")
	}

	clock.advance(ignoreCooldown - time.Second)
	if _, ignore := ledger.preferences(model); !equalStrings(ignore, []string{"molasses"}) {
		t.Fatalf("a second before the cooldown expires ignore = %v, want it still refused", ignore)
	}

	clock.advance(2 * time.Second)
	order, ignore := ledger.preferences(model)
	if len(ignore) != 0 {
		t.Fatalf("past the cooldown ignore = %v, want the lane tried again", ignore)
	}
	if want := []string{"quicksilver", "molasses"}; !equalStrings(order, want) {
		t.Fatalf("past the cooldown order = %v, want it back but demoted (%v)", order, want)
	}

	// PROBATION: it came back one strike short of refusal, so the very next slow
	// answer refuses it again rather than granting a fresh three.
	ledger.laggy(model, "molasses")
	if _, ignore = ledger.preferences(model); !equalStrings(ignore, []string{"molasses"}) {
		t.Fatalf("after one more slow answer ignore = %v, want it refused again at once", ignore)
	}
}

func TestVelocityFastAnswerPaysBackAStrike(t *testing.T) {
	ledger, _ := testLedger()
	const model = "vendor/fast-model"
	ledger.brisk(model, "quicksilver")
	ledger.laggy(model, "molasses")
	ledger.laggy(model, "molasses")
	ledger.brisk(model, "molasses")

	// One strike back is one strike below the demotion line: the lane is healthy
	// again, and the ledger is a measurement rather than a ratchet.
	if order, _ := ledger.preferences(model); !equalStrings(order, []string{"quicksilver", "molasses"}) {
		t.Fatalf("order = %v, want the recovered lane back among the healthy", order)
	}
	ledger.laggy(model, "molasses")
	if order, _ := ledger.preferences(model); !equalStrings(order, []string{"quicksilver", "molasses"}) {
		t.Fatalf("order = %v, want two fresh strikes needed to demote it again", order)
	}
}

// When nothing is healthy, the order is dropped entirely: provider.order names
// what to try FIRST, so a list of nothing but demoted endpoints would pin the
// slowest thing known to the front — the exact inverse of the intent.
func TestVelocityOmitsOrderWhenNoLaneIsHealthy(t *testing.T) {
	ledger, _ := testLedger()
	const model = "vendor/fast-model"
	ledger.laggy(model, "molasses")
	ledger.laggy(model, "molasses")
	ledger.laggy(model, "treacle")
	ledger.laggy(model, "treacle")
	ledger.laggy(model, "treacle")

	order, ignore := ledger.preferences(model)
	if len(order) != 0 {
		t.Fatalf("order = %v, want none — a list of only-demoted endpoints pins the worst one first", order)
	}
	if !equalStrings(ignore, []string{"treacle"}) {
		t.Fatalf("ignore = %v, want the refusal to stand regardless", ignore)
	}
}

// THE LEDGER MAY NEVER REFUSE EVERYTHING IT KNOWS. On a single-provider model
// three slow answers used to put the only name in `ignore`, and every request
// for five minutes died on an instant "All providers have been ignored" 404 —
// an outage this process built for itself. When nothing is left to prefer, the
// verdict is no verdict at all.
func TestVelocityNeverRefusesEveryLaneItKnows(t *testing.T) {
	ledger, _ := testLedger()
	const model = "vendor/one-provider-model"
	for i := 0; i < ignoreAfter; i++ {
		ledger.laggy(model, "molasses")
	}
	order, ignore := ledger.preferences(model)
	if len(order) != 0 || len(ignore) != 0 {
		t.Fatalf("order = %v ignore = %v, want no verdict — refusing the only lane is an outage", order, ignore)
	}

	// A second lane that is also struck out must not turn the guard back off.
	for i := 0; i < ignoreAfter; i++ {
		ledger.laggy(model, "treacle")
	}
	order, ignore = ledger.preferences(model)
	if len(order) != 0 || len(ignore) != 0 {
		t.Fatalf("with every lane refused order = %v ignore = %v, want no verdict at all", order, ignore)
	}

	// The moment one lane recovers, the refusals stand again: the guard is
	// about condemning the whole set, never about forgiving a slow lane.
	ledger.brisk(model, "zephyr")
	_, ignore = ledger.preferences(model)
	if !equalStrings(ignore, []string{"molasses", "treacle"}) {
		t.Fatalf("with a healthy lane back ignore = %v, want both slow lanes refused again", ignore)
	}
}

// A 429 that names its endpoint is that endpoint saying "not now" — better
// evidence than any timed answer, acted on at once and recovered from the same
// way a laggy refusal is.
func TestAPacedProviderIsRefusedAtOnceAndWalksBackOut(t *testing.T) {
	ledger, clock := testLedger()
	const model = "vendor/fast-model"
	ledger.brisk(model, "quicksilver")

	ledger.pace(model, "molasses", 30*time.Second)
	if _, ignore := ledger.preferences(model); !equalStrings(ignore, []string{"molasses"}) {
		t.Fatalf("after a named 429 ignore = %v, want the lane refused at once", ignore)
	}

	// The named wait is honoured, and expiry lands the lane on probation —
	// demoted, one strike short — exactly as a laggy refusal expires.
	clock.advance(31 * time.Second)
	order, ignore := ledger.preferences(model)
	if len(ignore) != 0 {
		t.Fatalf("past the named wait ignore = %v, want the lane tried again", ignore)
	}
	if want := []string{"quicksilver", "molasses"}; !equalStrings(order, want) {
		t.Fatalf("past the named wait order = %v, want it back but demoted (%v)", order, want)
	}

	// One fast answer starts walking it out, the same ledger law as ever.
	ledger.brisk(model, "molasses")
	ledger.brisk(model, "molasses")
	if order, _ := ledger.preferences(model); !equalStrings(order, []string{"quicksilver", "molasses"}) {
		t.Fatalf("after fast answers order = %v, want the lane healthy again", order)
	}
}

// A wait the provider did not name, or named absurdly, is clamped to the same
// cooldown a laggy lane serves: pacing is a claim about the next minutes.
func TestAPacedProviderWaitIsClampedToTheCooldown(t *testing.T) {
	ledger, clock := testLedger()
	const model = "vendor/fast-model"
	ledger.pace(model, "molasses", 24*time.Hour)
	ledger.brisk(model, "quicksilver")
	clock.advance(ignoreCooldown - time.Second)
	if _, ignore := ledger.preferences(model); !equalStrings(ignore, []string{"molasses"}) {
		t.Fatalf("inside the cooldown ignore = %v, want the lane still refused", ignore)
	}
	clock.advance(2 * time.Second)
	if _, ignore := ledger.preferences(model); len(ignore) != 0 {
		t.Fatalf("a day-long wait was honoured: ignore = %v, want the clamp at the cooldown", ignore)
	}
}

// pacedProviderName reads the router's own 429 body, and only that: a refusal
// shaped any other way answers "" and keeps the pacing behaviour it always had.
func TestPacedProviderNameReadsTheRoutersMetadata(t *testing.T) {
	routed := `{"error":{"message":"Provider returned error","code":429,` +
		`"metadata":{"raw":"model is temporarily rate-limited upstream","provider_name":"Sundial"}}}`
	if got := pacedProviderName([]byte(routed)); got != "Sundial" {
		t.Fatalf("named provider = %q, want Sundial", got)
	}
	for _, body := range []string{
		`{"error":{"message":"too many requests","code":429}}`,
		`{"message":"slow down"}`,
		`not json at all`,
		``,
	} {
		if got := pacedProviderName([]byte(body)); got != "" {
			t.Fatalf("body %q named %q, want nothing", body, got)
		}
	}
}

// A rate computed over a twelve-token answer measures the handshake, so short
// answers are judged on their first token only.
func TestVelocityDoesNotRateAnswersBelowTheFloor(t *testing.T) {
	ledger, _ := testLedger()
	const model = "vendor/fast-model"
	sighting := ledger.observe(model, "quicksilver", 100*time.Millisecond, ratedFloor-1, 10*time.Second)
	if sighting.Rate != 0 {
		t.Fatalf("rate = %v, want it unrated below the floor", sighting.Rate)
	}
	if sighting.Laggy {
		t.Fatal("a short prompt answer was called laggy on a rate that measures the handshake")
	}
	// The same short answer, kept waiting, IS laggy: TTFT judges on its own.
	slow := ledger.observe(model, "quicksilver", LagTTFT+time.Millisecond, 4, time.Second)
	if !slow.Laggy {
		t.Fatal("a first token past the threshold was not called laggy")
	}
}

func TestVelocityRatesSustainedOutput(t *testing.T) {
	ledger, _ := testLedger()
	const model = "vendor/fast-model"
	fast := ledger.observe(model, "quicksilver", 100*time.Millisecond, 900, 10*time.Second)
	if fast.Rate != 90 || fast.Laggy {
		t.Fatalf("90 tok/s read as %v laggy=%v, want a healthy rate", fast.Rate, fast.Laggy)
	}
	slow := ledger.observe(model, "molasses", 100*time.Millisecond, 200, 10*time.Second)
	if slow.Rate != 20 || !slow.Laggy {
		t.Fatalf("20 tok/s read as %v laggy=%v, want it under the %v floor", slow.Rate, slow.Laggy, LagRate)
	}
}

// An answer nobody signed is measured — the model's own last sighting is still
// a fact — but earns no strike, because a strike is a claim about an endpoint.
func TestVelocityAttributesNothingToAnUnnamedEndpoint(t *testing.T) {
	ledger, _ := testLedger()
	const model = "vendor/fast-model"
	ledger.laggy(model, "")
	ledger.laggy(model, "")
	ledger.laggy(model, "")
	if order, ignore := ledger.preferences(model); len(order) != 0 || len(ignore) != 0 {
		t.Fatalf("order=%v ignore=%v, want nothing attributed to an unnamed server", order, ignore)
	}
	if sighting, ok := ledger.lastServed(model); !ok || !sighting.Laggy {
		t.Fatalf("lastServed = %+v, %v — want the measurement kept even unattributed", sighting, ok)
	}
}

// The ledger is keyed on the model itself and not on how it was written: the
// "~" routing marker is aforge's own, and it must not split one model in two.
func TestVelocityKeysOnTheModelNotItsSpelling(t *testing.T) {
	ledger, _ := testLedger()
	ledger.laggy("Vendor/Fast-Model", "molasses")
	ledger.laggy("~vendor/fast-model", "molasses")
	ledger.brisk("vendor/fast-model", "zephyr")
	if order, _ := ledger.preferences("~VENDOR/FAST-MODEL"); !equalStrings(order, []string{"zephyr", "molasses"}) {
		t.Fatalf("order = %v, want one lane per endpoint however the model was spelled", order)
	}
}

// ── THE WIRE ────────────────────────────────────────────────────────────────

// routedClient is a client against a router-shaped endpoint, with its own
// ledger so one test's measurements cannot reach another's.
func routedClient(t *testing.T, strategy RoutingStrategy, handler http.Handler) (*Client, *capture) {
	t.Helper()
	recorded := &capture{}
	client, err := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   "vendor/fast-model",
		Routing: StaticRouting(strategy),
		HTTPClient: handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			recorded.record(request)
			handler.ServeHTTP(writer, request)
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	return client, recorded
}

func answered(body string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(body))
	})
}

const plainAnswer = `{"model":"vendor/fast-model","provider":"quicksilver",` +
	`"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],` +
	`"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`

// prefsOn reads the routing preference object off a recorded request.
func prefsOn(t *testing.T, recorded *capture, index int) map[string]any {
	t.Helper()
	body := recorded.body(index)
	if body == nil {
		t.Fatalf("no request recorded at %d", index)
	}
	prefs, ok := body["provider"].(map[string]any)
	if !ok {
		return nil
	}
	return prefs
}

func TestRequestAsksForTheFastestEndpointAndProtectsItsParameters(t *testing.T) {
	client, recorded := routedClient(t, RoutingLatency, answered(plainAnswer))
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs := prefsOn(t, recorded, 0)
	if prefs == nil {
		t.Fatal("no provider preferences on the wire")
	}
	if prefs["sort"] != "latency" {
		t.Fatalf("sort = %v, want latency", prefs["sort"])
	}
	if prefs["allow_fallbacks"] != true {
		t.Fatalf("allow_fallbacks = %v, want true — every veto here stays advisory", prefs["allow_fallbacks"])
	}
	if prefs["require_parameters"] != true {
		t.Fatalf("require_parameters = %v, want true — the reasoning knob must not be silently dropped",
			prefs["require_parameters"])
	}
	// Nothing has been measured yet, so the ledger says nothing.
	if _, ok := prefs["order"]; ok {
		t.Fatalf("order = %v on the first request, want none until something has been timed", prefs["order"])
	}
	if _, ok := prefs["ignore"]; ok {
		t.Fatalf("ignore = %v on the first request, want none", prefs["ignore"])
	}
}

func TestRequestSortsOnPriceWhenAsked(t *testing.T) {
	client, recorded := routedClient(t, RoutingPrice, answered(plainAnswer))
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if prefs := prefsOn(t, recorded, 0); prefs == nil || prefs["sort"] != "price" {
		t.Fatalf("preferences = %v, want sort price", prefs)
	}
}

// Off is a real answer and it is total: no preference object, and no ledger.
func TestRoutingOffSendsNoPreferencesAndMeasuresNothing(t *testing.T) {
	client, recorded := routedClient(t, RoutingOff, answered(plainAnswer))
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if raw := recorded.body(0); raw != nil {
		if _, ok := raw["provider"]; ok {
			t.Fatalf("provider = %v with routing off, want the field absent entirely", raw["provider"])
		}
	}
	if sighting, ok := client.velocity.lastServed("vendor/fast-model"); ok {
		t.Fatalf("measured %+v with routing off — a session that asked for no routing asked for no ledger", sighting)
	}
}

// The preference object is a router's dialect. An endpoint that is not a router
// either ignores it or 400s on it, and neither is worth risking.
func TestNonRouterEndpointCarriesNoPreferences(t *testing.T) {
	client, recorded := newTestClient(t, Config{Routing: StaticRouting(RoutingLatency)})
	client.velocity = newVelocityLedger()
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if raw := recorded.body(0); raw != nil {
		if _, ok := raw["provider"]; ok {
			t.Fatalf("provider = %v on a plain endpoint, want the field absent", raw["provider"])
		}
	}
}

// The ledger's verdict reaches the wire, and it is read at ENCODE time: what
// the last answer earned applies to the request being written now.
func TestLedgerVerdictRidesTheNextRequest(t *testing.T) {
	client, recorded := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "quicksilver")
	client.velocity.laggy(model, "molasses")
	client.velocity.laggy(model, "molasses")
	client.velocity.laggy(model, "treacle")
	client.velocity.laggy(model, "treacle")
	client.velocity.laggy(model, "treacle")

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs := prefsOn(t, recorded, 0)
	if prefs == nil {
		t.Fatal("no provider preferences on the wire")
	}
	if got := words(prefs["order"]); !equalStrings(got, []string{"quicksilver", "molasses"}) {
		t.Fatalf("order = %v, want the healthy lane first and the demoted one behind it", got)
	}
	if got := words(prefs["ignore"]); !equalStrings(got, []string{"treacle"}) {
		t.Fatalf("ignore = %v, want the refused lane", got)
	}
	// And it is still a sort request, not a pin.
	if prefs["sort"] != "latency" || prefs["allow_fallbacks"] != true {
		t.Fatalf("preferences = %v, want the standing ask kept alongside the verdict", prefs)
	}
}

// A cut is stronger evidence than a slow completion: the endpoint stopped
// producing the answer entirely, so one cut must steer the very next encode.
func TestNamedCutRefusesTheEndpointOnTheNextRequest(t *testing.T) {
	client, recorded := cutThenAnswerClient(t, RoutingLatency, "molasses")
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "quicksilver")
	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})

	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("the stalled first stream landed, want a guard cut")
	}
	prefs := client.providerPreferences(model)
	if prefs == nil || !equalStrings(prefs.Order, []string{"quicksilver"}) ||
		!equalStrings(prefs.Ignore, []string{"molasses"}) {
		t.Fatalf("preferences after cut = %#v, want the named endpoint refused behind the healthy lane", prefs)
	}
	if _, err := client.CompleteWithMessages(ctx, userMessages("again")); err != nil {
		t.Fatal(err)
	}
	written := prefsOn(t, recorded, 1)
	if got := words(written["order"]); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("next request order = %v, want the healthy endpoint", got)
	}
	if got := words(written["ignore"]); !equalStrings(got, []string{"molasses"}) {
		t.Fatalf("next request ignore = %v, want the endpoint that was cut", got)
	}
}

func TestUnnamedCutNotesNothing(t *testing.T) {
	client, _ := cutThenAnswerClient(t, RoutingLatency, "")
	client.velocity.brisk("vendor/fast-model", "quicksilver")
	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("the stalled first stream landed, want a guard cut")
	}
	prefs := client.providerPreferences("vendor/fast-model")
	if prefs == nil || !equalStrings(prefs.Order, []string{"quicksilver"}) || len(prefs.Ignore) != 0 {
		t.Fatalf("preferences after unnamed cut = %#v, want no endpoint attributed", prefs)
	}
}

func TestRoutingOffNotesNothingForACut(t *testing.T) {
	client, _ := cutThenAnswerClient(t, RoutingOff, "molasses")
	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("the stalled first stream landed, want a guard cut")
	}
	if order, ignore := client.velocity.preferences("vendor/fast-model"); len(order) != 0 || len(ignore) != 0 {
		t.Fatalf("routing off recorded order=%v ignore=%v, want no cut ledger entry", order, ignore)
	}
}

// cutThenAnswerClient makes the first stream identify itself, write one token,
// and stop until the guard cancels it. The second stream lands, exposing the
// preferences rebuilt after the cut without involving the turn loop's retry.
func cutThenAnswerClient(t *testing.T, strategy RoutingStrategy, served string) (*Client, *capture) {
	t.Helper()
	t.Cleanup(shortenStallBounds(t, time.Second, 20*time.Millisecond))
	recorded := &capture{}
	var mu sync.Mutex
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorded.record(request)
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			provider := ""
			if served != "" {
				provider = `,"provider":"` + served + `"`
			}
			reader, writer := io.Pipe()
			go func() {
				_, _ = writer.Write([]byte(`data: {"id":"cut"` + provider +
					`,"choices":[{"index":0,"delta":{"content":"start"}}]}` + "\n\n"))
				<-request.Context().Done()
				_ = writer.CloseWithError(request.Context().Err())
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       reader,
				Request:    request,
			}, nil
		}
		body := `data: {"id":"done","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}` +
			"\n\ndata: [DONE]\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "https://openrouter.ai/api/v1", Model: "vendor/fast-model",
		Routing: StaticRouting(strategy), HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	return client, recorded
}

// ── THE MEASUREMENT ─────────────────────────────────────────────────────────

// The streamed path is the only one where the two facts are separable: how long
// the endpoint took to say anything, and how fast it wrote once it started.
func TestStreamedAnswerIsTimedAndAttributed(t *testing.T) {
	const model = "vendor/fast-model"
	chunks := []string{
		`{"id":"one","provider":"quicksilver","choices":[{"index":0,"delta":{"role":"assistant","content":"the "}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":10,"completion_tokens":600,"total_tokens":610}}`,
	}
	client, _ := routedClient(t, RoutingLatency, sseHandler(chunks...))
	clock := newClock()
	// The send costs 400ms, and the answer's two chunks a second between them.
	ticks := []time.Duration{400 * time.Millisecond, time.Second, 0}
	step := 0
	client.now = func() time.Time {
		at := clock.now()
		if step < len(ticks) {
			clock.advance(ticks[step])
			step++
		}
		return at
	}
	client.velocity.now = clock.now

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	sighting, ok := client.velocity.lastServed(model)
	if !ok {
		t.Fatal("nothing measured for a streamed answer")
	}
	if sighting.Provider != "quicksilver" {
		t.Fatalf("provider = %q, want the endpoint the stream named", sighting.Provider)
	}
	if sighting.TTFT != 400*time.Millisecond {
		t.Fatalf("TTFT = %s, want the wait before the first token and not the whole call", sighting.TTFT)
	}
	if sighting.Rate != 600 {
		t.Fatalf("rate = %v tok/s, want 600 output tokens over the one-second generation window", sighting.Rate)
	}
	if sighting.Laggy {
		t.Fatal("a prompt, fast answer was called laggy")
	}
}

// A slow stream is what actually earns strikes, and two of them change the next
// request — end to end, through the wire rather than through the ledger's API.
func TestTwoSlowStreamsDemoteTheEndpointOnTheNextRequest(t *testing.T) {
	const served = "molasses"
	chunk := `{"id":"one","provider":"` + served + `","choices":[{"index":0,"delta":{"content":"slow"},` +
		`"finish_reason":"stop"}],"usage":{"completion_tokens":100,"total_tokens":100}}`
	client, recorded := routedClient(t, RoutingLatency, sseHandler(chunk))
	clock := newClock()
	client.now = func() time.Time {
		at := clock.now()
		clock.advance(LagTTFT + time.Second)
		return at
	}
	client.velocity.now = clock.now
	// A healthy lane, so the demotion has something to stand behind.
	client.velocity.brisk("vendor/fast-model", "quicksilver")

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	for turn := 0; turn < 3; turn++ {
		if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
			t.Fatal(err)
		}
	}
	// The first request knows only the healthy lane it was seeded with — the
	// slow endpoint has not answered yet, so there is nothing to demote.
	if got := words(prefsOn(t, recorded, 0)["order"]); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("the first request's order = %v, want only the lane already measured", got)
	}
	prefs := prefsOn(t, recorded, 2)
	if prefs == nil {
		t.Fatal("no provider preferences on the third request")
	}
	if got := words(prefs["order"]); !equalStrings(got, []string{"quicksilver", served}) {
		t.Fatalf("order = %v, want the twice-slow endpoint demoted behind the healthy one", got)
	}
}

func TestNonStreamedAnswerIsAttributedButNotJudgedOnFirstToken(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	clock := newClock()
	client.now = func() time.Time {
		at := clock.now()
		clock.advance(10 * time.Second)
		return at
	}
	client.velocity.now = clock.now
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	sighting, ok := client.velocity.lastServed("vendor/fast-model")
	if !ok {
		t.Fatal("nothing measured for a non-streamed answer")
	}
	if sighting.Provider != "quicksilver" {
		t.Fatalf("provider = %q, want the endpoint the body named", sighting.Provider)
	}
	if sighting.TTFT != 0 {
		t.Fatalf("TTFT = %s, want zero: a non-streamed answer has no first token to wait for", sighting.TTFT)
	}
	if sighting.Laggy {
		t.Fatal("a ten-second non-streamed call was called laggy — there is no TTFT to judge it on")
	}
}

// The whole point of the ledger is that a surface can say who served. It is a
// copy, and it is per model.
func TestLastServedIsTheProcessesLatestSighting(t *testing.T) {
	ledger, _ := testLedger()
	ledger.brisk("vendor/fast-model", "quicksilver")
	ledger.brisk("vendor/other-model", "zephyr")
	if sighting, ok := ledger.lastServed("vendor/fast-model"); !ok || sighting.Provider != "quicksilver" {
		t.Fatalf("lastServed = %+v, %v", sighting, ok)
	}
	if _, ok := ledger.lastServed("vendor/unknown-model"); ok {
		t.Fatal("a model nothing has answered reported a sighting")
	}
}

func TestParseRoutingStrategyFallsBackToTheDefault(t *testing.T) {
	for word, want := range map[string]RoutingStrategy{
		"latency": RoutingLatency,
		"PRICE":   RoutingPrice,
		" off ":   RoutingOff,
	} {
		if got, ok := ParseRoutingStrategy(word); got != want || !ok {
			t.Fatalf("ParseRoutingStrategy(%q) = %v, %v, want %v, true", word, got, ok, want)
		}
	}
	// A word this build does not know must not take routing away.
	if got, ok := ParseRoutingStrategy("fastest-please"); got != RoutingLatency || ok {
		t.Fatalf("ParseRoutingStrategy(unknown) = %v, %v, want latency, false", got, ok)
	}
	if got, _ := ParseRoutingStrategy(""); got != RoutingLatency {
		t.Fatalf("ParseRoutingStrategy(empty) = %v, want the default", got)
	}
}

// words reads a JSON array of strings back out of a decoded body.
func words(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, _ := item.(string)
		out = append(out, text)
	}
	return out
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The preference object is exactly the five fields, spelled the way the router
// spells them. A rename here is a silent routing change, so it is pinned.
func TestPreferenceObjectWireNames(t *testing.T) {
	yes := true
	encoded, err := json.Marshal(providerPrefs{
		Sort: "latency", Order: []string{"a"}, Ignore: []string{"b"},
		AllowFallbacks: &yes, RequireParameters: &yes,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sort":"latency","order":["a"],"ignore":["b"],"allow_fallbacks":true,"require_parameters":true}`
	if string(encoded) != want {
		t.Fatalf("provider preferences = %s, want %s", encoded, want)
	}
	// And an empty one is nothing at all, so a sort-only request stays clean.
	if encoded, _ = json.Marshal(providerPrefs{Sort: "latency"}); strings.Contains(string(encoded), "order") {
		t.Fatalf("sort-only preferences = %s, want no empty lists", encoded)
	}
}
