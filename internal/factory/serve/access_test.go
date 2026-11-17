package serve

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func accessRequest(h *Handler, method, path, token string, cookie *http.Cookie, tlsConnection bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(url.Values{"token": {token}}.Encode()))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if tlsConnection {
		r.TLS = &tls.ConnectionState{}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAccessTokenCreatesBrowserSession(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, token string
		secure                    bool
	}{
		{"form", "POST", "/access", "test-secret", false},
		{"query", "GET", "/access?token=test-secret", "", false},
		{"https", "POST", "/access", "test-secret", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
			h := New(factory.FixtureSeam(now), Config{AccessToken: "test-secret"})
			defer h.Close()
			h.sessions.now = func() time.Time { return now }
			w := accessRequest(h, tc.method, tc.path, tc.token, nil, tc.secure)
			cookies := w.Result().Cookies()
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" || len(cookies) != 1 {
				t.Fatalf("login = %d, location %q, cookies %v", w.Code, w.Header().Get("Location"), cookies)
			}
			cookie := cookies[0]
			if cookie.Name != accessCookieName || len(cookie.Value) != 64 || cookie.Value == "test-secret" ||
				!cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure != tc.secure ||
				cookie.Path != "/" || cookie.MaxAge != int(accessSessionTTL.Seconds()) || !cookie.Expires.Equal(now.Add(accessSessionTTL)) {
				t.Fatalf("session cookie = %+v", cookie)
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("login can leak the one-time query through caching or referrers")
			}
			for _, path := range []string{"/v1/queue", "/", "/jobs/12"} {
				if response := accessRequest(h, "GET", path, "", cookie, tc.secure); response.Code != http.StatusOK {
					t.Fatalf("cookie GET %s = %d: %s", path, response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestAccessFormAndInvalidTokens(t *testing.T) {
	h := New(factory.FixtureSeam(time.Now()), Config{AccessToken: "test-secret"})
	defer h.Close()
	form := accessRequest(h, "GET", "/access", "", nil, false)
	if form.Code != http.StatusOK || !strings.Contains(form.Body.String(), `method="post"`) || len(form.Result().Cookies()) != 0 {
		t.Fatalf("public token form = %d: %s", form.Code, form.Body.String())
	}
	for _, tc := range []struct{ method, path, token string }{
		{"GET", "/access?token=wrong", ""},
		{"GET", "/access?token=", ""},
		{"POST", "/access", "wrong"},
		{"POST", "/access", ""},
	} {
		w := accessRequest(h, tc.method, tc.path, tc.token, nil, false)
		if w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 || len(h.sessions.ids) != 0 {
			t.Fatalf("bad token %s %s = %d with sessions %v", tc.method, tc.path, w.Code, h.sessions.ids)
		}
	}
	if w := accessRequest(h, "PUT", "/access", "test-secret", nil, false); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /access = %d", w.Code)
	}
	if w := accessRequest(h, "POST", "/access", strings.Repeat("x", accessFormLimit), nil, false); w.Code != http.StatusBadRequest {
		t.Fatalf("oversized form = %d", w.Code)
	}
}

func TestAccessSessionExpiresAtThirtyDays(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	h := New(factory.FixtureSeam(now), Config{AccessToken: "test-secret"})
	defer h.Close()
	h.sessions.now = func() time.Time { return now }
	cookie := accessRequest(h, "POST", "/access", "test-secret", nil, false).Result().Cookies()[0]
	if w := accessRequest(h, "GET", "/v1/queue", "", &http.Cookie{Name: accessCookieName, Value: "unknown"}, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown cookie = %d", w.Code)
	}
	now = now.Add(accessSessionTTL - time.Second)
	if w := accessRequest(h, "GET", "/v1/queue", "", cookie, false); w.Code != http.StatusOK {
		t.Fatalf("live cookie = %d", w.Code)
	}
	now = now.Add(time.Second)
	if w := accessRequest(h, "GET", "/v1/queue", "", cookie, false); w.Code != http.StatusUnauthorized || len(h.sessions.ids) != 0 {
		t.Fatalf("expired cookie = %d with sessions %v", w.Code, h.sessions.ids)
	}
}

func TestAccessRevokesAllSessionsAndKeepsBearerAccess(t *testing.T) {
	h := New(factory.FixtureSeam(time.Now()), Config{AccessToken: "test-secret"})
	defer h.Close()
	first := accessRequest(h, "POST", "/access", "test-secret", nil, false).Result().Cookies()[0]
	second := accessRequest(h, "GET", "/access?token=test-secret", "", nil, false).Result().Cookies()[0]
	if first.Value == second.Value {
		t.Fatal("different sign-ins share a session id")
	}
	if w := accessRequest(h, "POST", "/v1/access/revoke-sessions", "", first, false); w.Code != http.StatusUnauthorized || len(h.sessions.ids) != 2 {
		t.Fatalf("cookie-only revoke = %d with sessions %v", w.Code, h.sessions.ids)
	}
	r := httptest.NewRequest("POST", "/v1/access/revoke-sessions", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || len(h.sessions.ids) != 0 {
		t.Fatalf("revoke = %d with sessions %v", w.Code, h.sessions.ids)
	}
	for _, cookie := range []*http.Cookie{first, second} {
		if w := accessRequest(h, "GET", "/v1/queue", "", cookie, false); w.Code != http.StatusUnauthorized {
			t.Fatalf("revoked cookie = %d", w.Code)
		}
	}
	r = httptest.NewRequest("GET", "/v1/queue", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer test-secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("bearer after revocation = %d", w.Code)
	}
	if w := accessRequest(h, "POST", "/access", "test-secret", nil, false); w.Code != http.StatusSeeOther {
		t.Fatalf("sign in after revocation = %d", w.Code)
	}
}
