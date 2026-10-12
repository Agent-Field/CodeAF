package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

type keyTransport func(*http.Request) (*http.Response, error)

func (f keyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func keyServer(t *testing.T, transport keyTransport) (*Server, *config.AppCredentials) {
	t.Helper()
	keys := config.NewAppCredentials()
	return New(testToken, Options{Credentials: keys, ProfileDir: t.TempDir(), KeyHTTPClient: &http.Client{Transport: transport}}), keys
}

func keyAnswer(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestKeyReplacementChecksTheCandidateOnceAndReturnsOnlyModels(t *testing.T) {
	const key = "opaque-provider-key-123456789"
	calls := 0
	s, keys := keyServer(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != config.DefaultBaseURL+"/models/user" || r.Header.Get("Authorization") != "Bearer "+key || r.Method != "GET" {
			t.Fatal("check used the wrong provider, path or credential")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > modelsource.ProbeTimeout {
			t.Fatal("check has no ten-second ceiling")
		}
		return keyAnswer(200, `{"data":[{"id":"vendor/model-a"},{"id":"vendor/model-b"}]}`), nil
	})
	w := request(t, s, "PUT", "/app/v1/keys/openrouter", `{"key":"`+key+`"}`, bearer(), "127.0.0.1:4000")
	if w.Code != 200 || calls != 1 || strings.Contains(w.Body.String(), key) {
		t.Fatalf("check: %d %s (%d calls)", w.Code, w.Body, calls)
	}
	var result struct {
		OK     bool     `json:"ok"`
		Models []string `json:"models"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.OK || strings.Join(result.Models, ",") != "vendor/model-a,vendor/model-b" {
		t.Fatalf("wrong models: %s", w.Body)
	}
	if value, _ := keys.Key("openrouter"); value != key {
		t.Fatal("accepted key did not reach memory")
	}
	w = request(t, s, "GET", "/app/v1/keys", "", bearer(), "127.0.0.1:4000")
	if w.Code != 200 || strings.Contains(w.Body.String(), key) {
		t.Fatal("key status disclosed a credential")
	}
	var rows []KeyStatus
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Provider == "openrouter" {
			found = row.Present && row.Last4 == "6789" && len(row.Models) == 2
		}
	}
	if !found {
		t.Fatalf("masked status lost key metadata: %s", w.Body)
	}
	w = request(t, s, "DELETE", "/app/v1/keys/openrouter", "", bearer(), "127.0.0.1:4000")
	if w.Code != 200 || calls != 1 {
		t.Fatal("delete made a provider call")
	}
	if value, ok := keys.Key("openrouter"); !ok || value != "" {
		t.Fatal("delete did not install the removal")
	}
	w = request(t, s, "GET", "/app/v1/keys", "", bearer(), "127.0.0.1:4000")
	if strings.Contains(w.Body.String(), "vendor/model-a") || strings.Contains(w.Body.String(), "6789") {
		t.Fatal("delete retained key metadata")
	}
}

func TestKeyFailurePreservesTheOldCredentialAndRedactsProviderWords(t *testing.T) {
	const secret = "opaque-refused-provider-key-123456789"
	for _, answer := range []struct {
		name   string
		code   int
		body   string
		err    error
		needle string
	}{
		{"refusal", 401, `{"error":{"message":"invalid API key ` + secret + `"}}`, nil, "invalid API key [redacted] (401)"},
		{"timeout", 0, "", context.DeadlineExceeded, "No answer"},
		{"malformed", 200, `{"data":null}`, nil, "unreadable"},
		{"redirect", 302, "", nil, "302"},
		{"empty ids", 200, `{"data":[{}]}`, nil, "unreadable"},
	} {
		t.Run(answer.name, func(t *testing.T) {
			calls := 0
			s, keys := keyServer(t, func(*http.Request) (*http.Response, error) {
				calls++
				if answer.err != nil {
					return nil, answer.err
				}
				return keyAnswer(answer.code, answer.body), nil
			})
			keys.Set("openrouter", "old-credential-123456789")
			w := request(t, s, "PUT", "/app/v1/keys/openrouter", `{"key":"`+secret+`"}`, bearer(), "127.0.0.1:4000")
			if w.Code != 422 || calls != 1 || strings.Contains(w.Body.String(), secret) || !strings.Contains(w.Body.String(), answer.needle) {
				t.Fatalf("bad refusal: %d %s (%d calls)", w.Code, w.Body, calls)
			}
			var result KeyCheckResult
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.OK || result.Models == nil || result.Error == "" || result.RequestID == "" {
				t.Fatal("failed check lost its result envelope")
			}
			if value, _ := keys.Key("openrouter"); value != "old-credential-123456789" {
				t.Fatal("failed check replaced the key")
			}
		})
	}
}

func TestKeyRoutesRefuseBadBodiesMethodsAndForeignCallersBeforeTheCheck(t *testing.T) {
	s, _ := keyServer(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request reached the provider")
		return nil, errors.New("unexpected call")
	})
	for _, body := range []string{"", "null", "[]", "garbage", `{}`, `{"key":1}`, `{"key":""}`, `{"key":"line\nbreak"}`, `{"key":"secret","extra":true}`, `{"key":"secret"} {}`, `{"key":"` + strings.Repeat("x", 16<<10) + `"}`} {
		w := request(t, s, "PUT", "/app/v1/keys/openrouter", body, bearer(), "127.0.0.1:4000")
		if w.Code != 422 {
			t.Fatalf("invalid body accepted: %d", w.Code)
		}
	}
	for _, route := range []struct{ path, methods string }{{"/app/v1/keys", "GET"}, {"/app/v1/keys/openrouter", "PUT, DELETE"}} {
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"} {
			if strings.Contains(route.methods, method) {
				continue
			}
			w := request(t, s, method, route.path, "{}", bearer(), "127.0.0.1:4000")
			if w.Code != 405 || w.Header().Get("Allow") != route.methods {
				t.Fatalf("method not refused: %s %s %d", method, route.path, w.Code)
			}
		}
		for _, method := range []string{"GET", "PUT", "DELETE"} {
			w := request(t, s, method, route.path, `{"key":"opaque-key-123456789"}`, nil, "127.0.0.1:4000")
			if w.Code != 401 {
				t.Fatal("key route bypassed authentication")
			}
			headers := bearer()
			headers.Set("Origin", "https://foreign.example")
			w = request(t, s, method, route.path, "{}", headers, "127.0.0.1:4000")
			if w.Code != 403 {
				t.Fatal("key route bypassed origin guard")
			}
			w = request(t, s, method, route.path, "{}", bearer(), "foreign.example:4000")
			if w.Code != 421 {
				t.Fatal("key route bypassed host guard")
			}
		}
	}
	for _, id := range []string{"unknown", "codex", "OPENROUTER"} {
		w := request(t, s, "PUT", "/app/v1/keys/"+id, `{"key":"opaque-key-123456789"}`, bearer(), "127.0.0.1:4000")
		if w.Code != 422 {
			t.Fatalf("unknown provider accepted: %s", id)
		}
	}
}

func TestGitHubAndAnthropicChecksSendCredentialsOnlyToTheirService(t *testing.T) {
	for _, id := range []string{"github", "anthropic", "deepseek"} {
		t.Run(id, func(t *testing.T) {
			s, _ := keyServer(t, func(r *http.Request) (*http.Response, error) {
				if id == "anthropic" {
					if r.URL.Host != "api.anthropic.com" || r.Header.Get("x-api-key") != "opaque-service-key-123456789" || r.Header.Get("Authorization") != "" {
						t.Fatal("wrong Anthropic authentication")
					}
				} else if r.Header.Get("Authorization") != "Bearer opaque-service-key-123456789" {
					t.Fatal("wrong bearer")
				}
				if id == "github" {
					if r.URL.String() != "https://api.github.com/user" {
						t.Fatal("wrong GitHub endpoint")
					}
					return keyAnswer(200, `{"login":"person"}`), nil
				}
				return keyAnswer(200, `{"data":[{"id":"model-one"}]}`), nil
			})
			w := request(t, s, "PUT", "/app/v1/keys/"+id, `{"key":"opaque-service-key-123456789"}`, bearer(), "127.0.0.1:4000")
			if w.Code != 200 {
				t.Fatalf("provider check: %d %s", w.Code, w.Body)
			}
		})
	}
}
