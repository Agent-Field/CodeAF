package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── a stub factory: a scripted /v1/me and a scripted open-pr route ──────────
//
// httptest only: no model, no GitHub, no provider. It logs every request so a
// test can say exactly what was sent, and how often.

type prStub struct {
	t   *testing.T
	srv *httptest.Server
	mu  sync.Mutex
	log []string
	// the last POST's bearer and body
	auths, bodies []string
	me            api.Me
	meStatus      int
	post          func(w http.ResponseWriter, r *http.Request)
	onPost        func()
}

func newPRStub(t *testing.T, me api.Me) *prStub {
	t.Helper()
	s := &prStub{t: t, me: me, meStatus: http.StatusOK}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.log = append(s.log, r.Method+" "+r.URL.EscapedPath())
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			s.auths = append(s.auths, r.Header.Get("Authorization"))
			s.bodies = append(s.bodies, string(body))
		}
		me, status, post, onPost := s.me, s.meStatus, s.post, s.onPost
		s.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/me":
			if status != http.StatusOK {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"ghp_leaked_by_me"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(me)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pipeline/open-pr"):
			if onPost != nil {
				onPost()
			}
			if post != nil {
				post(w, r)
				return
			}
			prOpened(prDefaultBranch, "")(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *prStub) posts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.log {
		if strings.HasPrefix(l, "POST ") {
			n++
		}
	}
	return n
}

func (s *prStub) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

const (
	prToken         = "tok_held_for_pipeline_pr_0123456789"
	prRepo          = "octo/Hello.x_y"
	prDefaultBranch = "codeaf/pipeline-1a2b3c"
	prBaseSHA       = "0123456789abcdef0123456789abcdef01234567"
	prHeadSHA       = "fedcba9876543210fedcba9876543210fedcba98"
)

func prWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// prOpened answers the route's 201. repo "" means the repository asked about.
func prOpened(branch, repo string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if repo == "" {
			repo = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/repos/"), "/pipeline/open-pr")
		}
		prWriteJSON(w, 201, api.PipelinePRResponse{Factory: api.Factory{Name: "Acme"}, Outcome: api.PipelinePROpened, BaseSHA: prBaseSHA,
			PR:   &api.PipelinePRResult{Number: 820, URL: "https://github.com/" + repo + "/pull/820", Branch: branch, Base: "main", BaseSHA: prBaseSHA, HeadSHA: prHeadSHA},
			Note: "Pull request #820 opened with your GitHub access."})
	}
}

func prFailure(status int, f api.PipelinePRFailure) func(http.ResponseWriter, *http.Request) {
	if f.RequestID == "" {
		f.RequestID = "9f8e7d6c"
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		f.Error = "ghp_leaked_secret at /v1/repos/octo/Hello.x_y and branch codeaf/leak"
		f.Factory = api.Factory{Name: "Acme"}
		prWriteJSON(w, status, f)
	}
}

type prWire struct {
	*factoryWire
	hub *appFactoryHub
	id  string
}

func newPRWire(t *testing.T, stub *prStub, local bool, mode, token string) *prWire {
	t.Helper()
	hub := testFactoryHub(t)
	w := newFactoryWire(t, hub)
	hub.resume = nil
	id := appFactoryID(stub.srv.URL)
	if local {
		id = appLocalID
	}
	hub.mu.Lock()
	hub.rows = []appFactoryRow{{ID: id, URL: stub.srv.URL, Name: "Acme", Mode: mode, Local: local}}
	hub.loaded = true
	hub.mu.Unlock()
	if token != "" {
		hub.hold(id, token, 0)
	}
	return &prWire{factoryWire: w, hub: hub, id: id}
}

func (r *prWire) open(p appwire.FactoryPipelinePRParams) (appwire.FactoryPipelinePRDone, error) {
	p.Factory, p.Repo = r.id, prRepo
	if p.Text == "" {
		p.Text = "pipeline: {}\n"
	}
	return r.backend.FactoryPipelinePR(context.Background(), p)
}

func prWantCode(t *testing.T, err error, code string) *appwire.Error {
	t.Helper()
	if got := wireCodeOf(t, err); got != code {
		t.Fatalf("code %q (%v), want %q", got, err, code)
	}
	var w *appwire.Error
	asWire(err, &w)
	return w
}

