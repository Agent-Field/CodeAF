package codexauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/paymentrefusal"
	"github.com/Agent-Field/codeaf/internal/provider"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func validTokens(now time.Time) Tokens {
	return Tokens{AccessToken: "access-token-secret", RefreshToken: "refresh-token-secret", IDToken: "identity-token-secret", AccountID: "account-one", ExpiresAt: now.Add(time.Hour)}
}

func TestTransportScrubsEveryOwnedTokenFromBodyStatusAndErrors(t *testing.T) {
	// C6 and C18: access, refresh and identity tokens are equally secret. The
	// response and error are inspected at the transport boundary so the test
	// proves the bytes are gone before any downstream sink can receive them.
	now := time.Now()
	dir := t.TempDir()
	tokens := Tokens{
		AccessToken: "access-boundary-secret", RefreshToken: "refresh-boundary-secret",
		IDToken: "identity-boundary-secret", AccountID: "account-one", ExpiresAt: now.Add(time.Hour),
	}
	if err := Save(dir, tokens); err != nil {
		t.Fatal(err)
	}
	secrets := []string{tokens.AccessToken, tokens.RefreshToken, tokens.IDToken}
	echo := strings.Join(secrets, " ")
	responseTransport := Translate(dir, Options{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest, Status: "400 " + echo,
			Header: make(http.Header), Body: io.NopCloser(strings.NewReader(echo)), Request: request,
		}, nil
	})}})
	request, _ := http.NewRequest(http.MethodGet, "https://example.invalid/echo", nil)
	response, err := responseTransport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	observable := response.Status + "\n" + string(body)
	for _, secret := range secrets {
		if strings.Contains(observable, secret) {
			t.Errorf("response exposed token bytes %q: %s", secret, observable)
		}
	}

	errorTransport := Translate(dir, Options{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(echo)
	})}})
	_, err = errorTransport.RoundTrip(request)
	if err == nil {
		t.Fatal("echoed-token transport error unexpectedly succeeded")
	}
	for _, secret := range secrets {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("transport error exposed token bytes %q: %v", secret, err)
		}
	}
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
	_, translated := doTurnResponse(t, client, endpoint, body)
	return translated
}

func doTurnResponse(t *testing.T, client *http.Client, endpoint string, body map[string]any) (int, string) {
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
	return response.StatusCode, string(translated)
}

func TestFailedResponseEventsAreClassifiableOnStreamingAndWholeResponseRoads(t *testing.T) {
	// C17 and §4.2: either terminal event becomes a numeric chat-completions
	// refusal. Quota retains its payment status and shared sentence; every
	// other backend failure retains its message under 502.
	for _, stream := range []bool{true, false} {
		for _, eventKind := range []string{"response.failed", "error"} {
			for _, quota := range []bool{true, false} {
				name := fmt.Sprintf("stream=%t/%s/quota=%t", stream, eventKind, quota)
				t.Run(name, func(t *testing.T) {
					now := time.Now()
					dir := t.TempDir()
					if err := Save(dir, validTokens(now)); err != nil {
						t.Fatal(err)
					}
					message, code := "backend broke while answering", "backend_failed"
					if quota {
						message, code = "usage_limit_reached for this account", "rate_limit_exceeded"
					}
					backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						writer.Header().Set("Content-Type", "text/event-stream")
						if eventKind == "response.failed" {
							fmt.Fprintf(writer, "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":%q,\"type\":\"server_error\",\"code\":%q}}}\n\n", message, code)
							return
						}
						fmt.Fprintf(writer, "data: {\"type\":\"error\",\"message\":%q,\"code\":%q}\n\n", message, code)
					}))
					defer backend.Close()
					client := ClientWithOptions(dir, Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }})
					status, body := doTurnResponse(t, client, backend.URL+"/chat/completions", map[string]any{
						"model": "gpt-5.5", "stream": stream,
						"messages": []any{map[string]any{"role": "user", "content": "hello"}},
					})
					wantStatus, wantCode, wantMessage := http.StatusBadGateway, `"code":502`, message
					if quota {
						wantStatus, wantCode, wantMessage = http.StatusPaymentRequired, `"code":402`, QuotaWords
					}
					if stream {
						wantStatus = http.StatusOK
					}
					if status != wantStatus || !strings.Contains(body, wantCode) || !strings.Contains(body, wantMessage) {
						t.Fatalf("translated failure status=%d body=%s; want status=%d, %s and %q", status, body, wantStatus, wantCode, wantMessage)
					}
				})
			}
		}
	}
}

