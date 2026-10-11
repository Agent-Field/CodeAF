package serve_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// The dashboard owns every path outside /v1, and /v1 keeps its JSON 404 so a
// client typo is never answered with a web page.
func TestDashboardAtRootAndAPIUnderV1(t *testing.T) {
	s := httptest.NewServer(serve.New(factory.FixtureSeam(now), config))
	defer s.Close()
	for path, want := range map[string]string{
		"/":          "text/html",
		"/jobs/12":   "text/html",
		"/v1/queue":  "application/json",
		"/v1/nope":   "application/json",
		"/v1/events": "application/json",
	} {
		res, err := http.Get(s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, want) {
			t.Errorf("GET %s Content-Type = %q, want %s (%d %.80s)", path, got, want, res.StatusCode, body)
		}
		if want == "text/html" && (res.StatusCode != http.StatusOK || !strings.Contains(string(body), `id="root"`)) {
			t.Errorf("GET %s = %d, want the dashboard page", path, res.StatusCode)
		}
	}
}

// The dashboard is read-only: a write to a page path is refused, never
// answered with the app.
func TestDashboardRefusesWrites(t *testing.T) {
	s := httptest.NewServer(serve.New(factory.FixtureSeam(now), config))
	defer s.Close()
	res, err := http.Post(s.URL+"/jobs/12", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /jobs/12 = %d, want 405", res.StatusCode)
	}
}
