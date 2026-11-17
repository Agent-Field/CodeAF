package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFactoryReposCarriesAvailableReadAt(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	err = st.SetAvailableRepos(store.AvailableRepos{Identity: "alice", Credential: fmt.Sprintf("%x", sha256.Sum256([]byte("repo-token"))), ReadAt: at, Repos: []factory.RepoInfo{{Full: "acme/api", Name: "api", Owner: "acme"}}})
	if err != nil {
		t.Fatal(err)
	}
	h := serve.New(factory.Seam{GitHub: func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: "alice", Via: factory.ViaToken}, nil
	}, Repos: func(context.Context) ([]string, []factory.RepoInfo, error) {
		t.Error("unexpected discovery")
		return nil, nil, nil
	}}, serve.Config{Store: st, Mode: api.FactorySolo, AccessToken: testHostToken, GitHubToken: func(context.Context) string { return "repo-token" }})
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); h.Close() })
	w := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	var got struct {
		AvailableReadAt time.Time `json:"available_read_at"`
	}
	w.must("factory.repos", map[string]any{"factory": conn.ID}, &got)
	if !got.AvailableReadAt.Equal(at) {
		t.Fatalf("available_read_at=%s, want %s", got.AvailableReadAt, at)
	}
}

func TestFactoryReposExpiredReadGrantKeepsFailing(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tokensPath := filepath.Join(t.TempDir(), "tokens.json")
	token := "expired-" + t.Name()
	err = factorygithub.SaveReadGrant(factorygithub.GrantPath(tokensPath), factorygithub.ReadGrant{Token: token, Login: "alice", GrantedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	err = st.SetAvailableRepos(store.AvailableRepos{Identity: "alice", Credential: fmt.Sprintf("%x", sha256.Sum256([]byte(token))), ReadAt: time.Now(), Repos: []factory.RepoInfo{{Full: "acme/private"}}})
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	h := serve.New(factoryServerSeam(st, tokensPath, &githubLimit{}), serve.Config{Store: st, Mode: api.FactoryTeam, AccessToken: testHostToken, TokensPath: tokensPath})
	defer h.Close()
	for range 2 {
		req := httptest.NewRequest("GET", "/v1/repos", nil)
		req.Header.Set("Authorization", "Bearer "+testHostToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 500 || strings.Contains(w.Body.String(), "acme/private") {
			t.Fatalf("expired grant: %d %s", w.Code, w.Body)
		}
	}
}
