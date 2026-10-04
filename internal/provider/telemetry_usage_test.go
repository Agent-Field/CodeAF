package provider

import (
	"context"
	"encoding/json"
	"net/http"
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
