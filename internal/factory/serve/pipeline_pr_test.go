package serve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

const (
	prTip       = "1111111111111111111111111111111111111111"
	prBlob      = "2222222222222222222222222222222222222222"
	prHead      = "3333333333333333333333333333333333333333"
	prSecretMsg = "UPSTREAM-SECRET-TEXT"
	prText      = "pipeline:\n  issue: [write, proof]\n"
)

// prGitHub is a scripted GitHub for the pipeline pull request routes. It
// records every call and fails the test on any verb the route must never use.
type prGitHub struct {
	t  *testing.T
	mu sync.Mutex
	// calls is "METHOD path" in order; auth is each Authorization header.
	calls []string
	auth  []string
	puts  []map[string]any
	posts map[string]map[string]any

	push, archived bool
	file           []byte // nil: .codeaf/factory.yaml does not exist
	branchExists   bool
	defaultBranch  string
	tipReads       int
	loseDefault    bool
	status         map[string]int    // stage -> forced status
	drop           map[string]bool   // stage -> transport error
	body           map[string]string // stage -> forced 2xx body
	cancel         map[string]context.CancelFunc
	// pull is the pull request GET /repos/acme/api/pulls/{n} answers; nil
	// answers 404.
	pull *prPull
}

// prPull is the part of a GitHub pull request the update flow reads.
type prPull struct {
	number           int
	state            string
	headRef, headSHA string
	headRepo         string // "" answers a null head repository (a deleted fork)
	baseRef, baseSHA string
	baseRepo         string
}

func newPRGitHub(t *testing.T) *prGitHub {
	return &prGitHub{t: t, push: true, posts: map[string]map[string]any{}, status: map[string]int{}, drop: map[string]bool{}, body: map[string]string{}, cancel: map[string]context.CancelFunc{}}
}

func (g *prGitHub) stage(r *http.Request) string {
	p := r.URL.Path
	switch {
	case r.Method == "GET" && p == "/repos/acme/api":
		return "repo"
	case r.Method == "GET" && p == "/repos/acme/api/git/ref/heads/"+g.defaultName():
		return "tip"
	case r.Method == "GET" && p == "/repos/acme/api/contents/"+factory.PipelineFile:
		return "file"
	case r.Method == "GET" && strings.HasPrefix(p, "/repos/acme/api/git/ref/heads/"):
		return "exists"
	case r.Method == "POST" && p == "/repos/acme/api/git/refs":
		return "branch"
	case r.Method == "PUT" && p == "/repos/acme/api/contents/"+factory.PipelineFile:
		return "commit"
	case r.Method == "POST" && p == "/repos/acme/api/pulls":
		return "pull_request"
	case r.Method == "GET" && strings.HasPrefix(p, "/repos/acme/api/pulls/"):
		return "pull"
	}
	return ""
}

func (g *prGitHub) defaultName() string {
	if g.defaultBranch != "" {
		return g.defaultBranch
	}
	return "main"
}

