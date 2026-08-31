package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ── THE 400 NOBODY COULD READ ───────────────────────────────────────────────
//
// SWE-Marathon run s2, 22:45 UTC. A ten-hour benchmark cell settled with five
// hours of its budget unspent, and the whole of what anybody was ever told was:
//
//	error: after 3 retries: API error (400): Provider returned error
//
// No provider name. No upstream body. Nothing in the session journal at all. And
// "Provider returned error" is OpenRouter saying that SOMEBODY ELSE refused — the
// somebody and the refusal both sitting in `error.metadata`, which this client
// decoded and threw away. These tests pin the two halves of the answer: the
// refusal carries who and what, and an upstream that refuses loses the lane.

// A REFUSAL NAMES WHO REFUSED AND WHAT THEY SAID.
func TestAProviderRefusalCarriesTheUpstreamAndItsOwnWords(t *testing.T) {
	body := `{"error":{"message":"Provider returned error","code":400,` +
		`"metadata":{"provider_name":"Baidu","raw":"input length 97445 exceeds the maximum of 65536 tokens. shorten the prompt."}}}`
	err := apiError(400, []byte(body))

	refusal, ok := RefusalFrom(err)
	if !ok {
		t.Fatal("a provider refusal is not recoverable as a value")
	}
	if refusal.Provider != "Baidu" {
		t.Fatalf("provider = %q, want the upstream the router named", refusal.Provider)
	}
	if !strings.Contains(refusal.Raw, "exceeds the maximum") {
		t.Fatalf("raw = %q, want the upstream's own body", refusal.Raw)
	}
	// AND THE PERSON'S LINE SAYS BOTH, still opening with the status the
	// harness's taxonomy reads out of it.
	printed := err.Error()
	if !strings.HasPrefix(printed, "API error (400): Provider returned error") {
		t.Fatalf("the head of the sentence changed: %q", printed)
	}
	if !strings.Contains(printed, "Baidu") || !strings.Contains(printed, "input length 97445") {
		t.Fatalf("the person is still told nothing about the 400: %q", printed)
	}
	// One sentence, not the whole body: this is a line in a terminal.
	if strings.Contains(printed, "shorten the prompt") {
		t.Fatalf("the whole upstream body reached the person's line: %q", printed)
	}
}

// AND THE TWO KINDS OF 4xx ARE TOLD APART BY SHAPE, NEVER BY A STATUS LIST.
//
// An upstream refused: another endpoint may serve, so the ladder goes on. The
// router refused our own bytes: every endpoint alive says the same thing, so the
// ladder stops. The only difference on the wire is whether a provider was named.
func TestARefusalKnowsWhetherTheUpstreamOrOurRequestIsAtFault(t *testing.T) {
	upstream, _ := RefusalFrom(apiError(400, []byte(
		`{"error":{"message":"Provider returned error","metadata":{"provider_name":"Baidu","raw":"nope"}}}`)))
	if !upstream.FromUpstream() {
		t.Error("a 400 that named its upstream was read as our own mistake")
	}
	if upstream.OurRequest() {
		t.Error("a 400 relayed from an upstream stopped the ladder")
	}

	ours, _ := RefusalFrom(apiError(400, []byte(
		`{"error":{"message":"tools[0].function.name: invalid","code":400}}`)))
	if ours.FromUpstream() {
		t.Error("a 400 the router answered for itself was blamed on an upstream")
	}
	if !ours.OurRequest() {
		t.Error("a malformed request was retried into three identical walls")
	}

	// PACING IS NOT A VERDICT ON THE REQUEST, so a 429 is never "ours" however
	// anonymous it is — it has its own patience in retry.go.
	paced, _ := RefusalFrom(apiError(429, []byte(`{"error":{"message":"slow down"}}`)))
	if paced.OurRequest() {
		t.Error("a 429 was read as a malformed request and given up on")
	}
	// And a server fault is nobody's request being wrong either.
	broken, _ := RefusalFrom(apiError(503, []byte(`{"error":{"message":"unavailable"}}`)))
	if broken.OurRequest() {
		t.Error("a 503 was read as a malformed request")
	}
}

