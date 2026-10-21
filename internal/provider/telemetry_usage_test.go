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

func TestProviderUsageReceiptsDescribeRouteAndMissingUsage(t *testing.T) {
	for _, reported := range []bool{true, false} {
		t.Run(map[bool]string{true: "reported", false: "missing"}[reported], func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			telemetry.EnableForTest(t, true)
			telemetry.VersionForTest(t, "v0.7.0")
			finish := telemetry.BeginUsageSession(telemetry.ModeTask, "run")
			defer finish()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := `{"model":"anthropic/claude-sonnet-4","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]`
				if reported {
					body += `,"usage":{"prompt_tokens":4,"completion_tokens":1}`
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body + "}"))
			})
			client, err := NewClient(Config{APIKey: "test", BaseURL: "https://openrouter.ai/api/v1", Model: "anthropic/claude-sonnet-4", HTTPClient: handlerClient(handler), SupportsParameter: func(string, string) (bool, bool) { return true, true }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
				t.Fatal(err)
			}
			rows := telemetry.SpoolContents()
			if len(rows) != 1 {
				t.Fatalf("expected one receipt, got %d", len(rows))
			}
			var event struct {
				Props map[string]any `json:"props"`
			}
			if err := json.Unmarshal(rows[0], &event); err != nil {
				t.Fatal(err)
			}
			if event.Props["routing_provider"] != "openrouter" || event.Props["model_family"] != "claude" || event.Props["accounting_source"] != "provider" {
				t.Fatal(event.Props)
			}
			if reported && event.Props["total_tokens"] != float64(5) {
				t.Fatal(event.Props)
			}
			if !reported {
				if _, exists := event.Props["total_tokens"]; exists {
					t.Fatal("missing receipt invented a token total")
				}
			}
		})
	}
}

func TestRecoveredReceiptAddsOnlyPreviouslyMissingTokens(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeTask, "original")
	defer finish()
	results := make(chan Reconciled, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"total_cost":0.02,"tokens_prompt":10,"tokens_completion":3}}`))
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "https://openrouter.ai/api/v1", Model: "deepseek/deepseek-v4.1-flash", HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithReconcile(context.Background(), func(answer Reconciled) { results <- answer })
	// The real settle door queues recovery only for an absent usage block.
	client.settle(ctx, client.config.Model, &ai.Response{ID: "generation-one"}, receiptTornReason, 1)
	answer := receiptResult(t, results)
	if !answer.Found {
		t.Fatal("provider receipt was not recovered")
	}
	rows := telemetry.SpoolContents()
	if len(rows) != 1 {
		t.Fatalf("recovered receipt produced %d rows", len(rows))
	}
	var event struct {
		Props map[string]any `json:"props"`
	}
	if err := json.Unmarshal(rows[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.Props["total_tokens"] != float64(13) || event.Props["usage_status"] != "reported" {
		t.Fatal(event.Props)
	}
	// A stream that already supplied usage never enters recovery, including
	// a partial receipt, so it cannot add its usage a second time here.
	client.settle(ctx, client.config.Model, &ai.Response{ID: "generation-two", Usage: &ai.Usage{PromptTokens: 4, CompletionTokens: 1}}, receiptTornReason, 1)
	if rows := telemetry.SpoolContents(); len(rows) != 1 {
		t.Fatalf("reported usage entered late recovery: %d rows", len(rows))
	}
}

func TestOverflowUsageNamesTheActualAttemptRoute(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeTask, "overflow")
	defer finish()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Host == "api.anthropic.com" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"1316","message":"plan window exhausted"}`))
			return
		}
		if r.URL.Host != "openrouter.ai" {
			t.Errorf("unexpected host: %s", r.URL.Host)
		}
		_, _ = w.Write([]byte(`{"model":"anthropic/claude-sonnet-4","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":4,"completion_tokens":1}}`))
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "https://api.anthropic.com/v1", Model: "anthropic/claude-sonnet-4", Direct: true, BillingDoor: "coding plan", PlanOverflow: "https://openrouter.ai/api/v1", PlanOverflowDoor: "pay-as-you-go", OverflowOnPlanPause: true, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	rows := telemetry.SpoolContents()
	if len(rows) != 2 {
		t.Fatalf("expected refused and paid attempts, got %d", len(rows))
	}
	var paid struct {
		Props map[string]any `json:"props"`
	}
	if err := json.Unmarshal(rows[1], &paid); err != nil {
		t.Fatal(err)
	}
	if paid.Props["routing_provider"] != "openrouter" || paid.Props["total_tokens"] != float64(5) {
		t.Fatal(paid.Props)
	}
}

