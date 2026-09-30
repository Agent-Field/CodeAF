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

func TestResentOverflowLogsRefusalAndAnswer(t *testing.T) {
	read := loggingTo(t)
	forgetLanes(t)
	body, err := os.ReadFile("testdata/mara-overflow.json")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/endpoints") {
			_, _ = w.Write([]byte(`{"data":{"endpoints":[{"provider_name":"Mara","context_length":32768,"supported_parameters":[]},{"provider_name":"Wide","context_length":131072,"supported_parameters":["tools"]}]}}`))
			return
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"answered"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	model := "review/logged-overflow"
	client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	if err := lanes.Default().Sheet().Refresh(context.Background(), model); err != nil {
		t.Fatal(err)
	}
	ctx := WithContextBudget(context.Background(), ContextBudget{Window: 131072, Reserve: 8192, PromptFloor: 40000})
	if _, err := client.CompleteWithMessages(ctx, userMessages("read"), ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}})); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("HTTP calls = %d, want 2", calls.Load())
	}
	var refused, answered bool
	for _, row := range ended(read()) {
		refused = refused || row.Status == http.StatusBadRequest
		answered = answered || row.Status == http.StatusOK
	}
	if !refused || !answered {
		t.Fatalf("call log has refusal=%v answer=%v", refused, answered)
	}
}

func TestStrictPinLearnsSmallWindowWithoutResending(t *testing.T) {
	forgetLanes(t)
	body, err := os.ReadFile("testdata/mara-overflow.json")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/endpoints") {
			_, _ = w.Write([]byte(`{"data":{"endpoints":[{"provider_name":"Mara","context_length":32768,"supported_parameters":[]},{"provider_name":"Wide","context_length":131072,"supported_parameters":["tools"]}]}}`))
			return
		}
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	model := "review/pinned-overflow"
	client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	if err := lanes.Default().Sheet().Refresh(context.Background(), model); err != nil {
		t.Fatal(err)
	}
	ctx := WithLaneChoice(context.Background(), lanes.Choice{Only: []string{"Mara"}, Pinned: true})
	ctx = WithContextBudget(ctx, ContextBudget{Window: 131072, Reserve: 8192, PromptFloor: 40000})
	tool := ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}})
	_, _ = client.CompleteWithMessages(ctx, userMessages("read"), tool)
	if calls.Load() != 1 {
		t.Fatalf("strict pin paid %d identical refusals, want 1", calls.Load())
	}
	if got := client.servingWindow(model, &providerPrefs{Only: []string{"Mara"}}, 131072, true); got != 32768 {
		t.Fatalf("next pinned tool request sized against %d, want 32768", got)
	}
	_, _ = client.CompleteWithMessages(ctx, userMessages("read"), tool)
	if calls.Load() != 1 {
		t.Fatalf("second request went to HTTP despite learned local limit: calls=%d", calls.Load())
	}
}

func TestEqualContextLimitRefreshPersists(t *testing.T) {
	t.Cleanup(quirks.resetForTests)
	base, model, endpoint := "http://fixture.test", "review/limit", "Wide"
	key := contextLimitKey(base, model, endpoint)
	old := time.Now().Add(-servingFactHold / 2)
	path := filepath.Join(t.TempDir(), quirksFile)
	wire := quirksWire{ContextLimits: map[string]ContextLimit{key: {Base: base, Model: model, Provider: endpoint, Tokens: 32768, At: old}}}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	quirks.load(path)
	client, err := NewClient(Config{BaseURL: base, Model: model, Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	failure := &APIError{Overflow: true, ContextLimit: 32768, Provider: endpoint}
	client.rememberContextLimit(model, failure)
	if failure.BudgetChanged {
		t.Fatal("equal limit was marked as a new recovery budget")
	}
	quirks.settle()
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk quirksWire
	if err := json.Unmarshal(bytes, &onDisk); err != nil {
		t.Fatal(err)
	}
	if !onDisk.ContextLimits[key].At.After(old.Add(time.Minute)) {
		t.Fatalf("equal limit date on disk = %s", onDisk.ContextLimits[key].At)
	}
}

func TestFutureDatedContextLimitClampsOnLoadAndStore(t *testing.T) {
	t.Cleanup(quirks.resetForTests)
	base, model, endpoint := "http://fixture.test", "review/future-limit", "Wide"
	key := contextLimitKey(base, model, endpoint)
	future := time.Now().Add(24 * time.Hour)
	path := filepath.Join(t.TempDir(), quirksFile)
	wire := quirksWire{ContextLimits: map[string]ContextLimit{key: {Base: base, Model: model, Provider: endpoint, Tokens: 32768, At: future}}}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	quirks.load(path)
	if got := quirks.contextLimits[key].At; got.After(time.Now()) {
		t.Fatalf("loaded future date = %s", got)
	}
	quirks.settle()
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var repaired quirksWire
	if err := json.Unmarshal(bytes, &repaired); err != nil {
		t.Fatal(err)
	}
	if got := repaired.ContextLimits[key].At; got.After(time.Now()) {
		t.Fatalf("future date survived on disk = %s", got)
	}
	storeContextLimit(ContextLimit{Base: base, Model: model, Provider: endpoint, Tokens: 32768, At: future})
	if got := quirks.contextLimits[key].At; got.After(time.Now()) {
		t.Fatalf("stored future date = %s", got)
	}
}

func TestUnboundDefaultCeilingIsOmitted(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "http://budget.test", Model: "budget/default", Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		prompt  int
		wantCap bool
	}{{60000, false}, {100000, true}} {
		request := &ai.Request{Model: "budget/default", Messages: userMessages("read")}
		body, err := client.encodeRequest(request, callKnobs{contextBudget: ContextBudget{Window: 131072, Reserve: 65536, PromptFloor: tc.prompt}})
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatal(err)
		}
		_, capped := wire["max_tokens"]
		if capped != tc.wantCap {
			t.Fatalf("prompt %d max_tokens present=%v, want %v", tc.prompt, capped, tc.wantCap)
		}
	}
}
