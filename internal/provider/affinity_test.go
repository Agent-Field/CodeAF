package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDocumentedSessionAffinitySurvivesAChangedPrompt(t *testing.T) {
	for _, key := range []string{"conversation", strings.Repeat("long-lineage", 40)} {
		t.Run(fmt.Sprintf("key-bytes-%d", len(key)), func(t *testing.T) {
			client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
				answering(ok(answerFrom("quicksilver", 90000, 0, 0))))
			for _, prompt := range []string{"before compaction", "changed opening after compaction", "continue"} {
				if _, err := client.CompleteWithMessages(lineage(key), userMessages(prompt)); err != nil {
					t.Fatal(err)
				}
			}
			var session string
			for _, headers := range recorded.headers {
				got := headers.Get("X-Session-Id")
				if got == "" || len(got) > 256 || session != "" && got != session {
					t.Fatalf("invalid or changing session identity: %q", got)
				}
				session = got
			}
			if order := orderOn(t, recorded, 2); len(order) == 0 || order[0] != "quicksilver" {
				t.Fatalf("cold but successful compaction displaced the endpoint: %v", order)
			}
		})
	}
}

// ── THE ENDPOINT THAT HOLDS THE CACHE ───────────────────────────────────────
//
// A benchmark run of one chat turn crossed six OpenRouter endpoints in eleven
// moves, and each move re-sent a 94k-token transcript to a machine that had
// never seen it: $0.00846 cold against $0.00179 warm for the same context.
// These tests pin the answer — a lineage asks for the endpoint that served it
// last, and lets go only when that endpoint fails it or
// charges above the ceiling.

// answerFrom is one router answer, spelled the way OpenRouter spells it under
// usage accounting: who served it, how long the prompt was, how much of that
// prompt was read from the endpoint's cache, and what it cost. A cost of zero is
// left off entirely, because "the endpoint did not say" is a different fact from
// "it was free" (affinity.go's overPriceCeiling reads exactly that difference).
func answerFrom(endpoint string, prompt, cached int, cost float64) string {
	usage := fmt.Sprintf(`"usage":{"prompt_tokens":%d,"completion_tokens":2,"total_tokens":%d,`+
		`"prompt_tokens_details":{"cached_tokens":%d}`, prompt, prompt+2, cached)
	if cost > 0 {
		usage += fmt.Sprintf(`,"cost":%v`, cost)
	}
	return fmt.Sprintf(`{"model":"vendor/fast-model","provider":%q,`+
		`"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],`+
		`%s}}`, endpoint, usage)
}

// reply is one scripted turn of a fake endpoint: the status it answered with and
// the body it carried.
type reply struct {
	status int
	body   string
}

// answering serves one scripted reply per request, in order, repeating the last
// one for anything beyond the script.
func answering(script ...reply) http.Handler {
	var mu sync.Mutex
	turn := 0
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		current := script[min(turn, len(script)-1)]
		turn++
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if current.status != 0 && current.status != http.StatusOK {
			writer.WriteHeader(current.status)
		}
		_, _ = writer.Write([]byte(current.body))
	})
}

// ok is the ordinary rung of a script.
func ok(body string) reply { return reply{status: http.StatusOK, body: body} }

// pinningClient is a router-shaped client with its own pin ledger and NO
// velocity ledger.
//
// The velocity ledger is left out on purpose: it also writes `provider.order`,
// from its own speed ranking, and a test that let both speak could not say which
// one put a name on the wire. Its interplay with the pin has a test of its own
// below, where the ledger is wired back in deliberately.
func pinningClient(t *testing.T, routing RoutingSource, prompt, completion float64, known bool, handler http.Handler) (*Client, *capture) {
	t.Helper()
	forgetLanes(t)
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
			handler.ServeHTTP(writer, request)
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = nil
	client.pins = newEndpointPins()
	return client, recorded
}

// lineage is a context carrying one conversation's cache key — the unit a prompt
// prefix, and therefore a pin, belongs to.
func lineage(key string) context.Context {
	return WithCacheKey(context.Background(), key)
}

// orderOn reads `provider.order` off a recorded request.
func orderOn(t *testing.T, recorded *capture, index int) []string {
	t.Helper()
	prefs := prefsOn(t, recorded, index)
	if prefs == nil {
		return nil
	}
	return words(prefs["order"])
}

// ── THE PIN ─────────────────────────────────────────────────────────────────