func TestLateReceiptFollowsTheOverflowRoute(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	telemetry.EnableForTest(t, true)
	telemetry.VersionForTest(t, "v0.7.0")
	finish := telemetry.BeginUsageSession(telemetry.ModeTask, "overflow-late")
	defer finish()
	results := make(chan Reconciled, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "openrouter.ai" || r.URL.Path != "/api/v1/generation" {
			t.Errorf("receipt followed wrong route: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"total_cost":0.02,"tokens_prompt":10,"tokens_completion":3}}`))
	})
	client, err := NewClient(Config{APIKey: "test", BaseURL: "https://api.anthropic.com/v1", Model: "anthropic/claude-sonnet-4", Direct: true, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithReconcile(context.Background(), func(answer Reconciled) { results <- answer })
	client.settle(ctx, client.config.Model, &ai.Response{ID: "generation-overflow"}, receiptTornReason, 1, &callTrace{attemptBase: "https://openrouter.ai/api/v1"})
	if result := receiptResult(t, results); !result.Found {
		t.Fatal("overflow receipt was not recovered")
	}
	rows := telemetry.SpoolContents()
	if len(rows) != 1 {
		t.Fatalf("expected one recovered receipt, got %d", len(rows))
	}
	var event struct {
		Props map[string]any `json:"props"`
	}
	if err := json.Unmarshal(rows[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.Props["routing_provider"] != "openrouter" || event.Props["total_tokens"] != float64(13) {
		t.Fatal(event.Props)
	}
}

// The fixture only sends a usage frame when the streaming request opts in,
// matching compatible local servers rather than gifting every caller counts.
func TestLocalStreamingUsageIsRequestedAndCounted(t *testing.T) {
	for _, source := range []string{"ollama", "custom"} {
		for _, reported := range []bool{true, false} {
			name := fmt.Sprintf("%s/reported=%t", source, reported)
			t.Run(name, func(t *testing.T) {
				t.Setenv(home.EnvVar, t.TempDir())
				telemetry.EnableForTest(t, true)
				telemetry.VersionForTest(t, "v0.7.0")
				finish := telemetry.BeginUsageSession(telemetry.ModeChat, "local")
				defer finish()
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request struct {
						Stream  bool `json:"stream"`
						Options struct {
							IncludeUsage bool `json:"include_usage"`
						} `json:"stream_options"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Fatal(err)
					}
					if !request.Stream || !request.Options.IncludeUsage {
						t.Error("stream did not request token usage")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`+"\n\n")
					fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
					if reported && request.Options.IncludeUsage {
						fmt.Fprint(w, `data: {"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":2,"total_tokens":42}}`+"\n\n")
					}
					fmt.Fprint(w, "data: [DONE]\n\n")
				})
				client, err := NewClient(Config{SourceID: source, BaseURL: "http://private-runner.example:11434/v1", Model: "qwen3", Direct: true, KeyOptional: true, HTTPClient: handlerClient(handler)})
				if err != nil {
					t.Fatal(err)
				}
				ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
				response, err := client.CompleteWithMessages(ctx, userMessages("hello"))
				if err != nil {
					t.Fatal(err)
				}
				if response.Text() != "ok" {
					t.Fatalf("response = %q", response.Text())
				}
				rows := telemetry.SpoolContents()
				if len(rows) != 1 {
					t.Fatalf("receipts = %d, want 1", len(rows))
				}
				var event struct {
					Props map[string]any `json:"props"`
				}
				if err := json.Unmarshal(rows[0], &event); err != nil {
					t.Fatal(err)
				}
				want := "other"
				if source == "ollama" {
					want = "ollama"
				}
				if event.Props["routing_provider"] != want {
					t.Fatal(event.Props)
				}
				if reported {
					if event.Props["total_tokens"] != float64(42) || event.Props["usage_status"] != "reported" {
						t.Fatal(event.Props)
					}
				} else {
					if _, exists := event.Props["total_tokens"]; exists {
						t.Fatal("missing usage invented tokens")
					}
					if event.Props["usage_status"] != "estimated" || event.Props["estimated_total_tokens"] == nil {
						t.Fatal(event.Props)
					}
				}
				if strings.Contains(string(rows[0]), "private-runner") {
					t.Fatal("private endpoint escaped")
				}
			})
		}
	}
}

func TestUsageProviderKeepsOllamaIdentityOnItsOwnRouteOnly(t *testing.T) {
	for _, tc := range []struct{ source, base, attempt, want string }{
		{"ollama", "http://localhost:11434/v1", "http://localhost:11434/v1", "ollama"},
		{"ollama", "http://[::1]:9000/v1", "http://[::1]:9000/v1", "ollama"},
		{"ollama", "http://runner.example/v1", "https://openrouter.ai/api/v1", "openrouter"},
		{"custom", "http://localhost:11434/v1", "http://localhost:11434/v1", "other"},
		{"private-service", "http://localhost:8080/v1", "http://localhost:8080/v1", "other"},
	} {
		client := &Client{config: Config{SourceID: tc.source, BaseURL: tc.base}}
		if got := client.usageRoutingProvider(tc.attempt); got != tc.want {
			t.Errorf("%+v: got %s", tc, got)
		}
	}
}

func TestNonStreamingRequestDoesNotSendStreamOptions(t *testing.T) {
	client, err := NewClient(Config{APIKey: "test", BaseURL: "http://localhost:9000/v1", Model: "qwen3", Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	body, err := client.encodeRequest(&ai.Request{Messages: userMessages("hello")}, callKnobs{})
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	if _, exists := request["stream_options"]; exists {
		t.Fatal("non-streamed call sent stream_options")
	}
}
