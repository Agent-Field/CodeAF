package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

func usageC3Session(t *testing.T) {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	t.Cleanup(telemetry.BeginUsageSession(telemetry.ModeChat, "c3"))
}

func usageC3Events(t *testing.T) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, row := range telemetry.SpoolContents() {
		var e struct {
			Props map[string]any `json:"props"`
		}
		if err := json.Unmarshal(row, &e); err != nil {
			t.Fatal(err)
		}
		events = append(events, e.Props)
	}
	return events
}

func TestK10TrimmedGenerationPairsRecoveryAndBlankUsesAttempt(t *testing.T) {
	usageC3Session(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/generation") {
			if r.URL.Query().Get("id") != "late" {
				t.Error("generation id was not canonicalized")
			}
			fmt.Fprint(w, `{"data":{"tokens_prompt":10,"tokens_completion":3,"total_cost":0.01}}`)
		} else {
			fmt.Fprint(w, `{"id":" late ","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
		}
	})
	c, err := NewClient(Config{APIKey: "test", BaseURL: "https://openrouter.ai/api/v1", Model: "qwen3", HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan Reconciled, 1)
	ctx := WithUnmeteredReceipts(WithReconcile(context.Background(), func(r Reconciled) { result <- r }))
	if _, err := c.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if !receiptResult(t, result).Found {
		t.Fatal("receipt missing")
	}
	rows := usageC3Events(t)
	if len(rows) != 2 || rows[0]["usage_receipt"] != rows[1]["usage_receipt"] {
		t.Fatalf("receipts: %+v", rows)
	}
	if rows[0]["usage_status"] != "estimated" || rows[1]["usage_status"] != "reported" {
		t.Fatal(rows)
	}
	for _, generation := range []string{"", " \t "} {
		if got := usageReceiptID("base", generation, "attempt"); got != "attempt" {
			t.Fatal(got)
		}
	}
}

func TestK5RepeatedToolNamesCountOnceAndEveryArgumentFragmentCounts(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		o := usageObservation{}
		zero := 0
		for i := 0; i < 10; i++ {
			fragment := toolCallDelta{Function: toolFunctionDelta{Name: "read", Arguments: "x"}}
			if explicit {
				fragment.Index = &zero
			}
			o.observeDelta(streamDelta{ToolCalls: []toolCallDelta{fragment}})
		}
		if o.outputBytes != 14 {
			t.Fatalf("explicit=%v: bytes=%d, want 14", explicit, o.outputBytes)
		}
	}
	// Without indexes, an id opens a call and id-less fragments continue it.
	o := usageObservation{}
	for _, f := range []toolCallDelta{
		{ID: "first", Function: toolFunctionDelta{Name: "read", Arguments: "x"}},
		{Function: toolFunctionDelta{Name: "read", Arguments: "x"}},
		{ID: "second", Function: toolFunctionDelta{Name: "read", Arguments: "x"}},
		{Function: toolFunctionDelta{Name: "read", Arguments: "x"}},
	} {
		o.observeDelta(streamDelta{ToolCalls: []toolCallDelta{f}})
	}
	if o.outputBytes != 12 {
		t.Fatal(o.outputBytes)
	}
}

func TestK5OpaqueInputCannotGrowATextEstimate(t *testing.T) {
	c, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "qwen3", Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{4, 400} {
		ctx := WithMessageReasoning(context.Background(), []MessageReasoning{{}, {Details: json.RawMessage(fmt.Sprintf(`[{"type":"reasoning.encrypted","data":%q}]`, strings.Repeat("x", size)))}})
		knobs := knobsFrom(ctx)
		req := &ai.Request{Model: "qwen3", Messages: reasoningTranscript()[:2]}
		if _, err := c.encodeRequest(req, knobs); err != nil {
			t.Fatal(err)
		}
		d := c.usageDimensions(recordFacts{status: 200, response: &ai.Response{}, knobs: knobs}, "attempt", "qwen3")
		if d.EstimatedInput != nil {
			t.Fatalf("ciphertext %d estimated: %+v", size, d)
		}
	}
	for _, field := range []string{"signature", "reasoning_details", "opaque"} {
		raw := json.RawMessage(fmt.Sprintf(`{"role":"assistant","content":"hello",%q:"%s"}`, field, strings.Repeat("x", 400)))
		if _, known := usageInputBytes([]json.RawMessage{raw}, nil, userMessages("hello")); known {
			t.Fatalf("%s counted as text", field)
		}
	}
}

func TestK2IndependentRepairsComposeInEitherOrderAndStayBounded(t *testing.T) {
	for _, order := range []string{"reasoning-first", "usage-first", "usage-repeated", "reasoning-repeated", "learned-first"} {
		t.Run(order, func(t *testing.T) {
			model := "qwen/c3-" + order
			quirksAt(t, model)
			requests := 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				messages := body["messages"].([]any)
				thought := messages[1].(map[string]any)["reasoning_content"]
				usage := body["stream_options"] != nil
				refusal := ""
				status := 400
				switch {
				case order == "learned-first" && requests == 1:
					refusal = "reasoning cannot be disabled"
				case order == "usage-repeated" || (order == "usage-first" && usage) || (order == "reasoning-first" && thought == nil && usage) || (order == "learned-first" && usage && requests > 1):
					refusal = "unknown field stream_options"
				case order == "reasoning-repeated" || thought != nil:
					refusal = "Your request contains encrypted reasoning or compaction content that was produced under a different model. Encrypted payloads can only be replayed to the endpoint that created them."
					status = 404
				}
				if refusal != "" {
					w.WriteHeader(status)
					json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": refusal}})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			})
			c, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: model, Direct: true, HTTPClient: handlerClient(handler)})
			if err != nil {
				t.Fatal(err)
			}
			ctx := WithMessageReasoning(context.Background(), []MessageReasoning{{}, {Field: "reasoning_content", Text: "carry"}})
			knobs := knobsFrom(ctx)
			if order == "usage-repeated" {
				knobs.reasoning = nil
			}
			if order == "learned-first" {
				knobs.reasoning = nil
				knobs.effort = effortRequest{effort: EffortOff, explicit: true}
			}
			request := &ai.Request{Model: model, Messages: reasoningTranscript()[:2], Stream: true}
			response, err := c.sendRepaired(ctx, request, knobs, true)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			repeated := strings.HasSuffix(order, "repeated")
			want := 3
			if repeated {
				want = 2
			}
			if requests != want || (response.StatusCode >= 400) != repeated {
				t.Fatalf("requests=%d status=%d", requests, response.StatusCode)
			}
			if !repeated && !strings.HasPrefix(order, "learned") {
				// The endpoint memo survives the call, while foreign reasoning is repaired anew.
				r, err := c.sendRepaired(ctx, request, knobsFrom(ctx), true)
				if err != nil {
					t.Fatal(err)
				}
				r.Body.Close()
				if requests != 5 {
					t.Fatalf("memo lost: requests=%d", requests)
				}
			}
		})
	}
}

func TestK7RoleOnlyCutKeepsOutputUnknownAndFinishedEmptyObservesZero(t *testing.T) {
	for _, finish := range []bool{false, true} {
		t.Run(fmt.Sprint(finish), func(t *testing.T) {
			usageC3Session(t)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"reasoning_details\":[{\"data\":\"opaque\"}]}}],\"usage\":{\"prompt_tokens\":7}}\n\n")
				if finish {
					fmt.Fprint(w, "data: [DONE]\n\n")
				}
			})
			c, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "qwen3", Direct: true, HTTPClient: handlerClient(handler)})
			if err != nil {
				t.Fatal(err)
			}
			chunks, errs := c.StreamComplete(context.Background(), "hello")
			for range chunks {
			}
			var failed bool
			for err := range errs {
				failed = failed || err != nil
			}
			rows := usageC3Events(t)
			if len(rows) != 1 {
				t.Fatal(rows)
			}
			p := rows[0]
			if finish {
				if failed || p["usage_status"] != "mixed" || p["estimated_output_tokens"] != float64(0) {
					t.Fatal(p)
				}
			} else if !failed || p["usage_status"] != "partial" || p["estimated_output_tokens"] != nil {
				t.Fatal(p)
			}
		})
	}
	for _, finished := range []bool{false, true} {
		o := usageObservation{}
		tail := ""
		if finished {
			tail = `,"finish_reason":"stop"`
		}
		o.observeWhole([]byte(`{"choices":[{"message":{"role":"assistant"}` + tail + `}]}`))
		if o.outputObserved != finished {
			t.Fatalf("whole observation: %+v", o)
		}
	}
}

func TestK2RefusalMustQualifyTheUsageOption(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"error":{"message":"stream_options is supported; unknown tool type"}}`, false},
		{`{"error":{"message":"unknown tool type and stream_options is supported"}}`, false},
		{`{"error":{"message":"Unrecognized request argument supplied: stream_options"}}`, true},
		{`{"error":{"message":"unknown field stream_options"}}`, true},
		{`{"error":{"message":"stream_options is not supported"}}`, true},
		{`{"error":{"message":"include_usage not supported"}}`, true},
		{`{"detail":[{"loc":["body","stream_options"],"msg":"Extra inputs are not permitted"}]}`, true},
		{`{"detail":[{"loc":["body","stream_options"],"msg":"supported"},{"loc":["body","tool"],"msg":"Extra inputs are not permitted"}]}`, false},
		{`{"error":{"message":"Extra inputs are not permitted: stream_options"}}`, true},
	} {
		if got := refusesStreamUsage([]byte(tc.body)); got != tc.want {
			t.Errorf("%s: %v", tc.body, got)
		}
	}
}