func (g *prGitHub) transport(next http.RoundTripper) http.RoundTripper {
	return githubTransport(func(r *http.Request) (*http.Response, error) {
		// Only the pipeline pull request flow asks for this media type; sign-in
		// and visibility checks go to the sign-in fake.
		if r.Header.Get("Accept") != "application/vnd.github+json" && next != nil {
			return next.RoundTrip(r)
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		stage := g.stage(r)
		g.calls = append(g.calls, r.Method+" "+r.URL.Path)
		g.auth = append(g.auth, r.Header.Get("Authorization"))
		if stage == "" {
			g.t.Errorf("forbidden GitHub call: %s %s", r.Method, r.URL)
			return reply(500, `{}`), nil
		}
		if r.URL.Host != "api.github.com" || r.URL.Scheme != "https" || r.URL.User != nil || strings.Contains(r.URL.String(), "Bearer") {
			g.t.Errorf("bad URL %s", r.URL)
		}
		var in map[string]any
		if r.Body != nil {
			if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
				if err := json.Unmarshal(raw, &in); err != nil {
					g.t.Errorf("request body: %v", err)
				}
			}
		}
		if cancel := g.cancel[stage]; cancel != nil {
			cancel()
			return nil, context.Canceled
		}
		if g.drop[stage] {
			return nil, errors.New("connection reset " + r.Header.Get("Authorization"))
		}
		if code := g.status[stage]; code != 0 {
			res := reply(code, fmt.Sprintf(`{"message":%q}`, prSecretMsg+" "+r.Header.Get("Authorization")))
			if code >= 300 && code < 400 {
				res.Header.Set("Location", "https://evil.example/steal")
			}
			return res, nil
		}
		if forced, ok := g.body[stage]; ok {
			code := 200
			if r.Method == "POST" || r.Method == "PUT" {
				code = 201
			}
			return reply(code, forced), nil
		}
		switch stage {
		case "repo":
			return reply(200, fmt.Sprintf(`{"default_branch":%q,"archived":%v,"permissions":{"push":%v}}`, g.defaultName(), g.archived, g.push)), nil
		case "tip":
			g.tipReads++
			if g.loseDefault && g.tipReads > 1 {
				return reply(404, `{}`), nil
			}
			return reply(200, fmt.Sprintf(`{"object":{"type":"commit","sha":%q}}`, prTip)), nil
		case "file":
			if want := g.fileRef(); r.URL.Query().Get("ref") != want {
				g.t.Errorf("file read is not pinned to the base commit: %s", r.URL.RawQuery)
			}
			if g.file == nil {
				return reply(404, `{}`), nil
			}
			return reply(200, fmt.Sprintf(`{"type":"file","encoding":"base64","sha":%q,"content":%q}`, prBlob, base64.StdEncoding.EncodeToString(g.file))), nil
		case "exists":
			if g.branchExists {
				return reply(200, `{}`), nil
			}
			return reply(404, `{}`), nil
		case "branch":
			g.posts["refs"] = in
			return reply(201, fmt.Sprintf(`{"ref":%q,"object":{"type":"commit","sha":%q}}`, in["ref"], in["sha"])), nil
		case "commit":
			g.puts = append(g.puts, in)
			return reply(201, fmt.Sprintf(`{"commit":{"sha":%q}}`, prHead)), nil
		case "pull":
			return g.pullReply(r.URL.Path), nil
		case "pull_request":
			g.posts["pulls"] = in
			return reply(201, `{"number":820,"html_url":"https://github.com/acme/api/pull/820"}`), nil
		}
		return reply(500, `{}`), nil
	})
}

// fileRef is the commit the flow must read the file at: the pull request's
// head when one is being updated, else the default branch's tip.
func (g *prGitHub) fileRef() string {
	if g.pull != nil {
		return g.pull.headSHA
	}
	return prTip
}

func (g *prGitHub) pullReply(path string) *http.Response {
	if g.pull == nil || path != fmt.Sprintf("/repos/acme/api/pulls/%d", g.pull.number) {
		return reply(404, `{}`)
	}
	repo := func(name string) any {
		if name == "" {
			return nil
		}
		return map[string]string{"full_name": name}
	}
	data, _ := json.Marshal(map[string]any{
		"number": g.pull.number, "state": g.pull.state, "html_url": fmt.Sprintf("https://github.com/acme/api/pull/%d", g.pull.number),
		"head": map[string]any{"ref": g.pull.headRef, "sha": g.pull.headSHA, "repo": repo(g.pull.headRepo)},
		"base": map[string]any{"ref": g.pull.baseRef, "sha": g.pull.baseSHA, "repo": repo(g.pull.baseRepo)},
	})
	return reply(200, string(data))
}

func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func (g *prGitHub) writes() []string {
	var out []string
	for _, c := range g.calls {
		if !strings.HasPrefix(c, "GET ") {
			out = append(out, c)
		}
	}
	return out
}

func (g *prGitHub) noDeleteEver() {
	g.t.Helper()
	for _, c := range g.calls {
		if strings.HasPrefix(c, "DELETE ") || strings.HasPrefix(c, "PATCH ") {
			g.t.Fatalf("a ref was deleted or moved: %v", g.calls)
		}
	}
}

// soloPR is a solo host holding its own profile token.
func soloPR(t *testing.T, g *prGitHub, seam factory.Seam) *Handler {
	t.Helper()
	h := New(seam, Config{Name: "factory", Version: "test", Address: "http://factory", Mode: api.FactorySolo,
		GitHubToken:      func(context.Context) string { return "profile-token" },
		GitHubHTTPClient: &http.Client{Transport: g.transport(nil)}})
	t.Cleanup(h.Close)
	return h
}

