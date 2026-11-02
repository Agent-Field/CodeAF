//go:build e2e

package github_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
)

// THE LIVE REPOSITORY LIST. The picker's own lister reads the real account's
// repositories with the token in GH_TOKEN, READ-ONLY: every request is
// counted, and a request that is not the REST list's GET or the GraphQL count's
// POST fails the run. One repository's open and open_issues are logged so a
// person can compare them with GitHub's own search counts.
//
// Opt-in: CODEAF_LIVE_REPOS_OPEN_ISSUES=1, GH_TOKEN, and
// CODEAF_LIVE_FACTORY_REPO=<owner/name> to log. CODEAF_LIVE_MAX_CALLS caps
// the requests (default 28).
func TestLiveRepoListCountsOpenIssuesApartFromPullRequests(t *testing.T) {
	if os.Getenv("CODEAF_LIVE_REPOS_OPEN_ISSUES") != "1" || os.Getenv("GH_TOKEN") == "" {
		t.Skip("opt-in live lane: CODEAF_LIVE_REPOS_OPEN_ISSUES=1 and GH_TOKEN; it only reads")
	}
	want := strings.ToLower(strings.TrimSpace(os.Getenv("CODEAF_LIVE_FACTORY_REPO")))
	var rest, graphql, other atomic.Int32
	real := http.DefaultTransport
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user/repos":
			rest.Add(1)
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			graphql.Add(1)
		default:
			other.Add(1)
			t.Errorf("a request outside the read: %s %s", r.Method, r.URL.Path)
			return nil, http.ErrNotSupported
		}
		if int(rest.Load()+graphql.Load()) > maxCalls() {
			t.Errorf("more than %d GitHub requests", maxCalls())
			return nil, http.ErrNotSupported
		}
		return real.RoundTrip(r)
	})
	t.Cleanup(func() { http.DefaultTransport = real })

	repos, err := factorygithub.Lister(t.TempDir())(context.Background())
	if err != nil || len(repos) == 0 {
		t.Fatalf("lister: %d repositories, %v", len(repos), err)
	}
	counted := 0
	for _, r := range repos {
		if r.OpenIssues != nil {
			counted++
		}
		if strings.EqualFold(r.Full, want) {
			if r.OpenIssues == nil {
				t.Fatalf("%s: open %d, open_issues not said", r.Full, r.Open)
			}
			t.Logf("%s: open %d open_issues %d", r.Full, r.Open, *r.OpenIssues)
		}
	}
	t.Logf("discovery of %d repositories (%d with open_issues): %d REST + %d GraphQL requests, %d refused", len(repos), counted, rest.Load(), graphql.Load(), other.Load())
	if counted != len(repos) {
		t.Errorf("%d of %d repositories had no open_issues", len(repos)-counted, len(repos))
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func maxCalls() int {
	n := 28
	if v := os.Getenv("CODEAF_LIVE_MAX_CALLS"); v != "" {
		var m int
		for _, ch := range v {
			m = m*10 + int(ch-'0')
		}
		n = m
	}
	return n
}
