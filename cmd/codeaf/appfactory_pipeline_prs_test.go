package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// pipelinePRsFactory is the real factory handler over a simulated, read-only
// GitHub with two open pull requests into main: #7 changes the pipeline file
// from the factory's own branch, #6 does not change it. token is the solo
// host's profile credential; "" is a host that holds none.
func pipelinePRsFactory(t *testing.T, token string) (*factoryWire, appwire.FactoryConnection, *atomic.Int32) {
	t.Helper()
	const head = "3333333333333333333333333333333333333333"
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("GitHub request escaped its read, credential or origin scope: %s %s", r.Method, r.URL)
		}
		body := `{}`
		switch r.URL.Path {
		case "/repos/acme/api":
			body = `{"default_branch":"main"}`
		case "/repos/acme/api/pulls":
			body = `[{"number":7,"html_url":"https://github.com/acme/api/pull/7","title":"Pipeline: add proof","draft":false,` +
				`"user":{"login":"mona"},"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-31T11:00:00Z",` +
				`"head":{"ref":"codeaf/pipeline-0a1b2c3d4e5f","sha":"` + head + `","repo":{"full_name":"acme/api"}}},` +
				`{"number":6,"html_url":"https://github.com/acme/api/pull/6","title":"Docs","draft":true,` +
				`"user":{"login":"lee"},"created_at":"2026-10-29T10:00:00Z","updated_at":"2026-10-29T11:00:00Z",` +
				`"head":{"ref":"docs","sha":"` + head + `","repo":{"full_name":"acme/api"}}}]`
		case "/repos/acme/api/pulls/7/files":
			body = `[{"filename":"` + factory.PipelineFile + `"}]`
		case "/repos/acme/api/pulls/6/files":
			body = `[{"filename":"README.md"}]`
		default:
			t.Errorf("unexpected simulated GitHub request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	h := serve.New(factory.Seam{}, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "alice",
		GitHubToken: func(context.Context) string { return token }, GitHubHTTPClient: client})
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); h.Close() })
	w := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	return w, conn, &calls
}

// The complete appwire -> app backend -> real factory handler -> simulated
// GitHub path: hello and capabilities announce the method, and the answer is
// the open pull request that changes the pipeline file and only that one.
func TestFactoryPipelinePRsListsTheOpenPipelinePullRequestsThroughTheRealFactory(t *testing.T) {
	w, conn, _ := pipelinePRsFactory(t, "simulated-profile-credential")
	if !strings.Contains(w.hello, `"`+appwire.FeatureFactoryPipelinePRs+`"`) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPipelinePRs, w.hello)
	}
	var caps appwire.FactoryCapabilities
	w.must("factory.capabilities", map[string]any{}, &caps)
	if !caps.PipelinePRs {
		t.Fatalf("capabilities: %+v", caps)
	}
	var out appwire.FactoryPipelinePRs
	w.must("factory.pipeline_prs", map[string]any{"factory": conn.ID, "repo": "acme/api"}, &out)
	if out.Factory != conn.ID || out.Repo != "acme/api" || !out.Complete || out.CheckedAt.IsZero() || len(out.PRs) != 1 {
		t.Fatalf("%+v", out)
	}
	pr := out.PRs[0]
	if pr.Number != 7 || pr.URL != "https://github.com/acme/api/pull/7" || pr.Title != "Pipeline: add proof" || pr.Branch != "codeaf/pipeline-0a1b2c3d4e5f" ||
		pr.HeadSHA != strings.Repeat("3", 40) || pr.Author != "mona" || pr.Draft || !pr.FactoryBranch ||
		pr.CreatedAt.Format("2006-01-02T15:04:05Z07:00") != "2026-10-30T10:00:00Z" || pr.UpdatedAt.Format("2006-01-02T15:04:05Z07:00") != "2026-10-31T11:00:00Z" {
		t.Fatalf("pr: %+v", pr)
	}
	w.neverSaid("simulated-profile-credential")
}

// NO GITHUB CREDENTIAL IS A TYPED REFUSAL the app can show as "Reconnect
// GitHub", and nothing is asked of GitHub.
func TestFactoryPipelinePRsWithoutAGitHubCredentialSaysReconnect(t *testing.T) {
	w, conn, calls := pipelinePRsFactory(t, "")
	w.mustFail("factory.pipeline_prs", map[string]any{"factory": conn.ID, "repo": "acme/api"}, appwire.CodeGitHubExpired)
	if n := calls.Load(); n != 0 {
		t.Fatalf("%d GitHub calls without a credential", n)
	}
}
