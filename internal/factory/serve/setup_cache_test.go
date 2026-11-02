package serve

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestSetupAllPages304SkipsGraphQLAndChangedPageCountsAgain(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	fail := false
	setupGitHub(t, f, &fail)
	base := f.h.config.GitHubHTTPClient.Transport
	tag := `"repos-1"`
	graphql := 0
	conditional := 0
	f.h.config.GitHubHTTPClient.Transport = githubTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/graphql" {
			graphql++
		}
		if r.URL.Path == "/user/repos" {
			if r.Header.Get("If-None-Match") == tag {
				conditional++
				return reply(304, ""), nil
			}
			resp, err := base.RoundTrip(r)
			if err == nil {
				resp.Header.Set("ETag", tag)
			}
			return resp, err
		}
		return base.RoundTrip(r)
	})
	var out api.FactorySetup
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "connect", Source: "token", Token: "setup-secret"}, 200, &out)
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "discover"}, 200, &out)
	if graphql != 1 || conditional != 1 || len(out.Repos) != 2 || out.Repos[0].OpenIssues != 2 || !out.Repos[0].OpenIssuesExact {
		t.Fatalf("unchanged: graphql=%d conditional=%d repos=%+v", graphql, conditional, out.Repos)
	}
	tag = `"repos-2"`
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "discover"}, 200, &out)
	if graphql != 2 {
		t.Fatalf("changed: graphql=%d", graphql)
	}
}

func TestSetupDiscoverySeedsStoredRepos(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	fail := false
	setupGitHub(t, f, &fail)
	var out api.FactorySetup
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "connect", Source: "token", Token: "setup-secret"}, 200, &out)
	f.h.seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: "owner", Via: factory.ViaToken}, nil
	}
	f.h.config.GitHubToken = func(context.Context) string { return "setup-secret" }
	f.h.seam.Repos = func(context.Context) ([]string, []factory.RepoInfo, error) {
		t.Error("setup list was not reused")
		return nil, nil, fmt.Errorf("unexpected discovery")
	}
	w := identityRequest(f.h, "GET", "/v1/repos", "", "operator-secret", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "acme/api") || !strings.Contains(w.Body.String(), "available_read_at") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
