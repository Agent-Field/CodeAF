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

func TestK2StreamUsageOptInFollowsTheActualRoad(t *testing.T) {
	for _, tc := range []struct {
		name, base   string
		direct, want bool
	}{
		{"router", "", false, false},
		{"connected", "", true, true},
		{"connected overflow to router", "https://openrouter.ai/api/v1", true, false},
		{"router overflow to connected", "https://vendor.example/v1", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "stub/model", Direct: tc.direct})
			if err != nil {
				t.Fatal(err)
			}
			knobs := knobsFrom(context.Background())
			knobs.trace.attemptBase = tc.base
			request, err := client.newRequest(userMessages("hello"), []ai.Option{ai.WithStream()})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := client.encodeRequest(request, knobs)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &body); err != nil {
				t.Fatal(err)
			}
			if got := string(body["stream_options"]); (got != "") != tc.want || tc.want && got != `{"include_usage":true}` {
				t.Fatalf("stream_options=%s, want present=%t", got, tc.want)
			}
			if !tc.direct && tc.base == "" && string(body["usage"]) != `{"include":true}` {
				t.Fatalf("router usage=%s", body["usage"])
			}
		})
	}
}

func TestK2StreamUsageRefusalIsRememberedOn400And422(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			requested, accepted := 0, 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["stream_options"] != nil {
					requested++
					w.WriteHeader(status)
					fmt.Fprint(w, `{"detail":[{"loc":["body","stream_options"],"msg":"Extra inputs are not permitted"}]}`)
					return
				}
				accepted++
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			})
			client, err := NewClient(Config{APIKey: "test", BaseURL: "http://legacy/v1", Model: "stub/model", Direct: true, HTTPClient: handlerClient(handler)})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
					t.Fatal(err)
				}
			}
			if requested != 1 || accepted != 2 {
				t.Fatalf("requests with option=%d, accepted=%d; want 1, 2", requested, accepted)
			}
		})
	}
}

func TestK3UnrelatedDirectRefusalKeepsTheDevErrorAndRequestCount(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			const payload = `{"error":{"message":"invalid tool schema sentinel"}}`
			requests := 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["stream_options"] == nil {
					t.Fatal("unrelated refusal disabled stream usage")
				}
				w.WriteHeader(status)
				fmt.Fprint(w, payload)
			})
			client, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "stub/model", Direct: true, HTTPClient: handlerClient(handler)})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				_, err := client.CompleteWithMessages(context.Background(), userMessages("hello"))
				if err == nil || err.Error() != apiError(status, []byte(payload)).Error() {
					t.Fatalf("error=%v, want %v", err, apiError(status, []byte(payload)))
				}
			}
			if requests != 2 {
				t.Fatalf("requests=%d, want one per call as on dev", requests)
			}
		})
	}
}

func TestK11RawStreamEndsAndBanksExactlyOnce(t *testing.T) {
	for _, mode := range []string{"completed", "cut measured", "cut missing", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			telemetry.EnableForTest(t, true)
			telemetry.VersionForTest(t, "v0.7.0")
			finish := telemetry.BeginUsageSession(telemetry.ModeChat, mode)
			defer finish()
			read := loggingTo(t)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				usage := `,"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13,"cost":0.01}`
				if mode == "cut missing" {
					usage = ""
				}
				fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]%s}\n\n", usage)
				if mode == "completed" {
					fmt.Fprint(w, "data: [DONE]\n\n")
				}
			})
			client, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "stub/model", Direct: true, HTTPClient: handlerClient(handler)})
			if err != nil {
				t.Fatal(err)
			}
			bills, receipts := 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx = WithBilling(ctx, func(b Billed) {
				bills++
				if b.PromptTokens != 10 || b.CompletionTokens != 3 || b.Cost != 0.01 {
					t.Errorf("bill=%+v", b)
				}
			})
			ctx = WithReconcile(ctx, func(Reconciled) { receipts++ })
			before := telemetry.Snapshot()
			chunks, errs := client.StreamComplete(ctx, "hello")
			for range chunks {
				if mode == "cancelled" {
					cancel()
				}
			}
			var failures []error
			for err := range errs {
				if err != nil {
					failures = append(failures, err)
				}
			}
			if mode == "completed" && len(failures) != 0 || mode != "completed" && len(failures) != 1 {
				t.Fatalf("errors=%v", failures)
			}
			for _, err := range failures {
				if strings.Contains(err.Error(), "panic") {
					t.Fatal(err)
				}
			}
			wantBills := 1
			if mode == "cut missing" {
				wantBills = 0
			}
			if bills != wantBills || receipts != 0 {
				t.Fatalf("bills=%d receipts=%d", bills, receipts)
			}
			if calls := telemetry.Snapshot().ModelCalls - before.ModelCalls; calls != 1 {
				t.Fatalf("model calls=%d, want one ending", calls)
			}
			rows := read()
			ends := ended(rows)
			if len(rows) != 2 || len(ends) != 1 || rows[0].ID != ends[0].ID {
				t.Fatalf("rows=%+v", rows)
			}
			if rows := telemetry.SpoolContents(); len(rows) != 1 {
				t.Fatalf("usage events=%d, want one ending", len(rows))
			}
		})
	}
}

