package provider

import (
	"net/http"
	"testing"
)

func TestC13CodexDirectRequestGetsProductIdentityWithoutRouterAttribution(t *testing.T) {
	// C13: the direct Codex backend sees codeaf's user agent and no OpenRouter attribution.
	client := &Client{config: Config{BaseURL: "https://chatgpt.com/backend-api/codex", Model: "gpt-5.5", Direct: true}}
	request, _ := http.NewRequest(http.MethodPost, client.config.BaseURL, nil)
	client.applyRequestIdentity(request)
	if request.Header.Get("User-Agent") != DirectUserAgent {
		t.Fatalf("user agent = %q", request.Header.Get("User-Agent"))
	}
	if request.Header.Get("HTTP-Referer") != "" || request.Header.Get("X-Title") != "" {
		t.Fatalf("router attribution = %v", request.Header)
	}
}
