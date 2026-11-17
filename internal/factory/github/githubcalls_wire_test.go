package github

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/githubcalls"
)

func githubLog(t *testing.T) (read func() string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "github.jsonl")
	t.Setenv(githubcalls.EnvVar, path)
	return func() string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
}

func TestAJobWriteReachesTheGitHubLog(t *testing.T) {
	read := githubLog(t)
	writes := JobWrites{Token: func(context.Context) string { return "secret" }, HTTP: &http.Client{Transport: jobTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}}
	if err := writes.Assign(context.Background(), factory.Item{Num: 7, Repo: "acme/api", Origin: factory.OriginForge}, "sam"); err != nil {
		t.Fatal(err)
	}
	if log := read(); !strings.Contains(log, `"m":"PATCH"`) || !strings.Contains(log, `"ep":"/repos/:owner/:repo/issues/:n"`) {
		t.Fatalf("log: %s", log)
	}
}

func TestAskingGhWhoIsLoggedInIsInTheGitHubLog(t *testing.T) {
	read := githubLog(t)
	fakeGH(t, "echo 'Logged in to github.com account priya (keyring)'")
	if login, _ := GHLogin(context.Background()); login != "priya" {
		t.Fatalf("login %q", login)
	}
	if log := read(); !strings.Contains(log, `"ep":"gh auth status"`) {
		t.Fatalf("log: %s", log)
	}
}