// prNoNames fails when an error reveals anything the factory said or anything
// scoped to the repository, branch or pull request.
func prNoNames(t *testing.T, err error, names ...string) {
	t.Helper()
	blob, _ := json.Marshal(err)
	for _, n := range append(names, "ghp_leaked", "leaked", prToken, "/v1/repos", "820") {
		if strings.Contains(string(blob)+err.Error(), n) {
			t.Fatalf("the error carries %q: %s", n, blob)
		}
	}
}

// ── the yes paths ──────────────────────────────────────────────────────────

func TestPipelinePROpensOnePOSTAsTheHeldCallerWithTheRoutesOwnFields(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam, Profile: api.Profile{Handle: "alice"}}) // a member, not an operator
	stub.post = prOpened(prDefaultBranch, "octo/Hello.x_y")
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	got, err := r.open(appwire.FactoryPipelinePRParams{Text: "  pipeline: {a: 1}\n", Title: "T", Body: "B", Branch: prDefaultBranch, ExpectedBaseSHA: prBaseSHA})
	if err != nil {
		t.Fatal(err)
	}
	want := appwire.FactoryPipelinePRDone{Factory: r.id, Repo: prRepo, Outcome: "opened", BaseSHA: prBaseSHA, PR: &appwire.FactoryPipelinePR{
		Number: 820, URL: "https://github.com/octo/Hello.x_y/pull/820", Branch: prDefaultBranch, Base: "main", BaseSHA: prBaseSHA, HeadSHA: prHeadSHA}}
	if got.PR == nil || *got.PR != *want.PR || got.Outcome != want.Outcome || got.Factory != want.Factory || got.Repo != want.Repo || got.BaseSHA != want.BaseSHA {
		t.Fatalf("receipt %+v", got)
	}
	if log := strings.Join(stub.requests(), "|"); log != "GET /v1/me|POST /v1/repos/octo/Hello.x_y/pipeline/open-pr" {
		t.Fatalf("requests %s", log)
	}
	if stub.auths[0] != "Bearer "+prToken {
		t.Fatalf("sent %q", stub.auths[0])
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(stub.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 5 || sent["text"] != "  pipeline: {a: 1}\n" || sent["title"] != "T" || sent["body"] != "B" || sent["branch"] != prDefaultBranch || sent["expected_base_sha"] != prBaseSHA {
		t.Fatalf("body %s", stub.bodies[0])
	}
	// Optional fields left out are left out, not sent as empty strings.
	stub2 := newPRStub(t, api.Me{Mode: api.FactorySolo})
	r2 := newPRWire(t, stub2, false, api.FactorySolo, prToken)
	if _, err := r2.open(appwire.FactoryPipelinePRParams{}); err != nil {
		t.Fatal(err)
	}
	if stub2.bodies[0] != `{"text":"pipeline: {}\n"}` {
		t.Fatalf("body %s", stub2.bodies[0])
	}
}

func TestPipelinePRUnchangedCarriesNoPullRequest(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	stub.post = func(w http.ResponseWriter, _ *http.Request) {
		prWriteJSON(w, 200, api.PipelinePRResponse{Outcome: api.PipelinePRUnchanged, BaseSHA: prBaseSHA, Note: "same"})
	}
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	got, err := r.open(appwire.FactoryPipelinePRParams{})
	if err != nil || got.Outcome != "unchanged" || got.PR != nil || got.BaseSHA != prBaseSHA || got.Repo != prRepo {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPipelinePRServesSoloAndTheOwnedLocalFactoryOnlyWithAHeldCredential(t *testing.T) {
	for name, c := range map[string]struct {
		local bool
		mode  string
	}{"attached solo": {false, api.FactorySolo}, "owned local": {true, api.FactorySolo}, "mode not learned": {false, ""}} {
		t.Run(name, func(t *testing.T) {
			stub := newPRStub(t, api.Me{Mode: api.FactorySolo})
			r := newPRWire(t, stub, c.local, c.mode, prToken)
			if _, err := r.open(appwire.FactoryPipelinePRParams{}); err != nil {
				t.Fatal(err)
			}
			if stub.posts() != 1 || stub.auths[0] != "Bearer "+prToken {
				t.Fatalf("requests %v auths %v", stub.requests(), stub.auths)
			}
		})
	}
}

func TestPipelinePRNeedsAHeldCredentialAndSendsNothingWithout(t *testing.T) {
	t.Setenv("GH_TOKEN", "ghp_ambient_host")
	t.Setenv("GITHUB_TOKEN", "ghp_ambient_host")
	t.Setenv("CODEAF_FACTORY_TOKEN", "tok_ambient_host_key")
	for name, c := range map[string]struct {
		local bool
		mode  string
		code  string
	}{"team": {false, api.FactoryTeam, appwire.CodeSignedOut}, "solo": {false, api.FactorySolo, appwire.CodeSignedOut}, "owned local": {true, api.FactorySolo, appwire.CodeUnreachable}} {
		t.Run(name, func(t *testing.T) {
			stub := newPRStub(t, api.Me{Mode: api.FactoryTeam, Operator: true})
			r := newPRWire(t, stub, c.local, c.mode, "")
			_, err := r.open(appwire.FactoryPipelinePRParams{})
			prWantCode(t, err, c.code)
			if got := stub.requests(); len(got) != 0 {
				t.Fatalf("a request left with no credential: %v", got)
			}
			r.hub.hold(r.id, prToken, time.Nanosecond)
			time.Sleep(2 * time.Millisecond)
			_, err = r.open(appwire.FactoryPipelinePRParams{})
			prWantCode(t, err, c.code)
			if got := stub.requests(); len(got) != 0 {
				t.Fatalf("a request left with an expired credential: %v", got)
			}
		})
	}
	// An unknown connection is not_found and sends nothing.
	stub := newPRStub(t, api.Me{})
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	_, err := r.backend.FactoryPipelinePR(context.Background(), appwire.FactoryPipelinePRParams{Factory: "factory_nope", Repo: prRepo, Text: "x"})
	prWantCode(t, err, appwire.CodeNotFound)
	if len(stub.requests()) != 0 {
		t.Fatalf("sent %v", stub.requests())
	}
}

func TestPipelinePRSendsNothingWhenWhoAmIFails(t *testing.T) {
	for _, status := range []int{401, 403, 500, 502} {
		stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
		stub.meStatus = status
		r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
		_, err := r.open(appwire.FactoryPipelinePRParams{})
		if err == nil || stub.posts() != 0 {
			t.Fatalf("status %d: %v after %d posts", status, err, stub.posts())
		}
		prNoNames(t, err)
	}
}

// ── the typed failures ─────────────────────────────────────────────────────

func TestPipelinePRTypedFailuresKeepTheRoutesOutcomeStageAndBranchLeft(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		f       api.PipelinePRFailure
		code    string
		outcome string
	}{
		{"no github credential", 401, api.PipelinePRFailure{Outcome: "refused", Stage: "credential"}, appwire.CodeGitHubExpired, "refused"},
		{"cannot push", 403, api.PipelinePRFailure{Outcome: "refused", Stage: "permission", GitHubStatus: 0}, appwire.CodeForbidden, "refused"},
		{"base moved", 409, api.PipelinePRFailure{Outcome: "refused", Stage: "base"}, appwire.CodeConflict, "refused"},
		{"branch exists", 409, api.PipelinePRFailure{Outcome: "refused", Stage: "branch", Branch: "codeaf/pipeline-x"}, appwire.CodeConflict, "refused"},
		{"bad input", 422, api.PipelinePRFailure{Outcome: "refused", Stage: "request"}, appwire.CodeBadParams, "refused"},
		{"already opening", 409, api.PipelinePRFailure{Outcome: "refused", Stage: "request"}, appwire.CodeBadParams, "refused"},
		{"repo hidden by github", 404, api.PipelinePRFailure{Outcome: "refused", Stage: "repository", GitHubStatus: 404}, appwire.CodeNotFound, "refused"},
		{"github down on a read", 502, api.PipelinePRFailure{Outcome: "not_opened", Stage: "repository"}, appwire.CodeEngine, "not_opened"},
		{"rate limited", 429, api.PipelinePRFailure{Outcome: "not_opened", Stage: "file", GitHubStatus: 429}, appwire.CodeEngine, "not_opened"},
		{"commit refused after branch", 422, api.PipelinePRFailure{Outcome: "not_opened", Stage: "commit", GitHubStatus: 422, Branch: "codeaf/pipeline-x", BranchLeft: true}, appwire.CodeEngine, "not_opened"},
		{"unknown branch create", 502, api.PipelinePRFailure{Outcome: "unknown", Stage: "branch", Branch: "codeaf/pipeline-x", BranchLeft: true}, appwire.CodeUncertain, "unknown"},
		{"unknown commit", 502, api.PipelinePRFailure{Outcome: "unknown", Stage: "commit", GitHubStatus: 500, Branch: "codeaf/pipeline-x", BranchLeft: true}, appwire.CodeUncertain, "unknown"},
		{"unknown pull request", 502, api.PipelinePRFailure{Outcome: "unknown", Stage: "pull_request", Branch: "codeaf/pipeline-x", BranchLeft: true}, appwire.CodeUncertain, "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
			stub.post = prFailure(c.status, c.f)
			r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
			_, err := r.open(appwire.FactoryPipelinePRParams{})
			we := prWantCode(t, err, c.code)
			d, ok := we.Detail.(appwire.FactoryPipelinePRFailure)
			if !ok {
				t.Fatalf("detail %T", we.Detail)
			}
			if d.Outcome != c.outcome || d.Stage != c.f.Stage || d.Branch != c.f.Branch || d.BranchLeft != c.f.BranchLeft ||
				d.GitHubStatus != c.f.GitHubStatus || d.RequestID != "9f8e7d6c" || d.Factory != r.id || d.Repo != prRepo {
				t.Fatalf("detail %+v", d)
			}
			// Fixed words only: nothing the factory wrote.
			blob, _ := json.Marshal(we)
			for _, leak := range []string{"ghp_leaked", "leaked_secret", "/v1/repos", prToken} {
				if strings.Contains(string(blob), leak) {
					t.Fatalf("leaked %q: %s", leak, blob)
				}
			}
			if c.outcome == "unknown" && strings.Contains(strings.ToLower(we.Message), "failed") {
				t.Fatalf("an unknown outcome was called a failure: %q", we.Message)
			}
			if c.f.BranchLeft && !strings.Contains(we.Message, "branch was left") {
				t.Fatalf("a left branch was not named in words: %q", we.Message)
			}
			if stub.posts() != 1 {
				t.Fatalf("posts = %d, want 1 (no retry)", stub.posts())
			}
		})
	}
}

// A 4xx or 5xx that is not the flow's own typed failure proves nothing, so it
// is uncertain, never "not opened", and it names nothing. One POST, always.
func TestPipelinePRAnythingElseIsUncertainAndNamesNothing(t *testing.T) {
	generic := func(status int) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, status, map[string]any{"error": "ghp_leaked at /v1/repos/octo/Hello.x_y 820", "reason": "not_allowed"})
		}
	}
	marked := func(status int) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
			generic(status)(w, r)
		}
	}
	text := func(status int, body string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status); _, _ = io.WriteString(w, body) }
	}
	ok201 := func(mut func(*api.PipelinePRResponse)) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			pr := api.PipelinePRResponse{Outcome: api.PipelinePROpened, BaseSHA: prBaseSHA,
				PR: &api.PipelinePRResult{Number: 820, URL: "https://github.com/octo/Hello.x_y/pull/820", Branch: "codeaf/leak", Base: "main", BaseSHA: prBaseSHA, HeadSHA: prHeadSHA}}
			mut(&pr)
			prWriteJSON(w, 201, pr)
		}
	}
	fail := func(status int, mut func(*api.PipelinePRFailure)) func(http.ResponseWriter, *http.Request) {
		f := api.PipelinePRFailure{Outcome: "refused", Stage: "permission", RequestID: "9f8e7d6c"}
		mut(&f)
		return prFailure(status, f)
	}
	cases := map[string]func(http.ResponseWriter, *http.Request){
		"bare 400": generic(400), "bare 401": generic(401), "bare 403": generic(403), "bare 404": generic(404), "bare 408": generic(408),
		"bare 409": generic(409), "bare 413": generic(413), "bare 422": generic(422), "bare 429": generic(429), "bare 500": generic(500),
		"bare 502": generic(502), "bare 503": generic(503), "bare 504": generic(504),
		"marked pre-mutation 403 without a typed body": marked(403), "marked 404": marked(404),
		"202 accepted": func(w http.ResponseWriter, r *http.Request) {
			rec := httptest.NewRecorder()
			prOpened("codeaf/leak", "")(rec, r)
			w.WriteHeader(202)
			_, _ = w.Write(rec.Body.Bytes())
		},
		"204": text(204, ""), "200 empty": text(200, ""), "201 empty": text(201, ""), "201 not json": text(201, "<html>ghp_leaked</html>"),
		"201 truncated": text(201, `{"outcome":"opened","pr":{"number":820,`), "200 json null": text(200, "null"), "201 array": text(201, "[]"),
		"301": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "http://127.0.0.1:1/x")
			w.WriteHeader(301)
		},
		"307": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "http://127.0.0.1:1/x")
			w.WriteHeader(307)
		},
		"201 other repository": prOpened("codeaf/leak", "evil/Hello.x_y"),
		"201 url case differs": prOpened("codeaf/leak", "octo/hello.x_y"),
		"201 number zero": ok201(func(p *api.PipelinePRResponse) {
			p.PR.Number = 0
			p.PR.URL = "https://github.com/octo/Hello.x_y/pull/0"
		}),
		"201 number negative": ok201(func(p *api.PipelinePRResponse) {
			p.PR.Number = -3
			p.PR.URL = "https://github.com/octo/Hello.x_y/pull/-3"
		}),
		"201 no pr":           ok201(func(p *api.PipelinePRResponse) { p.PR = nil }),
		"201 unchanged":       ok201(func(p *api.PipelinePRResponse) { p.Outcome = api.PipelinePRUnchanged }),
		"201 unknown outcome": ok201(func(p *api.PipelinePRResponse) { p.Outcome = "merged" }),
		"201 empty outcome":   ok201(func(p *api.PipelinePRResponse) { p.Outcome = "" }),
		"201 bad head sha":    ok201(func(p *api.PipelinePRResponse) { p.PR.HeadSHA = "abc" }),
		"201 bad base sha":    ok201(func(p *api.PipelinePRResponse) { p.BaseSHA = "abc"; p.PR.BaseSHA = "abc" }),
		"201 base mismatch":   ok201(func(p *api.PipelinePRResponse) { p.PR.BaseSHA = strings.Repeat("b", 40) }),
		"201 bad branch":      ok201(func(p *api.PipelinePRResponse) { p.PR.Branch = "a..b" }),
		"201 empty base":      ok201(func(p *api.PipelinePRResponse) { p.PR.Base = "" }),
		"201 huge number":     text(201, `{"outcome":"opened","pr":{"number":1e99}}`),
		"200 with pr": func(w http.ResponseWriter, r *http.Request) {
			prWriteJSON(w, 200, api.PipelinePRResponse{Outcome: api.PipelinePRUnchanged, BaseSHA: prBaseSHA, PR: &api.PipelinePRResult{Number: 1}})
		},
		"200 opened": func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 200, api.PipelinePRResponse{Outcome: api.PipelinePROpened, BaseSHA: prBaseSHA})
		},
		"200 unchanged no sha": func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 200, api.PipelinePRResponse{Outcome: api.PipelinePRUnchanged})
		},
		"failure unknown outcome":            fail(403, func(f *api.PipelinePRFailure) { f.Outcome = "denied" }),
		"failure empty outcome":              fail(403, func(f *api.PipelinePRFailure) { f.Outcome = "" }),
		"failure outcome opened":             fail(403, func(f *api.PipelinePRFailure) { f.Outcome = "opened" }),
		"failure unchanged":                  fail(403, func(f *api.PipelinePRFailure) { f.Outcome = "unchanged" }),
		"failure unknown stage":              fail(403, func(f *api.PipelinePRFailure) { f.Stage = "somewhere" }),
		"failure empty stage":                fail(403, func(f *api.PipelinePRFailure) { f.Stage = "" }),
		"failure no request id":              text(403, `{"error":"x","outcome":"refused","stage":"permission","branch_left":false}`),
		"failure hostile request id":         fail(403, func(f *api.PipelinePRFailure) { f.RequestID = "x\" ghp_leaked" }),
		"failure github status absurd":       fail(403, func(f *api.PipelinePRFailure) { f.GitHubStatus = 9999 }),
		"failure refused but branch left":    fail(403, func(f *api.PipelinePRFailure) { f.BranchLeft = true; f.Branch = "codeaf/leak" }),
		"failure refused at commit":          fail(403, func(f *api.PipelinePRFailure) { f.Stage = "commit" }),
		"failure refused 5xx":                fail(502, func(f *api.PipelinePRFailure) {}),
		"failure not_opened 500":             fail(500, func(f *api.PipelinePRFailure) { f.Outcome = "not_opened" }),
		"failure not_opened at commit no br": fail(422, func(f *api.PipelinePRFailure) { f.Outcome = "not_opened"; f.Stage = "commit" }),
		"failure left without a branch":      fail(502, func(f *api.PipelinePRFailure) { f.Outcome = "not_opened"; f.Stage = "commit"; f.BranchLeft = true }),
		"failure branch invalid": fail(502, func(f *api.PipelinePRFailure) {
			f.Outcome = "unknown"
			f.Stage = "commit"
			f.BranchLeft = true
			f.Branch = "a b\n"
		}),
		"failure unknown on 4xx": fail(422, func(f *api.PipelinePRFailure) {
			f.Outcome = "unknown"
			f.Stage = "commit"
			f.BranchLeft = true
			f.Branch = "x"
		}),
		"failure unknown at a read stage": fail(502, func(f *api.PipelinePRFailure) {
			f.Outcome = "unknown"
			f.Stage = "file"
			f.BranchLeft = true
			f.Branch = "x"
		}),
		"failure unknown not left": fail(502, func(f *api.PipelinePRFailure) { f.Outcome = "unknown"; f.Stage = "commit"; f.Branch = "x" }),
		"failure not json":         text(403, "nope"),
		"failure json null":        text(403, "null"),
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
			stub.post = h
			r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
			_, err := r.open(appwire.FactoryPipelinePRParams{})
			we := prWantCode(t, err, appwire.CodeUncertain)
			if we.Detail != nil {
				t.Fatalf("an uncertain answer carries a detail: %+v", we.Detail)
			}
			prNoNames(t, err, "octo", "Hello", "codeaf/leak", prBaseSHA, prHeadSHA)
			if stub.posts() != 1 {
				t.Fatalf("posts = %d, want exactly 1 (no retry, no downgrade)", stub.posts())
			}
			r.neverSaid(prToken, "ghp_leaked", "leaked_secret")
		})
	}
}

