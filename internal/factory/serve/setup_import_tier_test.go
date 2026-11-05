package serve

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Issues read in during first-run setup record who may write to the
// repository, so a person who starts their own imported issue is not
// refused as an outsider before the first poll.
func TestFactorySetupImportRecordsEachAuthorsWriteAccess(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	fail := false
	setupGitHub(t, f, &fail)
	original := f.h.config.GitHubHTTPClient.Transport
	f.h.config.GitHubHTTPClient.Transport = githubTransport(func(r *http.Request) (*http.Response, error) {
		body, status := "", 200
		switch r.URL.Path {
		case "/repos/acme/api/issues":
			body = `[{"number":1,"title":"Mine","user":{"login":"Owner"}},{"number":2,"title":"Teammate","user":{"login":"ann"}},{"number":3,"title":"Outsider","user":{"login":"olu"}},{"number":4,"title":"Ghost","user":{"login":""}},{"number":5,"title":"Unreachable","user":{"login":"zed"}}]`
		case "/repos/acme/api/collaborators/ann/permission":
			body = `{"permission":"write"}`
		case "/repos/acme/api/collaborators/olu/permission":
			body = `{"permission":"read"}`
		case "/repos/acme/api/collaborators/zed/permission":
			status, body = 502, `{}`
		default:
			return original.RoundTrip(r)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	var setup api.FactorySetup
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "connect", Source: "token", Token: "setup-secret"}, 200, &setup)
	f.request(t, "POST", "/v1/factory-setup", "operator-secret", api.FactorySetupRequest{ID: "local", Verb: "select_repos", RepoIDs: []string{"acme/api"}}, 200, &setup)
	waitSetup(t, f.h)
	items, err := f.st.List()
	if err != nil || len(items) != 5 {
		t.Fatalf("jobs: %+v %v", items, err)
	}
	want := map[int]factory.Tier{1: factory.TierOwner, 2: factory.TierCollab, 3: factory.TierStranger, 4: factory.TierStranger, 5: factory.TierStranger}
	for _, it := range items {
		if it.Tier != want[it.Num] {
			t.Errorf("issue %d by %q: tier %q, want %q", it.Num, it.Author, it.Tier, want[it.Num])
		}
	}
}
