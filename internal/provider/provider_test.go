package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestAdaptiveCompletionTimeoutScalesAndBoundsRequests(t *testing.T) {
	tests := []struct {
		name       string
		maxTokens  int
		configured time.Duration
		want       time.Duration
	}{
		{name: "floor", maxTokens: 4_096, want: 5 * time.Minute},
		{name: "scaled", maxTokens: 32_768, want: 512 * time.Second},
		{name: "configured floor", maxTokens: 4_096, configured: 10 * time.Minute, want: 10 * time.Minute},
		{name: "ceiling", maxTokens: 1_000_000, want: 15 * time.Minute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := adaptiveCompletionTimeout(test.maxTokens, test.configured); got != test.want {
				t.Fatalf("adaptiveCompletionTimeout(%d, %s) = %s, want %s", test.maxTokens, test.configured, got, test.want)
			}
		})
	}
}

// capture records exactly what the adapter put on the wire. The whole point of
// this package is the request shape, so the tests assert bytes and headers
// rather than behavior described in prose.
type capture struct {
	mu      sync.Mutex
	bodies  []map[string]any
	raw     [][]byte
	headers []http.Header
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

// handlerClient exercises the complete HTTP request/response boundary without
// opening a listener. Besides working in network-denied sandboxes, it keeps
// these wire-shape tests deterministic: no kernel socket is part of the test.
func handlerClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
}

func (c *capture) record(request *http.Request) {
	payload, _ := io.ReadAll(request.Body)
	var decoded map[string]any
	_ = json.Unmarshal(payload, &decoded)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, decoded)
	c.raw = append(c.raw, payload)
	c.headers = append(c.headers, request.Header.Clone())
}

func (c *capture) body(index int) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index >= len(c.bodies) {
		return nil
	}
	return c.bodies[index]
}

func newTestClient(t *testing.T, config Config) (*Client, *capture) {
	t.Helper()
	forgetLanes(t)
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	})

	config.APIKey = "test-key"
	config.BaseURL = "http://provider.test"
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

func userMessages(text string) []ai.Message {
	return []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: text}}}}
}

func TestAdapterAlwaysOptsIntoUsageAccounting(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	usage, _ := recorded.body(0)["usage"].(map[string]any)
	if include, _ := usage["include"].(bool); !include {
		t.Fatalf("request body = %#v, want usage accounting enabled", recorded.body(0))
	}
}

func TestAdapterCarriesOneRunStableCacheKeyAcrossEveryCall(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	key := RunCacheKey("fix the failing test", "sim/model")
	ctx := WithCacheKey(context.Background(), key)
	for range 3 {
		if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
			t.Fatal(err)
		}
	}
	for index := range 3 {
		if got, _ := recorded.body(index)["prompt_cache_key"].(string); got != key {
			t.Fatalf("call %d prompt_cache_key = %q, want the run-stable %q", index, got, key)
		}
		if got := recorded.headers[index].Get("X-Session-Affinity"); got != key {
			t.Fatalf("call %d session affinity header = %q, want %q", index, got, key)
		}
	}
	// Derived from run identity, never from a clock or a random source, so a
	// second process running the same task rejoins the same warm prefix.
	if RunCacheKey("fix the failing test", "sim/model") != key {
		t.Fatal("run cache key is not deterministic")
	}
	if RunCacheKey("a different task", "sim/model") == key {
		t.Fatal("run cache key does not distinguish runs")
	}
}

func TestAdapterOmitsCacheKeyWhenNoRunIsPinned(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["prompt_cache_key"]; present {
		t.Fatalf("request body = %#v, want no cache key", recorded.body(0))
	}
	if got := recorded.headers[0].Get("X-Session-Affinity"); got != "" {
		t.Fatalf("session affinity header = %q, want none", got)
	}
}

