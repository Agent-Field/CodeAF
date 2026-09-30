package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

func TestRelayedSmallToollessWindowGetsOneResendWithoutCappingTools(t *testing.T) {
	forgetLanes(t)
	model := "review/windows"
	mara, err := os.ReadFile("testdata/mara-overflow.json")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/endpoints") {
			_, _ = w.Write([]byte(`{"data":{"endpoints":[{"provider_name":"Mara","context_length":32768,"max_completion_tokens":8192,"supported_parameters":[]},{"provider_name":"Wide","context_length":131072,"max_completion_tokens":8192,"supported_parameters":["tools"]}]}}`))
			return
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(mara)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"answered"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	if err := lanes.Default().Sheet().Refresh(context.Background(), model); err != nil {
		t.Fatal(err)
	}
	ctx := WithContextBudget(context.Background(), ContextBudget{Window: 131072, Reserve: 8192, PromptFloor: 40000})
	tools := ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}})
	for i := 0; i < 2; i++ {
		answer, err := client.CompleteWithMessages(ctx, userMessages("read"), tools)
		if err != nil || answer == nil || answer.Text() != "answered" {
			t.Fatalf("tool request %d: answer=%v error=%v calls=%d", i+1, answer, err, calls.Load())
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("tool requests made %d HTTP calls, want the first retried once and the next sent once", got)
	}
	if got := client.servingWindow(model, nil, 131072, true); got != 131072 {
		t.Fatalf("tool request inherited Mara's learned window: %d", got)
	}
	_, err = client.CompleteWithMessages(ctx, userMessages("plain"))
	failure, ok := RefusalFrom(err)
	if !ok || !failure.Local || !failure.Overflow || calls.Load() != 3 {
		t.Fatalf("no-tools request: error=%v calls=%d; want local small-window refusal", err, calls.Load())
	}
}

func TestRelayedSmallWindowStopsAfterOneResend(t *testing.T) {
	mara, err := os.ReadFile("testdata/mara-overflow.json")
	if err != nil {
		t.Fatal(err)
	}
	// A router can answer a resend from another endpoint, so it gets exactly
	// one; a direct base is the one endpoint, so resending it the same request
	// would only pay the refusal twice.
	for _, road := range []struct {
		name   string
		direct bool
		want   int32
	}{{"router", false, 2}, {"direct", true, 1}} {
		t.Run(road.name, func(t *testing.T) {
			forgetLanes(t)
			model := "review/repeated-" + road.name
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/endpoints") {
					_, _ = w.Write([]byte(`{"data":{"endpoints":[{"provider_name":"Mara","context_length":32768,"supported_parameters":[]},{"provider_name":"Wide","context_length":131072,"supported_parameters":["tools"]}]}}`))
					return
				}
				calls.Add(1)
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write(mara)
			}))
			defer server.Close()
			client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: model, Direct: road.direct})
			if err != nil {
				t.Fatal(err)
			}
			if !road.direct {
				if err := lanes.Default().Sheet().Refresh(context.Background(), model); err != nil {
					t.Fatal(err)
				}
			}
			ctx := WithContextBudget(context.Background(), ContextBudget{Window: 131072, Reserve: 8192, PromptFloor: 40000})
			_, err = client.CompleteWithMessages(ctx, userMessages("read"), ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}}))
			failure, ok := RefusalFrom(err)
			if !ok || !failure.Overflow || calls.Load() != road.want {
				t.Fatalf("overflow: error=%v requests=%d, want the overflow after %d request(s)", err, calls.Load(), road.want)
			}
		})
	}
}

func TestContextLimitExpiresOnLoadAndFreshToolLimitStillApplies(t *testing.T) {
	forgetLanes(t)
	t.Cleanup(quirks.resetForTests)
	model := "review/ttl"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/endpoints") {
			_, _ = w.Write([]byte(`{"data":{"endpoints":[{"provider_name":"Wide","context_length":131072,"supported_parameters":["tools"]}]}}`))
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	if err := lanes.Default().Sheet().Refresh(context.Background(), model); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), quirksFile)
	key := contextLimitKey(server.URL, model, "Wide")
	encoded, _ := json.Marshal(quirksWire{ContextLimits: map[string]ContextLimit{
		key: {Base: server.URL, Model: model, Provider: "Wide", Tokens: 32768},
	}})
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	quirks.load(path)
	if got := client.servingWindow(model, nil, 131072, true); got != 131072 {
		t.Fatalf("undated persisted limit survived load: %d", got)
	}
	storeContextLimit(ContextLimit{Base: server.URL, Model: model, Provider: "Wide", Tokens: 65536})
	if got := client.servingWindow(model, nil, 131072, true); got != 65536 {
		t.Fatalf("fresh tool endpoint limit = %d, want 65536", got)
	}
	storeContextLimit(ContextLimit{Base: server.URL, Model: model, Provider: "Unlisted", Tokens: 49152})
	if got := client.servingWindow(model, nil, 131072, true); got != 49152 {
		t.Fatalf("unknown endpoint limit = %d, want conservative 49152", got)
	}
	quirks.forget()
	stale, err := json.Marshal(quirksWire{ContextLimits: map[string]ContextLimit{
		key: {Base: server.URL, Model: model, Provider: "Wide", Tokens: 32768, At: time.Now().Add(-time.Hour)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	quirks.load(path)
	if got := client.servingWindow(model, nil, 131072, true); got != 131072 {
		t.Fatalf("expired dated limit = %d, want catalog window", got)
	}
}
