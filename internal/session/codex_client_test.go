package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	account "github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/trace"
)

func TestC13C14RealAgentUsesTheConfigOwnedCodexClientDoor(t *testing.T) {
	// C13: a real session.Agent reaches Codex through ResolveSources and ClientConfigFor.
	// C14: its ordinary streamed text and usage cross the same provider boundary as every direct service.
	var mutex sync.Mutex
	var path, authorization string
	var body map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		path, authorization = request.URL.Path, request.Header.Get("Authorization")
		_ = json.NewDecoder(request.Body).Decode(&body)
		mutex.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(writer, `data: {"type":"response.created","response":{"id":"agent-1","model":"gpt-5.5","created_at":1800000000}}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.output_text.delta","delta":"agent answer"}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":2}}}`)
		fmt.Fprintln(writer)
	}))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	profile := t.TempDir()
	if err := codexauth.Save(profile, codexauth.Tokens{AccessToken: "agent-access-token", RefreshToken: "agent-refresh-token", IDToken: "agent-identity-token", AccountID: "agent-account", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := account.WriteSources(profile, []account.PersistedSource{{ID: "codex", Written: "codex", Key: codexauth.Sentinel, Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "codex/gpt-5.5", System: "Answer briefly.",
		Sources: account.ResolveSources(profile, "", account.DefaultBaseURL),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	drainTurn(t, agent, "hello from the agent")
	mutex.Lock()
	defer mutex.Unlock()
	encoded, _ := json.Marshal(body)
	if path != "/responses" || authorization != "Bearer agent-access-token" || !strings.Contains(string(encoded), "hello from the agent") {
		t.Fatalf("agent request path=%q authorization=%q body=%s", path, authorization, encoded)
	}
}

func TestCodexQuotaRefusalEndsARealHeadlessTurnInThePlansWords(t *testing.T) {
	// C17: the final EventError says what happened to the plan. The transport's
	// typed payment refusal must survive the session boundary that used to turn
	// every 402 into "your key was not accepted for this model".
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusPaymentRequired)
		_, _ = fmt.Fprintf(writer, `{"error":{"message":%q,"code":402}}`, codexauth.QuotaWords)
	}))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)

	profile := t.TempDir()
	if err := codexauth.Save(profile, codexauth.Tokens{
		AccessToken: "quota-access-token", RefreshToken: "quota-refresh-token",
		IDToken: "quota-identity-token", AccountID: "quota-account", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := account.WriteSources(profile, []account.PersistedSource{{
		ID: "codex", Written: "codex", Key: codexauth.Sentinel, Listed: &listed,
	}}); err != nil {
		t.Fatal(err)
	}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "codex/gpt-5.5",
		Sources: account.ResolveSources(profile, "", account.DefaultBaseURL),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	failure := turnFailure(t, agent, "use the plan")
	want := "codex accepted the key but the account cannot pay — " + codexauth.QuotaWords
	if failure.Err == nil || failure.Err.Error() != want {
		t.Fatalf("final EventError = %v, want %q", failure.Err, want)
	}
}

func TestCodexExpiredOrRemovedSignInEndsARealTurnWithTheRecoverySentence(t *testing.T) {
	// C16: an issuer refusal and a token file removed beneath a live agent are
	// the same observable condition. Neither path retries, and neither exposes
	// a filesystem error or the generic transport sentence.
	const want = "codex sign-in has expired · /connect or codeaf connect codex signs in again"
	for _, testCase := range []struct {
		name   string
		remove bool
	}{
		{name: "issuer refused refresh"},
		{name: "token file removed", remove: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var issuerCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/oauth/token" {
					issuerCalls.Add(1)
					writer.WriteHeader(http.StatusUnauthorized)
					_, _ = fmt.Fprintln(writer, `{"error":"invalid_grant"}`)
					return
				}
				t.Fatalf("expired sign-in reached backend path %q", request.URL.Path)
			}))
			defer server.Close()
			t.Setenv("CODEAF_CODEX_ISSUER", server.URL)
			t.Setenv("CODEAF_CODEX_BACKEND", server.URL)
			profile := t.TempDir()
			expires := time.Now().Add(-time.Minute)
			if testCase.remove {
				expires = time.Now().Add(time.Hour)
			}
			if err := codexauth.Save(profile, codexauth.Tokens{
				AccessToken: "expiry-access-token", RefreshToken: "expiry-refresh-token",
				IDToken: "expiry-identity-token", AccountID: "expiry-account", ExpiresAt: expires,
			}); err != nil {
				t.Fatal(err)
			}
			listed := true
			if err := account.WriteSources(profile, []account.PersistedSource{{
				ID: "codex", Written: "codex", Key: codexauth.Sentinel, Listed: &listed,
			}}); err != nil {
				t.Fatal(err)
			}
			agent, err := New(Config{
				Workspace: t.TempDir(), Model: "codex/gpt-5.5",
				Sources: account.ResolveSources(profile, "", account.DefaultBaseURL),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.Close() })
			if testCase.remove {
				if err := os.Remove(codexauth.Path(profile)); err != nil {
					t.Fatal(err)
				}
			}
			failure := turnFailure(t, agent, "continue this conversation")
			if failure.Err == nil || failure.Err.Error() != want {
				t.Fatalf("final EventError = %v, want %q", failure.Err, want)
			}
			wantIssuerCalls := int32(1)
			if testCase.remove {
				wantIssuerCalls = 0
			}
			if issuerCalls.Load() != wantIssuerCalls {
				t.Fatalf("issuer calls = %d, want %d", issuerCalls.Load(), wantIssuerCalls)
			}
		})
	}
}

