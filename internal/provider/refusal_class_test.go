package provider

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── THE GATE IS STRUCTURAL ──────────────────────────────────────────────────
//
// endpoints.go's ladder used to be entered on a list of sentences the router had
// been seen to say, and on 2026-08-28 the router said a new one: "No endpoints
// available matching your guardrail restrictions and data policy". The ladder
// whose price rung is the exact recovery never fired, three identical 404s
// went out, and a headless task died. Adding the sentence to the list bought
// one sentence of coverage.
//
// These tests hold the gate that does not go out of date, and — just as
// importantly — the four things that must still NOT enter it. Rule 1 of
// docs/design/failsafe/FAILSAFE.md is what they are pinning.

// knownModelPrice is a catalog that has resolved this build's model: a published
// list price is how [Client.catalogKnowsModel] asks about membership.
func knownModelPrice() func(string) (float64, float64, bool) {
	return func(string) (float64, float64, bool) { return 0.66e-6, 1.98e-6, true }
}

// unseenRefusal is a sentence NO version of endpointRefusalPhrases contains. It
// is deliberately plausible and deliberately new: the point of the class is that
// the next thing a router invents is already covered.
const unseenRefusal = `{"error":{"message":"Your request could not be matched to a serving lane ` +
	`under the current account configuration.","code":404}}`

// countingRouter answers `refusal` with `status` until `accept` is satisfied,
// counting every request it is given.
func countingRouter(t *testing.T, status int, refusal string, accept func(map[string]any) bool) (*capture, http.Handler) {
	t.Helper()
	recorded := &capture{}
	return recorded, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(len(recorded.bodies) - 1)
		writer.Header().Set("Content-Type", "application/json")
		if accept != nil && accept(body) {
			_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop",` +
				`"message":{"role":"assistant","content":"ok"}}]}`))
			return
		}
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(refusal))
	})
}

// classClient is a router-shaped adapter with its own ledger, so a memo written
// by one test is not read by the next.
func classClient(t *testing.T, handler http.Handler, config Config) *Client {
	t.Helper()
	config.APIKey = "test-key"
	if config.BaseURL == "" {
		config.BaseURL = "https://openrouter.ai/api/v1"
	}
	if config.Model == "" {
		config.Model = "sim/model"
	}
	config.HTTPClient = handlerClient(handler)
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	client.pins = newEndpointPins()
	client.wait = func(context.Context, time.Duration) error { return nil }
	return client
}

// A SENTENCE NOBODY HAS EVER SEEN STILL CLIMBS THE LADDER.
//
// 404, the router's own envelope, a model the catalog knows, nothing relayed
// from an upstream: there is no other thing this can be, whatever it says.
func TestAnUnseenRefusalSentenceStillClimbsTheLadderAndLandsOnRungOne(t *testing.T) {
	if endpointRefusalPhrase([]byte(unseenRefusal)) {
		t.Fatal("the sentence this test is built on is in the phrase list; it proves nothing")
	}
	recorded, handler := countingRouter(t, http.StatusNotFound, unseenRefusal, func(body map[string]any) bool {
		prefs, _ := body["provider"].(map[string]any)
		return prefs == nil || prefs["require_parameters"] == nil
	})
	client := classClient(t, handler, Config{ModelPrice: knownModelPrice()})

	var notices []string
	response, err := client.CompleteWithMessages(
		noticeContext(context.Background(), &notices), userMessages("hi"))
	if err != nil {
		t.Fatalf("the first rung should have landed the call: %v", err)
	}
	if response == nil {
		t.Fatal("no response")
	}
	if got := len(recorded.bodies); got != 2 {
		t.Fatalf("made %d requests, want 2: the refused one and the relaxed retry", got)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "relaxed the endpoint filter") {
		t.Fatalf("notices = %#v, want exactly one line, the endpoint filter relaxed", notices)
	}
}

// A 404 WITH NO ENVELOPE IS A WRONG BASE URL, AND IS SURFACED.
//
// This is the clause that makes the structural gate safe to widen: a proxy, a
// static host or a mistyped path answers in HTML or bare text, and nothing that
// is not the router can produce the router's error object.
func TestA404WithNoRouterEnvelopeIsSurfacedRatherThanRetried(t *testing.T) {
	for _, body := range []string{
		"<html><head><title>404 Not Found</title></head><body>nginx</body></html>",
		"Not Found",
		`{"detail":"Not Found"}`,
	} {
		recorded, handler := countingRouter(t, http.StatusNotFound, body, nil)
		client := classClient(t, handler, Config{ModelPrice: knownModelPrice()})

		var notices []string
		if _, err := client.CompleteWithMessages(
			noticeContext(context.Background(), &notices), userMessages("hi"), toolRequest()...); err == nil {
			t.Fatalf("a 404 from %q answered successfully", body)
		}
		if got := len(recorded.bodies); got != 1 {
			t.Fatalf("body %q: made %d requests, want exactly 1 — a wrong base URL is not a shape to negotiate", body, got)
		}
		if len(notices) != 0 {
			t.Fatalf("body %q: notices = %#v, want nothing said", body, notices)
		}
	}
}