func TestAdapterSendsPlanReasoningEffortAndOmitsItForWorkers(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	planCtx := WithReasoningEffort(context.Background(), EffortLow)
	if _, err := client.CompleteWithMessages(planCtx, userMessages("route this")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("do the work")); err != nil {
		t.Fatal(err)
	}
	reasoning, _ := recorded.body(0)["reasoning"].(map[string]any)
	if effort, _ := reasoning["effort"].(string); effort != "low" {
		t.Fatalf("plan request = %#v, want reasoning effort low", recorded.body(0))
	}
	if _, present := recorded.body(1)["reasoning"]; present {
		t.Fatalf("worker request = %#v, want the provider default and no knob", recorded.body(1))
	}
}

// TestAPinnedSeatCarriesItsEffortOnEveryCall is C1: the level belongs to the
// seat, so a run-wide economy on the context cannot displace it on any request.
func TestAPinnedSeatCarriesItsEffortOnEveryCall(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		Model:  "vendor/model",
		Effort: EffortHigh,
		SupportsParameter: func(string, string) (bool, bool) {
			return true, true
		},
	})
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	for range 3 {
		if _, err := client.CompleteWithMessages(ctx, userMessages("plan this")); err != nil {
			t.Fatal(err)
		}
	}
	for index := range 3 {
		reasoning, _ := recorded.body(index)["reasoning"].(map[string]any)
		if effort, _ := reasoning["effort"].(string); effort != "high" {
			t.Fatalf("call %d request = %#v, want the seat's high effort", index, recorded.body(index))
		}
		if _, present := reasoning["max_tokens"]; present {
			t.Fatalf("call %d gave a word pin a thinking budget: %#v", index, recorded.body(index))
		}
	}
}

func TestInnerEffortNoneEscapesAnOuterRunWideEconomy(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	// The run stamps EffortOff for planning; the executor layers "model
	// default" on top. If the inner value did not shadow the outer one, every
	// executor call would inherit the planner's economy — the exact
	// configuration that ran an agent for 139 turns without writing a file.
	runCtx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	execCtx := WithConfiguredReasoningEffort(runCtx, EffortNone)
	if _, err := client.CompleteWithMessages(runCtx, userMessages("plan")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(execCtx, userMessages("work")); err != nil {
		t.Fatal(err)
	}
	reasoning, _ := recorded.body(0)["reasoning"].(map[string]any)
	if enabled, ok := reasoning["enabled"].(bool); !ok || enabled {
		t.Fatalf("plan request = %#v, want reasoning disabled", recorded.body(0))
	}
	if _, present := recorded.body(1)["reasoning"]; present {
		t.Fatalf("exec request = %#v, want no reasoning knob at all", recorded.body(1))
	}
}

func TestAdapterNeverSendsReasoningToAModelThatWouldRejectIt(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return false, true },
	})
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortHigh)
	if _, err := client.CompleteWithMessages(ctx, userMessages("route this")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["reasoning"]; present {
		t.Fatalf("request = %#v, want the knob omitted for a model that does not support it", recorded.body(0))
	}
}

// AND THE CATALOG'S NO IS THE ONE ANSWER A SEAT'S PIN DOES NOT ARGUE WITH.
//
// A pin is an operator's own choice and travels where a harness default would
// not (the explicit rung in [Client.requestedEffort]), which is exactly why it
// needs saying that a row stating the model takes no reasoning knob at all
// still ends the question. The alternative is a pinned seat that 400s every
// call it makes, and a knob that breaks the run is worse than a knob that did
// not travel — the log names the pin instead ([Client.recordedEffortPin]).
func TestAPinIsStillRefusedByAModelThatTakesNoReasoningKnob(t *testing.T) {
	read := loggingTo(t)
	client, recorded := newTestClient(t, Config{
		Effort:            EffortHigh,
		SupportsParameter: func(string, string) (bool, bool) { return false, true },
	})
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("route this")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["reasoning"]; present {
		t.Fatalf("request = %#v, want the pin dropped for a model that does not support it", recorded.body(0))
	}
	done := ended(read())
	if len(done) != 1 || done[0].Effort != "" || done[0].EffortPin != "high" {
		t.Fatalf("the row should carry no effort and name the pin that did not travel: %+v", done)
	}
}

