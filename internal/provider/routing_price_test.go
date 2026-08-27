package provider

import (
	"context"
	"net/http"
	"testing"
)

// ── THE PRICE OF SPEED ──────────────────────────────────────────────────────
//
// A cost autopsy over 44 bench cells found this adapter paying ~3.5× the list
// price of the very models it named, because `sort: latency` asked for the
// fastest endpoint and named no ceiling at all. These tests pin the two halves
// of the answer: a latency ask carries a ceiling derived from the model's own
// published price, and a call nobody is waiting on does not ask for latency in
// the first place.

// pricedClient is a router-shaped client that knows what its model lists at.
// The prices are per token in US dollars, as the catalog publishes them.
func pricedClient(t *testing.T, routing RoutingSource, prompt, completion float64, known bool) (*Client, *capture) {
	t.Helper()
	recorded := &capture{}
	client, err := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   "vendor/fast-model",
		Routing: routing,
		ModelPrice: func(string) (float64, float64, bool) {
			return prompt, completion, known
		},
		HTTPClient: handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			recorded.record(request)
			answered(plainAnswer).ServeHTTP(writer, request)
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	return client, recorded
}

// ceilingOn reads the price ceiling off a recorded request, nil when none rode.
func ceilingOn(t *testing.T, recorded *capture, index int) map[string]any {
	t.Helper()
	prefs := prefsOn(t, recorded, index)
	if prefs == nil {
		return nil
	}
	ceiling, ok := prefs["max_price"].(map[string]any)
	if !ok {
		return nil
	}
	return ceiling
}

func TestALatencyAskCarriesACeilingDerivedFromTheModelsOwnListPrice(t *testing.T) {
	// $0.40/M in and $1.60/M out, spelled per token the way the catalog does.
	client, recorded := pricedClient(t, nil, 0.0000004, 0.0000016, true)
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs := prefsOn(t, recorded, 0)
	if prefs == nil || prefs["sort"] != "latency" {
		t.Fatalf("preferences = %v, want the latency ask a person waiting is owed", prefs)
	}
	ceiling := ceilingOn(t, recorded, 0)
	if ceiling == nil {
		t.Fatal("a latency ask rode with no ceiling at all, which is the 3.5× the autopsy measured")
	}
	// The router spells its ceilings in dollars per MILLION tokens, and the
	// figure is the model's own list price times latencyPriceCeiling.
	wantPrompt := 0.0000004 * 1_000_000 * latencyPriceCeiling
	wantCompletion := 0.0000016 * 1_000_000 * latencyPriceCeiling
	if got, ok := ceiling["prompt"].(float64); !ok || !closeEnough(got, wantPrompt) {
		t.Fatalf("max_price.prompt = %v, want %v", ceiling["prompt"], wantPrompt)
	}
	if got, ok := ceiling["completion"].(float64); !ok || !closeEnough(got, wantCompletion) {
		t.Fatalf("max_price.completion = %v, want %v", ceiling["completion"], wantCompletion)
	}
}

// ABSENCE, NEVER A GUESS. A model nobody published a price for gets no ceiling,
// because a ceiling invented from nothing would refuse endpoints on a number
// that does not exist.
func TestNoPublishedPriceSendsNoCeiling(t *testing.T) {
	client, recorded := pricedClient(t, nil, 0, 0, false)
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs := prefsOn(t, recorded, 0)
	if prefs == nil || prefs["sort"] != "latency" {
		t.Fatalf("preferences = %v, want the latency ask kept", prefs)
	}
	if _, carried := prefs["max_price"]; carried {
		t.Fatalf("a model with no published price carried a ceiling: %v", prefs["max_price"])
	}
}

// A client with no price seam wired at all is every caller that existed before
// the ceiling did, and it routes exactly as it always has.
func TestAnUnwiredPriceSeamSendsNoCeiling(t *testing.T) {
	client, recorded := routedClient(t, RoutingLatency, answered(plainAnswer))
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if _, carried := prefsOn(t, recorded, 0)["max_price"]; carried {
		t.Fatal("an unwired client invented a ceiling")
	}
}

// ── WHO IS WAITING ──────────────────────────────────────────────────────────

func TestACallNobodyIsWaitingOnRoutesByPrice(t *testing.T) {
	client, recorded := pricedClient(t, nil, 0.0000004, 0.0000016, true)
	ctx := WithRoutingIntent(context.Background(), IntentBackground)
	if _, err := client.CompleteWithMessages(ctx, userMessages("name this")); err != nil {
		t.Fatal(err)
	}
	prefs := prefsOn(t, recorded, 0)
	if prefs == nil || prefs["sort"] != "price" {
		t.Fatalf("preferences = %v, want an errand routed on price", prefs)
	}
	// And no ceiling with it: sorting by price is already asking for the
	// cheapest thing available, and a ceiling could only take endpoints away.
	if _, carried := prefs["max_price"]; carried {
		t.Fatalf("a price-sorted request carried a ceiling: %v", prefs["max_price"])
	}
}

func TestTheConversationsOwnTurnStillRoutesByLatency(t *testing.T) {
	client, recorded := pricedClient(t, nil, 0.0000004, 0.0000016, true)
	ctx := WithRoutingIntent(context.Background(), IntentInteractive)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if prefs := prefsOn(t, recorded, 0); prefs == nil || prefs["sort"] != "latency" {
		t.Fatalf("preferences = %v, want the person's own turn still chasing speed", prefs)
	}
}

