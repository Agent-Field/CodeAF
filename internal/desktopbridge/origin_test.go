package desktopbridge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// guarded is the handler as the integrator wires it: the guard first, then the bridge.
func guarded(b *Bridge) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status, msg := guardRequest(r); status != 0 {
			fail(w, status, msg)
			return
		}
		b.ServeHTTP(w, r)
	})
}

func guardedGet(h http.Handler, host, origin string, token bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/api/engine/health", nil)
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if token {
		r.Header.Set("Authorization", "Bearer "+testToken)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAForeignOriginIsRefusedEvenWithTheToken(t *testing.T) {
	b, _, _ := fixture(t)
	for _, origin := range []string{"https://evil.example", "null", "http://localhost:1421", "http://localhost:1420/", "tauri://localhost.evil"} {
		w := guardedGet(guarded(b), "127.0.0.1:4000", origin, true)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "this connection is only for the codeaf app") {
			t.Fatalf("%s: %d %s", origin, w.Code, w.Body.String())
		}
	}
	// An empty Origin header is present, so it is not the same as none.
	r := httptest.NewRequest("GET", "/api/engine/health", nil)
	r.Host = "127.0.0.1:4000"
	r.Header["Origin"] = []string{""}
	if status, _ := guardRequest(r); status != 403 {
		t.Fatalf("empty origin: %d", status)
	}
}

func TestNativeOriginsAndNoOriginPass(t *testing.T) {
	b, _, _ := fixture(t)
	for _, origin := range []string{"", "tauri://localhost", "http://tauri.localhost", "http://localhost:1420", "http://127.0.0.1:1420"} {
		if w := guardedGet(guarded(b), "127.0.0.1:4000", origin, true); w.Code != 200 {
			t.Fatalf("%q with token: %d", origin, w.Code)
		}
	}
	if w := guardedGet(guarded(b), "localhost:4000", "", false); w.Code != 401 {
		t.Fatalf("no origin and no token must still be refused by the token check: %d", w.Code)
	}
}

func TestANonLoopbackHostIsRefused(t *testing.T) {
	b, _, _ := fixture(t)
	for _, host := range []string{"evil.example:4000", "127.0.0.1", "localhost", "127.0.0.1:x", "10.0.0.5:4000", "localhost.evil.example:4000", ""} {
		if w := guardedGet(guarded(b), host, "", true); w.Code != 421 {
			t.Fatalf("host %q: %d", host, w.Code)
		}
	}
	for _, host := range []string{"127.0.0.1:4000", "[::1]:4000", "localhost:4000"} {
		if w := guardedGet(guarded(b), host, "", true); w.Code != 200 {
			t.Fatalf("host %q: %d", host, w.Code)
		}
	}
}

func TestPreflightFromAForeignOriginGetsNoCORSHeaders(t *testing.T) {
	b, _, _ := fixture(t)
	r := httptest.NewRequest("OPTIONS", "/api/engine/sessions", nil)
	r.Host = "127.0.0.1:4000"
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	guarded(b).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("preflight: %d", w.Code)
	}
	for k := range w.Header() {
		if strings.HasPrefix(k, "Access-Control") {
			t.Fatalf("foreign preflight got %s", k)
		}
	}
}