// TestAnUnpinnedClientKeepsEveryRequestShapeAndRowUnchanged is C6: a client
// with no level of its own preserves each caller's wire bytes and never invents
// a displaced-pin reading in the model-call log.
func TestAnUnpinnedClientKeepsEveryRequestShapeAndRowUnchanged(t *testing.T) {
	read := loggingTo(t)
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	requests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "no request", ctx: context.Background(),
			want: `{"model":"sim/model","stream":true,"usage":{"include":true},"messages":[{"role":"user","content":"unchanged"}]}`,
		},
		{
			name: "phase default", ctx: WithReasoningEffort(context.Background(), EffortLow),
			want: `{"model":"sim/model","stream":true,"usage":{"include":true},"messages":[{"role":"user","content":"unchanged"}],"reasoning":{"effort":"low"}}`,
		},
		{
			name: "run-wide economy", ctx: WithConfiguredReasoningEffort(context.Background(), EffortOff),
			want: `{"model":"sim/model","stream":true,"usage":{"include":true},"messages":[{"role":"user","content":"unchanged"}],"reasoning":{"enabled":false}}`,
		},
		{
			name: "required answer bound", ctx: WithRequiredReasoningEffort(context.Background(), EffortHigh),
			want: `{"model":"sim/model","stream":true,"usage":{"include":true},"messages":[{"role":"user","content":"unchanged"}],"reasoning":{"effort":"high"}}`,
		},
	}
	for _, request := range requests {
		if _, err := client.CompleteWithMessages(request.ctx, userMessages("unchanged")); err != nil {
			t.Fatalf("%s: %v", request.name, err)
		}
	}
	for index, request := range requests {
		if got := string(recorded.raw[index]); got != request.want {
			t.Errorf("%s wire bytes changed:\ngot  %s\nwant %s", request.name, got, request.want)
		}
	}
	for _, row := range read() {
		if row.EffortPin != "" {
			t.Errorf("an unpinned call logged a displaced pin: %+v", row)
		}
	}
}

func TestAdapterSendsOnlyRequiredOrConfiguredEffortWhenTheCatalogIsSilent(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return false, false },
	})
	if _, err := client.CompleteWithMessages(WithReasoningEffort(context.Background(), EffortLow), userMessages("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(WithConfiguredReasoningEffort(context.Background(), EffortLow), userMessages("b")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(WithRequiredReasoningEffort(context.Background(), EffortOff), userMessages("c")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["reasoning"]; present {
		t.Fatalf("harness default reached an unknown model: %#v", recorded.body(0))
	}
	reasoning, _ := recorded.body(1)["reasoning"].(map[string]any)
	if effort, _ := reasoning["effort"].(string); effort != "low" {
		t.Fatalf("configured request = %#v, want the operator's explicit effort", recorded.body(1))
	}
	required, _ := recorded.body(2)["reasoning"].(map[string]any)
	if enabled, present := required["enabled"].(bool); !present || enabled {
		t.Fatalf("required request = %#v, want the disable even with a silent catalog", recorded.body(2))
	}
}

