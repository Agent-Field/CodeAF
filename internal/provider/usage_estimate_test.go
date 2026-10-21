package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

func TestUsageFallbackWorksAcrossCompatibleProviders(t *testing.T) {
	for _, source := range []string{"ollama", "vllm", "lmstudio", "llamacpp", "custom", "openai", "openrouter"} {
		for _, streamed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/stream=%t", source, streamed), func(t *testing.T) {
				t.Setenv(home.EnvVar, t.TempDir())
				telemetry.EnableForTest(t, true)
				telemetry.VersionForTest(t, "v0.7.0")
				finish := telemetry.BeginUsageSession(telemetry.ModeChat, "fallback")
				defer finish()
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if streamed {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, `data: {"id":"generation-private","choices":[{"index":0,"delta":{"content":"hello world"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"id":"generation-private","choices":[{"index":0,"message":{"role":"assistant","content":"hello world","reasoning":"visible thought"},"finish_reason":"stop"}]}`)
					}
				})
				client, err := NewClient(Config{SourceID: source, APIKey: "test", BaseURL: "http://private-host/v1", Model: "qwen3", Direct: true, HTTPClient: handlerClient(handler)})
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				if streamed {
					ctx = WithStreamObserver(ctx, func(StreamEvent) {})
				}
				if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
					t.Fatal(err)
				}
				rows := telemetry.SpoolContents()
				if len(rows) != 1 {
					t.Fatalf("rows=%d", len(rows))
				}
				var event struct {
					Props map[string]any `json:"props"`
				}
				if err := json.Unmarshal(rows[0], &event); err != nil {
					t.Fatal(err)
				}
				if event.Props["usage_status"] != "estimated" || event.Props["estimated_input_tokens"].(float64) <= 0 || event.Props["estimated_output_tokens"].(float64) <= 0 {
					t.Fatal(event.Props)
				}
				if _, exists := event.Props["total_tokens"]; exists {
					t.Fatal("estimate became measured usage")
				}
				for _, secret := range []string{"hello world", "private-host", "generation-private"} {
					if strings.Contains(string(rows[0]), secret) {
						t.Fatalf("private value escaped: %s", secret)
					}
				}
			})
		}
	}
}

func TestUsagePresencePreservesZeroAliasesAndPartialReceipts(t *testing.T) {
	for _, tc := range []struct {
		body               string
		input, output      bool
		prompt, completion int
	}{
		{`{}`, false, false, 0, 0}, {`{"cost":0.1}`, false, false, 0, 0},
		{`{"total_tokens":12}`, false, false, 0, 0},
		{`{"prompt_tokens":0,"completion_tokens":0}`, true, true, 0, 0},
		{`{"prompt_tokens":7}`, true, false, 7, 0}, {`{"output_tokens":3}`, false, true, 0, 3},
		{`{"input_tokens":7,"output_tokens":3,"cache_read_input_tokens":5,"cache_creation_input_tokens":2}`, true, true, 14, 3},
		{`{"prompt_tokens":7,"completion_tokens":3,"cache_read_input_tokens":5}`, true, true, 7, 3},
		{`{"prompt_tokens":null,"completion_tokens":2}`, false, true, 0, 2},
	} {
		var usage usageWire
		if err := json.Unmarshal([]byte(tc.body), &usage); err != nil {
			t.Fatal(err)
		}
		if usage.inputReported != tc.input || usage.outputReported != tc.output || usage.PromptTokens != tc.prompt || usage.CompletionTokens != tc.completion {
			t.Fatalf("%s: %+v", tc.body, usage)
		}
	}
}

func TestUsageEstimatesKeepComponentsAndPartialScopeHonest(t *testing.T) {
	client := &Client{config: Config{BaseURL: "http://local/v1"}}
	for _, tc := range []struct {
		name                    string
		o                       usageObservation
		usage                   *ai.Usage
		err                     error
		want                    string
		estimateIn, estimateOut bool
	}{
		{"partial", usageObservation{inputBytes: 80, inputTextOnly: true, outputBytes: 20, outputObserved: true, usageParsed: true, inputReported: true}, &ai.Usage{PromptTokens: 11}, nil, "mixed", false, true},
		{"cost_only", usageObservation{inputBytes: 80, inputTextOnly: true, outputBytes: 20, outputObserved: true, usageParsed: true}, &ai.Usage{}, nil, "estimated", true, true},
		{"zero", usageObservation{usageParsed: true, inputReported: true, outputReported: true}, &ai.Usage{}, nil, "reported", false, false},
		{"media", usageObservation{outputBytes: 20, outputObserved: true, usageParsed: true}, nil, nil, "estimated", false, true},
		{"cut", usageObservation{inputBytes: 80, inputTextOnly: true, outputBytes: 20, outputObserved: true, usageParsed: true}, nil, context.Canceled, "estimated", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := recordFacts{status: 200, response: &ai.Response{Usage: tc.usage}, err: tc.err, knobs: callKnobs{trace: &callTrace{usageObservation: tc.o}}}
			d := client.usageDimensions(facts, "attempt", "qwen3")
			if d.UsageStatus != tc.want || (d.EstimatedInput != nil) != tc.estimateIn || (d.EstimatedOutput != nil) != tc.estimateOut {
				t.Fatalf("%+v", d)
			}
			if tc.name == "cut" && d.EstimateScope != "observed_partial" {
				t.Fatal(d)
			}
			if tc.name == "media" && d.EstimateScope != "multimodal_input" {
				t.Fatal(d)
			}
			facts.status = 400
			d = client.usageDimensions(facts, "attempt", "qwen3")
			if d.EstimatedInput != nil || d.EstimatedOutput != nil {
				t.Fatal("refused request invented usage")
			}
		})
	}
}

