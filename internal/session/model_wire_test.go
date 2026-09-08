package session

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestSetModelChangesTheWireModel is the full-stack contract: session.New →
// provider client → HTTP body. The user observed /model change the status
// line while OpenRouter kept logging the old model; this test stands up a
// real HTTP server and reads the "model" field out of the bodies it receives,
// so a regression anywhere in the chain — latch, WithModel, newRequest,
// modelFor, encode — fails here and not in somebody's dashboard.
func TestSetModelChangesTheWireModel(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(raw, &body); err == nil {
			mu.Lock()
			bodies = append(bodies, body)
			mu.Unlock()
		}
		// Minimal OpenAI-shaped SSE: one delta, then DONE.
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	agent, err := New(Config{
		Workspace: t.TempDir(),
		Model:     "vendor/model-a",
		APIKey:    "test",
		BaseURL:   server.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer agent.Close()

	drain := func(text string) {
		t.Helper()
		ctx, cancel := deadline(5 * time.Second)
		defer cancel()
		events, err := agent.Submit(ctx, text)
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		for event := range events {
			if event.Kind == EventError {
				t.Fatalf("turn errored: %v", event.Err)
			}
		}
	}

	drain("first")
	agent.SetModel("vendor/model-b")
	drain("second")

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(bodies))
	}
	model := func(i int) string {
		var s string
		if err := json.Unmarshal(bodies[i]["model"], &s); err != nil {
			t.Fatalf("body %d model: %v", i, err)
		}
		return s
	}
	if got := model(0); got != "vendor/model-a" {
		t.Fatalf("first request model = %q, want vendor/model-a", got)
	}
	if got := model(1); got != "vendor/model-b" {
		t.Fatalf("second request model = %q, want vendor/model-b — the status line moved but the wire did not", got)
	}
}

func deadline(d time.Duration) (ctx context.Context, cancel context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
