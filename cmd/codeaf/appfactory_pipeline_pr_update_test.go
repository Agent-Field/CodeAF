package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

const (
	prUpdateBranch = "codeaf/pipeline-open"
	prOldHeadSHA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// prUpdated answers the route's 200 for a commit added to pull request 820.
func prUpdated(outcome, head string, edit func(*api.PipelinePRResult)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		pr := &api.PipelinePRResult{Number: 820, URL: "https://github.com/" + prRepo + "/pull/820", Branch: prUpdateBranch, Base: "main", BaseSHA: prBaseSHA, HeadSHA: head}
		if edit != nil {
			edit(pr)
		}
		prWriteJSON(w, 200, api.PipelinePRResponse{Factory: api.Factory{Name: "Acme"}, Outcome: outcome, PR: pr, BaseSHA: prBaseSHA, Note: "ghp_leaked"})
	}
}

func TestPipelinePRUpdateSendsPRAndExpectedHeadAndReadsUpdated(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactorySolo})
	stub.post = prUpdated(api.PipelinePRUpdated, prHeadSHA, nil)
	r := newPRWire(t, stub, false, api.FactorySolo, prToken)
	got, err := r.open(appwire.FactoryPipelinePRParams{Title: "T", PR: 820, ExpectedHeadSHA: prOldHeadSHA})
	if err != nil {
		t.Fatal(err)
	}
	want := appwire.FactoryPipelinePR{Number: 820, URL: "https://github.com/" + prRepo + "/pull/820", Branch: prUpdateBranch, Base: "main", BaseSHA: prBaseSHA, HeadSHA: prHeadSHA}
	if got.Outcome != appwire.PipelinePROutcomeUpdated || got.PR == nil || *got.PR != want || got.BaseSHA != prBaseSHA || got.Repo != prRepo {
		t.Fatalf("receipt %+v", got)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(stub.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 4 || sent["pr"] != float64(820) || sent["expected_head_sha"] != prOldHeadSHA || sent["title"] != "T" {
		t.Fatalf("body %s", stub.bodies[0])
	}
	if stub.posts() != 1 {
		t.Fatalf("posts %d", stub.posts())
	}
}

func TestPipelinePRUpdateUnchangedCarriesThePullRequestsHead(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactorySolo})
	stub.post = prUpdated(api.PipelinePRUnchanged, prOldHeadSHA, nil)
	r := newPRWire(t, stub, false, api.FactorySolo, prToken)
	got, err := r.open(appwire.FactoryPipelinePRParams{PR: 820, ExpectedHeadSHA: prOldHeadSHA})
	if err != nil || got.Outcome != "unchanged" || got.PR == nil || got.PR.HeadSHA != prOldHeadSHA || got.PR.Number != 820 {
		t.Fatalf("%+v %v", got, err)
	}
}

// An answer that is not about the pull request asked for, or that contradicts
// itself, is uncertain and names nothing.
func TestPipelinePRUpdateAnswersThatDisagreeAreUncertain(t *testing.T) {
	for name, post := range map[string]func(http.ResponseWriter, *http.Request){
		"opened instead":  prOpened(prUpdateBranch, prRepo),
		"other number":    prUpdated(api.PipelinePRUpdated, prHeadSHA, func(p *api.PipelinePRResult) { p.Number = 821; p.URL = strings.Replace(p.URL, "820", "821", 1) }),
		"other repo url":  prUpdated(api.PipelinePRUpdated, prHeadSHA, func(p *api.PipelinePRResult) { p.URL = "https://github.com/x/y/pull/820" }),
		"head not moved":  prUpdated(api.PipelinePRUpdated, prOldHeadSHA, nil),
		"bad head":        prUpdated(api.PipelinePRUpdated, "main", nil),
		"bad branch":      prUpdated(api.PipelinePRUpdated, prHeadSHA, func(p *api.PipelinePRResult) { p.Branch = "a..b" }),
		"base disagrees":  prUpdated(api.PipelinePRUpdated, prHeadSHA, func(p *api.PipelinePRResult) { p.BaseSHA = prHeadSHA }),
		"unchanged moved": prUpdated(api.PipelinePRUnchanged, prHeadSHA, nil),
		"unchanged bare": func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 200, api.PipelinePRResponse{Outcome: api.PipelinePRUnchanged, BaseSHA: prBaseSHA})
		},
		"updated as 201": func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRewriter{ResponseWriter: w, status: 201}
			prUpdated(api.PipelinePRUpdated, prHeadSHA, nil)(rec, r)
		},
		"left a branch": prFailure(502, api.PipelinePRFailure{Outcome: "unknown", Stage: "commit", Branch: prUpdateBranch, BranchLeft: true}),
		"opening stage": prFailure(422, api.PipelinePRFailure{Outcome: "not_opened", Stage: "pull_request", GitHubStatus: 422}),
		"branch stage":  prFailure(409, api.PipelinePRFailure{Outcome: "refused", Stage: "branch"}),
	} {
		t.Run(name, func(t *testing.T) {
			stub := newPRStub(t, api.Me{Mode: api.FactorySolo})
			stub.post = post
			r := newPRWire(t, stub, false, api.FactorySolo, prToken)
			_, err := r.open(appwire.FactoryPipelinePRParams{PR: 820, ExpectedHeadSHA: prOldHeadSHA})
			prWantCode(t, err, appwire.CodeUncertain)
			prNoNames(t, err, prUpdateBranch)
			if stub.posts() != 1 {
				t.Fatalf("posts %d", stub.posts())
			}
		})
	}
	// Without pr, an "updated" answer is not believed either.
	stub := newPRStub(t, api.Me{Mode: api.FactorySolo})
	stub.post = prUpdated(api.PipelinePRUpdated, prHeadSHA, nil)
	r := newPRWire(t, stub, false, api.FactorySolo, prToken)
	_, err := r.open(appwire.FactoryPipelinePRParams{})
	prWantCode(t, err, appwire.CodeUncertain)
}