func TestK9NativeCacheAliasesKeepCanonicalBillingAndPrice(t *testing.T) {
	var bills []Billed
	for _, body := range []string{
		`{"input_tokens":7,"output_tokens":3,"cache_read_input_tokens":5,"cache_creation_input_tokens":2}`,
		`{"prompt_tokens":14,"completion_tokens":3,"cache_read_input_tokens":5,"cache_creation_input_tokens":2}`,
	} {
		var wire usageWire
		if err := json.Unmarshal([]byte(body), &wire); err != nil {
			t.Fatal(err)
		}
		c := &Client{}
		c.bill(WithBilling(context.Background(), func(b Billed) { bills = append(bills, b) }), "qwen3", &ai.Response{Usage: wire.usage()})
	}
	if len(bills) != 2 || !reflect.DeepEqual(bills[0], bills[1]) || bills[0].PromptTokens != 14 || bills[0].CachedTokens != 5 || bills[0].Cost != 0 {
		t.Fatal(bills)
	}
	// SpendGuard.after subtracts cached reads from the inclusive prompt count
	// before applying the regular prompt rate, and prices cache reads separately.
	price := func(b Billed) float64 {
		return float64(b.PromptTokens-b.CachedTokens)*2 + float64(b.CachedTokens)*0.5 + float64(b.CompletionTokens)*4
	}
	if price(bills[0]) != 32.5 || price(bills[0]) != price(bills[1]) {
		t.Fatal(bills)
	}
}

