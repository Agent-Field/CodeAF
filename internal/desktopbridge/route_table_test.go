package desktopbridge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Wave 1 routes are reached through ServeHTTP, not only through the handler
// each lane tested on its own. A miss here is a 404 from the table.
func TestWave1RoutesAreOnTheTable(t *testing.T) {
	b, _, id := fixture(t)

	foreign := httptest.NewRequest(http.MethodGet, "/api/engine/health", nil)
	foreign.Host = "127.0.0.1:1420"
	foreign.Header.Set("Origin", "https://evil.example")
	foreign.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, foreign)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "this connection is only for the codeaf app") {
		t.Fatalf("foreign origin: %d %s", w.Code, w.Body.String())
	}
	for k := range w.Header() {
		if strings.HasPrefix(k, "Access-Control") {
			t.Fatalf("foreign origin learned %s", k)
		}
	}

	rebind := httptest.NewRequest(http.MethodGet, "/api/engine/health", nil)
	rebind.Host = "evil.example:4000"
	rebind.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	b.ServeHTTP(w, rebind)
	if w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("non-loopback host: %d", w.Code)
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/api/engine/workspaces/now", nil)
	preflight.Host = "127.0.0.1:1420"
	preflight.Header.Set("Origin", "http://localhost:1420")
	w = httptest.NewRecorder()
	b.ServeHTTP(w, preflight)
	if w.Code != http.StatusNoContent || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "If-Match") {
		t.Fatalf("preflight: %d %q", w.Code, w.Header().Get("Access-Control-Allow-Headers"))
	}

	jobs := request(b, http.MethodGet, "/api/engine/sessions/"+id+"/jobs", "")
	if jobs.Code != http.StatusConflict || !strings.Contains(jobs.Body.String(), "this engine cannot list its jobs") {
		t.Fatalf("jobs: %d %s", jobs.Code, jobs.Body.String())
	}
	detach := request(b, http.MethodPost, "/api/engine/sessions/"+id+"/detach", "")
	if detach.Code != http.StatusOK || strings.TrimSpace(detach.Body.String()) != "{}" {
		t.Fatalf("detach: %d %s", detach.Code, detach.Body.String())
	}
	icon := request(b, http.MethodGet, "/api/engine/favicon", "")
	if icon.Code != http.StatusOK || strings.TrimSpace(icon.Body.String()) != "{}" {
		t.Fatalf("favicon: %d %s", icon.Code, icon.Body.String())
	}
	key := request(b, http.MethodGet, "/api/engine/settings/key", "")
	if key.Code != http.StatusNotFound || !strings.Contains(key.Body.String(), "this engine has no model settings") {
		t.Fatalf("settings: %d %s", key.Code, key.Body.String())
	}
}

func TestNewStartsTheIdleReaperAndCloseStopsIt(t *testing.T) {
	b := New(testToken, func(string) (Connection, error) { return Connection{}, nil })
	b.mu.Lock()
	started := b.lifecycle != nil && b.lifecycle.done != nil && !b.lifecycle.stopped
	b.mu.Unlock()
	if !started {
		t.Fatal("New did not start the idle reaper")
	}
	b.Close()
	b.mu.Lock()
	stopped := b.lifecycle != nil && b.lifecycle.stopped
	b.mu.Unlock()
	if !stopped {
		t.Fatal("Close did not stop the idle reaper")
	}
}