func TestUsageEstimateIncludesToolsAndVisibleReasoning(t *testing.T) {
	o := usageObservation{}
	o.observeDelta(streamDelta{Content: "abcd", Reasoning: "abcdefgh", ReasoningContent: "abcdefgh", ToolCalls: []toolCallDelta{{Function: toolFunctionDelta{Name: "read", Arguments: `{"x":1}`}}}})
	if o.outputBytes != 4+8+4+7 {
		t.Fatalf("output bytes=%d", o.outputBytes)
	}
	messages := []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)}
	size, known := usageInputBytes(messages, []json.RawMessage{json.RawMessage(`{"name":"read","schema":{"type":"object"}}`)}, userMessages("hello"))
	if !known || size <= len(messages[0]) {
		t.Fatal("tool schema missing from estimate")
	}
	source := userMessages("hello")
	source[0].Content = append(source[0].Content, ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{URL: "data:image/png;base64,secret"}})
	if _, known := usageInputBytes(messages, nil, source); known {
		t.Fatal("image data estimated as text")
	}
}

func TestRawStreamUsageReachesTelemetry(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeChat, "raw")
	defer finish()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"raw","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "qwen3", Direct: true, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	chunks, errs := client.StreamComplete(context.Background(), "hello")
	for range chunks {
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if rows := telemetry.SpoolContents(); len(rows) != 1 {
		t.Fatalf("raw stream receipts=%d", len(rows))
	}
}

func TestLateProviderReceiptSupersedesEstimateWithSameIdentity(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeChat, "recovery")
	defer finish()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/generation") {
			fmt.Fprint(w, `{"data":{"tokens_prompt":10,"tokens_completion":3,"total_cost":0.01}}`)
			return
		}
		fmt.Fprint(w, `{"id":"late-private","choices":[{"index":0,"message":{"role":"assistant","content":"hello world"},"finish_reason":"stop"}]}`)
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "https://openrouter.ai/api/v1", Model: "qwen3", HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan Reconciled, 1)
	ctx := WithUnmeteredReceipts(WithReconcile(context.Background(), func(r Reconciled) { result <- r }))
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if answer := receiptResult(t, result); !answer.Found {
		t.Fatal("receipt not recovered")
	}
	rows := telemetry.SpoolContents()
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	var first, second struct {
		ID    string         `json:"event_id"`
		Props map[string]any `json:"props"`
	}
	if err := json.Unmarshal(rows[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rows[1], &second); err != nil {
		t.Fatal(err)
	}
	if first.Props["usage_status"] != "estimated" || second.Props["usage_status"] != "reported" {
		t.Fatal(first.Props, second.Props)
	}
	if first.Props["usage_receipt"] != second.Props["usage_receipt"] || first.ID == second.ID {
		t.Fatal("estimate and receipt cannot be reconciled")
	}
	if first.Props["total_tokens"] != nil || second.Props["total_tokens"] != float64(13) {
		t.Fatal("measured totals double counted")
	}
}

func TestUsageOptionRefusalFallsBackWithoutBreakingOlderServers(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeChat, "legacy")
	defer finish()
	requested, accepted := 0, 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream_options"] != nil {
			requested++
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"message":"unknown field stream_options"}}`)
			return
		}
		accepted++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "http://legacy/v1", Model: "qwen3", Direct: true, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	for i := 0; i < 2; i++ {
		if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
			t.Fatal(err)
		}
	}
	if requested != 1 || accepted != 2 {
		t.Fatalf("opt-in refusals=%d, accepted=%d", requested, accepted)
	}
	rows := telemetry.SpoolContents()
	if len(rows) != 3 {
		t.Fatalf("receipts=%d", len(rows))
	}
	var refused struct {
		Props map[string]any `json:"props"`
	}
	if err := json.Unmarshal(rows[0], &refused); err != nil {
		t.Fatal(err)
	}
	if refused.Props["estimated_total_tokens"] != nil {
		t.Fatal("400 request counted as consumption")
	}
	if refusesStreamUsage([]byte(`{"error":"stream_options is valid but tool schema is invalid"}`)) {
		t.Fatal("unrelated refusal disables usage")
	}
}

func TestUsageTotalOnlyReceiptIsKeptWithoutInventingASplit(t *testing.T) {
	for _, total := range []int{0, 12} {
		var wire usageWire
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"total_tokens":%d}`, total)), &wire); err != nil {
			t.Fatal(err)
		}
		o := usageObservation{inputBytes: 80, inputTextOnly: true, outputBytes: 20, outputObserved: true}
		o.observeUsage(&wire)
		client := &Client{config: Config{BaseURL: "http://local/v1"}}
		d := client.usageDimensions(recordFacts{status: 200, response: &ai.Response{Usage: wire.usage()}, knobs: callKnobs{trace: &callTrace{usageObservation: o}}}, "attempt", "qwen3")
		if d.UsageStatus != "total_only" || d.ReportedTotal == nil || *d.ReportedTotal != total || d.EstimatedInput != nil || d.EstimatedOutput != nil {
			t.Fatal(d)
		}
	}
}