func TestPipelinePRRequestedBranchAndBaseMustBeWhatTheReplyConfirms(t *testing.T) {
	for name, c := range map[string]struct {
		p appwire.FactoryPipelinePRParams
		h func(http.ResponseWriter, *http.Request)
	}{
		"branch differs": {appwire.FactoryPipelinePRParams{Branch: "codeaf/mine"}, prOpened("codeaf/other", "")},
		"base differs":   {appwire.FactoryPipelinePRParams{ExpectedBaseSHA: strings.Repeat("b", 40)}, prOpened("codeaf/x", "")},
		"failure branch": {appwire.FactoryPipelinePRParams{Branch: "codeaf/mine"}, prFailure(502, api.PipelinePRFailure{Outcome: "unknown", Stage: "commit", Branch: "codeaf/other", BranchLeft: true})},
		"unchanged base": {appwire.FactoryPipelinePRParams{ExpectedBaseSHA: strings.Repeat("b", 40)}, func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 200, api.PipelinePRResponse{Outcome: api.PipelinePRUnchanged, BaseSHA: prBaseSHA})
		}},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
			stub.post = c.h
			r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
			_, err := r.open(c.p)
			prWantCode(t, err, appwire.CodeUncertain)
			if stub.posts() != 1 {
				t.Fatalf("posts = %d", stub.posts())
			}
		})
	}
}

