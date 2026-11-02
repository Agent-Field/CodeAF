package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// commentGitHub is a scripted GitHub for the issue comment route. It records
// every call and fails the test on any other request.
type commentGitHub struct {
	t     *testing.T
	mu    sync.Mutex
	calls []string
	auth  []string
	posts []map[string]any

	pull       bool           // issue 7 is a pull request
	status     map[string]int // "issue" | "comment" -> forced status
	drop       map[string]bool
	body       map[string]string // forced 2xx body
	commentMax int               // posts answered so far
}

func newCommentGitHub(t *testing.T) *commentGitHub {
	return &commentGitHub{t: t, status: map[string]int{}, drop: map[string]bool{}, body: map[string]string{}}
}

func (g *commentGitHub) transport(next http.RoundTripper) http.RoundTripper {
	return githubTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Accept") != "application/vnd.github+json" && next != nil {
			return next.RoundTrip(r)
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		g.calls = append(g.calls, r.Method+" "+r.URL.Path)
		g.auth = append(g.auth, r.Header.Get("Authorization"))
		stage := ""
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/acme/api/issues/7":
			stage = "issue"
		case r.Method == "POST" && r.URL.Path == "/repos/acme/api/issues/7/comments":
			stage = "comment"
			var in map[string]any
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &in); err != nil {
				g.t.Errorf("comment body: %v", err)
			}
			g.posts = append(g.posts, in)
		default:
			g.t.Errorf("forbidden GitHub call: %s %s", r.Method, r.URL)
			return reply(500, `{}`), nil
		}
		if r.URL.Host != "api.github.com" || r.URL.Scheme != "https" {
			g.t.Errorf("bad URL %s", r.URL)
		}
		if g.drop[stage] {
			return nil, errors.New("connection reset " + r.Header.Get("Authorization"))
		}
		if code := g.status[stage]; code != 0 {
			return reply(code, fmt.Sprintf(`{"message":%q}`, prSecretMsg)), nil
		}
		if forced, ok := g.body[stage]; ok {
			code := 200
			if stage == "comment" {
				code = 201
			}
			return reply(code, forced), nil
		}
		if stage == "issue" {
			pr := ""
			if g.pull {
				pr = `,"pull_request":{"url":"https://api.github.com/repos/acme/api/pulls/7"}`
			}
			return reply(200, `{"number":7,"title":"Flaky login"`+pr+`}`), nil
		}
		return reply(201, `{"id":9001,"html_url":"https://github.com/acme/api/issues/7#issuecomment-9001","created_at":"2026-11-01T12:00:00Z"}`), nil
	})
}

func (g *commentGitHub) writes() int {
	n := 0
	for _, c := range g.calls {
		if strings.HasPrefix(c, "POST ") {
			n++
		}
	}
	return n
}

// soloComment is a solo host holding its own profile token and watching acme/api.
func soloComment(t *testing.T, g *commentGitHub) *Handler {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	h := New(factory.Seam{}, Config{Name: "factory", Version: "test", Address: "http://factory", Mode: api.FactorySolo, Store: st,
		GitHubToken:      func(context.Context) string { return "profile-token" },
		GitHubHTTPClient: &http.Client{Transport: g.transport(nil)}})
	t.Cleanup(h.Close)
	return h
}

