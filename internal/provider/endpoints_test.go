package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// refusalBody is the router's own words for the failure this chain answers.
const refusalBody = `{"error":{"message":"No endpoints found that can handle the requested parameters.","code":404}}`

// refusingClient is an OpenRouter-shaped adapter whose endpoint refuses every
// request until `accept` says the body is finally servable.
//
// The base URL matters and is not decoration: the routing preference object —
// the first rung of the ladder — is only ever sent to a router, so a test on
// provider.test would be testing a shorter ladder than the product climbs.
func refusingClient(t *testing.T, accept func(map[string]any) bool, config Config) (*Client, *capture) {
	t.Helper()
	// The ladder these tests climb is the ranked road's ([rankedRoad]).
	config = rankedRoad(config)
	forgetLanes(t)
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(len(recorded.bodies) - 1)
		writer.Header().Set("Content-Type", "application/json")
		if !accept(body) {
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(refusalBody))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop",` +
			`"message":{"role":"assistant","content":"ok"}}]}`))
	})
	config.APIKey = "test-key"
	config.BaseURL = "https://openrouter.ai/api/v1"
	config.HTTPClient = handlerClient(handler)
	if config.Model == "" {
		config.Model = "sim/model"
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	return client, recorded
}

// toolRequest is a chat-v3-shaped call: a belt, an output cap, and a reasoning
// level somebody set with ctrl+t. All three are optional, and all three narrow
// the endpoint set on a router that was told to require them.
func toolRequest() []ai.Option {
	cap := 4096
	return []ai.Option{
		ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}}),
		func(request *ai.Request) error { request.MaxTokens = &cap; return nil },
	}
}

func TestRefusalStripsOptionalParametersInOrderAndSaysSo(t *testing.T) {
	// The endpoint accepts a request only once the tools are gone — the last
	// rung — so the whole ladder is climbed and every line it says is observable.
	client, recorded := refusingClient(t, func(body map[string]any) bool {
		_, hasTools := body["tools"]
		return !hasTools
	}, Config{})

	var notices []string
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortHigh)
	response, err := client.CompleteWithMessages(noticeContext(ctx, &notices), userMessages("hi"), toolRequest()...)
	if err != nil {
		t.Fatalf("the chain should have landed the call: %v", err)
	}
	if response == nil {
		t.Fatal("no response")
	}

	want := []string{
		"Retry 1/4: relaxed the endpoint filter",
		"Retry 2/4: removed reasoning",
		"Retry 3/4: removed max_tokens",
		"Retry 4/4: removed tools",
	}
	if len(notices) != len(want) {
		t.Fatalf("notices = %#v, want %d lines", notices, len(want))
	}
	for index, line := range want {
		if notices[index] != line {
			t.Fatalf("notice %d = %q, want %q", index, notices[index], line)
		}
	}

	// And the wire agrees with the words: each rung takes off exactly what it
	// said it did, on top of everything before it.
	if _, sent := recorded.body(0)["provider"].(map[string]any)["require_parameters"]; !sent {
		t.Fatalf("first attempt = %#v, want the endpoint filter that caused this", recorded.body(0))
	}
	if prefs, _ := recorded.body(1)["provider"].(map[string]any); prefs["require_parameters"] != nil {
		t.Fatalf("attempt 2 still filters endpoints: %#v", prefs)
	}
	if reasoning := recorded.body(2)["reasoning"]; reasoning != nil {
		t.Fatalf("attempt 3 still asks for reasoning: %#v", reasoning)
	}
	if cap := recorded.body(3)["max_tokens"]; cap != nil {
		t.Fatalf("attempt 4 still caps output: %#v", cap)
	}
	if tools := recorded.body(4)["tools"]; tools != nil {
		t.Fatalf("attempt 5 still carries tools: %#v", tools)
	}
	// The conversation itself is never touched. Every rung is a knob.
	for index := range want {
		if messages, _ := recorded.body(index)["messages"].([]any); len(messages) != 1 {
			t.Fatalf("attempt %d rewrote the conversation: %#v", index+1, recorded.body(index))
		}
	}
}