// ── transport ──────────────────────────────────────────────────────────────

func TestPipelinePRALostConnectionIsUncertainAndIsNeverRetried(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	stub.post = func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	_, err := r.open(appwire.FactoryPipelinePRParams{Branch: "codeaf/mine"})
	prWantCode(t, err, appwire.CodeUncertain)
	prNoNames(t, err, "octo", "Hello", "codeaf/mine")
	time.Sleep(50 * time.Millisecond)
	if stub.posts() != 1 {
		t.Fatalf("posts = %d, want 1 (a lost reply is never resent)", stub.posts())
	}
}

func TestPipelinePRACancelledWaitIsUncertainAndNamesNothing(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	ctx, cancel := context.WithCancel(context.Background())
	stub.post = func(w http.ResponseWriter, req *http.Request) {
		cancel()
		time.Sleep(100 * time.Millisecond)
		prOpened("codeaf/x", "")(w, req)
	}
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	_, err := r.backend.FactoryPipelinePR(ctx, appwire.FactoryPipelinePRParams{Factory: r.id, Repo: prRepo, Text: "x"})
	prWantCode(t, err, appwire.CodeUncertain)
	if stub.posts() != 1 {
		t.Fatalf("posts = %d", stub.posts())
	}
}

func TestPipelinePRAServerThatIsDownIsUnreachableAndNothingIsSent(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	stub.srv.Close()
	_, err := r.open(appwire.FactoryPipelinePRParams{})
	prWantCode(t, err, appwire.CodeUnreachable)
}

