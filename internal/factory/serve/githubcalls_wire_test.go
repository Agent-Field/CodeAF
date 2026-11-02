package serve

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/githubcalls"
)

func TestARouteNamesItselfOnTheGitHubCallItCauses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github.jsonl")
	t.Setenv(githubcalls.EnvVar, path)
	g := &fakeGitHub{t: t, login: "priya"}
	h := New(factory.Seam{}, Config{GitHubClientID: "client", GitHubHTTPClient: g.client(), AccessToken: "host-token"})
	defer h.Close()
	req := httptest.NewRequest("POST", "/v1/auth/device", strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer host-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"caller":"POST /v1/auth/device"`) {
		t.Fatalf("log: %s", raw)
	}
}
