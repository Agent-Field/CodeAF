package appapi

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestChatGPTBrowserSignInClaimsBundleOnceAndDisconnects(t *testing.T) {
	jwt := func(claims string) string {
		return "header." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".signature"
	}
	access := jwt(`{"exp":2000000000}`)
	identity := jwt(`{"email":"person@example.test","https://api.openai.com/auth":{"chatgpt_account_id":"account-test","chatgpt_plan_type":"pro"}}`)
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/oauth/token" || r.Form.Get("code_verifier") == "" || r.Form.Get("code") != "browser-code" {
			t.Error("incorrect ChatGPT PKCE exchange")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": access, "refresh_token": "private-refresh-token", "id_token": identity})
	}))
	defer issuer.Close()
	s := settingsServer(t)
	var listener net.Listener
	s.options.ChatGPTAuth = codexauth.Options{Issuer: issuer.URL, Listen: func(_, _ string) (net.Listener, error) {
		var err error
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		return listener, err
	}}
	path := "/app/v1/providers/chatgpt/sign-in"
	w := request(t, s, "GET", path, "", bearer(), "127.0.0.1:4000")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"canceled"`) {
		t.Fatalf("initial state: %s", w.Body)
	}
	w = request(t, s, "POST", path, `{}`, bearer(), "127.0.0.1:4000")
	var start ProviderSignInResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &start) != nil {
		t.Fatal(w.Body)
	}
	uri, err := url.Parse(start.VerificationURI)
	if err != nil || uri.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("missing PKCE")
	}
	s.chatGPT.Lock()
	done := s.chatGPT.done
	s.chatGPT.Unlock()
	response, err := http.Get("http://" + listener.Addr().String() + "/auth/callback?code=browser-code&state=" + url.QueryEscape(uri.Query().Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	waitBrowser(t, done)
	for _, read := range []string{path, "/app/v1/keys", "/app/v1/settings"} {
		w = request(t, s, "GET", read, "", bearer(), "127.0.0.1:4000")
		if strings.Contains(w.Body.String(), access) || strings.Contains(w.Body.String(), "private-refresh-token") {
			t.Fatal("GET leaked tokens")
		}
	}
	headers := bearer()
	headers.Set("Idempotency-Key", "claim-secret")
	w = request(t, s, "POST", path+"/claim", `{}`, headers, "127.0.0.1:4000")
	var claim ProviderSignInClaimResponse
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &claim) != nil || claim.Provider != "chatgpt" {
		t.Fatal(w.Body)
	}
	var tokens codexauth.Tokens
	if json.Unmarshal([]byte(claim.Key), &tokens) != nil || tokens.AccessToken != access || tokens.AccountID != "account-test" {
		t.Fatal("claim omitted refreshable account bundle")
	}
	if again := request(t, s, "POST", path+"/claim", `{}`, headers, "127.0.0.1:4000"); again.Code != 409 {
		t.Fatal("idempotency replayed a secret claim")
	}
	restore := config.UseAppCredentials(s.options.Credentials)
	defer restore()
	if !codexauth.Connected(s.options.ProfileDir) {
		t.Fatal("engine cannot use app's ChatGPT bundle")
	}
	if _, ok := config.ResolveSources(s.options.ProfileDir, "", config.DefaultBaseURL).ByID("codex"); !ok {
		t.Fatal("ChatGPT did not connect the engine transport")
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+access || r.Header.Get("chatgpt-account-id") != "account-test" {
			t.Error("model listing lost ChatGPT identity")
		}
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-test","visibility":"list","context_window":272000}]}`))
	}))
	defer backend.Close()
	s.options.Credentials.Set("openrouter", "")
	s.options.ModelCatalog = nil
	s.options.ChatGPTAuth.Backend = backend.URL
	w = request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
	var models ModelsResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &models) != nil || len(models.Models) != 1 || models.Models[0].ID != "codex/gpt-test" || models.Models[0].Provider != "chatgpt" || !models.Models[0].Available {
		t.Fatalf("ChatGPT model choices: %s", w.Body)
	}

	w = request(t, s, "DELETE", path, "", bearer(), "127.0.0.1:4000")
	if w.Code != 200 || codexauth.Connected(s.options.ProfileDir) {
		t.Fatal("disconnect retained ChatGPT credential")
	}
	if _, err := os.Stat(codexauth.Path(s.options.ProfileDir)); !os.IsNotExist(err) {
		t.Fatal("app wrote browser credential to disk")
	}
}

