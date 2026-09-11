package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A client may exist before its key does — the first-run setup's case — and it
// must send NOTHING until the key lands, then send with it.
func TestAKeylessClientRefusesUntilItIsHandedAKey(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Model: "test/model", Timeout: time.Second})
	if err != nil {
		t.Fatalf("a keyless client must build: %v", err)
	}
	_, err = client.CompleteWithMessages(context.Background(), []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hi"}}}})
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("a request with no key must refuse with ErrNoAPIKey, got %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("nothing may reach the wire without a key; %d requests did", len(seen))
	}

	if err := client.SetAPIKey("sk-or-v1-later"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hi"}}}}); err != nil {
		t.Fatalf("after the key landed the request must go: %v", err)
	}
	if len(seen) != 1 || seen[0] != "Bearer sk-or-v1-later" {
		t.Fatalf("the request must carry the key it was handed, got %v", seen)
	}
}

func TestAServiceThatExplicitlyNeedsNoKeyReachesTheWireWithoutOne(t *testing.T) {
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"local/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Model: "local/model", Timeout: time.Second, KeyOptional: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hi"}}}}); err != nil {
		t.Fatalf("key-optional request: %v", err)
	}
	if _, _, err := client.ExecuteToolCallLoop(context.Background(), userMessages("use a tool"), nil,
		ai.ToolCallConfig{MaxTurns: 2, MaxToolCalls: 2},
		func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
			t.Fatal("the fixture returned no tool call")
			return nil, nil
		}); err != nil {
		t.Fatalf("key-optional tool loop: %v", err)
	}
	if len(authorizations) != 2 {
		t.Fatalf("key-optional paths sent %d requests, want 2", len(authorizations))
	}
	for _, authorization := range authorizations {
		if authorization != "" {
			t.Fatalf("key-optional request sent Authorization %q", authorization)
		}
	}
}