func TestPipelinePRUpdateTypedFailures(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		f      api.PipelinePRFailure
		code   string
		words  string
	}{
		{"closed or moved", 409, api.PipelinePRFailure{Outcome: "refused", Stage: "head"}, appwire.CodeConflict, "pull request"},
		{"missing", 403, api.PipelinePRFailure{Outcome: "refused", Stage: "head", GitHubStatus: 404}, appwire.CodeConflict, "pull request"},
		{"file moved", 409, api.PipelinePRFailure{Outcome: "not_opened", Stage: "commit", GitHubStatus: 409, Branch: prUpdateBranch}, appwire.CodeEngine, "not updated"},
		{"commit unknown", 502, api.PipelinePRFailure{Outcome: "unknown", Stage: "commit", GitHubStatus: 500, Branch: prUpdateBranch}, appwire.CodeUncertain, "updated"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub := newPRStub(t, api.Me{Mode: api.FactorySolo})
			stub.post = prFailure(c.status, c.f)
			r := newPRWire(t, stub, false, api.FactorySolo, prToken)
			_, err := r.open(appwire.FactoryPipelinePRParams{PR: 820})
			we := prWantCode(t, err, c.code)
			d, ok := we.Detail.(appwire.FactoryPipelinePRFailure)
			if !ok || d.Outcome != c.f.Outcome || d.Stage != c.f.Stage || d.BranchLeft || d.Branch != c.f.Branch {
				t.Fatalf("detail %+v", we.Detail)
			}
			if !strings.Contains(we.Message, c.words) || strings.Contains(we.Message, "opened") || strings.Contains(we.Message, "branch was left") {
				t.Fatalf("words %q", we.Message)
			}
		})
	}
}

func TestPipelinePRUpdateIsAdvertised(t *testing.T) {
	stub := newPRStub(t, api.Me{})
	r := newPRWire(t, stub, false, api.FactoryTeam, prToken)
	caps, err := r.backend.FactoryCapabilities(context.Background())
	if err != nil || !caps.PipelinePRUpdate {
		t.Fatalf("%+v %v", caps, err)
	}
	var e appwire.Engine = r.backend
	if _, ok := e.(appwire.FactoryPipelinePRUpdater); !ok {
		t.Fatal("the app backend does not update pull requests")
	}
}

// statusRewriter answers a different status than the handler wrote.
type statusRewriter struct {
	http.ResponseWriter
	status int
}

func (s *statusRewriter) WriteHeader(int) { s.ResponseWriter.WriteHeader(s.status) }

