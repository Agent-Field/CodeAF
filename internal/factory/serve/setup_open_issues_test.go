package serve

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A repository with 18 open issues and 12 open pull requests: GitHub's
// repository list says 30. Setup shows 18 and says it is issues alone when
// GraphQL counts it, and keeps 30, not marked exact, whenever GraphQL does not.
func TestFactorySetupOpenIssuesExcludePullRequestsAndFallBackWhenTheCountFails(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		graphql string
		want    int
		exact   bool
	}{
		{"counted", 200, `{"data":{"r0":{"issues":{"totalCount":18}}}}`, 18, true},
		{"refused", 502, `{}`, 30, false},
		{"graphql errors", 200, `{"data":null,"errors":[{"message":"rate limited"}]}`, 30, false},
		{"alias missing", 200, `{"data":{"r0":null},"errors":[{"message":"not found"}]}`, 30, false},
		{"not json", 200, `<html>`, 30, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := peopleV2Setup(t, api.FactorySolo)
			fail := false
			setupGitHub(t, f, &fail)
			original := f.h.config.GitHubHTTPClient.Transport
			f.h.config.GitHubHTTPClient.Transport = githubTransport(func(r *http.Request) (*http.Response, error) {
				body, status := "", 200
				switch r.URL.Path {
				case "/user/repos":
					body = `[{"full_name":"acme/api","open_issues_count":30}]`
				case "/graphql":
					body, status = c.graphql, c.status
				default:
					return original.RoundTrip(r)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			var setup api.FactorySetup
			f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "connect", Source: "token", Token: "setup-secret"}, 200, &setup)
			if len(setup.Repos) != 1 {
				t.Fatalf("repos: %+v", setup.Repos)
			}
			if r := setup.Repos[0]; r.OpenIssues != c.want || r.OpenIssuesExact != c.exact {
				t.Fatalf("open_issues %d exact %v, want %d and %v", r.OpenIssues, r.OpenIssuesExact, c.want, c.exact)
			}
		})
	}
}
