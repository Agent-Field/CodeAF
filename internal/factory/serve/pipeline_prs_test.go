package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

const prsPath = "/v1/repos/acme/api/pipeline/prs"

// prsGitHub is a scripted, read-only GitHub for the open pipeline pull
// requests read. Any write, or any path outside the read, fails the test.
type prsGitHub struct {
	t     *testing.T
	mu    sync.Mutex
	calls []string
	auth  []string
	// open is the default branch's open pull requests, newest first; files is
	// each one's changed paths.
	open   []prsPull
	files  map[int][]string
	status map[string]int // "repo", "pulls" or "files" -> forced status
}

type prsPull struct {
	number      int
	branch      string
	fork, draft bool
}

func newPRsGitHub(t *testing.T, open ...prsPull) *prsGitHub {
	return &prsGitHub{t: t, open: open, files: map[int][]string{}, status: map[string]int{}}
}

func (g *prsGitHub) transport(next http.RoundTripper) http.RoundTripper {
	return githubTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Accept") != "application/vnd.github+json" && next != nil {
			return next.RoundTrip(r)
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		g.calls = append(g.calls, r.Method+" "+r.URL.Path)
		g.auth = append(g.auth, r.Header.Get("Authorization"))
		p := r.URL.Path
		stage := ""
		switch {
		case r.Method != "GET":
		case p == "/repos/acme/api":
			stage = "repo"
		case p == "/repos/acme/api/pulls":
			stage = "pulls"
			if q := r.URL.Query(); q.Get("state") != "open" || q.Get("base") != "main" || q.Get("per_page") != "100" {
				g.t.Errorf("pulls query %s", r.URL.RawQuery)
			}
		case strings.HasPrefix(p, "/repos/acme/api/pulls/") && strings.HasSuffix(p, "/files"):
			stage = "files"
		}
		if stage == "" {
			g.t.Errorf("forbidden GitHub call: %s %s", r.Method, r.URL)
			return reply(500, `{}`), nil
		}
		if code := g.status[stage]; code != 0 {
			return reply(code, fmt.Sprintf(`{"message":%q}`, prSecretMsg)), nil
		}
		switch stage {
		case "repo":
			return reply(200, `{"default_branch":"main","permissions":{"push":false}}`), nil
		case "pulls":
			var out []map[string]any
			for _, pr := range g.open {
				head := map[string]any{"ref": pr.branch, "sha": strings.Repeat("3", 40), "repo": map[string]any{"full_name": "acme/api"}}
				if pr.fork {
					head["repo"] = map[string]any{"full_name": "mona/api"}
				}
				out = append(out, map[string]any{"number": pr.number, "html_url": fmt.Sprintf("https://github.com/acme/api/pull/%d", pr.number),
					"title": fmt.Sprintf("PR %d", pr.number), "draft": pr.draft, "user": map[string]any{"login": "mona"},
					"created_at": "2026-10-30T10:00:00Z", "updated_at": "2026-10-31T11:00:00Z",
					"head": head, "base": map[string]any{"ref": "main"}})
			}
			data, _ := json.Marshal(out)
			return reply(200, string(data)), nil
		}
		n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(p, "/repos/acme/api/pulls/"), "/files"))
		var out []map[string]string
		for _, f := range g.files[n] {
			out = append(out, map[string]string{"filename": f, "status": "modified"})
		}
		data, _ := json.Marshal(out)
		return reply(200, string(data)), nil
	})
}

func soloPRs(t *testing.T, g *prsGitHub) *Handler {
	t.Helper()
	h := New(factory.Seam{}, Config{Name: "factory", Version: "test", Address: "http://factory", Mode: api.FactorySolo,
		GitHubToken:      func(context.Context) string { return "profile-token" },
		GitHubHTTPClient: &http.Client{Transport: g.transport(nil)}})
	t.Cleanup(h.Close)
	return h
}

func readPRs(t *testing.T, w *httptest.ResponseRecorder) api.PipelinePRs {
	t.Helper()
	var out api.PipelinePRs
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	return out
}