// The router has a second spelling for "your filter removed everything":
// "All providers have been ignored", produced when `provider.ignore` covers
// every endpoint serving the model — which this process's own velocity ledger
// can cause on a model with one provider. The first rung of the ladder drops
// that list, so the phrase must be in the ladder's vocabulary; it once was
// not, and a wave of task nodes died on instant 404s instead of one retry.
func TestAnAllProvidersIgnoredRefusalClimbsTheLadder(t *testing.T) {
	const allIgnoredBody = `{"error":{"message":"All providers have been ignored. ` +
		`To change your default ignored providers, visit: https://openrouter.ai/settings/privacy","code":404}}`
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(len(recorded.bodies) - 1)
		writer.Header().Set("Content-Type", "application/json")
		// The endpoint accepts the moment the filter is gone — the first rung.
		if prefs, _ := body["provider"].(map[string]any); prefs != nil && prefs["require_parameters"] != nil {
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(allIgnoredBody))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop",` +
			`"message":{"role":"assistant","content":"ok"}}]}`))
	})
	config := rankedRoad(Config{APIKey: "test-key", BaseURL: "https://openrouter.ai/api/v1",
		HTTPClient: handlerClient(handler), Model: "sim/model"})
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	var notices []string
	response, err := client.CompleteWithMessages(noticeContext(context.Background(), &notices), userMessages("hi"))
	if err != nil {
		t.Fatalf("the first rung should have landed the call: %v", err)
	}
	if response == nil {
		t.Fatal("no response")
	}
	if len(notices) == 0 || !strings.Contains(notices[0], "relaxed the endpoint filter") {
		t.Fatalf("notices = %#v, want the endpoint filter relaxed first", notices)
	}
}

// THE LADDER ENDS AT THE REQUEST'S SHAPE AND NEVER REACHES A MODEL.
//
// It used to walk [Client.fallbackChain] at its foot: a model hop inside the
// adapter, on a budget nobody above could see, while internal/session's turn
// loop walked the same list for the same reason (docs/design/recovery/DESIGN.md
// §2.2). Live evidence from 2026-09-10 22:32 says what it cost beyond the double
// spend — the hop carried the ORIGINAL model's `provider.only` across and was
// answered `404 No allowed providers are available for the selected model`,
// because a lane pin is per model and nothing re-derived it.
//
// So a request nothing can serve comes back as a DIAGNOSIS about its shape, with
// the chain untouched, and the layer that owns the turn decides whether another
// model is worth asking (internal/session's nextFallback, reading
// [ModelsTried]). internal/taxonomy's classifier_law_test.go fails the build on
// a second reader of the chain.
func TestTheLadderNeverWalksToAnotherModel(t *testing.T) {
	client, recorded := refusingClient(t, func(body map[string]any) bool {
		return body["model"] == "other/model"
	}, Config{Fallbacks: []string{"other/model"}})

	var notices []string
	_, err := client.CompleteWithMessages(noticeContext(context.Background(), &notices),
		userMessages("hi"), toolRequest()...)
	if err == nil {
		t.Fatal("every endpoint serving this model refused; the call should have")
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("err = %T (%v), want the ladder's own diagnosis", err, err)
	}
	// NOT ONE REQUEST NAMED THE FALLBACK, and the fallback was configured and
	// would have answered — which is exactly what makes this an assertion rather
	// than an accident of the fixture.
	for index := range recorded.bodies {
		if model, _ := recorded.body(index)["model"].(string); model != "sim/model" {
			t.Fatalf("request %d went out on %q — the adapter changed the model", index, model)
		}
	}
	for _, notice := range notices {
		if strings.Contains(notice, "Falling back") {
			t.Fatalf("notices = %v, want nothing said about a model this layer does not change", notices)
		}
	}
	// AND THE DIAGNOSIS IS ABOUT THE SHAPE, which is the half this layer owns.
	if len(refusal.Stripped) == 0 {
		t.Fatalf("the diagnosis named nothing it took off: %v", err)
	}
}

// The catalog is asked only when the person has written no list of their own: an
// operator who answered this question must not be overruled by an inference.
func TestConfiguredFallbacksOutrankTheCatalogsNearestModel(t *testing.T) {
	client, _ := refusingClient(t, func(map[string]any) bool { return false }, Config{
		Fallbacks:     []string{"chosen/model"},
		NearestModels: func(string) []string { return []string{"guessed/model"} },
	})
	if got := client.fallbackChain("sim/model"); len(got) != 1 || got[0] != "chosen/model" {
		t.Fatalf("fallbackChain = %v, want only the operator's own list", got)
	}

	client, _ = refusingClient(t, func(map[string]any) bool { return false }, Config{
		NearestModels: func(string) []string { return []string{"guessed/model", "sim/model", "guessed/model"} },
	})
	// The failing model is never in its own chain, and nothing appears twice.
	if got := client.fallbackChain("sim/model"); len(got) != 1 || got[0] != "guessed/model" {
		t.Fatalf("fallbackChain = %v, want the catalog's suggestion, deduped and without the failing model", got)
	}
}