func TestPipelinePRDoesNotFollowARedirect(t *testing.T) {
	var hits int
	var mu sync.Mutex
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		prOpened("codeaf/x", "")(w, r)
	}))
	defer other.Close()
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	for _, code := range []int{301, 302, 307, 308} {
		stub.post = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", other.URL+"/x")
			w.WriteHeader(code)
		}
		r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
		_, err := r.open(appwire.FactoryPipelinePRParams{})
		prWantCode(t, err, appwire.CodeUncertain)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("a redirect was followed %d times", hits)
	}
}

func TestPipelinePRAnOversizedAnswerIsUncertain(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	stub.post = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"outcome":"opened","note":"`+strings.Repeat("x", appFactorySmall+10)+`"}`)
	}
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	_, err := r.open(appwire.FactoryPipelinePRParams{})
	prWantCode(t, err, appwire.CodeUncertain)
}

func TestPipelinePRTheLongTimeoutIsOnlyForThisRouteAndTheHubClientIsUntouched(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	before := r.hub.client.Timeout
	if _, err := r.open(appwire.FactoryPipelinePRParams{}); err != nil {
		t.Fatal(err)
	}
	if r.hub.client.Timeout != before || before > 30*time.Second {
		t.Fatalf("hub client timeout moved: %v -> %v", before, r.hub.client.Timeout)
	}
	if appFactoryPipelinePRTimeout < 60*time.Second {
		t.Fatalf("the route's own flow takes up to 60s; %v would cut a success short", appFactoryPipelinePRTimeout)
	}
}

// ── the caller changes while the write is in flight ────────────────────────

func TestPipelinePRAnAnswerForAReplacedCallerRevealsNothing(t *testing.T) {
	answers := map[string]func(http.ResponseWriter, *http.Request){
		"opened": prOpened("codeaf/leak", ""),
		"unchanged": func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 200, api.PipelinePRResponse{Outcome: "unchanged", BaseSHA: prBaseSHA})
		},
		"refused": prFailure(403, api.PipelinePRFailure{Outcome: "refused", Stage: "permission"}),
		"unknown": prFailure(502, api.PipelinePRFailure{Outcome: "unknown", Stage: "commit", Branch: "codeaf/leak", BranchLeft: true}),
	}
	for answer, h := range answers {
		for change, swap := range map[string]func(*prWire){
			"replaced by another sign-in": func(r *prWire) { r.hub.hold(r.id, "tok_other_person_0123456789", 0) },
			"signed out":                  func(r *prWire) { r.hub.dropSession(r.id, prToken) },
		} {
			t.Run(answer+" / "+change, func(t *testing.T) {
				stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
				stub.post = h
				r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
				stub.onPost = func() { swap(r) }
				_, err := r.open(appwire.FactoryPipelinePRParams{Branch: "codeaf/leak"})
				we := prWantCode(t, err, appwire.CodeUncertain)
				if we.Detail != nil {
					t.Fatalf("stale metadata: %+v", we.Detail)
				}
				prNoNames(t, err, "octo", "Hello", "codeaf/leak", prBaseSHA, prHeadSHA, "tok_other_person")
				if stub.posts() != 1 {
					t.Fatalf("posts = %d", stub.posts())
				}
			})
		}
	}
}

func TestPipelinePRACredentialReplacedBeforeTheSendSendsNothing(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	var once sync.Once
	stub.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/v1/me" {
			once.Do(func() { r.hub.hold(r.id, "tok_other_person_0123456789", 0) })
			_ = json.NewEncoder(w).Encode(api.Me{Mode: api.FactoryTeam})
			return
		}
		t.Errorf("a request left after the sign-in changed: %s %s", req.Method, req.URL.Path)
	})
	_, err := r.open(appwire.FactoryPipelinePRParams{})
	if err == nil {
		t.Fatal("sent for a caller who was replaced")
	}
	if code := wireCodeOf(t, err); code != appwire.CodeConflict && code != appwire.CodeSignedOut {
		t.Fatalf("code %s", code)
	}
}

// ── one press, one request ─────────────────────────────────────────────────

func TestPipelinePRASecondCallForTheSameRepositoryIsBusyAndSendsNothing(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	release := make(chan struct{})
	entered := make(chan struct{})
	stub.post = func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		prOpened("codeaf/x", "")(w, r)
	}
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	done := make(chan error, 1)
	go func() { _, err := r.open(appwire.FactoryPipelinePRParams{}); done <- err }()
	<-entered
	for _, repo := range []string{prRepo, strings.ToUpper(prRepo)} {
		_, err := r.backend.FactoryPipelinePR(context.Background(), appwire.FactoryPipelinePRParams{Factory: r.id, Repo: repo, Text: "x"})
		prWantCode(t, err, appwire.CodeBusy)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if stub.posts() != 1 {
		t.Fatalf("posts = %d", stub.posts())
	}
	// Once it ended, the repository is free again.
	stub.post = nil
	if _, err := r.open(appwire.FactoryPipelinePRParams{}); err != nil {
		t.Fatal(err)
	}
}

// ── the wire ───────────────────────────────────────────────────────────────

func TestPipelinePROverTheWireRefusesExtrasAndNeverEchoesAToken(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactoryTeam})
	stub.post = prOpened("codeaf/x", "")
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	if !strings.Contains(r.hello, `"factory.pipeline_pr"`) {
		t.Fatalf("hello did not announce the feature: %s", r.hello)
	}
	var out map[string]any
	r.must("factory.pipeline_pr", map[string]any{"factory": r.id, "repo": prRepo, "text": "pipeline: {}\n"}, &out)
	if out["outcome"] != "opened" || out["repo"] != prRepo || out["factory"] != r.id {
		t.Fatalf("receipt %v", out)
	}
	before := len(stub.requests())
	for _, params := range []map[string]any{
		{"factory": r.id, "repo": prRepo},
		{"factory": r.id, "repo": prRepo, "text": "x", "reviewers": []string{"mona"}},
		{"factory": r.id, "repo": prRepo, "text": "x", "token": "ghp_x"},
		{"factory": r.id, "repo": prRepo, "text": "x", "path": "/v1/repos/other/other/pipeline/open-pr"},
		{"factory": r.id, "repo": "octo/../x", "text": "x"},
		{"factory": r.id, "repo": prRepo, "text": "x", "branch": "-bad"},
		{"factory": r.id, "repo": prRepo, "text": "x", "expected_base_sha": "abc"},
	} {
		r.mustFail("factory.pipeline_pr", params, appwire.CodeBadParams)
	}
	// A request whose encoding passes 1 MiB (every "<" is six bytes in JSON) is
	// refused before the sign-in is even asked.
	r.mustFail("factory.pipeline_pr", map[string]any{"factory": r.id, "repo": prRepo, "text": strings.Repeat("<", appwire.PipelinePRTextMax)}, appwire.CodeBadParams)
	if len(stub.requests()) != before {
		t.Fatalf("a bad request reached the factory: %v", stub.requests()[before:])
	}
	r.neverSaid(prToken)

	// A typed failure travels with its detail, over the real wire.
	stub.post = prFailure(502, api.PipelinePRFailure{Outcome: "unknown", Stage: "pull_request", Branch: "codeaf/x", BranchLeft: true})
	_, code, msg := r.call("factory.pipeline_pr", map[string]any{"factory": r.id, "repo": prRepo, "text": "pipeline: {}\n"})
	if code != appwire.CodeUncertain || strings.Contains(msg, "ghp_") {
		t.Fatalf("%s: %s", code, msg)
	}
	r.neverSaid(prToken, "ghp_leaked", "leaked_secret")
	r.mu.Lock()
	defer r.mu.Unlock()
	last := r.lines[len(r.lines)-1]
	for _, want := range []string{`"detail"`, `"outcome":"unknown"`, `"stage":"pull_request"`, `"branch_left":true`, `"request_id":"9f8e7d6c"`} {
		if !strings.Contains(last, want) {
			t.Fatalf("the typed failure lost %s: %s", want, last)
		}
	}
}

func TestPipelinePRIsAdvertisedInCapabilities(t *testing.T) {
	stub := newPRStub(t, api.Me{})
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	caps, err := r.backend.FactoryCapabilities(context.Background())
	if err != nil || !caps.PipelinePR {
		t.Fatalf("%+v %v", caps, err)
	}
}
