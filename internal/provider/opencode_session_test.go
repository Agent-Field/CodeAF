package provider

import (
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// OpenCode Go refuses a request with no x-opencode-session before it reads the
// bearer. The header must carry the conversation's cache lineage so routing and
// prompt caching stay stable, and a call with no lineage must still carry this
// client's own stable identity rather than an empty header.
func TestOpenCodeGoRequestsCarryTheCacheLineageInTheSessionHeader(t *testing.T) {
	client := &Client{
		config:    Config{SourceID: "opencode-go", BaseURL: "https://opencode.ai/inference/go/v1", Model: "glm-5.3"},
		apiKey:    "oc_sk_opencode-0123456789",
		sessionID: "codeaf-fallback",
	}
	request, err := client.newHTTPRequest(t.Context(), &ai.Request{}, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("x-opencode-session"); got != "codeaf-fallback" {
		t.Fatalf("fallback session header = %q", got)
	}

	ctx := WithCacheKey(t.Context(), "session-lineage")
	request, err = client.newHTTPRequest(ctx, &ai.Request{}, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("x-opencode-session"); got != "session-lineage" {
		t.Fatalf("session header = %q, want the cache lineage", got)
	}
}

func TestAnotherServiceDoesNotCarryTheOpenCodeSessionHeader(t *testing.T) {
	client := &Client{
		config:    Config{SourceID: "deepseek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4-pro"},
		apiKey:    "sk-deepseek-0123456789",
		sessionID: "codeaf-fallback",
	}
	request, err := client.newHTTPRequest(t.Context(), &ai.Request{}, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("x-opencode-session"); got != "" {
		t.Fatalf("another service carried %q", got)
	}
}
