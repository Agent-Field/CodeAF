package main

import (
	"context"
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
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// The complete appwire -> app backend -> real factory handler -> simulated
// GitHub path for factory.issue_comment. No real GitHub request is made.
func TestIssueCommentWireAgainstTheRealFactoryHandler(t *testing.T) {
	for _, scenario := range []string{"posted", "pull_request", "not_watched", "forbidden", "lost"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			var posts []string
			client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				defer mu.Unlock()
				if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer simulated-profile-credential" {
					t.Error("GitHub request escaped its credential/origin scope")
				}
				status, body := 200, `{"number":7,"title":"Flaky login"}`
				switch {
				case r.Method == "GET" && r.URL.Path == "/repos/acme/api/issues/7":
					if scenario == "pull_request" {
						body = `{"number":7,"pull_request":{"url":"x"}}`
					}
				case r.Method == "POST" && r.URL.Path == "/repos/acme/api/issues/7/comments":
					raw, _ := io.ReadAll(r.Body)
					posts = append(posts, string(raw))
					switch scenario {
					case "forbidden":
						status, body = 403, `{"message":"Must have admin rights"}`
					case "lost":
						return nil, errors.New("simulated lost answer")
					default:
						status, body = 201, `{"id":9001,"html_url":"https://github.com/acme/api/issues/7#issuecomment-9001","created_at":"2026-11-01T12:00:00Z"}`
					}
				default:
					t.Errorf("unexpected simulated GitHub request: %s %s", r.Method, r.URL.Path)
					status = 500
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "not_watched" {
				if err := st.SetRepos([]string{"acme/api"}); err != nil {
					t.Fatal(err)
				}
			}
			h := serve.New(factory.Seam{}, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "alice", Store: st,
				GitHubToken: func(context.Context) string { return "simulated-profile-credential" }, GitHubHTTPClient: client})
			srv := httptest.NewServer(h)
			t.Cleanup(func() { srv.Close(); h.Close() })
			w := newFactoryWire(t, testFactoryHub(t))
			if !strings.Contains(w.hello, appwire.FeatureFactoryIssueComment) {
				t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryIssueComment, w.hello)
			}
			var caps appwire.FactoryCapabilities
			w.must("factory.capabilities", map[string]any{}, &caps)
			if !caps.IssueComment {
				t.Fatalf("capabilities %+v", caps)
			}
			var conn appwire.FactoryConnection
			w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
			params := appwire.FactoryIssueCommentParams{Factory: conn.ID, Repo: "acme/api", Number: 7, Body: "Which browser shows it?"}
			result, code, message := w.call("factory.issue_comment", params)
			mu.Lock()
			defer mu.Unlock()
			if scenario == "posted" {
				if code != "" {
					t.Fatalf("real handler outcome: %s: %s", code, message)
				}
				const receipt = `"repo":"acme/api","number":7,"comment":{"id":9001,"url":"https://github.com/acme/api/issues/7#issuecomment-9001","created_at":"2026-11-01T12:00:00Z"}}`
				if !strings.Contains(string(result), `"factory":"`+conn.ID+`",`+receipt) || len(posts) != 1 || posts[0] != `{"body":"Which browser shows it?"}` {
					t.Fatalf("receipt %s; posts %v", result, posts)
				}
				return
			}
			w.mu.Lock()
			last := append([]byte(nil), w.lines[len(w.lines)-1]...)
			w.mu.Unlock()
			var frame struct {
				Error struct {
					Detail json.RawMessage `json:"detail"`
				} `json:"error"`
			}
			if json.Unmarshal(last, &frame) != nil {
				t.Fatal("missing wire error")
			}
			var failure appwire.FactoryIssueCommentFailure
			if json.Unmarshal(frame.Error.Detail, &failure) != nil {
				t.Fatalf("missing typed failure: %s", last)
			}
			want := map[string]struct {
				code, outcome, stage string
				posts                int
			}{
				"pull_request": {appwire.CodeBadParams, "refused", "issue", 0},
				"not_watched":  {appwire.CodeNotFound, "refused", "repository", 0},
				"forbidden":    {appwire.CodeForbidden, "refused", "comment", 1},
				"lost":         {appwire.CodeUncertain, "unknown", "comment", 1},
			}[scenario]
			if code != want.code || failure.Outcome != want.outcome || failure.Stage != want.stage || len(posts) != want.posts ||
				failure.Factory != conn.ID || failure.Repo != "acme/api" || failure.Number != 7 || failure.RequestID == "" {
				t.Fatalf("%s: code %s %+v posts %v", scenario, code, failure, posts)
			}
			if strings.Contains(message, "admin rights") || strings.Contains(string(last), "simulated-profile-credential") {
				t.Fatalf("upstream words or the credential leaked: %s", last)
			}
		})
	}
}

// Anything the engine does not recognise is uncertain, never a claim either way.
func TestIssueCommentUnrecognisedAnswersAreUncertain(t *testing.T) {
	p := appwire.FactoryIssueCommentParams{Factory: "f", Repo: "acme/api", Number: 7, Body: "x"}
	for name, r := range map[string]linkReply{
		"bare 4xx":       {status: 403, body: []byte(`{"error":"only the operator can do this"}`)},
		"unknown stage":  {status: 403, body: []byte(`{"outcome":"refused","stage":"vibes","request_id":"ab"}`)},
		"refused at 500": {status: 500, body: []byte(`{"outcome":"refused","stage":"comment","request_id":"ab"}`)},
		"unknown at 403": {status: 403, body: []byte(`{"outcome":"unknown","stage":"comment","request_id":"ab"}`)},
		"unknown early":  {status: 502, body: []byte(`{"outcome":"unknown","stage":"issue","request_id":"ab"}`)},
		"200":            {status: 200, body: []byte(`{"repo":"acme/api","number":7,"comment":{"id":1,"url":"https://github.com/acme/api/issues/7#issuecomment-1","created_at":"2026-11-01T12:00:00Z"}}`)},
		"other issue":    {status: 201, body: []byte(`{"repo":"acme/api","number":8,"comment":{"id":1,"url":"https://github.com/acme/api/issues/8#issuecomment-1","created_at":"2026-11-01T12:00:00Z"}}`)},
		"other url":      {status: 201, body: []byte(`{"repo":"acme/api","number":7,"comment":{"id":1,"url":"https://evil.example/x","created_at":"2026-11-01T12:00:00Z"}}`)},
		"no id":          {status: 201, body: []byte(`{"repo":"acme/api","number":7,"comment":{"url":"https://github.com/acme/api/issues/7#issuecomment-0","created_at":"2026-11-01T12:00:00Z"}}`)},
		"no time":        {status: 201, body: []byte(`{"repo":"acme/api","number":7,"comment":{"id":1,"url":"https://github.com/acme/api/issues/7#issuecomment-1"}}`)},
		"unreadable":     {status: 201, bodyBad: true},
	} {
		if done, failure := classifyIssueComment(r, p); done != nil || failure != nil {
			t.Errorf("%s was believed: %+v %+v", name, done, failure)
		}
	}
}