func TestSettingsChangesEnvelopePersistsDoneDaysAndRejectsInvalidPatches(t *testing.T) {
	s := settingsServer(t)
	w := request(t, s, "PATCH", "/app/v1/settings", `{"changes":{"factory.done_days":7}}`, bearer(), "127.0.0.1:4000")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"factory.done_days":7`) || config.AppFactoryDoneDaysAt(s.options.ProfileDir) != 7 {
		t.Fatal(w.Body)
	}
	for _, body := range []string{`{"changes":{"factory.done_days":-1}}`, `{"changes":{"factory.done_days":1.5}}`, `{"changes":{"factory.done_days":null}}`, `{"changes":{"factory.done_days":2,"secret":true}}`, `{"changes":null}`, `{"changes":{},"factory.done_days":3}`} {
		w = request(t, s, "PATCH", "/app/v1/settings", body, bearer(), "127.0.0.1:4000")
		if w.Code != 422 || config.AppFactoryDoneDaysAt(s.options.ProfileDir) != 7 {
			t.Fatalf("invalid patch %s: %d %s", body, w.Code, w.Body)
		}
	}
	fresh := New(testToken, Options{Credentials: config.NewAppCredentials(), ProfileDir: s.options.ProfileDir})
	defer fresh.Close()
	w = request(t, fresh, "GET", "/app/v1/settings", "", bearer(), "127.0.0.1:4000")
	if !strings.Contains(w.Body.String(), `"factory.done_days":7`) {
		t.Fatal("retention did not survive restart")
	}
}

func TestAgentSnapshotServesSleepAndCalendarDayCapFacts(t *testing.T) {
	f := newAgentFixture(t)
	home := f.home
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s := f.server
	s.options.ProfileDir = home
	f.registry.now = func() time.Time { return now }
	w := f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`)
	var agent Agent
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &agent) != nil {
		t.Fatal(w.Body)
	}
	writeResourceLedger(t, home, []session.UsageLine{{At: now, USD: 20, Session: agent.ID}})
	s.asleep.Store(true)
	read := func() AgentResponse {
		w := f.request(t, "GET", "/app/v1/agents/"+agent.ID, "")
		var out AgentResponse
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal(w.Body)
		}
		return out
	}
	if out := read(); !out.Snapshot.Asleep || !out.Snapshot.DayCapReached {
		t.Fatalf("snapshot facts: %+v", out.Snapshot)
	}
	s.asleep.Store(false)
	now = now.Add(24 * time.Hour)
	if out := read(); out.Snapshot.Asleep || out.Snapshot.DayCapReached {
		t.Fatal("snapshot retained yesterday's facts")
	}
}

func TestChatGPTPendingFlowCancelAndCloseReleaseListener(t *testing.T) {
	for _, how := range []string{"cancel", "close"} {
		t.Run(how, func(t *testing.T) {
			s := settingsServer(t)
			var listener net.Listener
			s.options.ChatGPTAuth = codexauth.Options{Listen: func(_, _ string) (net.Listener, error) {
				var err error
				listener, err = net.Listen("tcp", "127.0.0.1:0")
				return listener, err
			}}
			path := "/app/v1/providers/chatgpt/sign-in"
			if w := request(t, s, "POST", path, `{}`, bearer(), "127.0.0.1:4000"); w.Code != 200 {
				t.Fatal(w.Body)
			}
			s.chatGPT.Lock()
			done := s.chatGPT.done
			s.chatGPT.Unlock()
			if how == "cancel" {
				request(t, s, "DELETE", path, "", bearer(), "127.0.0.1:4000")
			} else {
				s.Close()
			}
			waitBrowser(t, done)
			response, err := http.Get("http://" + listener.Addr().String() + "/auth/callback?code=late")
			if err == nil {
				_ = response.Body.Close()
				t.Fatal("canceled ChatGPT callback still listens")
			}
			if key, _ := s.options.Credentials.Key("chatgpt"); key != "" {
				t.Fatal("canceled flow installed credential")
			}
		})
	}
}