func TestTheSecondRequestAsksForTheEndpointThatServedTheFirst(t *testing.T) {
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		answering(ok(answerFrom("quicksilver", 90000, 89000, 0))))
	ctx := lineage("conversation-1")
	for _, prompt := range []string{"hello", "again"} {
		if _, err := client.CompleteWithMessages(ctx, userMessages(prompt)); err != nil {
			t.Fatal(err)
		}
	}
	if got := orderOn(t, recorded, 0); len(got) != 0 {
		t.Fatalf("first request order = %v, want nothing — there was no cache to come back to yet", got)
	}
	if got := orderOn(t, recorded, 1); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("second request order = %v, want the endpoint holding this lineage's prompt cache", got)
	}
	// The pin is a preference and never a demand, or one busy endpoint would
	// become a failed turn.
	if prefs := prefsOn(t, recorded, 1); prefs["allow_fallbacks"] != true {
		t.Fatalf("allow_fallbacks = %v on a pinned request, want true", prefs["allow_fallbacks"])
	}
}

// TWO LINEAGES DO NOT SHARE A PIN. Six leaves of one fan-out are six growing
// transcripts on one model, and pinning them together is the failure the leaf
// cache key exists to prevent (hints.go's WithLeafCacheKey).
func TestOneLineagesPinIsNotAnothersAndAnUnkeyedCallHasNone(t *testing.T) {
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		answering(ok(answerFrom("quicksilver", 90000, 89000, 0))))
	if _, err := client.CompleteWithMessages(lineage("leaf-a"), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(lineage("leaf-b"), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if got := orderOn(t, recorded, 1); len(got) != 0 {
		t.Fatalf("the second leaf's first request order = %v, want nothing — it is a different prefix", got)
	}
	// And a call with no lineage at all — internal/exec's bare loop is one —
	// routes exactly as it did before, and leaves nothing behind for anyone else.
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("again")); err != nil {
		t.Fatal(err)
	}
	if got := orderOn(t, recorded, 3); len(got) != 0 {
		t.Fatalf("an unkeyed call carried order = %v, want nothing", got)
	}
}

// ── AND WHEN IT FAILS YOU ───────────────────────────────────────────────────

func TestAFailedRequestLetsGoOfTheEndpointItWasPinnedTo(t *testing.T) {
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		answering(
			ok(answerFrom("quicksilver", 90000, 89000, 0)),
			reply{status: http.StatusBadRequest, body: `{"error":{"message":"the endpoint fell over"}}`},
			ok(answerFrom("zephyr", 90000, 0, 0)),
		))
	ctx := lineage("conversation-1")
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(ctx, userMessages("again")); err == nil {
		t.Fatal("the scripted failure landed as an answer, want an error")
	}
	if _, err := client.CompleteWithMessages(ctx, userMessages("once more")); err != nil {
		t.Fatal(err)
	}
	if got := orderOn(t, recorded, 1); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("the failing request's order = %v, want the pin it still held", got)
	}
	if got := orderOn(t, recorded, 2); len(got) != 0 {
		t.Fatalf("the request after a failure order = %v, want nothing — a lane that failed us is one we move off", got)
	}
}

// A COLD ANSWER CAN WARM THE NEXT REQUEST. Releasing this provider would pay
// for another cold prefix elsewhere, even though this endpoint just served us.
func TestAZeroCacheReadOnALongPromptKeepsTheSuccessfulEndpoint(t *testing.T) {
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		answering(
			ok(answerFrom("quicksilver", 90000, 89000, 0)),
			ok(answerFrom("quicksilver", 90000, 0, 0)),
		))
	ctx := lineage("conversation-1")
	for _, prompt := range []string{"hello", "again", "once more"} {
		if _, err := client.CompleteWithMessages(ctx, userMessages(prompt)); err != nil {
			t.Fatal(err)
		}
	}
	if got := orderOn(t, recorded, 1); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("second request order = %v, want the pin", got)
	}
	if got := orderOn(t, recorded, 2); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("order after a cold answer = %v, want the successful endpoint", got)
	}
}

// A SHORT PROMPT'S ZERO IS NOISE, NOT EVIDENCE. Endpoints report cache reads in
// blocks, so a small request honestly reads nothing back even on the machine
// that just answered it, and a lineage must not throw a warm cache away over a
// rounding rule.
func TestAZeroCacheReadOnAShortPromptKeepsThePin(t *testing.T) {
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		answering(ok(answerFrom("quicksilver", 100, 0, 0))))
	ctx := lineage("conversation-1")
	for _, prompt := range []string{"hello", "again", "once more"} {
		if _, err := client.CompleteWithMessages(ctx, userMessages(prompt)); err != nil {
			t.Fatal(err)
		}
	}
	if got := orderOn(t, recorded, 2); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("order = %v, want the pin kept through a short prompt's honest zero", got)
	}
}

