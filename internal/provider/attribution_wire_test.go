package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Proves the headers the chat stack puts on the wire.
func TestAttributionOnTheWire(t *testing.T) {
	seen := http.Header{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		APIKey:         "test-key",
		BaseURL:        srv.URL + "/v1",
		Model:          "openrouter/probe",
		SiteURL:        "https://agentfield.ai",
		SiteName:       "AgentField AI",
		SiteCategories: "cli-agent,programming-app",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := c.newHTTPRequest(context.Background(), &ai.Request{}, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	t.Logf("HTTP-Referer: %q", seen.Get("HTTP-Referer"))
	t.Logf("X-OpenRouter-Title: %q", seen.Get("X-OpenRouter-Title"))
	t.Logf("X-OpenRouter-Categories: %q", seen.Get("X-OpenRouter-Categories"))
	if seen.Get("HTTP-Referer") != "https://agentfield.ai" {
		t.Fatalf("referer missing: %v", seen)
	}
	if seen.Get("X-OpenRouter-Title") != "AgentField AI" {
		t.Fatalf("title missing: %v", seen)
	}
	if seen.Get("X-OpenRouter-Categories") != "cli-agent,programming-app" {
		t.Fatalf("categories missing: %v", seen)
	}
}