func TestK9UnmeteredDirectOverflowDoesNotLookUpWithVendorBearer(t *testing.T) {
	usageC3Session(t)
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method == http.MethodGet {
			t.Error("unmetered overflow sent a receipt lookup")
		}
		if r.Header.Get("Authorization") != "Bearer vendor-key" {
			t.Error("unexpected attempt bearer")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Host == "api.anthropic.com" {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"code":"1316","message":"plan window exhausted"}`)
			return
		}
		fmt.Fprint(w, `{"id":"overflow","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	})
	c, err := NewClient(Config{APIKey: "vendor-key", BaseURL: "https://api.anthropic.com/v1", Model: "qwen3", Direct: true, PlanOverflow: "https://openrouter.ai/api/v1", OverflowOnPlanPause: true, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithUnmeteredReceipts(WithReconcile(context.Background(), func(Reconciled) { t.Error("unmetered direct overflow reconciled") }))
	response, err := c.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil || response.Text() != "ok" || requests != 2 {
		t.Fatalf("response=%v err=%v requests=%d", response, err, requests)
	}
	rows := usageC3Events(t)
	if len(rows) != 2 || rows[1]["routing_provider"] != "openrouter" || rows[1]["usage_status"] != "estimated" {
		t.Fatal(rows)
	}
	// The pre-existing torn-stream route still uses the client's original key.
	req, err := c.newHTTPRequestAt(context.Background(), &ai.Request{APIKeyOverride: "overflow-key"}, []byte(`{}`), true, "https://openrouter.ai/api/v1")
	if err != nil || req.Header.Get("Authorization") != "Bearer overflow-key" {
		t.Fatal(req, err)
	}
}

func TestK10RecoveredTokenPresenceNeverFabricatesZeroes(t *testing.T) {
	for _, tc := range []struct {
		tokens, status string
		events         int
		in, out        any
	}{
		{``, "estimated", 1, nil, nil},
		{`,"tokens_prompt":10`, "partial", 2, float64(10), nil},
		{`,"tokens_completion":0`, "partial", 2, nil, float64(0)},
		{`,"tokens_prompt":0,"tokens_completion":0`, "reported", 2, float64(0), float64(0)},
	} {
		t.Run(tc.tokens, func(t *testing.T) {
			usageC3Session(t)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/generation") {
					fmt.Fprint(w, `{"data":{"total_cost":0.01`+tc.tokens+`}}`)
					return
				}
				fmt.Fprint(w, `{"id":"late","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
			})
			c, err := NewClient(Config{APIKey: "test", BaseURL: "https://openrouter.ai/api/v1", Model: "qwen3", HTTPClient: handlerClient(handler)})
			if err != nil {
				t.Fatal(err)
			}
			results := make(chan Reconciled, 1)
			ctx := WithUnmeteredReceipts(WithReconcile(context.Background(), func(r Reconciled) { results <- r }))
			if _, err := c.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
				t.Fatal(err)
			}
			answer := receiptResult(t, results)
			if !answer.Found || answer.Cost != 0.01 {
				t.Fatal(answer)
			}
			if tc.in != nil && answer.PromptTokens != int(tc.in.(float64)) {
				t.Fatal(answer)
			}
			if tc.out != nil && answer.CompletionTokens != int(tc.out.(float64)) {
				t.Fatal(answer)
			}
			rows := usageC3Events(t)
			if len(rows) != tc.events {
				t.Fatal(rows)
			}
			last := rows[len(rows)-1]
			if last["usage_status"] != tc.status || last["input_tokens"] != tc.in || last["output_tokens"] != tc.out {
				t.Fatal(last)
			}
			if tc.events == 2 && rows[0]["usage_receipt"] != last["usage_receipt"] {
				t.Fatal(rows)
			}
		})
	}
}

func TestK3SupportedUsageMentionDoesNotResendAnUnknownTool(t *testing.T) {
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"stream_options is supported; unknown tool type"}}`)
	})
	c, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "stub/model", Direct: true, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CompleteWithMessages(context.Background(), userMessages("hello")); err == nil {
		t.Fatal("expected the tool refusal")
	}
	if requests != 1 || !c.streamUsageSupported(callKnobs{}) {
		t.Fatalf("requests=%d option disabled=%v", requests, !c.streamUsageSupported(callKnobs{}))
	}
}

func TestK10RecoveredExplicitZeroesKeepTheExistingEmptyMoneyDoor(t *testing.T) {
	usageC3Session(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"total_cost":0,"tokens_prompt":0,"tokens_completion":0}}`)
	})
	c, err := NewClient(Config{APIKey: "test", BaseURL: "https://openrouter.ai/api/v1", Model: "qwen3", HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	c.wait = func(context.Context, time.Duration) error { return nil }
	result := make(chan Reconciled, 1)
	c.settle(WithReconcile(context.Background(), func(r Reconciled) { result <- r }), "qwen3", &ai.Response{ID: "zero-recovery"}, receiptTornReason, 1)
	if r := receiptResult(t, result); r.Found || r.Cost != 0 {
		t.Fatal("empty money behavior changed", r)
	}
	rows := usageC3Events(t)
	if len(rows) != 1 || rows[0]["usage_status"] != "reported" || rows[0]["input_tokens"] != float64(0) || rows[0]["output_tokens"] != float64(0) {
		t.Fatal(rows)
	}
}
