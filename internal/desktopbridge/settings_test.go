package desktopbridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/config"
)

// parkedSession is an open conversation Close can shut without a live engine.
// Local is the only fact the engine-status route reads off it.
func parkedSession(local bool) *conversation {
	return &conversation{conn: Connection{Local: local, Close: func() {}}, done: make(chan struct{})}
}

func settingsCall(b *Bridge, method, path, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	b.ServeSettings(w, r)
	return w
}

func settingsBridge(t *testing.T, profile string) *Bridge {
	t.Helper()
	b := New(testToken, func(string) (Connection, error) {
		t.Fatal("settings routes must not open a conversation")
		return Connection{}, nil
	})
	t.Cleanup(b.Close)
	if profile != "" {
		b.UseModels(&Models{ProfileDir: profile})
	}
	return b
}

func decodeStatus[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	dec := json.NewDecoder(strings.NewReader(w.Body.String()))
	dec.DisallowUnknownFields()
	var got T
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

func assertNoSecret(t *testing.T, body string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(body, secret) {
			t.Fatal("response contains the provider key")
		}
	}
}

// TestKeyStatusNamesTheSourceNeverTheValue is BE-SET-04 and BE-SEC-13: the
// body names the rung and the canary key is absent from it.
func TestKeyStatusNamesTheSourceNeverTheValue(t *testing.T) {
	canary := "sk-or-v1-canary-key-status-do-not-echo"
	other := "sk-or-v1-other-rung-do-not-echo"
	t.Setenv("CODEAF_BASE_URL", "")
	for _, row := range []struct {
		name   string
		router string
		openai string
		saved  string
		source string
	}{
		{name: "shell OpenRouter", router: canary, openai: other, saved: other, source: keySourceOpenRouter},
		{name: "profile", saved: canary, source: keySourceProfile},
		{name: "shell OpenAI", openai: canary, source: keySourceOpenAI},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv(config.APIKeyEnv, row.router)
			t.Setenv("OPENAI_API_KEY", row.openai)
			profile := t.TempDir()
			if row.saved != "" {
				if err := config.WriteAPIKey(profile, row.saved); err != nil {
					t.Fatal(err)
				}
			}
			b := settingsBridge(t, profile)
			w := settingsCall(b, http.MethodGet, "/api/engine/settings/key", "Bearer "+testToken)
			assertNoSecret(t, w.Body.String(), canary, other, row.router, row.openai, row.saved)
			got := decodeStatus[KeyStatus](t, w)
			if !got.Present || got.Source != row.source {
				t.Fatalf("present=%v source=%q", got.Present, got.Source)
			}
			// The integrator calls settingsRoutes with the prefix already gone.
			stripped := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/settings/key", nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			claimed := b.settingsRoutes(stripped, req, "/settings/key")
			if !claimed || stripped.Code != http.StatusOK {
				t.Fatalf("stripped path claimed=%v status=%d", claimed, stripped.Code)
			}
			assertNoSecret(t, stripped.Body.String(), canary, other)
		})
	}
}

func TestNoKeyIsPresentFalse(t *testing.T) {
	t.Setenv("CODEAF_BASE_URL", "")
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	b := settingsBridge(t, t.TempDir())
	w := settingsCall(b, http.MethodGet, "/api/engine/settings/key", "Bearer "+testToken)
	got := decodeStatus[KeyStatus](t, w)
	if got.Present || got.Source != "" {
		t.Fatalf("present=%v source=%q", got.Present, got.Source)
	}
	if strings.Contains(w.Body.String(), "source") {
		t.Fatal("an absent key still named a source")
	}
}