func TestK9MissingUsageEstimatesDoNotEnterBillingOrCallLogCounts(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeTask, "estimate-only")
	defer finish()
	read := loggingTo(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"observed answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "stub/model", Direct: true, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	bills, receipts := 0, 0
	ctx := WithBilling(context.Background(), func(Billed) { bills++ })
	ctx = WithReconcile(WithUnmeteredReceipts(ctx), func(Reconciled) { receipts++ })
	response, err := client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if response.Usage != nil || bills != 0 || receipts != 0 {
		t.Fatalf("response usage=%+v bills=%d receipts=%d", response.Usage, bills, receipts)
	}
	rows := ended(read())
	if len(rows) != 1 || rows[0].PromptTokens != 0 || rows[0].CompletionTokens != 0 || rows[0].Cost != 0 {
		t.Fatalf("call log=%+v", rows)
	}
	var event struct {
		Props map[string]any `json:"props"`
	}
	spool := telemetry.SpoolContents()
	if len(spool) != 1 {
		t.Fatalf("events=%d", len(spool))
	}
	if err := json.Unmarshal(spool[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.Props["usage_status"] != "estimated" || event.Props["estimated_total_tokens"] == nil || event.Props["total_tokens"] != nil {
		t.Fatalf("usage=%+v", event.Props)
	}
}

func TestK7AnUnobservedOutputComponentStaysPartial(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeChat, "partial")
	defer finish()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10}}\n\ndata: [DONE]\n\n")
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "stub/model", Direct: true, HTTPClient: handlerClient(handler)})
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
	rows := telemetry.SpoolContents()
	if len(rows) != 1 {
		t.Fatalf("events=%d", len(rows))
	}
	var event struct {
		Props map[string]any `json:"props"`
	}
	if err := json.Unmarshal(rows[0], &event); err != nil {
		t.Fatal(err)
	}
	p := event.Props
	if p["usage_status"] != "partial" || p["input_tokens"] != float64(10) || p["total_tokens"] != float64(10) || p["output_tokens"] != nil || p["estimated_total_tokens"] != nil {
		t.Fatalf("usage=%+v", p)
	}
}

func TestK11RawCutRecoversOneLateReceipt(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeChat, "raw-recovery")
	defer finish()
	read := loggingTo(t)
	requests, lookups := 0, 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/generation") {
			lookups++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"tokens_prompt":10,"tokens_completion":3,"total_cost":0.01}}`)
			return
		}
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"raw-recovery-private\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n")
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "https://openrouter.ai/api/v1", Model: "stub/model", HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan Reconciled, 2)
	bills := 0
	ctx := WithReconcile(WithBilling(context.Background(), func(Billed) { bills++ }), func(r Reconciled) { results <- r })
	before := telemetry.Snapshot()
	chunks, errs := client.StreamComplete(ctx, "hello")
	for range chunks {
	}
	failures := 0
	for err := range errs {
		if err != nil {
			failures++
		}
	}
	result := receiptResult(t, results)
	if !result.Found || result.PromptTokens != 10 || result.CompletionTokens != 3 || result.Cost != 0.01 || failures != 1 || bills != 0 || requests != 1 || lookups != 1 {
		t.Fatalf("result=%+v failures=%d bills=%d requests=%d lookups=%d", result, failures, bills, requests, lookups)
	}
	select {
	case extra := <-results:
		t.Fatalf("duplicate settlement: %+v", extra)
	default:
	}
	if rows := read(); len(rows) != 2 || len(ended(rows)) != 1 {
		t.Fatalf("call log=%+v", rows)
	}
	if calls := telemetry.Snapshot().ModelCalls - before.ModelCalls; calls != 1 {
		t.Fatalf("model calls=%d", calls)
	}
	rows := telemetry.SpoolContents()
	if len(rows) != 2 {
		t.Fatalf("usage events=%d", len(rows))
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
	if first.Props["usage_status"] != "estimated" || first.Props["estimate_scope"] != "observed_partial" || second.Props["usage_status"] != "reported" || first.Props["usage_receipt"] != second.Props["usage_receipt"] || first.ID == second.ID {
		t.Fatalf("receipts=%+v %+v", first, second)
	}
}

func TestK10RepeatedProviderGenerationIDsKeepDistinctAnswerReceipts(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeChat, "reused-provider-id")
	defer finish()
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"reused-id\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d}}\n\ndata: [DONE]\n\n", calls+9, calls+2)
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "http://local/v1", Model: "stub/model", Direct: true, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
			t.Fatal(err)
		}
	}
	rows := telemetry.SpoolContents()
	if len(rows) != 2 {
		t.Fatalf("events=%d", len(rows))
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
	if first.Props["usage_status"] != "reported" || second.Props["usage_status"] != "reported" || first.Props["input_tokens"] != float64(10) || second.Props["input_tokens"] != float64(11) || first.Props["usage_receipt"] == second.Props["usage_receipt"] || first.ID == second.ID {
		t.Fatalf("receipts=%+v %+v", first, second)
	}
}