func TestExhaustedChainEndsInADiagnosisRatherThanA404(t *testing.T) {
	client, _ := refusingClient(t, func(map[string]any) bool { return false },
		Config{Model: "sim/exhausted-price", Fallbacks: []string{"other/model"}, ModelPrice: knownModelPrice()})

	_, err := client.CompleteWithMessages(context.Background(), userMessages("hi"), toolRequest()...)
	if err == nil {
		t.Fatal("every endpoint refused; the call should have")
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("err = %T (%v), want a *RefusalError", err, err)
	}
	message := err.Error()
	for _, want := range []string{
		"sim/exhausted-price", // which model
		"tools", "max_tokens", // what was sent
		"provider.require_parameters", "provider.max_price", // including the economies nobody asked for
		"retried without",    // what was taken off
		"No endpoints found", // the provider's own words, kept
		"/model",             // and the one thing a person can do
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("terminal error = %q, want it to name %q", message, want)
		}
	}
	// The refusal is still reachable underneath, so a caller classifying by
	// status finds the 404 rather than a sentence it has to parse.
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusNotFound {
		t.Fatalf("unwrapped = %#v, want the 404 underneath", api)
	}
}

// A 404 that says nothing about parameters — a mistyped base URL, a model slug
// that does not exist — is NOT this class. Retrying it four times would spend a
// person's turn discovering that.
func TestAPlain404IsSurfacedRatherThanRetried(t *testing.T) {
	var mu sync.Mutex
	served := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		served++
		mu.Unlock()
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"error":{"message":"model not found","code":404}}`))
	})
	client, err := NewClient(Config{APIKey: "k", BaseURL: "https://openrouter.ai/api/v1", Model: "sim/model",
		HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hi"), toolRequest()...); err == nil {
		t.Fatal("a 404 should still fail the call")
	}
	mu.Lock()
	defer mu.Unlock()
	if served != 1 {
		t.Fatalf("served %d requests, want exactly 1 — a plain 404 is not a shape to negotiate", served)
	}
}

// A rung for a field the request never carried is not a retry, it is the same
// request sent twice — and it would inflate the counter the person is reading.
func TestTheLadderOnlyOffersRungsThatWouldChangeTheBody(t *testing.T) {
	client, _ := refusingClient(t, func(map[string]any) bool { return false }, Config{Routing: StaticRouting(RoutingOff)})
	plan := client.relaxationPlan(&ai.Request{Messages: userMessages("hi")}, callKnobs{}, "sim/model")
	if len(plan) != 0 {
		t.Fatalf("plan = %#v, want nothing to strip off a bare request with routing off", plan)
	}
}

func TestDroppedAttachmentsLeaveASentenceBehind(t *testing.T) {
	messages := []ai.Message{{Role: "user", Content: []ai.ContentPart{
		{Type: "text", Text: "what is wrong here"},
		{Type: "image_url", ImageURL: &ai.ImageURLData{URL: "data:image/png;base64,AAAA"}},
	}}}
	stripped := dropAttachments(messages)
	if carriesAttachments(stripped) {
		t.Fatalf("stripped = %#v, want no attachment parts", stripped)
	}
	// The message REFERS to the picture. Answering it against nothing at all is
	// a confident answer about nothing, which is worse than saying it went.
	if len(stripped[0].Content) != 2 || stripped[0].Content[1].Text != attachmentNote {
		t.Fatalf("stripped = %#v, want the note in place of the image", stripped[0].Content)
	}
	// A conversation with nothing to drop is returned untouched, so a text-only
	// request keeps producing byte-identical bodies.
	plain := userMessages("hi")
	if got := dropAttachments(plain); &got[0] != &plain[0] {
		t.Fatal("a text-only conversation should pass through untouched")
	}
}

// noticeContext installs a stream observer that records the chain's lines. It
// streams, because that is what the surface does — and the notices are raised
// before the stream opens, which is the ordering a room depends on.
func noticeContext(ctx context.Context, into *[]string) context.Context {
	var mu sync.Mutex
	return WithStreamObserver(ctx, func(event StreamEvent) {
		if event.Kind != StreamNotice {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		*into = append(*into, event.Delta)
	})
}

// streamedRefusal is the same handler as [refusingClient]'s, answering in SSE
// once it is willing to answer at all.
func streamedRefusal(accept func(map[string]any) bool, recorded *capture) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(len(recorded.bodies) - 1)
		if !accept(body) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(refusalBody))
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
}

// The notices have to reach a STREAMED turn, which is the only kind the chat
// surface makes, and they have to arrive before the stream opens.
func TestNoticesReachAStreamedTurnBeforeItStarts(t *testing.T) {
	recorded := &capture{}
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "https://openrouter.ai/api/v1", Model: "sim/model",
		HTTPClient: handlerClient(streamedRefusal(func(body map[string]any) bool {
			_, hasTools := body["tools"]
			return !hasTools
		}, recorded)),
	})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var order []string
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		switch event.Kind {
		case StreamNotice:
			order = append(order, "notice:"+event.Delta)
		case StreamStarted:
			order = append(order, "started")
		}
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hi"), toolRequest()...); err != nil {
		t.Fatalf("the chain should have landed the streamed call: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) < 2 || order[len(order)-1] != "started" {
		t.Fatalf("event order = %v, want every notice ahead of the stream opening", order)
	}
	for _, event := range order[:len(order)-1] {
		if !strings.HasPrefix(event, "notice:Retry ") {
			t.Fatalf("event order = %v, want only retry notices before the stream", order)
		}
	}
}

// ── THE SECOND DOOR: PATIENCE SPENT ON PACING ───────────────────────────────
//
// A 429 that never clears is the other way a model runs out of ability to
// answer, and the answer is the same one the refusal ladder ends in: ask a
// different model, through the same chain, in the same words.

// pacingUntil is a handler that 429s every request naming a model in `paced`
// and answers everything else.
func pacingUntil(paced map[string]bool) (http.Handler, *capture) {
	recorded := &capture{}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(len(recorded.bodies) - 1)
		model, _ := body["model"].(string)
		writer.Header().Set("Content-Type", "application/json")
		if paced[model] {
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":{"message":"rate limit exceeded","code":429}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"` + model + `","choices":[{"index":0,"finish_reason":"stop",` +
			`"message":{"role":"assistant","content":"ok"}}]}`))
	}), recorded
}

