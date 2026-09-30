package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestToollessMarkExpiresAndFailedToolsOffRetryDoesNotLearn(t *testing.T) {
	var calls atomic.Int32
	var firstDone atomic.Bool
	var secondHadTools atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, tools := body["tools"]
		calls.Add(1)
		if firstDone.Load() {
			secondHadTools.Store(tools)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		} else if tools {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"does not support tools"}}`))
		} else {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"temporary request failure"}}`))
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: "review/unknown", Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	tool := ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}})
	_, _ = client.CompleteWithMessages(context.Background(), userMessages("first"), tool)
	firstDone.Store(true)
	_, err = client.CompleteWithMessages(context.Background(), userMessages("second"), tool)
	if err != nil || !secondHadTools.Load() {
		t.Fatalf("second request: error=%v tools=%v calls=%d", err, secondHadTools.Load(), calls.Load())
	}
	client.toolless.refused.Store(normalizeModel("review/unknown"), time.Now().Add(-time.Hour))
	if client.toolless.learned("review/unknown") {
		t.Fatal("expired tool-less mark remained active")
	}
}
