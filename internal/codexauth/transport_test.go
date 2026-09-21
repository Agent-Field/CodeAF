package codexauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/paymentrefusal"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
)

func validTokens(now time.Time) Tokens {
	return Tokens{AccessToken: "access-token-secret", RefreshToken: "refresh-token-secret", IDToken: "identity-token-secret", AccountID: "account-one", ExpiresAt: now.Add(time.Hour)}
}

func TestC12ListingUsesAccountHeadersFiltersVisibilityAndCachesLevels(t *testing.T) {
	// C12: the account listing, not generic /models, supplies only visible Codex models.
	now := time.Unix(1_800_000_000, 0)
	dir := t.TempDir()
	if err := Save(dir, validTokens(now)); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/models" || request.URL.Query().Get("client_version") != clientVersion {
			t.Fatalf("listing request = %s", request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer access-token-secret" || request.Header.Get("chatgpt-account-id") != "account-one" || request.Header.Get("originator") != Originator {
			t.Fatalf("listing headers = %v", request.Header)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"models": []any{
			map[string]any{"slug": "shown", "visibility": "list", "supported_reasoning_levels": []any{map[string]any{"effort": "low"}, map[string]any{"effort": "high"}}},
			map[string]any{"slug": "hidden", "visibility": "hide"},
		}})
	}))
	defer backend.Close()
	models, err := List(context.Background(), dir, Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "shown" || strings.Join(models[0].ReasoningLevels, ",") != "low,high" {
		t.Fatalf("models = %+v", models)
	}
	if got := clampEffort(dir, "shown", "medium"); got != "low" {
		t.Fatalf("clamped effort = %q", got)
	}
}

func TestC13C14C15TransportMapsTurnHeadersStreamAndReasoningRoundTrip(t *testing.T) {
	// C13: a Codex turn carries account headers, one session id and no router attribution.
	// C14: text, reasoning, tool calls, finish reason and all token counts map to chat chunks.
	// C15: encrypted reasoning and a tool result return as Responses input items.
	now := time.Unix(1_800_000_000, 0)
	dir := t.TempDir()
	if err := Save(dir, validTokens(now)); err != nil {
		t.Fatal(err)
	}
	var requests []map[string]any
	var headers []http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			t.Fatalf("turn path = %q", request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, body)
		headers = append(headers, request.Header.Clone())
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(writer, `data: {"type":"response.created","response":{"id":"resp-1","model":"gpt-5.5","created_at":1800000000}}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.reasoning_summary_text.delta","delta":"thinking"}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.output_text.delta","delta":"answer"}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call-1","name":"read"}}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\\"path\\":\\"a\\"}"}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.output_item.done","item":{"type":"reasoning","id":"rs-1","encrypted_content":"ciphertext"}}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.completed","response":{"id":"resp-1","model":"gpt-5.5","usage":{"input_tokens":11,"output_tokens":7,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}}`)
		fmt.Fprintln(writer)
	}))
	defer backend.Close()
	client := ClientWithOptions(dir, Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }, SessionID: "conversation-one"})
	first := map[string]any{
		"model": "gpt-5.5", "stream": true,
		"messages": []any{map[string]any{"role": "system", "content": "system one"}, map[string]any{"role": "user", "content": "hello"}},
		"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "read", "description": "read a file", "parameters": map[string]any{"type": "object"}}}},
	}
	responseBody := doTurn(t, client, backend.URL+"/chat/completions", first)
	for _, want := range []string{`"reasoning":"thinking"`, `"content":"answer"`, `"tool_calls"`, `"reasoning_details"`, `"finish_reason":"tool_calls"`, `"prompt_tokens":11`, `"cached_tokens":3`, `"reasoning_tokens":2`} {
		if !strings.Contains(responseBody, want) {
			t.Errorf("translated stream missing %s: %s", want, responseBody)
		}
	}
	second := map[string]any{
		"model": "gpt-5.5", "stream": true,
		"messages": []any{
			map[string]any{"role": "assistant", "content": "answer", "reasoning_details": []any{map[string]any{"type": "reasoning.encrypted", "format": "openai-responses-v1", "id": "rs-1", "data": "ciphertext"}, "foreign"}, "tool_calls": []any{map[string]any{"id": "call-1", "function": map[string]any{"name": "read", "arguments": "{}"}}}},
			map[string]any{"role": "tool", "tool_call_id": "call-1", "content": "file words"},
		},
	}
	_ = doTurn(t, client, backend.URL+"/chat/completions", second)
	if len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
	encoded, _ := json.Marshal(requests[1]["input"])
	for _, want := range []string{`"type":"reasoning"`, `"encrypted_content":"ciphertext"`, `"type":"function_call"`, `"type":"function_call_output"`, `"output":"file words"`} {
		if !bytes.Contains(encoded, []byte(want)) {
			t.Errorf("round trip missing %s: %s", want, encoded)
		}
	}
	for _, header := range headers {
		if header.Get("Authorization") != "Bearer access-token-secret" || header.Get("chatgpt-account-id") != "account-one" || header.Get("originator") != Originator || header.Get("OpenAI-Beta") != "responses=experimental" || header.Get("session_id") != "conversation-one" || header.Get("User-Agent") != provider.DirectUserAgent {
			t.Errorf("turn headers = %v", header)
		}
		if header.Get("HTTP-Referer") != "" || header.Get("X-Title") != "" || header.Get("Authorization") == "Bearer "+Sentinel {
			t.Errorf("router or sentinel header escaped: %v", header)
		}
	}
}

