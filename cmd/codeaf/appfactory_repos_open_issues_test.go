package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// reposGitHub is a fake GitHub for the repository picker: `repos` rows on
// /user/repos (open_issues_count = issues + pulls), and GraphQL answering each
// repository's open issues alone. It counts every request it serves.
type reposGitHub struct {
	mu      sync.Mutex
	rest    int
	graphql int
	repos   int
	refuse  bool // GraphQL answers 403
}

func (g *reposGitHub) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		switch {
		case r.Method == "GET" && r.URL.Path == "/user/repos":
			g.rest++
			page := 0
			fmt.Sscan(r.URL.Query().Get("page"), &page)
			var rows []string
			for i := (page - 1) * 100; i < g.repos && i < page*100; i++ {
				// Repository i has i open issues and 2 open pull requests.
				rows = append(rows, fmt.Sprintf(`{"full_name":"acme/r%d","name":"r%d","owner":{"login":"acme"},"pushed_at":"2026-10-01T00:00:00Z","open_issues_count":%d}`, i, i, i+2))
			}
			_, _ = w.Write([]byte("[" + strings.Join(rows, ",") + "]"))
		case r.Method == "POST" && r.URL.Path == "/graphql":
			g.graphql++
			if g.refuse {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
				return
			}
			var body struct {
				Variables map[string]string `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("graphql body: %v", err)
			}
			data := map[string]any{}
			for key, name := range body.Variables {
				if !strings.HasPrefix(key, "n") {
					continue
				}
				var i int
				fmt.Sscanf(name, "r%d", &i)
				data["r"+key[1:]] = map[string]any{"issues": map[string]int{"totalCount": i}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		default:
			t.Errorf("unexpected GitHub request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// reposWire is factory.repos over the app wire, from the real factory route,
// whose list comes from the real forge client against g.
func reposWire(t *testing.T, g *reposGitHub) json.RawMessage {
	t.Helper()
	gh := g.serve(t)
	seam := factory.Seam{Repos: func(ctx context.Context) ([]string, []factory.RepoInfo, error) {
		repos, err := forge.NewClient("tok").WithBaseURL(gh.URL).Repos(ctx)
		return []string{}, factorygithub.RepoInfos(repos), err
	}}
	h := serve.New(seam, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "alice"})
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); h.Close() })
	w := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	raw, code, msg := w.call("factory.repos", map[string]any{"factory": conn.ID})
	if code != "" {
		t.Fatalf("factory.repos: %s: %s", code, msg)
	}
	return raw
}

type openIssuesRow struct {
	Full       string `json:"full"`
	Open       int    `json:"open"`
	OpenIssues *int   `json:"open_issues"`
}

// EACH REPOSITORY SAYS ITS OPEN ISSUES WITHOUT ITS PULL REQUESTS, beside the
// unchanged open (issues and pull requests together), and it costs one GraphQL
// request per 100 repositories on top of the REST pages.
func TestFactoryReposCarryOpenIssuesWithoutPullRequests(t *testing.T) {
	g := &reposGitHub{repos: 150}
	raw := reposWire(t, g)
	var out struct {
		Available []openIssuesRow `json:"available"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Available) != 150 {
		t.Fatalf("%v: %d rows", err, len(out.Available))
	}
	t.Logf("discovery of %d repositories: %d REST + %d GraphQL requests", g.repos, g.rest, g.graphql)
	for _, r := range out.Available {
		var i int
		fmt.Sscanf(r.Full, "acme/r%d", &i)
		if r.Open != i+2 || r.OpenIssues == nil || *r.OpenIssues != i {
			t.Fatalf("%s: open %d open_issues %v, want %d and %d", r.Full, r.Open, r.OpenIssues, i+2, i)
		}
	}
	if g.rest != 2 || g.graphql != 2 {
		t.Fatalf("discovery made %d REST and %d GraphQL requests, want 2 and 2", g.rest, g.graphql)
	}
}

// A GraphQL refusal leaves open_issues ABSENT, never 0 and never a guess, and
// the list itself still answers with open.
func TestFactoryReposWithoutGraphQLLeaveOpenIssuesAbsent(t *testing.T) {
	g := &reposGitHub{repos: 3, refuse: true}
	raw := reposWire(t, g)
	if strings.Contains(string(raw), "open_issues") {
		t.Fatalf("open_issues was said without GraphQL: %s", raw)
	}
	var out struct {
		Available []openIssuesRow `json:"available"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Available) != 3 || out.Available[0].Open == 0 {
		t.Fatalf("%v: %s", err, raw)
	}
}