// A MODEL THE CATALOG DOES NOT KNOW IS A DIFFERENT ERROR, AND THE CALLER SEES IT.
//
// The envelope is there and the status is right, but a slug nobody has a row for
// is a typo or a retired model — negotiating with it spends four attempts to
// discover what the first attempt already said.
func TestARefusalOnAModelTheCatalogDoesNotKnowIsSurfaced(t *testing.T) {
	for _, price := range []func(string) (float64, float64, bool){
		nil, // a build with no catalog wired at all knows nothing
		func(string) (float64, float64, bool) { return 0, 0, false },
	} {
		recorded, handler := countingRouter(t, http.StatusBadRequest, unseenRefusal, nil)
		client := classClient(t, handler, Config{ModelPrice: price})

		_, err := client.CompleteWithMessages(context.Background(), userMessages("hi"), toolRequest()...)
		if err == nil {
			t.Fatal("a 400 answered successfully")
		}
		var refusal *RefusalError
		if errors.As(err, &refusal) {
			t.Fatalf("an unknown model produced a ladder diagnosis: %v", err)
		}
		var api *APIError
		if !errors.As(err, &api) || api.Status != http.StatusBadRequest {
			t.Fatalf("err = %#v, want the 400 as the error it is", err)
		}
		if got := len(recorded.bodies); got != 1 {
			t.Fatalf("made %d requests, want exactly 1", got)
		}
	}
}

// A REFUSAL THE ROUTER RELAYED IS ONE ENDPOINT'S VERDICT, NOT THE ROUTING LAYER'S.
//
// `error.metadata.provider_name` is present exactly when the router forwarded
// somebody else's refusal — which means the routing layer DID find something to
// try, the opposite of "nothing can serve this shape". That failure has its own
// answer (refusal_test.go's lane rotation) and must not be answered by stripping
// the person's request instead.
func TestARelayedUpstreamRefusalDoesNotClimbTheLadder(t *testing.T) {
	const relayed = `{"error":{"message":"Provider returned error","code":400,` +
		`"metadata":{"provider_name":"Baidu","raw":"internal server failure"}}}`
	recorded, handler := countingRouter(t, http.StatusBadRequest, relayed, nil)
	client := classClient(t, handler, Config{ModelPrice: knownModelPrice()})

	var notices []string
	if _, err := client.CompleteWithMessages(
		noticeContext(context.Background(), &notices), userMessages("hi"), toolRequest()...); err == nil {
		t.Fatal("a relayed 400 answered successfully")
	}
	if got := len(recorded.bodies); got != 1 {
		t.Fatalf("made %d requests, want exactly 1 — an upstream's refusal is not the ladder's", got)
	}
	if len(notices) != 0 {
		t.Fatalf("notices = %#v, want nothing stripped off the request over an upstream fault", notices)
	}
}

// AN ENDPOINT THAT IS NOT A ROUTER HAS NO ENDPOINT SET TO EMPTY.
func TestANonRouterRefusalIsNotClassifiedStructurally(t *testing.T) {
	recorded, handler := countingRouter(t, http.StatusNotFound, unseenRefusal, nil)
	client := classClient(t, handler, Config{
		BaseURL: "https://api.example.com/v1", Model: "vendor/model",
		ModelPrice: knownModelPrice(), Fallbacks: []string{"other/model"},
	})

	var notices []string
	if _, err := client.CompleteWithMessages(
		noticeContext(context.Background(), &notices), userMessages("hi"), toolRequest()...); err == nil {
		t.Fatal("a 404 answered successfully")
	}
	if got := len(recorded.bodies); got != 1 {
		t.Fatalf("made %d requests, want exactly 1 from a plain endpoint", got)
	}
	if len(notices) != 0 {
		t.Fatalf("notices = %#v, want nothing", notices)
	}
}