func commentRequest(h *Handler, token, repo, number, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/v1/repos/"+repo+"/issues/"+number+"/comment", strings.NewReader(body))
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

func commentFailure(t *testing.T, w *httptest.ResponseRecorder) api.IssueCommentFailure {
	t.Helper()
	var f api.IssueCommentFailure
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil || f.Outcome == "" || f.RequestID == "" {
		t.Fatalf("not a typed failure: %v: %d %s", err, w.Code, w.Body)
	}
	return f
}

const commentBody = `{"body":"Which browser shows the flaky login?\n\nA screenshot helps."}`

func TestIssueCommentPostsOnceAsTheCallerAndAnswersTheReceipt(t *testing.T) {
	g := newCommentGitHub(t)
	w := commentRequest(soloComment(t, g), "", "acme/api", "7", commentBody)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var out api.IssueCommentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	want := api.IssueCommentResult{ID: 9001, URL: "https://github.com/acme/api/issues/7#issuecomment-9001", CreatedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	if out.Repo != "acme/api" || out.Number != 7 || out.Comment != want || out.Factory.Name != "factory" {
		t.Fatalf("receipt %+v", out)
	}
	if !strings.Contains(w.Body.String(), `"created_at":"2026-11-01T12:00:00Z"`) {
		t.Fatalf("created_at is not RFC 3339: %s", w.Body)
	}
	if got := strings.Join(g.calls, "\n"); got != "GET /repos/acme/api/issues/7\nPOST /repos/acme/api/issues/7/comments" {
		t.Fatalf("calls:\n%s", got)
	}
	for _, a := range g.auth {
		if a != "Bearer profile-token" {
			t.Fatalf("authorization %q", a)
		}
	}
	if len(g.posts) != 1 || g.posts[0]["body"] != "Which browser shows the flaky login?\n\nA screenshot helps." || len(g.posts[0]) != 1 {
		t.Fatalf("posted %+v", g.posts)
	}
	assertClean(t, w)
}

// Every refusal before the post names its stage, posts nothing and leaks nothing.
func TestIssueCommentRefusalsPostNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		set    func(*commentGitHub)
		repo   string
		number string
		body   string
		code   int
		stage  string
		github int
	}{
		"pull request":   {set: func(g *commentGitHub) { g.pull = true }, code: 422, stage: "issue"},
		"not watched":    {repo: "acme/web", code: 404, stage: "repository"},
		"no issue":       {set: func(g *commentGitHub) { g.status["issue"] = 404 }, code: 404, stage: "issue", github: 404},
		"issue hidden":   {set: func(g *commentGitHub) { g.status["issue"] = 403 }, code: 403, stage: "issue", github: 403},
		"github down":    {set: func(g *commentGitHub) { g.drop["issue"] = true }, code: 502, stage: "issue"},
		"empty body":     {body: `{"body":"  \n "}`, code: 422, stage: "request"},
		"huge body":      {body: fmt.Sprintf(`{"body":%q}`, strings.Repeat("x", api.IssueCommentBodyMax+1)), code: 422, stage: "request"},
		"nul body":       {body: `{"body":"a\u0000b"}`, code: 422, stage: "request"},
		"zero number":    {number: "0", code: 422, stage: "request"},
		"padded number":  {number: "07", code: 422, stage: "request"},
		"word number":    {number: "seven", code: 422, stage: "request"},
		"other issue no": {set: func(g *commentGitHub) { g.body["issue"] = `{"number":8}` }, code: 502, stage: "issue"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newCommentGitHub(t)
			if tc.set != nil {
				tc.set(g)
			}
			repo, number, body := "acme/api", "7", commentBody
			if tc.repo != "" {
				repo = tc.repo
			}
			if tc.number != "" {
				number = tc.number
			}
			if tc.body != "" {
				body = tc.body
			}
			w := commentRequest(soloComment(t, g), "", repo, number, body)
			f := commentFailure(t, w)
			if w.Code != tc.code || f.Outcome != api.IssueCommentRefused || f.Stage != tc.stage || f.GitHubStatus != tc.github || g.writes() != 0 {
				t.Fatalf("%d %s calls=%v", w.Code, w.Body, g.calls)
			}
			assertClean(t, w)
		})
	}
}

// A definitive 4xx to the post says it was not made; anything else is unknown,
// and the post is never repeated.
func TestIssueCommentPostFailuresAreTypedAndNeverRetried(t *testing.T) {
	for name, tc := range map[string]struct {
		set     func(*commentGitHub)
		code    int
		outcome string
		github  int
	}{
		"forbidden":  {set: func(g *commentGitHub) { g.status["comment"] = 403 }, code: 403, outcome: api.IssueCommentRefused, github: 403},
		"not found":  {set: func(g *commentGitHub) { g.status["comment"] = 404 }, code: 403, outcome: api.IssueCommentRefused, github: 404},
		"invalid":    {set: func(g *commentGitHub) { g.status["comment"] = 422 }, code: 422, outcome: api.IssueCommentRefused, github: 422},
		"expired":    {set: func(g *commentGitHub) { g.status["comment"] = 401 }, code: 401, outcome: api.IssueCommentRefused, github: 401},
		"lost":       {set: func(g *commentGitHub) { g.drop["comment"] = true }, code: 502, outcome: api.IssueCommentUnknown},
		"server":     {set: func(g *commentGitHub) { g.status["comment"] = 502 }, code: 502, outcome: api.IssueCommentUnknown, github: 502},
		"redirect":   {set: func(g *commentGitHub) { g.status["comment"] = 307 }, code: 502, outcome: api.IssueCommentUnknown, github: 307},
		"unreadable": {set: func(g *commentGitHub) { g.body["comment"] = `{"id":` }, code: 502, outcome: api.IssueCommentUnknown, github: 201},
		"no id": {set: func(g *commentGitHub) {
			g.body["comment"] = `{"html_url":"https://github.com/acme/api/issues/7#issuecomment-1","created_at":"2026-11-01T12:00:00Z"}`
		}, code: 502, outcome: api.IssueCommentUnknown, github: 201},
		"other url": {set: func(g *commentGitHub) {
			g.body["comment"] = `{"id":1,"html_url":"https://evil.example/acme/api/issues/7#issuecomment-1","created_at":"2026-11-01T12:00:00Z"}`
		}, code: 502, outcome: api.IssueCommentUnknown, github: 201},
		"bad time": {set: func(g *commentGitHub) {
			g.body["comment"] = `{"id":1,"html_url":"https://github.com/acme/api/issues/7#issuecomment-1","created_at":"yesterday"}`
		}, code: 502, outcome: api.IssueCommentUnknown, github: 201},
	} {
		t.Run(name, func(t *testing.T) {
			g := newCommentGitHub(t)
			tc.set(g)
			w := commentRequest(soloComment(t, g), "", "acme/api", "7", commentBody)
			f := commentFailure(t, w)
			if w.Code != tc.code || f.Outcome != tc.outcome || f.Stage != "comment" || f.GitHubStatus != tc.github || g.writes() != 1 {
				t.Fatalf("%d %s calls=%v", w.Code, w.Body, g.calls)
			}
			assertClean(t, w)
		})
	}
}