func doTurn(t *testing.T, client *http.Client, endpoint string, body map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(body)
	request, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("HTTP-Referer", "must-go")
	request.Header.Set("X-Title", "must-go")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	translated, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(translated)
}

func TestC16RefreshesBeforeExpirySingleFlightAndRetriesOneUnauthorizedCall(t *testing.T) {
	// C16: near-expiry refresh is single-flight and a 401 forces exactly one refresh and retry.
	now := time.Unix(1_800_000_000, 0)
	dir := t.TempDir()
	tokens := validTokens(now)
	tokens.ExpiresAt = now.Add(time.Minute)
	if err := Save(dir, tokens); err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int32
	var calls atomic.Int32
	access := jwt(t, map[string]any{"exp": now.Add(time.Hour).Unix()})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == tokenPath {
			refreshes.Add(1)
			_ = json.NewEncoder(writer).Encode(map[string]string{"access_token": access, "refresh_token": "rotated-refresh", "id_token": ""})
			return
		}
		if calls.Add(1) == 1 && request.Header.Get("Authorization") == "Bearer "+access {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"models": []any{map[string]any{"slug": "gpt-5.5", "visibility": "list"}}})
	}))
	defer server.Close()
	options := Options{Issuer: server.URL, Backend: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }}
	var group sync.WaitGroup
	errorsFound := make(chan error, 20)
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := List(context.Background(), dir, options)
			errorsFound <- err
		}()
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if refreshes.Load() != 2 {
		t.Fatalf("refreshes = %d, want expiry plus one 401 refresh", refreshes.Load())
	}
}

func TestC16RefusedRefreshReturnsTheSignInSentenceAndMarksTokensUnusable(t *testing.T) {
	// C16: a refused rotating token stops without a retry storm and requires sign-in again.
	now := time.Unix(1_800_000_000, 0)
	dir := t.TempDir()
	tokens := validTokens(now)
	tokens.ExpiresAt = now
	if err := Save(dir, tokens); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	_, err := List(context.Background(), dir, Options{Issuer: server.URL, Backend: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if !errors.Is(err, ErrSignInExpired) {
		t.Fatalf("refresh error = %v", err)
	}
	if Connected(dir) {
		t.Fatal("refused refresh still reads connected")
	}
}

func TestC17QuotaResponseNamesCodexAndTheAutomaticReset(t *testing.T) {
	// C17: a backend quota envelope becomes the one plain Codex plan-limit sentence.
	now := time.Unix(1_800_000_000, 0)
	dir := t.TempDir()
	if err := Save(dir, validTokens(now)); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, `{"error":{"code":"usage_limit_reached"}}`)
	}))
	defer backend.Close()
	client := ClientWithOptions(dir, Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }})
	raw, _ := json.Marshal(map[string]any{"model": "gpt-5.5", "stream": true, "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	request, _ := http.NewRequest(http.MethodPost, backend.URL+"/chat/completions", bytes.NewReader(raw))
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	// The status is the one every service's payment refusal already carries,
	// so the session ends the turn without any vendor-wide word list.
	if response.StatusCode != http.StatusPaymentRequired || !strings.Contains(string(body), QuotaWords) {
		t.Fatalf("quota response = %d %s", response.StatusCode, body)
	}
	if !paymentrefusal.Matches(response.StatusCode, body) {
		t.Fatalf("the rewritten quota response is not read as a payment refusal: %d %s", response.StatusCode, body)
	}
}