func TestAdapterScrubsToolCallIDsAndKeepsResultsPaired(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	messages := []ai.Message{
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "go"}}},
		{Role: "assistant", ToolCalls: []ai.ToolCall{{
			ID: "call:a b/1", Type: "function", Function: ai.ToolCallFunction{Name: "r", Arguments: "{}"},
		}}},
		{Role: "tool", ToolCallID: "call:a b/1", Content: []ai.ContentPart{{Type: "text", Text: "{}"}}},
	}
	if _, err := client.CompleteWithMessages(context.Background(), messages); err != nil {
		t.Fatal(err)
	}
	wire, _ := recorded.body(0)["messages"].([]any)
	assistant, _ := wire[1].(map[string]any)
	calls, _ := assistant["tool_calls"].([]any)
	first, _ := calls[0].(map[string]any)
	scrubbed, _ := first["id"].(string)
	result, _ := wire[2].(map[string]any)
	if scrubbed != "call_x3aa_x20b_x2f1" {
		t.Fatalf("scrubbed id = %q", scrubbed)
	}
	if resultID, _ := result["tool_call_id"].(string); resultID != scrubbed {
		t.Fatalf("tool result id = %q, want the same scrubbed id %q", resultID, scrubbed)
	}
	// The harness never sees the rewrite: its own transcript is untouched, so
	// the byte-stable prefix it maintains is not disturbed by hygiene.
	if messages[1].ToolCalls[0].ID != "call:a b/1" {
		t.Fatal("hygiene mutated the caller's transcript")
	}
}

func TestAdapterHygieneIsANoOpOnCleanMessages(t *testing.T) {
	clean := []ai.Message{
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "go"}}},
		{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "reading"}}, ToolCalls: []ai.ToolCall{{
			ID: "call_abc-123", Type: "function", Function: ai.ToolCallFunction{Name: "r", Arguments: "{}"},
		}}},
		{Role: "tool", ToolCallID: "call_abc-123", Content: []ai.ContentPart{{Type: "text", Text: "{}"}}},
	}
	if got := sanitizeMessages(clean); &got[0] != &clean[0] {
		t.Fatal("clean messages were copied; hygiene must be a no-op")
	}
	if got := scrubToolCallID("call_abc-123"); got != "call_abc-123" {
		t.Fatalf("clean id was rewritten: %q", got)
	}
}

func TestAdapterDropsEmptyAssistantTextParts(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	messages := []ai.Message{
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "go"}}},
		{Role: "assistant", Content: []ai.ContentPart{
			{Type: "text", Text: ""},
			{Type: "text", Text: "a real observation"},
		}},
	}
	if _, err := client.CompleteWithMessages(context.Background(), messages); err != nil {
		t.Fatal(err)
	}
	// A single surviving text part serializes as a plain string, which is the
	// most compatible shape and proves the empty block was removed.
	if !strings.Contains(string(recorded.raw[0]), `"content":"a real observation"`) {
		t.Fatalf("wire body = %s", recorded.raw[0])
	}
	if strings.Contains(string(recorded.raw[0]), `"text":""`) {
		t.Fatalf("empty assistant part reached the wire: %s", recorded.raw[0])
	}
}

func TestAdapterKeepsAContentlessAssistantMessageStable(t *testing.T) {
	// Dropping the last empty part would change the message's wire shape, so a
	// content-less assistant message with no tool calls is left exactly alone.
	content := []ai.ContentPart{{Type: "text", Text: ""}}
	got, dropped := dropEmptyTextParts("assistant", content, false)
	if dropped || len(got) != 1 {
		t.Fatalf("dropped=%t content=%#v", dropped, got)
	}
	if _, dropped := dropEmptyTextParts("assistant", content, true); !dropped {
		t.Fatal("an empty part alongside tool calls must be dropped")
	}
	if _, dropped := dropEmptyTextParts("user", content, false); dropped {
		t.Fatal("hygiene must only touch assistant content")
	}
}

func TestAdapterRequestsAreDeterministicForTheSameInput(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	ctx := WithReasoningEffort(WithCacheKey(context.Background(), "run-1"), EffortLow)
	for range 2 {
		if _, err := client.CompleteWithMessages(ctx, userMessages("identical")); err != nil {
			t.Fatal(err)
		}
	}
	if string(recorded.raw[0]) != string(recorded.raw[1]) {
		t.Fatalf("identical calls produced different bytes:\n%s\n%s", recorded.raw[0], recorded.raw[1])
	}
}