func prRequest(h *Handler, ctx context.Context, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/v1/repos/acme/api/pipeline/open-pr", strings.NewReader(body)).WithContext(ctx)
	r.RemoteAddr = "192.0.2.1:1234"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	} else {
		r.RemoteAddr = "127.0.0.1:1234"
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func prBody(extra string) string {
	return fmt.Sprintf(`{"text":%q,"title":"Pipeline: add proof"%s}`, prText, extra)
}

func encodedPipeline(t *testing.T) []byte {
	t.Helper()
	file, err := pipeline.Parse(prText)
	if err != nil {
		t.Fatal(err)
	}
	data, err := pipeline.Encode(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeFailure(t *testing.T, w *httptest.ResponseRecorder) api.PipelinePRFailure {
	t.Helper()
	var f api.PipelinePRFailure
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
	return f
}

// assertClean fails when a secret, upstream text or success prose leaked.
func assertClean(t *testing.T, w *httptest.ResponseRecorder, secrets ...string) {
	t.Helper()
	body := w.Body.String()
	for _, s := range append(secrets, prSecretMsg, "Bearer", "profile-token", "github-access-secret", "operator-secret") {
		if strings.Contains(body, s) {
			t.Fatalf("response leaked %q: %s", s, body)
		}
	}
}

func TestPipelinePROpensANewBranchAsTheCallerWithPinnedBase(t *testing.T) {
	g := newPRGitHub(t)
	g.file = []byte("old: file\n")
	h := soloPR(t, g, factory.Seam{})
	w := prRequest(h, t.Context(), "", prBody(`,"body":"why","branch":"codeaf/pipeline-api-proof","expected_base_sha":"`+prTip+`"`))
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var out api.PipelinePRResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Outcome != api.PipelinePROpened || out.PR == nil || out.PR.Number != 820 || out.PR.URL != "https://github.com/acme/api/pull/820" ||
		out.PR.Branch != "codeaf/pipeline-api-proof" || out.PR.Base != "main" || out.PR.BaseSHA != prTip || out.PR.HeadSHA != prHead {
		t.Fatalf("response %+v", out)
	}
	want := []string{
		"GET /repos/acme/api",
		"GET /repos/acme/api/git/ref/heads/main",
		"GET /repos/acme/api/contents/.codeaf/factory.yaml",
		"GET /repos/acme/api/git/ref/heads/codeaf/pipeline-api-proof",
		"POST /repos/acme/api/git/refs",
		"PUT /repos/acme/api/contents/.codeaf/factory.yaml",
		"POST /repos/acme/api/pulls",
	}
	if strings.Join(g.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(g.calls, "\n"))
	}
	for _, a := range g.auth {
		if a != "Bearer profile-token" {
			t.Fatalf("authorization %q", a)
		}
	}
	if g.posts["refs"]["ref"] != "refs/heads/codeaf/pipeline-api-proof" || g.posts["refs"]["sha"] != prTip {
		t.Fatalf("ref create %+v", g.posts["refs"])
	}
	put := g.puts[0]
	if put["branch"] != "codeaf/pipeline-api-proof" || put["sha"] != prBlob || put["content"] != base64.StdEncoding.EncodeToString(encodedPipeline(t)) {
		t.Fatalf("contents put %+v", put)
	}
	if pr := g.posts["pulls"]; pr["head"] != "codeaf/pipeline-api-proof" || pr["base"] != "main" || pr["body"] != "why" || pr["title"] != "Pipeline: add proof" {
		t.Fatalf("pull %+v", pr)
	}
	g.noDeleteEver()
	assertClean(t, w)
}

func TestPipelinePRNewFileAndDefaultBranchName(t *testing.T) {
	g := newPRGitHub(t)
	h := soloPR(t, g, factory.Seam{})
	w := prRequest(h, t.Context(), "", prBody(""))
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, has := g.puts[0]["sha"]; has {
		t.Fatalf("a new file must not name a blob: %+v", g.puts[0])
	}
	branch, _ := g.posts["refs"]["ref"].(string)
	if !strings.HasPrefix(branch, "refs/heads/codeaf/pipeline-") || strings.HasSuffix(branch, "/main") {
		t.Fatalf("default branch %q", branch)
	}
	if body := g.posts["pulls"]["body"]; !strings.Contains(fmt.Sprint(body), "made in codeaf") || strings.Contains(fmt.Sprint(body), "CodeAF") {
		t.Fatalf("default body %q", body)
	}
}

func TestPipelinePRUnchangedIsDistinctAndWritesNothing(t *testing.T) {
	g := newPRGitHub(t)
	g.file = encodedPipeline(t)
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(""))
	var out api.PipelinePRResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Outcome != api.PipelinePRUnchanged || out.PR != nil || out.BaseSHA != prTip {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(g.writes()) != 0 {
		t.Fatalf("writes %v", g.writes())
	}
}

func TestPipelinePRStaleExpectedBaseWritesNothing(t *testing.T) {
	g := newPRGitHub(t)
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"expected_base_sha":"`+strings.Repeat("a", 40)+`"`))
	f := decodeFailure(t, w)
	if w.Code != 409 || f.Stage != "base" || f.Outcome != api.PipelinePRRefused || f.BranchLeft || len(g.writes()) != 0 {
		t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
	}
}

func TestPipelinePRRefusesBeforeAnyGitHubCall(t *testing.T) {
	long := strings.Repeat("x", 201)
	for name, body := range map[string]string{
		"empty text":       `{"text":"  "}`,
		"bad yaml":         `{"text":"pipeline: [unclosed"}`,
		"reserved head":    prBody(`,"branch":"HEAD"`),
		"dotdot":           prBody(`,"branch":"codeaf/pipeline-../x"`),
		"reflog syntax":    prBody(`,"branch":"feature@{1}"`),
		"lock suffix":      prBody(`,"branch":"codeaf/pipeline-x.lock"`),
		"long title":       `{"text":"pipeline:\n  issue: [write]\n","title":"` + long + `"}`,
		"newline title":    `{"text":"pipeline:\n  issue: [write]\n","title":"a\nb"}`,
		"short base":       prBody(`,"expected_base_sha":"abc"`),
		"reviewers":        prBody(`,"reviewers":["octocat"]`),
		"update existing":  prBody(`,"pr_number":820`),
		"two objects":      prBody("") + prBody(""),
		"not json":         `text`,
		"oversized branch": prBody(`,"branch":"` + strings.Repeat("a", pipelinePRMaxBranch+1) + `"`),
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", body)
			if w.Code != 422 || len(g.calls) != 0 {
				t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
			}
		})
	}
}

func TestPipelinePRUsesTheCallersOwnGitHubCredential(t *testing.T) {
	g := newPRGitHub(t)
	signIn := &fakeGitHub{t: t}
	f := peopleV2Setup(t, api.FactoryTeam)
	hostReads := 0
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubToken = func(context.Context) string { hostReads++; return "host-read-grant" }
	f.h.config.GitHubHTTPClient = &http.Client{Transport: g.transport(signIn.client().Transport)}

	// A signed-in member who is not the operator may open it with their own token.
	priya := signInHandle(t, f.h, signIn, &f.now, "priya", "operator-secret")
	w := prRequest(f.h, t.Context(), priya, prBody(""))
	if w.Code != 201 {
		t.Fatalf("member: %d %s", w.Code, w.Body)
	}
	for _, a := range g.auth {
		if a != "Bearer github-access-secret" {
			t.Fatalf("a team write must carry the member's own token, got %q", a)
		}
	}
	if hostReads != 0 {
		t.Fatal("a team must never read the host's grant for a write")
	}

	// The host's floor token has no GitHub identity: no calls, no host fallback.
	g.calls, g.auth = nil, nil
	w = prRequest(f.h, t.Context(), "operator-secret", prBody(""))
	failure := decodeFailure(t, w)
	if w.Code != 401 || failure.Stage != "credential" || len(g.calls) != 0 || hostReads != 0 {
		t.Fatalf("floor: %d %s calls=%v host=%d", w.Code, w.Body, g.calls, hostReads)
	}
	assertClean(t, w, "host-read-grant")
}

func TestPipelinePRTeamMemberWithoutPushOrAnArchivedRepoIsRefusedWithZeroWrites(t *testing.T) {
	for name, set := range map[string]func(*prGitHub){
		"no push":  func(g *prGitHub) { g.push = false },
		"archived": func(g *prGitHub) { g.archived = true },
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			set(g)
			w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(""))
			f := decodeFailure(t, w)
			if w.Code != 403 || f.Stage != "permission" || f.Outcome != api.PipelinePRRefused || len(g.calls) != 1 {
				t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
			}
		})
	}
}

func TestPipelinePRHiddenRepositoryStaysHidden(t *testing.T) {
	g := newPRGitHub(t)
	signIn := &fakeGitHub{t: t, repoStatus: 404}
	f := peopleV2Setup(t, api.FactoryTeam)
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = &http.Client{Transport: g.transport(signIn.client().Transport)}
	signIn.repoStatus = 0
	priya := signInHandle(t, f.h, signIn, &f.now, "priya", "operator-secret")
	signIn.repoStatus = 404
	w := prRequest(f.h, t.Context(), priya, prBody(""))
	if w.Code != 404 || len(g.calls) != 0 {
		t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
	}
}

func TestPipelinePRExistingBranchIsNeverOverwrittenOrRenamed(t *testing.T) {
	t.Run("seen before the write", func(t *testing.T) {
		g := newPRGitHub(t)
		g.branchExists = true
		w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"branch":"codeaf/pipeline-taken"`))
		f := decodeFailure(t, w)
		if w.Code != 409 || f.Stage != "branch" || f.Branch != "codeaf/pipeline-taken" || f.BranchLeft || len(g.writes()) != 0 {
			t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
		}
	})
	t.Run("lost the race", func(t *testing.T) {
		g := newPRGitHub(t)
		g.status["branch"] = 422
		w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"branch":"codeaf/pipeline-raced"`))
		f := decodeFailure(t, w)
		if w.Code != 409 || f.Stage != "branch" || f.GitHubStatus != 422 || f.BranchLeft || len(g.writes()) != 1 {
			t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
		}
		assertClean(t, w)
	})
}

// stageNames maps the fake's route names to the stages the API reports.
var stageNames = map[string]string{"repo": "repository", "tip": "base", "file": "file", "branch": "branch", "commit": "commit", "pull_request": "pull_request"}

func TestPipelinePRPartialFailuresKeepTheBranchAndNeverRepeatOrDelete(t *testing.T) {
	type tc struct {
		stage, kind string
		code        int
		want        int
		outcome     string
		left        bool
		writes      int
	}
	cases := []tc{
		{"repo", "401", 401, 401, api.PipelinePRRefused, false, 0},
		{"repo", "404", 404, 404, api.PipelinePRRefused, false, 0},
		{"repo", "500", 500, 502, api.PipelinePRNotOpened, false, 0},
		{"tip", "403", 403, 403, api.PipelinePRRefused, false, 0},
		{"file", "500", 500, 502, api.PipelinePRNotOpened, false, 0},
		{"branch", "403", 403, 403, api.PipelinePRRefused, false, 1},
		{"branch", "500", 500, 502, api.PipelinePRUnknown, true, 1},
		{"commit", "403", 403, 403, api.PipelinePRNotOpened, true, 2},
		{"commit", "409", 409, 409, api.PipelinePRNotOpened, true, 2},
		{"commit", "422", 422, 422, api.PipelinePRNotOpened, true, 2},
		{"commit", "500", 500, 502, api.PipelinePRUnknown, true, 2},
		{"pull_request", "403", 403, 403, api.PipelinePRNotOpened, true, 3},
		{"pull_request", "422", 422, 422, api.PipelinePRNotOpened, true, 3},
		{"pull_request", "502", 502, 502, api.PipelinePRUnknown, true, 3},
		{"pull_request", "302", 302, 502, api.PipelinePRUnknown, true, 3},
		{"commit", "drop", 0, 502, api.PipelinePRUnknown, true, 2},
		{"pull_request", "drop", 0, 502, api.PipelinePRUnknown, true, 3},
		{"branch", "drop", 0, 502, api.PipelinePRUnknown, true, 1},
		{"tip", "drop", 0, 502, api.PipelinePRNotOpened, false, 0},
	}
	for _, c := range cases {
		t.Run(c.stage+" "+c.kind, func(t *testing.T) {
			g := newPRGitHub(t)
			if c.kind == "drop" {
				g.drop[c.stage] = true
			} else {
				g.status[c.stage] = c.code
			}
			w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"branch":"codeaf/pipeline-partial"`))
			f := decodeFailure(t, w)
			if w.Code != c.want || f.Outcome != c.outcome || f.Stage != stageNames[c.stage] || f.BranchLeft != c.left || len(g.writes()) != c.writes {
				t.Fatalf("%d %+v writes=%v", w.Code, f, g.writes())
			}
			if c.left && (f.Branch != "codeaf/pipeline-partial" || !strings.Contains(f.Error, "codeaf/pipeline-partial")) {
				t.Fatalf("a left branch must be named: %+v", f)
			}
			if c.code != 0 && f.GitHubStatus != c.code {
				t.Fatalf("status: %+v", f)
			}
			seen := map[string]int{}
			for _, call := range g.calls {
				seen[call]++
				if seen[call] > 1 {
					t.Fatalf("%s was repeated: %v", call, g.calls)
				}
			}
			g.noDeleteEver()
			assertClean(t, w)
			for _, bad := range []string{"opened", "committed", "pushed", "written to"} {
				if strings.Contains(f.Error, bad) {
					t.Fatalf("failure reads like success (%q): %s", bad, f.Error)
				}
			}
		})
	}
}