// THE ROTATION. THREE ATTEMPTS AFTER A "PROVIDER RETURNED ERROR" REACH AT LEAST
// TWO ENDPOINTS.
//
// This is the measured failure itself: the same request delivered three times to
// the same upstream, 2s and 4s and 8s apart, because releasing the pin only stops
// this process ASKING for an endpoint and does nothing to stop the router
// choosing it. The refusing lane now goes into the ledger, so every request
// encoded afterwards carries it in `provider.ignore`.
func TestThreeAttemptsAfterAnUpstreamRefusalReachTwoDistinctEndpoints(t *testing.T) {
	const refusing = "Baidu"
	var served, ignores []string
	// A router-shaped fake: it honours `provider.ignore`, and the upstream it
	// would otherwise pick answers 400 with its own name on the refusal.
	router := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asked := decodedBody(t, request)
		ignored := ignoredEndpoints(asked)
		ignores = append(ignores, strings.Join(ignored, ","))
		writer.Header().Set("Content-Type", "application/json")
		if contains(ignored, refusing) {
			served = append(served, "Quicksilver")
			_, _ = writer.Write([]byte(answerFrom("Quicksilver", 10, 0, 0)))
			return
		}
		served = append(served, refusing)
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"Provider returned error","code":400,` +
			`"metadata":{"provider_name":"` + refusing + `","raw":"upstream said no"}}}`))
	})

	client := ledgerClient(t, router)
	ctx := lineage("conversation-s2")
	// Three attempts: the ladder the turn loop walks on a "Provider returned
	// error" (internal/session's maxRetries).
	for attempt := 0; attempt < 3; attempt++ {
		_, _ = client.CompleteWithMessages(ctx, userMessages("carry on"))
	}

	distinct := map[string]bool{}
	for _, name := range served {
		distinct[name] = true
	}
	if len(distinct) < 2 {
		t.Fatalf("three attempts reached %d endpoint(s) (%v) — the refusing upstream was asked again",
			len(distinct), served)
	}
	// AND THE WAY IT MOVED IS THE LEDGER'S OWN, visible on the wire.
	if len(ignores) < 2 || ignores[1] != refusing {
		t.Fatalf("the second request ignored %q, want the upstream that refused the first", ignores)
	}
}

// AND A REQUEST THE ROUTER ITSELF REFUSED TAKES NOBODY'S LANE AWAY. There is no
// endpoint to blame for bytes we assembled wrongly, and striking one per attempt
// would empty the ledger over our own mistake.
func TestARouterRefusalOfOurOwnRequestStrikesNoEndpoint(t *testing.T) {
	router := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"tools[0].function.name: invalid","code":400}}`))
	})
	client, recorded := routedClient(t, RoutingLatency, router)

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err == nil {
			t.Fatal("a 400 answered as success")
		}
	}
	if prefs := prefsOn(t, recorded, 1); prefs != nil {
		if got := words(prefs["ignore"]); len(got) != 0 {
			t.Fatalf("ignore = %v after a refusal that named no upstream", got)
		}
	}
}

// A FAILED CALL NEVER LOOKS LIKE AN EMPTY ANSWER — THE STREAM'S HALF.
//
// A router that has already sent its headers reports an upstream breaking as an
// `error` object inside the 200. This decoder had no field for it, so the object
// was skipped, the stream ended, and the call returned an answer with no content,
// no tool call and no usage. Three of those in fifteen seconds is the front half
// of the measured failure: each one read to the turn loop as a model that had
// simply finished.
func TestAnErrorInsideAStreamIsARefusalAndNotAnEmptyAnswer(t *testing.T) {
	stream := "data: {\"id\":\"1\",\"provider\":\"Baidu\",\"choices\":[]}\n\n" +
		"data: {\"error\":{\"message\":\"Provider returned error\",\"code\":400," +
		"\"metadata\":{\"provider_name\":\"Baidu\",\"raw\":\"upstream said no\"}}}\n\n" +
		"data: [DONE]\n\n"
	router := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(stream))
	})
	client, _ := routedClient(t, RoutingLatency, router)

	ctx := WithStreamObserver(lineage("conversation-s2"), func(StreamEvent) {})
	response, err := client.CompleteWithMessages(ctx, userMessages("carry on"))
	if err == nil {
		t.Fatalf("a stream that carried a refusal answered successfully: %+v", response)
	}
	refusal, ok := RefusalFrom(err)
	if !ok {
		t.Fatalf("a mid-stream refusal is not a provider refusal: %v", err)
	}
	if refusal.Status != 400 || refusal.Provider != "Baidu" {
		t.Fatalf("mid-stream refusal = %d from %q, want the 400 the upstream sent", refusal.Status, refusal.Provider)
	}
	if !strings.Contains(refusal.Raw, "upstream said no") {
		t.Fatalf("raw = %q, want the upstream's own body", refusal.Raw)
	}
}

// A stream that names no code at all is still a refusal, and it is a 502: the
// headers landed, so somebody accepted the request and then failed it.
func TestAnUncodedMidStreamErrorIsABadGatewayAndNotASuccess(t *testing.T) {
	if err := streamRefusal(json.RawMessage(`{"message":"upstream disconnected"}`)); err == nil {
		t.Fatal("a mid-stream error with no code was read as no error at all")
	} else if refusal, _ := RefusalFrom(err); refusal.Status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", refusal.Status)
	}
	// And an absent or empty object is not a refusal — every ordinary chunk in
	// every ordinary stream goes through this.
	if err := streamRefusal(nil); err != nil {
		t.Fatalf("a chunk with no error field was read as a refusal: %v", err)
	}
	if err := streamRefusal(json.RawMessage(`{}`)); err != nil {
		t.Fatalf("an empty error object was read as a refusal: %v", err)
	}
}

// ledgerClient is a router-shaped client with a velocity ledger of its own and
// a handler that reads the request body itself — which is why it is not
// [routedClient]: that one drains the body into its capture before the handler
// sees it, and these tests are about what the handler is asked for.
func ledgerClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	forgetLanes(t)
	client, err := NewClient(Config{
		APIKey:     "test-key",
		BaseURL:    "https://openrouter.ai/api/v1",
		Model:      "vendor/fast-model",
		Routing:    StaticRouting(RoutingLatency),
		HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	client.pins = newEndpointPins()
	return client
}

// decodedBody reads one request's JSON body.
func decodedBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	payload, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	var decoded map[string]any
	_ = json.Unmarshal(payload, &decoded)
	return decoded
}

// ignoredEndpoints is the `provider.ignore` list a request carried.
func ignoredEndpoints(body map[string]any) []string {
	prefs, ok := body["provider"].(map[string]any)
	if !ok {
		return nil
	}
	return words(prefs["ignore"])
}

func contains(list []string, name string) bool {
	for _, entry := range list {
		if entry == name {
			return true
		}
	}
	return false
}