// A ROW A PERSON WROTE WINS OVER BOTH. The default moves with who is waiting;
// an explicit choice does not move at all.
func TestAnExplicitRoutingRowOverridesTheIntent(t *testing.T) {
	client, recorded := pricedClient(t, StaticRouting(RoutingLatency), 0.0000004, 0.0000016, true)
	ctx := WithRoutingIntent(context.Background(), IntentBackground)
	if _, err := client.CompleteWithMessages(ctx, userMessages("name this")); err != nil {
		t.Fatal(err)
	}
	if prefs := prefsOn(t, recorded, 0); prefs == nil || prefs["sort"] != "latency" {
		t.Fatalf("preferences = %v, want the written row to beat the errand's default", prefs)
	}

	priced, pricedCapture := pricedClient(t, StaticRouting(RoutingPrice), 0.0000004, 0.0000016, true)
	if _, err := priced.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs := prefsOn(t, pricedCapture, 0)
	if prefs == nil || prefs["sort"] != "price" {
		t.Fatalf("preferences = %v, want the written row to beat the turn's default", prefs)
	}
	if _, carried := prefs["max_price"]; carried {
		t.Fatal("an explicitly price-routed request carried a latency ceiling")
	}
}

// `routing off` is still the third answer, and an errand does not resurrect the
// preference object a person switched off.
func TestRoutingOffStaysOffForAnErrand(t *testing.T) {
	client, recorded := pricedClient(t, StaticRouting(RoutingOff), 0.0000004, 0.0000016, true)
	ctx := WithRoutingIntent(context.Background(), IntentBackground)
	if _, err := client.CompleteWithMessages(ctx, userMessages("name this")); err != nil {
		t.Fatal(err)
	}
	if prefs := prefsOn(t, recorded, 0); prefs != nil {
		t.Fatalf("preferences = %v, want nothing sent at all", prefs)
	}
}

// The ledger's order is a SPEED ranking, and `provider.order` names what to try
// first — so sending it beside a price sort would silently undo the sort. The
// refusals still ride: those are about endpoints that will not answer at all.
func TestAPriceSortedRequestCarriesTheRefusalsAndNoSpeedRanking(t *testing.T) {
	client, recorded := pricedClient(t, nil, 0.0000004, 0.0000016, true)
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "quicksilver")
	for i := 0; i < ignoreAfter; i++ {
		client.velocity.laggy(model, "molasses")
	}
	ctx := WithRoutingIntent(context.Background(), IntentBackground)
	if _, err := client.CompleteWithMessages(ctx, userMessages("name this")); err != nil {
		t.Fatal(err)
	}
	prefs := prefsOn(t, recorded, 0)
	if prefs == nil || prefs["sort"] != "price" {
		t.Fatalf("preferences = %v, want the price sort", prefs)
	}
	if order, carried := prefs["order"]; carried {
		t.Fatalf("a price-sorted request carried a speed ranking: %v", order)
	}
	if got := words(prefs["ignore"]); !equalStrings(got, []string{"molasses"}) {
		t.Fatalf("ignore = %v, want the refused endpoint kept", got)
	}
}

// The ceiling is a filter, so it comes off with the rest of the filter when no
// endpoint will take the request at all: "no endpoints found" is a worse answer
// than a dear one.
func TestTheRefusalLadderTakesTheCeilingOff(t *testing.T) {
	yes := true
	ceiling := &maxPrice{Prompt: 1, Completion: 2}
	relaxed := relaxedPreferences(&providerPrefs{
		Sort:              "latency",
		AllowFallbacks:    &yes,
		RequireParameters: &yes,
		MaxPrice:          ceiling,
	})
	if relaxed == nil {
		t.Fatal("relaxing dropped the preference object entirely")
	}
	if relaxed.MaxPrice != nil {
		t.Fatalf("the relaxed request kept its ceiling: %+v", relaxed.MaxPrice)
	}
	if relaxed.Sort != "latency" {
		t.Fatalf("relaxed sort = %q, want the ask among whatever is left", relaxed.Sort)
	}
}

// ── WHO ANSWERED ────────────────────────────────────────────────────────────

// The slot is how a caller learns which endpoint served ITS call, rather than
// whichever call happened to finish last anywhere in the process.
func TestTheServedSlotIsFilledWithTheEndpointThatAnswered(t *testing.T) {
	client, _ := pricedClient(t, nil, 0.0000004, 0.0000016, true)
	slot := &ServedEndpoint{}
	ctx := WithServedEndpoint(context.Background(), slot)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if got := slot.Name(); got != "quicksilver" {
		t.Fatalf("served = %q, want the endpoint the answer named", got)
	}
}

// A caller that opened no slot is every caller that existed before it, and the
// nil slot answers every question rather than needing a test around it.
func TestAnAbsentServedSlotIsSilentRatherThanAFailure(t *testing.T) {
	client, _ := pricedClient(t, nil, 0.0000004, 0.0000016, true)
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if got := ServedEndpointFrom(context.Background()).Name(); got != "" {
		t.Fatalf("an unopened slot answered %q", got)
	}
}

// closeEnough compares two dollar figures without asking a float to be exact.
func closeEnough(got, want float64) bool {
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-9
}