func TestPipelinePRInvalidPullRequestAnswersAreNotSuccess(t *testing.T) {
	for name, body := range map[string]string{
		"zero number":  `{"number":0,"html_url":"https://github.com/acme/api/pull/0"}`,
		"negative":     `{"number":-3,"html_url":"https://github.com/acme/api/pull/-3"}`,
		"no url":       `{"number":820}`,
		"other host":   `{"number":820,"html_url":"https://evil.example/acme/api/pull/820"}`,
		"other repo":   `{"number":820,"html_url":"https://github.com/other/api/pull/820"}`,
		"unsafe query": `{"number":820,"html_url":"https://github.com/acme/api/pull/820?x=1"}`,
		"not json":     `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			g.body["pull_request"] = body
			w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"branch":"codeaf/pipeline-odd"`))
			f := decodeFailure(t, w)
			if w.Code != 502 || f.Outcome != api.PipelinePRUnknown || !f.BranchLeft || f.Stage != "pull_request" || len(g.writes()) != 3 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			g.noDeleteEver()
		})
	}
}

func TestPipelinePRRedirectsAreNotFollowed(t *testing.T) {
	g := newPRGitHub(t)
	g.status["repo"] = 302
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(""))
	if w.Code != 502 || len(g.calls) != 1 || len(g.writes()) != 0 {
		t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
	}
	assertClean(t, w, "evil.example")
}

