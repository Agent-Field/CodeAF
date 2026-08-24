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