func TestSettingsRoutesNeedTheToken(t *testing.T) {
	canary := "sk-or-v1-canary-key-status-do-not-echo"
	t.Setenv("CODEAF_BASE_URL", "")
	t.Setenv(config.APIKeyEnv, canary)
	t.Setenv("OPENAI_API_KEY", "")
	b := settingsBridge(t, t.TempDir())
	for _, path := range []string{"/api/engine/settings/key", "/api/engine/settings/engine"} {
		for _, auth := range []string{"", "Bearer nope", "Bearer " + testToken + "x"} {
			w := settingsCall(b, http.MethodGet, path, auth)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s auth %q: status %d", path, auth, w.Code)
			}
			assertNoSecret(t, w.Body.String(), canary)
		}
		if w := settingsCall(b, http.MethodGet, path, "Bearer "+testToken); w.Code == http.StatusUnauthorized {
			t.Fatalf("%s refused the bridge token", path)
		}
	}
	// ServeHTTP refuses before any route runs, which is the production gate
	// the integrator's call sits behind.
	r := httptest.NewRequest(http.MethodGet, "/api/engine/settings/key", nil)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("ServeHTTP status %d", w.Code)
	}
	assertNoSecret(t, w.Body.String(), canary)
}

func TestEngineConnectionReportsWhereTheEngineRuns(t *testing.T) {
	canary := "sk-or-v1-canary-key-status-do-not-echo"
	t.Setenv("CODEAF_BASE_URL", "")
	t.Setenv(config.APIKeyEnv, canary)
	t.Setenv("OPENAI_API_KEY", "")
	profile := t.TempDir()
	b := settingsBridge(t, profile)
	w := settingsCall(b, http.MethodGet, "/api/engine/settings/engine", "Bearer "+testToken)
	assertNoSecret(t, w.Body.String(), canary)
	got := decodeStatus[EngineStatus](t, w)
	if !got.Local || got.Connection != engineConnectionLocal || got.Model != Model || got.Version != buildinfo.String() {
		t.Fatalf("%+v", got)
	}

	if err := config.WriteDesktopRole(profile, config.DesktopRoleConversation, "z/model", ""); err != nil {
		t.Fatal(err)
	}
	w = settingsCall(b, http.MethodGet, "/api/engine/settings/engine", "Bearer "+testToken)
	got = decodeStatus[EngineStatus](t, w)
	if got.Model != "z/model" {
		t.Fatalf("model %q", got.Model)
	}

	b.mu.Lock()
	b.sessions["local"] = parkedSession(true)
	b.mu.Unlock()
	w = settingsCall(b, http.MethodGet, "/api/engine/settings/engine", "Bearer "+testToken)
	got = decodeStatus[EngineStatus](t, w)
	if !got.Local || got.Connection != engineConnectionLocal {
		t.Fatalf("local session: %+v", got)
	}

	b.mu.Lock()
	b.sessions["remote"] = parkedSession(false)
	b.mu.Unlock()
	w = settingsCall(b, http.MethodGet, "/api/engine/settings/engine", "Bearer "+testToken)
	got = decodeStatus[EngineStatus](t, w)
	if got.Local || got.Connection != engineConnectionForwarded {
		t.Fatalf("forwarded: %+v", got)
	}
	assertNoSecret(t, w.Body.String(), canary)

	if w := settingsCall(b, http.MethodPost, "/api/engine/settings/engine", "Bearer "+testToken); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d", w.Code)
	}
	if w := settingsCall(b, http.MethodGet, "/api/engine/settings/missing", "Bearer "+testToken); w.Code != http.StatusNotFound {
		t.Fatalf("unknown status %d", w.Code)
	}
	if w := settingsCall(b, http.MethodGet, "/api/engine/models", "Bearer "+testToken); w.Code != http.StatusNotFound {
		t.Fatalf("models must not be claimed by settings: %d", w.Code)
	}
}

func TestKeyStatusWithoutAProfileDoorIsAbsent(t *testing.T) {
	t.Setenv("CODEAF_BASE_URL", "")
	t.Setenv(config.APIKeyEnv, "sk-or-v1-canary-key-status-do-not-echo")
	b := settingsBridge(t, "")
	w := settingsCall(b, http.MethodGet, "/api/engine/settings/key", "Bearer "+testToken)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d", w.Code)
	}
	assertNoSecret(t, w.Body.String(), "sk-or-v1-canary-key-status-do-not-echo")
}