func pacedChainClient(t *testing.T, handler http.Handler, fallbacks []string) *Client {
	t.Helper()
	client, err := NewClient(Config{APIKey: "k", BaseURL: "https://openrouter.ai/api/v1",
		Model: "sim/model", Fallbacks: fallbacks, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	// The waits themselves are retry.go's business and are proved there; what
	// this file is about is where the call goes once they are spent.
	client.wait = func(context.Context, time.Duration) error { return nil }
	return client
}

// A POOL THAT WOULD NOT STOP PACING US COMES BACK WHOLE, CHAIN OR NO CHAIN.
//
// There was a second door here — `recoverFromPacing` — which walked the fallback
// models when the attempt loop's patience ran out. It was the other half of the
// two-model-hop defect (docs/design/recovery/DESIGN.md §2.2) and it is deleted.
// The 429 now travels back exactly as it arrived: the response boundary reads it
// as the wire, the turn spends its own budget on it, and if that budget runs out
// the ONE model hop in this build takes it to the next model knowing what has
// already been tried.
//
// A CHAIN CHANGES NOTHING HERE, which is the assertion. Both halves of this test
// used to be two tests with opposite outcomes.
func TestPacingThatNeverClearsComesBackForTheLayerThatOwnsTheTurn(t *testing.T) {
	for _, fallbacks := range [][]string{nil, {"other/model"}} {
		handler, recorded := pacingUntil(map[string]bool{"sim/model": true})
		client := pacedChainClient(t, handler, fallbacks)

		var notices []string
		_, err := client.CompleteWithMessages(
			noticeContext(context.Background(), &notices), userMessages("hi"))
		if err == nil {
			t.Fatalf("fallbacks %v: every attempt was paced; the call should have failed", fallbacks)
		}
		if !strings.Contains(err.Error(), "429") {
			t.Fatalf("fallbacks %v: the error %q no longer names what the provider said", fallbacks, err)
		}
		if len(notices) != 0 {
			t.Fatalf("fallbacks %v: notices = %v, want nothing said about a model this layer does not change",
				fallbacks, notices)
		}
		for index := range recorded.bodies {
			if model, _ := recorded.body(index)["model"].(string); model != "sim/model" {
				t.Fatalf("fallbacks %v: request %d went out on %q", fallbacks, index, model)
			}
		}
	}
}

// A FAULT IS NOT PACING. A 500 keeps the short patience and the error it always
// had; only a provider that would not stop pacing us opens this door.
func TestAServerFaultDoesNotEnterTheChain(t *testing.T) {
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"message":"server error","code":500}}`))
	})
	client := pacedChainClient(t, handler, []string{"other/model"})

	var notices []string
	if _, err := client.CompleteWithMessages(
		noticeContext(context.Background(), &notices), userMessages("hi")); err == nil {
		t.Fatal("a failing server should have failed the call")
	}
	if len(notices) != 0 {
		t.Fatalf("notices = %v, want a fault to stay a fault", notices)
	}
	for index := range recorded.bodies {
		if got, _ := recorded.body(index)["model"].(string); got != "sim/model" {
			t.Fatalf("request %d rode %q; a fault never moves a model", index, got)
		}
	}
}
