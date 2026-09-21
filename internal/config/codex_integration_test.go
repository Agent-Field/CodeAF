package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestC13C14RealConfigChainAndProviderClientReachCodexTransport(t *testing.T) {
	// C13: the real ResolveSources → ClientConfigFor → provider.Client road reaches /responses.
	// C14: the real provider decoder receives answer and usage while Codex price stays unknown.
	var seen map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			t.Fatalf("provider reached %q, want /responses", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer real-chain-access" || request.Header.Get("Authorization") == "Bearer "+codexauth.Sentinel {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("HTTP-Referer") != "" || request.Header.Get("X-Title") != "" {
			t.Fatalf("OpenRouter attribution escaped: %v", request.Header)
		}
		if err := json.NewDecoder(request.Body).Decode(&seen); err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(writer, `data: {"type":"response.created","response":{"id":"real-1","model":"gpt-5.5","created_at":1800000000}}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.output_text.delta","delta":"real answer"}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":2,"input_tokens_details":{"cached_tokens":1},"output_tokens_details":{"reasoning_tokens":1}}}}`)
		fmt.Fprintln(writer)
	}))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	dir := t.TempDir()
	tokens := codexauth.Tokens{AccessToken: "real-chain-access", RefreshToken: "real-chain-refresh", IDToken: "real-chain-identity", AccountID: "acct-real", ExpiresAt: time.Now().Add(time.Hour)}
	if err := codexauth.Save(dir, tokens); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := WriteSources(dir, []PersistedSource{{ID: "codex", Written: "codex", Key: codexauth.Sentinel, Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	configured := ClientConfigFor(ResolveSources(dir, "", DefaultBaseURL), "codex/gpt-5.5")
	if configured.HTTPClient == nil || !configured.Direct || configured.Model != "gpt-5.5" || configured.APIKey != codexauth.Sentinel || configured.BillingDoor != "" {
		t.Fatalf("configured client = %+v", configured)
	}
	if _, _, known := configured.ModelPrice("gpt-5.5"); known {
		t.Fatal("codex price is known")
	}
	client, err := provider.NewClient(configured)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.CompleteWithMessages(context.Background(), []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello through the real chain"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Choices) == 0 || len(response.Choices[0].Message.Content) == 0 || response.Choices[0].Message.Content[0].Text != "real answer" {
		t.Fatalf("provider response = %+v", response)
	}
	encoded, _ := json.Marshal(seen)
	if !strings.Contains(string(encoded), "hello through the real chain") || !strings.Contains(string(encoded), `"stream":true`) {
		t.Fatalf("responses request = %s", encoded)
	}
}

func TestC12ConnectCodexFallsBackAndResolvedModelsRemainQualified(t *testing.T) {
	// C12: an unreachable live list persists the connection and returns the four bare fallback ids for qualification by the service.
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusBadGateway) }))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	dir := t.TempDir()
	tokens := codexauth.Tokens{AccessToken: "fallback-access", RefreshToken: "fallback-refresh", IDToken: "fallback-identity", AccountID: "acct", ExpiresAt: time.Now().Add(time.Hour)}
	outcome, err := ConnectCodex(context.Background(), dir, tokens)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Listed || outcome.Refreshed || len(outcome.ModelIDs) != 4 {
		t.Fatalf("fallback outcome = %+v", outcome)
	}
	service, ok := ResolveSources(dir, "", DefaultBaseURL).ByID("codex")
	if !ok || service.Qualify(outcome.ModelIDs[0]) != "codex/gpt-5.5" {
		t.Fatalf("qualified fallback = %+v, %v", service, outcome.ModelIDs)
	}
}

func TestC18CodexSentinelIsNeverAUsableBearerOutsideItsTransport(t *testing.T) {
	// C18: config exposes only the sentinel while its attached transport owns every real token.
	dir := t.TempDir()
	if err := codexauth.Save(dir, codexauth.Tokens{AccessToken: "private-access", RefreshToken: "private-refresh", IDToken: "private-identity"}); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := WriteSources(dir, []PersistedSource{{ID: "codex", Written: "codex", Key: codexauth.Sentinel, Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	configured := ClientConfigFor(ResolveSources(dir, "", DefaultBaseURL), "codex/gpt-5.5")
	if configured.APIKey != codexauth.Sentinel || strings.Contains(fmt.Sprintf("%v", configured), "private-access") {
		t.Fatalf("credential escaped config: %+v", configured)
	}
}
