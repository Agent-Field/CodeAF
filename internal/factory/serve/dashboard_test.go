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

// The factory serves no page: / is one plain line pointing at the clients, so
// is any other path outside /v1, and /v1 keeps its JSON 404.
func TestRootIsOnePlainLineAndAPIUnderV1(t *testing.T) {
	s := httptest.NewServer(serve.New(factory.FixtureSeam(now), config))
	defer s.Close()
	for path, want := range map[string]string{
		"/":          "text/plain",
		"/jobs/12":   "text/plain",
		"/v1/queue":  "application/json",
		"/v1/nope":   "application/json",
		"/v1/events": "text/event-stream",
	} {
		res, err := http.Get(s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		// The event stream stays open, so reading it to EOF would never return.
		if want == "text/event-stream" {
			res.Body.Close()
			if got := res.Header.Get("Content-Type"); res.StatusCode != http.StatusOK || !strings.HasPrefix(got, want) {
				t.Errorf("GET %s = %d %q, want 200 %s", path, res.StatusCode, got, want)
			}
			continue
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, want) {
			t.Errorf("GET %s Content-Type = %q, want %s (%d %.80s)", path, got, want, res.StatusCode, body)
		}
		if want == "text/plain" && (res.StatusCode != http.StatusOK || !strings.Contains(string(body), "API at /v1")) {
			t.Errorf("GET %s = %d %q, want the pointer line", path, res.StatusCode, body)
		}
	}
}
