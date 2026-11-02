package update

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/githubcalls"
)

func TestTheReleaseClientsRequestsReachTheGitHubLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github.jsonl")
	t.Setenv(githubcalls.EnvVar, path)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL+"/repos/o/r/releases/latest", nil)
	resp, err := (&Client{}).requestClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ep":"/repos/:owner/:repo/releases/latest"`) {
		t.Fatalf("log: %s", raw)
	}
}
