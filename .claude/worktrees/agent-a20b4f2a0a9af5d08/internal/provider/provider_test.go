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
	recorded := &capture{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}))
	t.Cleanup(server.Close)

	config.APIKey = "test-key"
	config.BaseURL = server.URL
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

func TestAdapterSendsOnlyOperatorConfiguredEffortWhenTheCatalogIsSilent(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return false, false },
	})
	if _, err := client.CompleteWithMessages(WithReasoningEffort(context.Background(), EffortLow), userMessages("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(WithConfiguredReasoningEffort(context.Background(), EffortLow), userMessages("b")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["reasoning"]; present {
		t.Fatalf("harness default reached an unknown model: %#v", recorded.body(0))
	}
	reasoning, _ := recorded.body(1)["reasoning"].(map[string]any)
	if effort, _ := reasoning["effort"].(string); effort != "low" {
		t.Fatalf("configured request = %#v, want the operator's explicit effort", recorded.body(1))
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
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		APIKey: "k", BaseURL: server.URL, Model: "sim/model",
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
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "sim/model"})
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