func TestIncompleteResponseCarriesLengthOrAClassifiedFailure(t *testing.T) {
	// §4.2: max-output exhaustion is a completed, cut reply; every other
	// incomplete reason is a failure and cannot become a silent [DONE].
	for _, stream := range []bool{true, false} {
		for _, reason := range []string{"max_output_tokens", "content_filter"} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, reason), func(t *testing.T) {
				now := time.Now()
				dir := t.TempDir()
				if err := Save(dir, validTokens(now)); err != nil {
					t.Fatal(err)
				}
				backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					writer.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintln(writer, `data: {"type":"response.created","response":{"id":"cut-1","model":"gpt-5.5","created_at":1800000000}}`)
					fmt.Fprintln(writer)
					fmt.Fprintf(writer, "data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"cut-1\",\"model\":\"gpt-5.5\",\"incomplete_details\":{\"reason\":%q}}}\n\n", reason)
				}))
				defer backend.Close()
				client := ClientWithOptions(dir, Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }})
				status, body := doTurnResponse(t, client, backend.URL+"/chat/completions", map[string]any{
					"model": "gpt-5.5", "stream": stream,
					"messages": []any{map[string]any{"role": "user", "content": "hello"}},
				})
				if reason == "max_output_tokens" {
					if status != http.StatusOK || !strings.Contains(body, `"finish_reason":"length"`) {
						t.Fatalf("max-output response status=%d body=%s", status, body)
					}
					return
				}
				wantStatus := http.StatusBadGateway
				if stream {
					wantStatus = http.StatusOK
				}
				if status != wantStatus || !strings.Contains(body, `"code":502`) || !strings.Contains(body, reason) {
					t.Fatalf("other incomplete status=%d body=%s", status, body)
				}
			})
		}
	}
}

func TestMissingUsageStaysAbsent(t *testing.T) {
	// The emptiness law: an upstream that supplied no usage creates no usage
	// object in either a terminal stream chunk or a whole completion.
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			now := time.Now()
			dir := t.TempDir()
			if err := Save(dir, validTokens(now)); err != nil {
				t.Fatal(err)
			}
			backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintln(writer, `data: {"type":"response.created","response":{"id":"empty-usage","model":"gpt-5.5","created_at":1800000000}}`)
				fmt.Fprintln(writer)
				fmt.Fprintln(writer, `data: {"type":"response.output_text.delta","delta":"answer"}`)
				fmt.Fprintln(writer)
				fmt.Fprintln(writer, `data: {"type":"response.completed","response":{"id":"empty-usage","model":"gpt-5.5"}}`)
				fmt.Fprintln(writer)
			}))
			defer backend.Close()
			client := ClientWithOptions(dir, Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }})
			_, body := doTurnResponse(t, client, backend.URL+"/chat/completions", map[string]any{
				"model": "gpt-5.5", "stream": stream,
				"messages": []any{map[string]any{"role": "user", "content": "hello"}},
			})
			if strings.Contains(body, `"usage"`) {
				t.Fatalf("absent usage became a usage object: %s", body)
			}
		})
	}
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