// ── AND THE CEILING STANDS ──────────────────────────────────────────────────

// The pin never buys speed or cache at any price: the same ceiling the latency
// ask has always carried rides on every pinned request too.
func TestAPinnedRequestStillCarriesThePriceCeiling(t *testing.T) {
	client, recorded := pinningClient(t, nil, 0.0000004, 0.0000016, true,
		answering(ok(answerFrom("quicksilver", 90000, 89000, 0))))
	ctx := lineage("conversation-1")
	for _, prompt := range []string{"hello", "again"} {
		if _, err := client.CompleteWithMessages(ctx, userMessages(prompt)); err != nil {
			t.Fatal(err)
		}
	}
	if got := orderOn(t, recorded, 1); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("second request order = %v, want the pin", got)
	}
	ceiling := ceilingOn(t, recorded, 1)
	if ceiling == nil {
		t.Fatal("a pinned request rode with no ceiling, which is how a warm cache becomes a dear one")
	}
	wantPrompt := 0.0000004 * 1_000_000 * latencyPriceCeiling
	if got, okAssert := ceiling["prompt"].(float64); !okAssert || !closeEnough(got, wantPrompt) {
		t.Fatalf("max_price.prompt = %v, want %v", ceiling["prompt"], wantPrompt)
	}
}

// An endpoint that charged above the ceiling for the very tokens it counted is
// one this lineage does not go back to, however warm its cache is.
func TestAnEndpointThatChargedAboveTheCeilingLosesThePin(t *testing.T) {
	// $0.40/M in, $1.60/M out, so 90000 prompt and 2 completion tokens are
	// allowed 90000/1e6 * 0.4 * 1.25 = $0.045 at the ceiling. The answer charges
	// well over it.
	client, recorded := pinningClient(t, nil, 0.0000004, 0.0000016, true,
		answering(ok(answerFrom("quicksilver", 90000, 89000, 0.2))))
	ctx := lineage("conversation-1")
	for _, prompt := range []string{"hello", "again"} {
		if _, err := client.CompleteWithMessages(ctx, userMessages(prompt)); err != nil {
			t.Fatal(err)
		}
	}
	if got := orderOn(t, recorded, 1); len(got) != 0 {
		t.Fatalf("order = %v, want nothing — a lane charging above the ceiling keeps no pin", got)
	}
}

func TestAnEndpointChargingUnderTheCeilingKeepsThePin(t *testing.T) {
	client, recorded := pinningClient(t, nil, 0.0000004, 0.0000016, true,
		answering(ok(answerFrom("quicksilver", 90000, 89000, 0.01))))
	ctx := lineage("conversation-1")
	for _, prompt := range []string{"hello", "again"} {
		if _, err := client.CompleteWithMessages(ctx, userMessages(prompt)); err != nil {
			t.Fatal(err)
		}
	}
	if got := orderOn(t, recorded, 1); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("order = %v, want the pin kept by an endpoint charging under the ceiling", got)
	}
}

// ── WHO ELSE IT APPLIES TO ──────────────────────────────────────────────────

// A worker nobody is waiting on asks for the cheapest endpoint on its first
// request and then comes back to it, because a cold prefix costs more than any
// two endpoints' tariffs differ by.
func TestACallNobodyIsWaitingOnPinsOnceItIsWarm(t *testing.T) {
	client, recorded := pinningClient(t, nil, 0.0000004, 0.0000016, true,
		answering(ok(answerFrom("quicksilver", 90000, 89000, 0))))
	ctx := WithRoutingIntent(lineage("worker-1"), IntentBackground)
	for _, prompt := range []string{"hello", "again"} {
		if _, err := client.CompleteWithMessages(ctx, userMessages(prompt)); err != nil {
			t.Fatal(err)
		}
	}
	if prefs := prefsOn(t, recorded, 0); prefs["sort"] != "price" {
		t.Fatalf("first request sort = %v, want price for a call nobody is waiting on", prefs["sort"])
	}
	if got := orderOn(t, recorded, 0); len(got) != 0 {
		t.Fatalf("first request order = %v, want nothing", got)
	}
	if got := orderOn(t, recorded, 1); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("second request order = %v, want the warm endpoint even under a price sort", got)
	}
}