func TestAdapterRewritesMaxTokensOnlyForVouchedOpenAIFamilies(t *testing.T) {
	client, recorded := newTestClient(t, Config{Model: "deepseek/deepseek-v4-flash"})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("a"), ai.WithMaxTokens(4096)); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["max_tokens"]; !present {
		t.Fatalf("request = %#v, want max_tokens for a non-OpenAI model", recorded.body(0))
	}
	if !needsMaxCompletionTokens("openai/gpt-5") || needsMaxCompletionTokens("openai/gpt-4") {
		t.Fatal("output-limit field selection regressed")
	}
}

func TestAdapterStreamsWithTheSameEconomyFields(t *testing.T) {
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	})

	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model", HTTPClient: handlerClient(handler),
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithReasoningEffort(WithCacheKey(context.Background(), "run-7"), EffortLow)
	chunks, errs := client.StreamComplete(ctx, "route this")
	var text strings.Builder
	for chunk := range chunks {
		for _, choice := range chunk.Choices {
			text.WriteString(choice.Delta.Content)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if text.String() != "hi" {
		t.Fatalf("streamed text = %q", text.String())
	}
	body := recorded.body(0)
	if stream, _ := body["stream"].(bool); !stream {
		t.Fatalf("stream request = %#v", body)
	}
	if key, _ := body["prompt_cache_key"].(string); key != "run-7" {
		t.Fatalf("stream cache key = %#v", body)
	}
	reasoning, _ := body["reasoning"].(map[string]any)
	if effort, _ := reasoning["effort"].(string); effort != "low" {
		t.Fatalf("stream reasoning = %#v", body)
	}
	usage, _ := body["usage"].(map[string]any)
	if include, _ := usage["include"].(bool); !include {
		t.Fatalf("stream usage accounting = %#v", body)
	}
}

func TestAdapterErrorsKeepTheStatusCodeTheHarnessClassifiesOn(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"message":"rate limited"}}`))
	})

	client, err := NewClient(Config{APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model", HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CompleteWithMessages(context.Background(), userMessages("a"))
	if err == nil || !strings.Contains(err.Error(), "API error (429)") {
		t.Fatalf("error = %v, want a status the provider taxonomy can read", err)
	}
}

func TestParseEffortRejectsUnknownValues(t *testing.T) {
	for _, value := range []string{"", "low", "MEDIUM", " high "} {
		if _, ok := ParseEffort(value); !ok {
			t.Fatalf("ParseEffort(%q) rejected a valid value", value)
		}
	}
	if _, ok := ParseEffort("maximum"); ok {
		t.Fatal("ParseEffort accepted a value that would 400")
	}
}

func TestObservedMessageCompletionStreamsAndReturnsAccumulatedResponse(t *testing.T) {
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"one\",\"model\":\"sim/model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"{\\\"reply\\\":\\\"hello\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"id\":\"one\",\"model\":\"sim/model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" there\\\"}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	})

	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model", HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		events = append(events, event)
	})
	response, err := client.CompleteWithMessages(ctx, userMessages("route this"))
	if err != nil {
		t.Fatal(err)
	}
	if got := response.Text(); got != `{"reply":"hello there"}` {
		t.Fatalf("accumulated response = %q", got)
	}
	if len(events) != 4 {
		t.Fatalf("stream events = %#v", events)
	}
	wantKinds := []StreamEventKind{StreamStarted, StreamDelta, StreamDelta, StreamFinished}
	var deltas strings.Builder
	for index, event := range events {
		if event.Kind != wantKinds[index] {
			t.Fatalf("event %d kind = %v, want %v", index, event.Kind, wantKinds[index])
		}
		if event.Kind == StreamDelta {
			deltas.WriteString(event.Delta)
		}
	}
	if deltas.String() != response.Text() {
		t.Fatalf("observed deltas = %q, response = %q", deltas.String(), response.Text())
	}
	if stream, _ := recorded.body(0)["stream"].(bool); !stream {
		t.Fatalf("observed completion did not use the streaming wire: %#v", recorded.body(0))
	}
}

// The structural half of the streamed path: a tool call arrives in fragments
// across several chunks, and until this was accumulated every streamed
// completion returned zero tool calls — which made the head's control belt
// spend a whole round trip that could not possibly succeed.
func TestStreamedToolCallsAccumulateIntoTheResponse(t *testing.T) {
	events := []string{
		`{"id":"one","model":"sim/model","choices":[{"index":0,"delta":{"role":"assistant","reasoning":"the user is asking about live work"}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{"reasoning":" so the board is the read"}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"board","arguments":""}}]}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"stat"}}]}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"us\":\"live\"}"}}]}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"control","arguments":"{\"verb\":\"cancel\"}"}}]},"finish_reason":"tool_calls"}]}`,
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			_, _ = writer.Write([]byte("data: " + event + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})

	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model", HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	response, err := client.CompleteWithMessages(ctx, userMessages("cancel the scans"),
		ai.WithTools([]ai.ToolDefinition{{Type: "function"}}))
	if err != nil {
		t.Fatal(err)
	}
	if !response.HasToolCalls() {
		t.Fatalf("streamed response carried no tool calls: %#v", response.Choices)
	}
	calls := response.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("accumulated %d tool calls, want 2: %#v", len(calls), calls)
	}
	if calls[0].ID != "call_1" || calls[0].Function.Name != "board" ||
		calls[0].Function.Arguments != `{"status":"live"}` {
		t.Fatalf("first call reassembled as %#v", calls[0])
	}
	if calls[1].ID != "call_2" || calls[1].Function.Name != "control" ||
		calls[1].Function.Arguments != `{"verb":"cancel"}` {
		t.Fatalf("second call reassembled as %#v", calls[1])
	}
	if response.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q", response.Choices[0].FinishReason)
	}

	// Reasoning is announced once for the run of it and never carries its text.
	thinking := 0
	for _, event := range observed {
		if event.Kind == StreamThinking {
			thinking++
			if event.Delta != "" {
				t.Fatalf("a thinking event carried reasoning text: %q", event.Delta)
			}
		}
		if event.Kind == StreamDelta {
			t.Fatalf("a tool-call stream produced a text delta: %q", event.Delta)
		}
	}
	if thinking != 1 {
		t.Fatalf("thinking events = %d, want exactly one for the run", thinking)
	}
	if observed[0].Kind != StreamStarted || observed[len(observed)-1].Kind != StreamFinished {
		t.Fatalf("stream boundaries = %#v", observed)
	}
}

// Not every endpoint indexes its fragments. One that spells a call out without
// an index must not have its arguments fused onto the previous call, and the
// zero value of the missing field must not read as "call 0".
func TestStreamedToolCallsWithoutAnIndexStayDistinct(t *testing.T) {
	events := []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"id":"a","function":{"name":"board","arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"id":"b","function":{"name":"result"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"{\"id\":\"scans\"}"}}]},"finish_reason":"tool_calls"}]}`,
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			_, _ = writer.Write([]byte("data: " + event + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model", HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	response, err := client.CompleteWithMessages(ctx, userMessages("what did the scans find"))
	if err != nil {
		t.Fatal(err)
	}
	calls := response.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("accumulated %d calls, want 2: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "board" || calls[0].Function.Arguments != "{}" {
		t.Fatalf("first call = %#v", calls[0])
	}
	if calls[1].Function.Name != "result" || calls[1].Function.Arguments != `{"id":"scans"}` {
		t.Fatalf("second call = %#v", calls[1])
	}
}

// The unstreamed path is unchanged, and a streamed answer that is only text
// still carries no tool calls at all — an empty slice would be a claim.
func TestStreamedTextAnswerCarriesNoToolCalls(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
	})
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model", HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	response, err := client.CompleteWithMessages(ctx, userMessages("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if response.HasToolCalls() || response.ToolCalls() != nil {
		t.Fatalf("a text answer claimed tool calls: %#v", response.ToolCalls())
	}
	if response.Text() != "hello" {
		t.Fatalf("streamed text = %q", response.Text())
	}
}