// THE OPEN PULL REQUESTS THAT CHANGE THE PIPELINE FILE, from any author, read
// as the caller, with the factory's own branches marked. Pull requests that do
// not touch the file are not listed. Nothing is written.
func TestPipelinePRsListsOpenPullRequestsThatChangeThePipelineFile(t *testing.T) {
	g := newPRsGitHub(t,
		prsPull{number: 7, branch: "codeaf/pipeline-0a1b2c3d4e5f"},
		prsPull{number: 6, branch: "feature/readme"},
		prsPull{number: 5, branch: "mona/steps", draft: true},
		prsPull{number: 4, branch: "codeaf/pipeline-ffffffffffff", fork: true},
	)
	g.files[7] = []string{factory.PipelineFile}
	g.files[6] = []string{"README.md"}
	g.files[5] = []string{"go.mod", factory.PipelineFile}
	g.files[4] = []string{factory.PipelineFile}
	h := soloPRs(t, g)
	out := readPRs(t, sourceRequest(h, "", "GET", prsPath))
	if out.Repo != "acme/api" || !out.Complete || out.CheckedAt.IsZero() || len(out.PRs) != 3 {
		t.Fatalf("%+v", out)
	}
	byNumber := map[int]api.PipelineOpenPR{}
	for _, pr := range out.PRs {
		byNumber[pr.Number] = pr
	}
	seven, five, four := byNumber[7], byNumber[5], byNumber[4]
	if seven.URL != "https://github.com/acme/api/pull/7" || seven.Branch != "codeaf/pipeline-0a1b2c3d4e5f" || !seven.FactoryBranch ||
		seven.HeadSHA != strings.Repeat("3", 40) || seven.Author != "mona" || seven.Title != "PR 7" || seven.Draft ||
		seven.CreatedAt.Format("2006-01-02T15:04:05Z") != "2026-10-30T10:00:00Z" || seven.UpdatedAt.IsZero() {
		t.Fatalf("seven: %+v", seven)
	}
	if !five.Draft || five.FactoryBranch {
		t.Fatalf("five: %+v", five)
	}
	// A fork's branch of the same name was not made by this factory.
	if four.Number != 4 || four.FactoryBranch {
		t.Fatalf("four: %+v", four)
	}
	if _, listed := byNumber[6]; listed {
		t.Fatal("a pull request that does not change the file was listed")
	}
	for i, c := range g.calls {
		if !strings.HasPrefix(c, "GET ") || g.auth[i] != "Bearer profile-token" {
			t.Fatalf("call %s as %q", c, g.auth[i])
		}
	}
	// One repository read, one list page, one files page per candidate.
	if len(g.calls) != 2+4 {
		t.Fatalf("calls: %v", g.calls)
	}
}

// THE READ IS BOUNDED: one page of 100 open pull requests and at most
// pipelinePRsMaxChecks files reads, the factory's own branches first. A read
// that could not look at everything says so (complete false).
func TestPipelinePRsIsBoundedAndSaysWhenItIsIncomplete(t *testing.T) {
	var open []prsPull
	for n := 100; n >= 1; n-- {
		branch := fmt.Sprintf("other/%d", n)
		if n == 1 {
			branch = "codeaf/pipeline-000000000001"
		}
		open = append(open, prsPull{number: n, branch: branch})
	}
	g := newPRsGitHub(t, open...)
	g.files[1] = []string{factory.PipelineFile}
	out := readPRs(t, sourceRequest(soloPRs(t, g), "", "GET", prsPath))
	if out.Complete || len(out.PRs) != 1 || out.PRs[0].Number != 1 {
		t.Fatalf("%+v", out)
	}
	if len(g.calls) != 2+pipelinePRsMaxChecks {
		t.Fatalf("%d calls", len(g.calls))
	}
}

// NO CREDENTIAL, NO READ: a team never falls back to the host's token, and the
// refusal is typed so a client can say "Reconnect GitHub".
func TestPipelinePRsUsesTheCallersOwnGitHubCredentialAndNeverTheHosts(t *testing.T) {
	g := newPRsGitHub(t, prsPull{number: 3, branch: "codeaf/pipeline-abc"})
	g.files[3] = []string{factory.PipelineFile}
	signIn := &fakeGitHub{t: t}
	f := peopleV2Setup(t, api.FactoryTeam)
	hostReads := 0
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubToken = func(context.Context) string { hostReads++; return "host-read-grant" }
	f.h.config.GitHubHTTPClient = &http.Client{Transport: g.transport(signIn.client().Transport)}

	priya := signInHandle(t, f.h, signIn, &f.now, "priya", "operator-secret")
	if out := readPRs(t, sourceRequest(f.h, priya, "GET", prsPath)); len(out.PRs) != 1 {
		t.Fatalf("%+v", out)
	}
	for _, a := range g.auth {
		if a != "Bearer github-access-secret" {
			t.Fatalf("a team read must carry the member's own token, got %q", a)
		}
	}
	g.calls = nil
	w := sourceRequest(f.h, "operator-secret", "GET", prsPath)
	var failure api.PipelinePRsFailure
	if json.Unmarshal(w.Body.Bytes(), &failure) != nil || w.Code != 401 || failure.Stage != api.PipelinePRsStageCredential ||
		failure.RequestID == "" || len(g.calls) != 0 || hostReads != 0 {
		t.Fatalf("floor: %d %s calls=%v host=%d", w.Code, w.Body, g.calls, hostReads)
	}
	assertClean(t, w, "host-read-grant")
}

// GitHub's refusals are typed by stage and never carry GitHub's own text; a
// query string is refused before anything is asked.
func TestPipelinePRsFailuresAreTypedAndReadOnly(t *testing.T) {
	if w := sourceRequest(soloPRs(t, newPRsGitHub(t)), "", "GET", prsPath+"?per_page=1"); w.Code != 422 {
		t.Fatalf("query: %d %s", w.Code, w.Body)
	}
	for _, c := range []struct {
		stage  string
		status int
		want   int
	}{
		{"repo", 404, 404}, {"repo", 401, 401}, {"pulls", 403, 403}, {"pulls", 500, 502}, {"files", 429, 429},
	} {
		g := newPRsGitHub(t, prsPull{number: 2, branch: "x"})
		g.status[c.stage] = c.status
		w := sourceRequest(soloPRs(t, g), "", "GET", prsPath)
		var f api.PipelinePRsFailure
		if json.Unmarshal(w.Body.Bytes(), &f) != nil || w.Code != c.want || f.GitHubStatus != c.status || f.RequestID == "" {
			t.Errorf("%s %d: %d %s", c.stage, c.status, w.Code, w.Body)
		}
		assertClean(t, w)
	}
}