// A session that asked not to be routed is not pinned either — the same gate the
// velocity ledger keeps.
func TestRoutingOffPinsNothing(t *testing.T) {
	client, recorded := pinningClient(t, StaticRouting(RoutingOff), 0, 0, false,
		answering(ok(answerFrom("quicksilver", 90000, 89000, 0))))
	ctx := lineage("conversation-1")
	for _, prompt := range []string{"hello", "again"} {
		if _, err := client.CompleteWithMessages(ctx, userMessages(prompt)); err != nil {
			t.Fatal(err)
		}
	}
	if prefs := prefsOn(t, recorded, 1); prefs != nil {
		t.Fatalf("a routing-off request carried preferences: %v", prefs)
	}
}

// ── ALONGSIDE THE SPEED LEDGER ──────────────────────────────────────────────

// The pin goes in FRONT of the ledger's speed ranking and appears in it once:
// the fastest endpoint is worth having, and the one holding 90k warm tokens is
// worth having first.
func TestThePinLeadsTheLedgersRankingAndIsNamedOnce(t *testing.T) {
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		answering(ok(answerFrom("quicksilver", 90000, 89000, 0))))
	client.velocity = newVelocityLedger()
	client.velocity.brisk("vendor/fast-model", "zephyr")
	client.velocity.brisk("vendor/fast-model", "quicksilver")
	ctx := lineage("conversation-1")
	for _, prompt := range []string{"hello", "again"} {
		if _, err := client.CompleteWithMessages(ctx, userMessages(prompt)); err != nil {
			t.Fatal(err)
		}
	}
	if got := orderOn(t, recorded, 1); !equalStrings(got, []string{"quicksilver", "zephyr"}) {
		t.Fatalf("order = %v, want the pinned endpoint first and the ranking behind it, each named once", got)
	}
}

// AND A REFUSED ENDPOINT LOSES ITS PIN. Asking for a name that travels in
// `provider.ignore` in the same breath is a request arguing with itself.
func TestAPinTheSpeedLedgerHasRefusedIsDropped(t *testing.T) {
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		answering(ok(answerFrom("quicksilver", 90000, 89000, 0))))
	client.velocity = newVelocityLedger()
	client.velocity.brisk("vendor/fast-model", "zephyr")
	ctx := lineage("conversation-1")
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	client.velocity.pace("vendor/fast-model", "quicksilver", time.Minute)
	if _, err := client.CompleteWithMessages(ctx, userMessages("again")); err != nil {
		t.Fatal(err)
	}
	if got := orderOn(t, recorded, 1); !equalStrings(got, []string{"zephyr"}) {
		t.Fatalf("order = %v, want the refused endpoint gone from the front", got)
	}
	if got := words(prefsOn(t, recorded, 1)["ignore"]); !equalStrings(got, []string{"quicksilver"}) {
		t.Fatalf("ignore = %v, want the refused endpoint", got)
	}
}

// ── WHAT THE JOURNAL CAN SEE ────────────────────────────────────────────────

// The served-endpoint slot is what a cost autopsy reads: it says which endpoint
// answered, whether that was the one the request came back for, and how many
// times the lineage moved.
func TestTheServedSlotCountsHopsAndSaysWhetherThePinHeld(t *testing.T) {
	client, _ := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		answering(
			ok(answerFrom("quicksilver", 90000, 89000, 0)),
			ok(answerFrom("zephyr", 90000, 89000, 0)),
			ok(answerFrom("zephyr", 90000, 89000, 0)),
		))
	slot := &ServedEndpoint{}
	ctx := WithServedEndpoint(lineage("conversation-1"), slot)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if slot.Hops() != 0 || slot.Pinned() {
		t.Fatalf("after one answer: hops = %d, pinned = %v — an arrival is not a move", slot.Hops(), slot.Pinned())
	}
	// The router fell back to another endpoint: the pin did not hold, and that
	// is a hop with a cold prompt cache on the far side.
	if _, err := client.CompleteWithMessages(ctx, userMessages("again")); err != nil {
		t.Fatal(err)
	}
	if slot.Name() != "zephyr" || slot.Hops() != 1 || slot.Pinned() {
		t.Fatalf("after a fallback: name = %q, hops = %d, pinned = %v", slot.Name(), slot.Hops(), slot.Pinned())
	}
	if _, err := client.CompleteWithMessages(ctx, userMessages("once more")); err != nil {
		t.Fatal(err)
	}
	if slot.Hops() != 1 || !slot.Pinned() {
		t.Fatalf("after coming back: hops = %d, pinned = %v — want the lineage held where its cache is",
			slot.Hops(), slot.Pinned())
	}
}