func TestCodexRefreshProcessHelper(t *testing.T) {
	profile := os.Getenv("CODEAF_CODEX_REFRESH_HELPER_PROFILE")
	if profile == "" {
		return
	}
	if err := os.WriteFile(os.Getenv("CODEAF_CODEX_REFRESH_HELPER_READY"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := os.Getenv("CODEAF_CODEX_REFRESH_HELPER_ENDPOINT")
	models, err := List(context.Background(), profile, Options{Issuer: endpoint, Backend: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-5.5" {
		t.Fatalf("models = %+v", models)
	}
}

func TestC16RotatingRefreshIsSafeAcrossProcesses(t *testing.T) {
	// C16 and D7: two independent processes start from the same expired file.
	// The issuer spends a refresh token once, so both calls can succeed only if
	// the second process reloads the winner's access token under a file lock.
	dir := t.TempDir()
	tokens := validTokens(time.Now())
	tokens.AccessToken = "expired-access-token"
	tokens.RefreshToken = "one-use-refresh-token"
	tokens.ExpiresAt = time.Now().Add(-time.Hour)
	if err := Save(dir, tokens); err != nil {
		t.Fatal(err)
	}
	access := jwt(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	var mutex sync.Mutex
	refreshUsed := false
	refreshes := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case tokenPath:
			_ = request.ParseForm()
			mutex.Lock()
			defer mutex.Unlock()
			refreshes++
			if refreshUsed || request.Form.Get("refresh_token") != "one-use-refresh-token" {
				writer.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(writer, `{"error":"invalid_grant"}`)
				return
			}
			refreshUsed = true
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"access_token": access, "refresh_token": "rotated-refresh-token",
			})
		case "/models":
			if request.Header.Get("Authorization") != "Bearer "+access {
				t.Fatalf("models authorization = %q", request.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"models": []any{
				map[string]any{"slug": "gpt-5.5", "visibility": "list"},
			}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	gate, err := lockTokenFile(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	type child struct {
		command *exec.Cmd
		ready   string
		output  bytes.Buffer
	}
	children := make([]*child, 2)
	for index := range children {
		ready := filepath.Join(t.TempDir(), "ready")
		command := exec.Command(os.Args[0], "-test.run=^TestCodexRefreshProcessHelper$")
		command.Env = append(os.Environ(),
			"CODEAF_CODEX_REFRESH_HELPER_PROFILE="+dir,
			"CODEAF_CODEX_REFRESH_HELPER_ENDPOINT="+server.URL,
			"CODEAF_CODEX_REFRESH_HELPER_READY="+ready,
		)
		children[index] = &child{command: command, ready: ready}
		command.Stdout, command.Stderr = &children[index].output, &children[index].output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, process := range children {
		waitForRefreshSignal(t, process.ready)
	}
	if err := filelock.Unlock(gate); err != nil {
		t.Fatal(err)
	}
	if err := gate.Close(); err != nil {
		t.Fatal(err)
	}
	for _, process := range children {
		if err := process.command.Wait(); err != nil {
			t.Fatalf("refresh helper failed: %v\n%s", err, process.output.String())
		}
	}
	mutex.Lock()
	defer mutex.Unlock()
	if refreshes != 1 {
		t.Fatalf("issuer received %d refreshes, want one use of the rotating token", refreshes)
	}
	kept, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if kept.AccessToken != access || kept.RefreshToken != "rotated-refresh-token" || !Connected(dir) {
		t.Fatalf("shared token file did not end signed in: %+v", kept)
	}
}

func TestC16HungRefreshEndsAtTheRequestDeadlineWithoutChangingTokens(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	tokens := validTokens(now)
	tokens.ExpiresAt = now.Add(-time.Minute)
	if err := Save(dir, tokens); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	issuer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(250 * time.Millisecond)
	}))
	defer issuer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = List(ctx, dir, Options{
		Issuer: issuer.URL, Backend: issuer.URL, HTTPClient: issuer.Client(),
		Now: func() time.Time { return now },
	})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung issuer error = %v, want the request deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("hung issuer returned after %s, want a bounded refresh", elapsed)
	}
	after, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("hung issuer changed token bytes:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestC16CancelledRefreshWaiterLeavesPromptlyAndTheLockIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	lockPath := Path(dir) + ".lock"
	if err := os.WriteFile(lockPath, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockPath, 0o666); err != nil {
		t.Fatal(err)
	}
	owner, err := lockTokenFile(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = filelock.Unlock(owner)
		_ = owner.Close()
	}()
	info, err := owner.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("refresh lock mode = %04o, want 0600", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	type result struct {
		file *os.File
		err  error
	}
	done := make(chan result, 1)
	go func() {
		file, waitErr := lockTokenFile(ctx, dir)
		done <- result{file: file, err: waitErr}
	}()
	select {
	case answer := <-done:
		if answer.file != nil {
			_ = filelock.Unlock(answer.file)
			_ = answer.file.Close()
			t.Fatal("an already-cancelled waiter acquired the refresh lock")
		}
		if answer.err == nil || answer.err.Error() != errRefreshAlreadyRunning.Error() {
			t.Fatalf("cancelled waiter error = %v, want %q", answer.err, errRefreshAlreadyRunning)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("an already-cancelled waiter remained behind the refresh owner")
	}
}

func waitForRefreshSignal(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("refresh helper did not become ready: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestC16OnlyIssuerCredentialRefusalsExpireTheSavedSignIn(t *testing.T) {
	// C16: a 400 or 401 changes the durable bytes to the disconnected shape.
	// Pacing, server trouble, and an unreadable success leave every byte alone,
	// and a second call reaches the issuer again instead of staying signed out.
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		expire bool
	}{
		{name: "invalid grant", status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`, expire: true},
		{name: "invalid client", status: http.StatusUnauthorized, body: `{"error":"invalid_client"}`, expire: true},
		{name: "pacing", status: http.StatusTooManyRequests, body: `{"error":"slow down"}`},
		{name: "issuer failure", status: http.StatusServiceUnavailable, body: `{"error":"try again"}`},
		{name: "malformed success", status: http.StatusOK, body: `{not-json`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Now()
			dir := t.TempDir()
			tokens := validTokens(now)
			tokens.ExpiresAt = now.Add(-time.Minute)
			if err := Save(dir, tokens); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(Path(dir))
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				writer.WriteHeader(testCase.status)
				_, _ = io.WriteString(writer, testCase.body)
			}))
			defer server.Close()
			options := Options{Issuer: server.URL, Backend: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }}
			_, firstErr := List(context.Background(), dir, options)
			if firstErr == nil {
				t.Fatal("failed refresh returned no error")
			}
			after, err := os.ReadFile(Path(dir))
			if err != nil {
				t.Fatal(err)
			}
			if testCase.expire {
				want := tokens
				want.AccessToken = ""
				want.ExpiresAt = time.Time{}
				wantBytes, _ := json.Marshal(want)
				if !errors.Is(firstErr, ErrSignInExpired) || !bytes.Equal(after, wantBytes) {
					t.Fatalf("expired result error=%v\nbytes=%s\nwant=%s", firstErr, after, wantBytes)
				}
				_, _ = List(context.Background(), dir, options)
				if requests.Load() != 1 {
					t.Fatalf("expired sign-in contacted issuer %d times, want one", requests.Load())
				}
				return
			}
			if errors.Is(firstErr, ErrSignInExpired) || !bytes.Equal(after, before) {
				t.Fatalf("transient failure changed sign-in: error=%v\nbefore=%s\nafter=%s", firstErr, before, after)
			}
			_, _ = List(context.Background(), dir, options)
			if requests.Load() != 2 {
				t.Fatalf("next turn made %d issuer attempts, want two", requests.Load())
			}
		})
	}
}

func TestC16NetworkRefreshFailureLeavesTheTokenFileByteForByte(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	tokens := validTokens(now)
	tokens.ExpiresAt = now.Add(-time.Minute)
	if err := Save(dir, tokens); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint, client := server.URL, server.Client()
	server.Close()
	options := Options{Issuer: endpoint, Backend: endpoint, HTTPClient: client, Now: func() time.Time { return now }}
	for range 2 {
		_, err := List(context.Background(), dir, options)
		if err == nil || errors.Is(err, ErrSignInExpired) {
			t.Fatalf("network refresh error = %v", err)
		}
	}
	after, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("network failure changed token bytes:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestRefreshWithoutADecodableExpiryClearsTheOldExpiry(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	tokens := validTokens(now)
	tokens.ExpiresAt = now.Add(-time.Minute)
	if err := Save(dir, tokens); err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == tokenPath {
			refreshes.Add(1)
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"access_token": "opaque-refreshed-access", "refresh_token": "new-refresh-token",
			})
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"models": []any{
			map[string]any{"slug": "gpt-5.5", "visibility": "list"},
		}})
	}))
	defer server.Close()
	options := Options{Issuer: server.URL, Backend: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }}
	for range 2 {
		if _, err := List(context.Background(), dir, options); err != nil {
			t.Fatal(err)
		}
	}
	kept, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !kept.ExpiresAt.IsZero() || refreshes.Load() != 1 {
		t.Fatalf("refreshed expiry=%v refreshes=%d, want absent expiry and one refresh", kept.ExpiresAt, refreshes.Load())
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