func TestCodexEchoedBearerNeverReachesAnyObservableFailureSink(t *testing.T) {
	// C6 and C18: the transport owns these credentials. A hostile backend may
	// echo the bearer, but the final event, transcript, optional body-bearing
	// call log, and debug record must all contain scrubbed bytes instead.
	const access = "codex-sink-access-secret"
	const refresh = "codex-sink-refresh-secret"
	const identity = "codex-sink-identity-secret"
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+access {
			t.Fatalf("backend bearer = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(writer, `{"error":{"message":%q}}`, "backend echoed Bearer "+access)
	}))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	profile := t.TempDir()
	if err := codexauth.Save(profile, codexauth.Tokens{
		AccessToken: access, RefreshToken: refresh, IDToken: identity,
		AccountID: "sink-account", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := account.WriteSources(profile, []account.PersistedSource{{
		ID: "codex", Written: "codex", Key: codexauth.Sentinel, Listed: &listed,
	}}); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	logPath := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv(calllog.EnvVar, logPath)
	t.Setenv(calllog.BodiesEnvVar, "1")
	calllog.Open("")
	t.Cleanup(func() {
		calllog.Close()
		_ = os.Setenv(calllog.EnvVar, calllog.OffValue)
		calllog.Open("")
	})
	ctx := trace.Begin(context.Background())
	if trace.EnableRun(ctx) == "" {
		t.Fatal("debug record did not turn on")
	}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "codex/gpt-5.5", SessionFile: journal,
		Sources: account.ResolveSources(profile, "", account.DefaultBaseURL),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	events, err := agent.Submit(ctx, "provoke the echoed bearer")
	if err != nil {
		t.Fatal(err)
	}
	collected := collect(t, events)
	failure, ok := firstOfKind(collected, EventError)
	if !ok || failure.Err == nil {
		t.Fatalf("turn ended without EventError: %v", kinds(collected))
	}
	agent.SettleWrites()
	calllog.Close()

	sinks := map[string][]byte{
		"EventError": []byte(failure.Err.Error()),
		"transcript": readSinkBytes(t, journal),
		"call log":   readSinkBytes(t, logPath),
		"debug record": readSinkBytes(t,
			trace.Dir(trace.RunFrom(ctx))),
	}
	for name, contents := range sinks {
		for _, secret := range []string{access, refresh, identity} {
			if bytes.Contains(contents, []byte(secret)) {
				t.Errorf("%s contains token bytes %q:\n%s", name, secret, contents)
			}
		}
	}
}

func readSinkBytes(t *testing.T, path string) []byte {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("read sink %s: %v", path, err)
	}
	if !info.IsDir() {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var all []byte
	err = filepath.WalkDir(path, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		all = append(all, raw...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func turnFailure(t *testing.T, agent *Agent, text string) Event {
	t.Helper()
	events, err := agent.Submit(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	collected := collect(t, events)
	failure, ok := firstOfKind(collected, EventError)
	if !ok {
		t.Fatalf("turn ended without EventError: %v", kinds(collected))
	}
	return failure
}
