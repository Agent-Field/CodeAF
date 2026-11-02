package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/githubcalls"
)

func TestTheForgeClientsCallsReachTheGitHubLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github.jsonl")
	t.Setenv(githubcalls.EnvVar, path)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"login":"priya"}`))
	}))
	defer server.Close()
	c := NewClient("token")
	c.baseURL = server.URL
	if _, err := c.Me(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ep":"/user"`) {
		t.Fatalf("log: %s", raw)
	}
}