func TestPipelinePRCancellationMidWriteIsUnknownAndStops(t *testing.T) {
	g := newPRGitHub(t)
	ctx, cancel := context.WithCancel(t.Context())
	g.cancel["commit"] = cancel
	w := prRequest(soloPR(t, g, factory.Seam{}), ctx, "", prBody(`,"branch":"codeaf/pipeline-cancel"`))
	f := decodeFailure(t, w)
	if f.Outcome != api.PipelinePRUnknown || !f.BranchLeft || f.Stage != "commit" || len(g.writes()) != 2 {
		t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
	}
	g.noDeleteEver()
}

func TestPipelinePRSecondRequestForTheSameRepositoryWaits(t *testing.T) {
	g := newPRGitHub(t)
	pipelinePRFlights.Store("acme/api", struct{}{})
	defer pipelinePRFlights.Delete("acme/api")
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(""))
	if w.Code != 409 || len(g.calls) != 0 {
		t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
	}
}

func TestPipelinePRNeverTouchesTheCheckoutOrExecs(t *testing.T) {
	dir := t.TempDir()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is needed to build the dirty checkout")
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-b", "main")
	git("config", "user.name", "test")
	git("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "tracked")
	git("commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		cfg, _ := os.ReadFile(filepath.Join(dir, ".git", "config"))
		head, _ := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
		tracked, _ := os.ReadFile(filepath.Join(dir, "tracked"))
		return git("status", "--porcelain") + git("branch", "--list", "-a") + string(cfg) + string(head) + string(tracked)
	}
	before := snapshot()

	// A git or gh on PATH that would be run is a failure.
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	for _, name := range []string{"git", "gh", "ssh"} {
		script := "#!/bin/sh\necho " + name + " >> " + marker + "\nexit 1\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	asked := 0
	seam := factory.Seam{RecipeAt: func(string) (factory.Recipe, []factory.Problem, string, error) {
		asked++
		return factory.DefaultRecipe(), nil, dir, nil
	}}
	g := newPRGitHub(t)
	h := soloPR(t, g, seam)
	t.Setenv("PATH", bin)
	w := prRequest(h, t.Context(), "", prBody(""))
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the route ran git, gh or ssh")
	}
	if asked != 0 {
		t.Fatal("the route must not look for a checkout")
	}
	if after := snapshot(); after != before {
		t.Fatalf("checkout changed:\n%s\n---\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(dir, factory.PipelineFile)); !os.IsNotExist(err) {
		t.Fatal("pipeline written into the checkout", err)
	}

	// Without any checkout at all it still works.
	h2 := soloPR(t, newPRGitHub(t), factory.Seam{})
	if w := prRequest(h2, t.Context(), "", prBody("")); w.Code != 201 {
		t.Fatalf("no checkout: %d %s", w.Code, w.Body)
	}
}