// ONLY THE OPERATOR MAY POST, and only with their own GitHub sign-in on a team:
// a member is refused before GitHub is asked, and the floor token, which has
// no GitHub identity, never falls back to the host's grant.
func TestIssueCommentIsTheOperatorsWithTheirOwnCredential(t *testing.T) {
	g := newCommentGitHub(t)
	signIn := &fakeGitHub{t: t}
	f := peopleV2Setup(t, api.FactoryTeam)
	if err := f.st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	hostReads := 0
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubToken = func(context.Context) string { hostReads++; return "host-read-grant" }
	f.h.config.GitHubHTTPClient = &http.Client{Transport: g.transport(signIn.client().Transport)}

	priya := signInHandle(t, f.h, signIn, &f.now, "priya", "operator-secret")
	w := commentRequest(f.h, priya, "acme/api", "7", commentBody)
	if failure := commentFailure(t, w); w.Code != 403 || failure.Stage != "operator" || len(g.calls) != 0 {
		t.Fatalf("member: %d %s calls=%v", w.Code, w.Body, g.calls)
	}

	w = commentRequest(f.h, "operator-secret", "acme/api", "7", commentBody)
	if failure := commentFailure(t, w); w.Code != 401 || failure.Stage != "credential" || len(g.calls) != 0 || hostReads != 0 {
		t.Fatalf("floor: %d %s calls=%v host=%d", w.Code, w.Body, g.calls, hostReads)
	}

	owner := signInHandle(t, f.h, signIn, &f.now, "owner", "operator-secret")
	w = commentRequest(f.h, owner, "acme/api", "7", commentBody)
	if w.Code != 201 || g.writes() != 1 || hostReads != 0 {
		t.Fatalf("operator: %d %s calls=%v host=%d", w.Code, w.Body, g.calls, hostReads)
	}
	for _, a := range g.auth {
		if a != "Bearer github-access-secret" {
			t.Fatalf("a team post must carry the operator's own token, got %q", a)
		}
	}
	assertClean(t, w, "host-read-grant")
}

// A double click posts once: a second call for the same issue while the first
// is in flight is refused, and a different issue is not held up by it.
func TestIssueCommentInFlightGuardPostsOnce(t *testing.T) {
	g := newCommentGitHub(t)
	h := soloComment(t, g)
	release := make(chan struct{})
	entered := make(chan struct{})
	inner := g.transport(nil)
	h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			close(entered)
			<-release
		}
		return inner.RoundTrip(r)
	})}
	first := make(chan *httptest.ResponseRecorder)
	go func() { first <- commentRequest(h, "", "acme/api", "7", commentBody) }()
	select {
	case <-entered:
	case w := <-first:
		t.Fatalf("the first call never reached the post: %d %s", w.Code, w.Body)
	}
	w := commentRequest(h, "", "ACME/api", "7", commentBody)
	if failure := commentFailure(t, w); w.Code != 409 || failure.Stage != "request" || failure.Outcome != api.IssueCommentRefused {
		t.Fatalf("second: %d %s", w.Code, w.Body)
	}
	close(release)
	if w := <-first; w.Code != 201 || g.writes() != 1 {
		t.Fatalf("first: %d %s calls=%v", w.Code, w.Body, g.calls)
	}
}