// PACING, A FAULT AND A TIMEOUT KEEP THE BEHAVIOUR THEY HAD.
//
// The header of endpoints.go promises this, and widening the gate is exactly the
// change that could break it: none of the three is a claim about the request's
// shape, so none of them may strip a field off it.
func TestPacingFaultsAndTimeoutsDoNotEnterTheLadder(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		refusal := `{"error":{"message":"Your request could not be matched to a serving lane.","code":` +
			strconv.Itoa(status) + `}}`
		_, handler := countingRouter(t, status, refusal, nil)
		client := classClient(t, handler, Config{ModelPrice: knownModelPrice()})

		var notices []string
		if _, err := client.CompleteWithMessages(
			noticeContext(context.Background(), &notices), userMessages("hi"), toolRequest()...); err == nil {
			t.Fatalf("a %d answered successfully", status)
		}
		if len(notices) != 0 {
			t.Fatalf("status %d: notices = %#v, want a paced or broken provider to stay one", status, notices)
		}
	}

	// AND A TIMEOUT, which never has a body to classify at all.
	var mu sync.Mutex
	sent := 0
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "https://openrouter.ai/api/v1", Model: "sim/model",
		ModelPrice: knownModelPrice(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			mu.Lock()
			sent++
			mu.Unlock()
			return nil, context.DeadlineExceeded
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.wait = func(context.Context, time.Duration) error { return nil }

	var notices []string
	if _, err := client.CompleteWithMessages(
		noticeContext(context.Background(), &notices), userMessages("hi"), toolRequest()...); err == nil {
		t.Fatal("a timeout answered successfully")
	}
	if len(notices) != 0 {
		t.Fatalf("notices = %#v, want a timeout to stay a timeout", notices)
	}
	mu.Lock()
	defer mu.Unlock()
	if sent == 0 {
		t.Fatal("nothing was sent; this test proves nothing")
	}
}

// THE CEILING MEMO FIRES AFTER TWO STRUCTURAL REFUSALS.
//
// The router names the LAST filter that emptied the set, and it is under no
// obligation to name the price. The memo therefore waits for the structural
// class twice: once before widening endpoint membership under the same ceiling,
// and once before taking that ceiling off.
func TestTheCeilingMemoFiresOnASentenceThatNeverMentionsThePrice(t *testing.T) {
	recorded, handler := countingRouter(t, http.StatusNotFound, unseenRefusal, func(body map[string]any) bool {
		prefs, _ := body["provider"].(map[string]any)
		return prefs == nil || prefs["max_price"] == nil
	})
	client := classClient(t, handler, Config{ModelPrice: knownModelPrice()})

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hi")); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if prefs, _ := recorded.body(0)["provider"].(map[string]any); prefs == nil || prefs["max_price"] == nil {
		t.Fatal("the first request carried no ceiling, so this test proves nothing")
	}
	if got := len(recorded.bodies); got != 3 {
		t.Fatalf("first call made %d requests, want the original and two distinct relaxation rungs", got)
	}
	if !client.velocity.ceilingRefused("sim/model") {
		t.Fatal("the ledger learnt nothing from a refusal that did not name the price")
	}

	// AND THE SECOND CALL IS SHAPED RIGHT FROM THE START.
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("again")); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := len(recorded.bodies); got != 4 {
		t.Fatalf("second call made %d requests, want exactly 1", got-3)
	}
	if prefs, _ := recorded.body(3)["provider"].(map[string]any); prefs != nil && prefs["max_price"] != nil {
		t.Fatal("the second call carried the ceiling the router already refused")
	}
}

// EVEN WHEN THE ROUTER NAMES PRICE OR POLICY, THE TWO CHANGES STAY SEPARATE.
// The refusal's words do not authorize dropping the cap on the membership
// rung; the next structural refusal does.
func TestARefusalThatNamedThePolicyStillUsesTheSeparatePriceRung(t *testing.T) {
	const policyBody = `{"error":{"message":"No endpoints available matching your guardrail ` +
		`restrictions and data policy.","code":404}}`
	_, handler := countingRouter(t, http.StatusNotFound, policyBody, func(body map[string]any) bool {
		prefs, _ := body["provider"].(map[string]any)
		return prefs == nil || prefs["max_price"] == nil
	})
	client := classClient(t, handler, Config{ModelPrice: knownModelPrice()})

	var notices []string
	if _, err := client.CompleteWithMessages(
		noticeContext(context.Background(), &notices), userMessages("hi")); err != nil {
		t.Fatalf("the refusal ladder should have landed the call: %v", err)
	}
	if len(notices) != 2 || strings.Contains(notices[0], "price ceiling") ||
		!strings.Contains(notices[1], "dropped the price ceiling") {
		t.Fatalf("notices = %#v, want endpoint widening before the price-ceiling rung", notices)
	}

	// A request that carried NO ceiling never claims to have dropped one, even
	// when the router's sentence mentions the policy.
	_, plain := countingRouter(t, http.StatusNotFound, policyBody, func(body map[string]any) bool {
		prefs, _ := body["provider"].(map[string]any)
		return prefs == nil || prefs["require_parameters"] == nil
	})
	priceless := classClient(t, plain, Config{})

	notices = nil
	if _, err := priceless.CompleteWithMessages(
		noticeContext(context.Background(), &notices), userMessages("hi")); err != nil {
		t.Fatalf("the first rung should have landed the call: %v", err)
	}
	if len(notices) == 0 || strings.Contains(notices[0], "price ceiling") {
		t.Fatalf("notices = %#v, want no claim about a ceiling that was never sent", notices)
	}
}
