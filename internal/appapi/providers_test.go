package appapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/openrouterauth"
)

func browserStart(t *testing.T, s *Server) (string, <-chan struct{}) {
	t.Helper()
	headers := bearer()
	headers.Set("Idempotency-Key", "browser-start")
	w := request(t, s, "POST", "/app/v1/providers/openrouter/sign-in", `{}`, headers, "127.0.0.1:4000")
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	var body struct {
		URI string `json:"verification_uri"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatal(w.Body)
	}
	replay := request(t, s, "POST", "/app/v1/providers/openrouter/sign-in", `{}`, headers, "127.0.0.1:4000")
	if replay.Body.String() != w.Body.String() {
		t.Fatal("browser retry created another flow")
	}
	uri, err := url.Parse(body.URI)
	if err != nil || uri.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("missing PKCE")
	}
	s.signIn.Lock()
	done := s.signIn.done
	s.signIn.Unlock()
	return uri.Query().Get("callback_url"), done
}

func waitBrowser(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal("browser completion did not stop")
	}
}

func TestBrowserSignInChecksPKCEPublishesAndClaimsKeyOnce(t *testing.T) {
	const secret = "sk-or-browser-credential-12345678"
	calls := 0
	exchange := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "POST" || body["code"] != "browser-code" || body["code_verifier"] == "" {
			t.Error("incorrect exchange")
		}
		_, _ = io.WriteString(w, `{"key":"`+secret+`"}`)
	}))
	defer exchange.Close()
	s := settingsServer(t)
	s.options.OpenRouterAuth = openrouterauth.Options{ExchangeURL: exchange.URL}
	callback, done := browserStart(t, s)
	w := request(t, s, "POST", "/app/v1/providers/openrouter/sign-in", `{}`, bearer(), "127.0.0.1:4000")
	if w.Code != 409 {
		t.Fatal("allowed concurrent browser attempt")
	}
	response, err := http.Get(callback + "?code=browser-code")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("browser callback failed")
	}
	waitBrowser(t, done)
	if calls != 1 {
		t.Fatal("exchange was repeated")
	}
	key, _ := s.options.Credentials.Key("openrouter")
	if key != secret {
		t.Fatal("browser did not connect memory source")
	}
	batch, _, _, _, _ := s.events.read(0)
	if len(batch) != 1 || batch[0].kind != EventKeysChanged || strings.Contains(string(batch[0].data), secret) {
		t.Fatal("browser event was missing or leaked a key")
	}
	for _, path := range []string{"/app/v1/keys", "/app/v1/settings", "/app/v1/providers/openrouter/sign-in"} {
		w = request(t, s, "GET", path, "", bearer(), "127.0.0.1:4000")
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("GET leaked browser key: %s", path)
		}
	}
	w = request(t, s, "POST", "/app/v1/providers/openrouter/sign-in/claim", `{}`, nil, "127.0.0.1:4000")
	if w.Code != 401 {
		t.Fatal("claim escaped guard")
	}
	w = request(t, s, "POST", "/app/v1/providers/openrouter/sign-in/claim", `{}`, bearer(), "127.0.0.1:4000")
	if w.Code != 200 || !strings.Contains(w.Body.String(), secret) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("native core could not save browser credential")
	}
	w = request(t, s, "POST", "/app/v1/providers/openrouter/sign-in/claim", `{}`, bearer(), "127.0.0.1:4000")
	if w.Code != 409 {
		t.Fatal("browser credential claimed twice")
	}
	raw, _ := os.ReadFile(config.BudgetConfigPath(s.options.ProfileDir))
	if strings.Contains(string(raw), secret) {
		t.Fatal("browser wrote key to disk")
	}
}

func TestBrowserSignInCancelAndEngineCloseReleaseTheCallback(t *testing.T) {
	for _, how := range []string{"cancel", "close", "context"} {
		t.Run(how, func(t *testing.T) {
			s := settingsServer(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.options.Context = ctx
			callback, done := browserStart(t, s)
			switch how {
			case "cancel":
				w := request(t, s, "DELETE", "/app/v1/providers/openrouter/sign-in", "", bearer(), "127.0.0.1:4000")
				if w.Code != 200 {
					t.Fatal(w.Body)
				}
			case "close":
				s.Close()
			case "context":
				cancel()
			}
			waitBrowser(t, done)
			key, _ := s.options.Credentials.Key("openrouter")
			if key != "memory-only-test-key" {
				t.Fatal("cancellation changed previous credential")
			}
			client := http.Client{}
			if response, err := client.Get(callback + "?code=late"); err == nil {
				_ = response.Body.Close()
				t.Fatal("canceled callback still listens")
			}
		})
	}
}

func TestBrowserSignInCannotUndoALaterKeyRemoval(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	exchange := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, `{"key":"sk-or-late-browser-credential"}`)
	}))
	defer exchange.Close()
	s := settingsServer(t)
	s.options.OpenRouterAuth = openrouterauth.Options{ExchangeURL: exchange.URL}
	callback, done := browserStart(t, s)
	returned := make(chan error, 1)
	go func() {
		response, err := http.Get(callback + "?code=browser-code")
		if err == nil {
			_ = response.Body.Close()
		}
		returned <- err
	}()
	<-entered
	w := request(t, s, "DELETE", "/app/v1/keys/openrouter", "", bearer(), "127.0.0.1:4000")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	close(release)
	if err := <-returned; err != nil {
		t.Fatal(err)
	}
	waitBrowser(t, done)
	if key, _ := s.options.Credentials.Key("openrouter"); key != "" {
		t.Fatal("late browser completion undid removal")
	}
	w = request(t, s, "POST", "/app/v1/providers/openrouter/sign-in/claim", `{}`, bearer(), "127.0.0.1:4000")
	if w.Code != 409 {
		t.Fatal("removed key could still be claimed")
	}
}

func TestBrowserSignInRefusesProvidersWithoutAnAppFlow(t *testing.T) {
	s := settingsServer(t)
	for _, id := range []string{"anthropic", "github", "codex", "unknown"} {
		w := request(t, s, "POST", "/app/v1/providers/"+id+"/sign-in", `{}`, bearer(), "127.0.0.1:4000")
		if w.Code != 422 {
			t.Fatalf("unbacked provider %s: %d", id, w.Code)
		}
	}
}