// The complete appwire -> app backend -> real factory handler -> simulated
// GitHub path for a commit added to an open pull request. No real GitHub
// request, credential, model call or runner is used.
func TestPipelinePRUpdateWireAgainstTheRealFactoryHandler(t *testing.T) {
	const head = "4444444444444444444444444444444444444444"
	const base = "5555555555555555555555555555555555555555"
	const next = "3333333333333333333333333333333333333333"
	const text = "pipeline:\n  issue: [write, proof]\n"
	file, err := pipeline.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := pipeline.Encode(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"updated", "unchanged", "head_moved", "closed", "lost_commit_answer"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			var writes []string
			client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				defer mu.Unlock()
				if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer simulated-profile-credential" {
					t.Error("GitHub request escaped its credential/origin scope")
				}
				status, body := 200, `{}`
				switch path := r.URL.Path; {
				case r.Method == "GET" && path == "/repos/acme/api":
					body = `{"default_branch":"main","permissions":{"push":true}}`
				case r.Method == "GET" && path == "/repos/acme/api/pulls/42":
					state := "open"
					if scenario == "closed" {
						state = "closed"
					}
					body = `{"number":42,"state":"` + state + `","head":{"ref":"codeaf/pipeline-open","sha":"` + head + `","repo":{"full_name":"acme/api"}},"base":{"ref":"main","sha":"` + base + `","repo":{"full_name":"acme/api"}}}`
				case r.Method == "GET" && path == "/repos/acme/api/git/ref/heads/codeaf/pipeline-open":
					body = `{"object":{"type":"commit","sha":"` + head + `"}}`
				case r.Method == "GET" && path == "/repos/acme/api/contents/"+factory.PipelineFile:
					if r.URL.Query().Get("ref") != head {
						t.Error("file was not read at the pull request's head")
					}
					old := []byte("default_budget: 3\n")
					if scenario == "unchanged" {
						old = canonical
					}
					data, _ := json.Marshal(map[string]any{"type": "file", "encoding": "base64", "sha": strings.Repeat("2", 40), "content": base64.StdEncoding.EncodeToString(old)})
					body = string(data)
				case r.Method == "PUT" && path == "/repos/acme/api/contents/"+factory.PipelineFile:
					writes = append(writes, "commit")
					if scenario == "lost_commit_answer" {
						return nil, errors.New("simulated lost answer")
					}
					var ask struct{ Content, Branch, SHA string }
					if json.NewDecoder(r.Body).Decode(&ask) != nil || ask.Branch != "codeaf/pipeline-open" || ask.SHA != strings.Repeat("2", 40) || ask.Content != base64.StdEncoding.EncodeToString(canonical) {
						t.Error("the commit differed from the reviewed pipeline or its pull request")
					}
					body = `{"commit":{"sha":"` + next + `"}}`
				default:
					t.Errorf("unexpected simulated GitHub request: %s %s", r.Method, path)
					status = 500
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			h := serve.New(factory.Seam{}, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "alice", GitHubToken: func(context.Context) string { return "simulated-profile-credential" }, GitHubHTTPClient: client})
			srv := httptest.NewServer(h)
			t.Cleanup(func() { srv.Close(); h.Close() })
			w := newFactoryWire(t, testFactoryHub(t))
			if !strings.Contains(w.hello, `"`+appwire.FeatureFactoryPipelinePRUpdate+`"`) {
				t.Fatalf("hello does not announce %s", appwire.FeatureFactoryPipelinePRUpdate)
			}
			var conn appwire.FactoryConnection
			w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
			expected := head
			if scenario == "head_moved" {
				expected = strings.Repeat("6", 40)
			}
			params := appwire.FactoryPipelinePRParams{Factory: conn.ID, Repo: "acme/api", Text: text, Title: "Factory pipeline: tighten proof", PR: 42, ExpectedHeadSHA: expected}
			result, code, message := w.call("factory.pipeline_pr", params)
			mu.Lock()
			defer mu.Unlock()
			var done appwire.FactoryPipelinePRDone
			_ = json.Unmarshal(result, &done)
			switch scenario {
			case "updated":
				if code != "" || done.Outcome != "updated" || done.PR == nil || done.PR.Number != 42 || done.PR.HeadSHA != next ||
					done.PR.Branch != "codeaf/pipeline-open" || done.BaseSHA != base || strings.Join(writes, ",") != "commit" {
					t.Fatalf("%s %s: %+v %v", code, message, done, writes)
				}
			case "unchanged":
				if code != "" || done.Outcome != "unchanged" || done.PR == nil || done.PR.HeadSHA != head || len(writes) != 0 {
					t.Fatalf("%s %s: %+v %v", code, message, done, writes)
				}
			case "head_moved", "closed":
				if code != appwire.CodeConflict || len(writes) != 0 {
					t.Fatalf("%s %s %v", code, message, writes)
				}
			case "lost_commit_answer":
				if code != appwire.CodeUncertain || strings.Contains(message, "branch was left") || strings.Join(writes, ",") != "commit" {
					t.Fatalf("%s %s %v", code, message, writes)
				}
			}
		})
	}
}

// TEXT THAT SPELLS A RUNTIME KEY keeps its seal with pr: with no attested read
// of the file it is refused and nothing is posted.
func TestPipelinePRUpdateRuntimeTextNeedsTheAttestedFile(t *testing.T) {
	stub := newPRStub(t, api.Me{Mode: api.FactorySolo})
	r := newPRWire(t, stub, false, api.FactorySolo, prToken)
	_, err := r.open(appwire.FactoryPipelinePRParams{Text: "steps:\n  plan:\n    effort: strong\n    prompt: p\n", PR: 820})
	if err == nil || stub.posts() != 0 {
		t.Fatalf("%v after %d posts", err, stub.posts())
	}
}
