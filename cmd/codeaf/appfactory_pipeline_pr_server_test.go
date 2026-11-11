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

// The complete appwire -> app backend -> real HTTP handler -> simulated GitHub
// path. No real GitHub requests, credentials, model calls or runner are used.
func TestPipelinePRWireAgainstTheRealFactoryHandler(t *testing.T) {
	const tip = "1111111111111111111111111111111111111111"
	const head = "3333333333333333333333333333333333333333"
	const branch = "codeaf/wire-integration"
	const text = "pipeline:\n  issue: [write, proof]\n"
	for _, scenario := range []string{"opened", "unchanged", "stale_base", "lost_branch_answer"} {
		t.Run(scenario, func(t *testing.T) {
			file, err := pipeline.Parse(text)
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := pipeline.Encode(file)
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var writes []string
			client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				defer mu.Unlock()
				if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer simulated-profile-credential" {
					t.Error("GitHub request escaped its credential/origin scope")
				}
				status, body := 200, `{}`
				path := r.URL.Path
				switch {
				case r.Method == "GET" && path == "/repos/acme/api":
					body = `{"default_branch":"main","permissions":{"push":true}}`
				case r.Method == "GET" && path == "/repos/acme/api/git/ref/heads/main":
					body = `{"object":{"type":"commit","sha":"` + tip + `"}}`
				case r.Method == "GET" && path == "/repos/acme/api/contents/"+factory.PipelineFile:
					if r.URL.Query().Get("ref") != tip {
						t.Error("file was not pinned to its base")
					}
					old := []byte("default_budget: 3\n")
					if scenario == "unchanged" {
						old = canonical
					}
					data, _ := json.Marshal(map[string]any{"type": "file", "encoding": "base64", "sha": strings.Repeat("2", 40), "content": base64.StdEncoding.EncodeToString(old)})
					body = string(data)
				case r.Method == "GET" && path == "/repos/acme/api/git/ref/heads/"+branch:
					status = 404
				case r.Method == "POST" && path == "/repos/acme/api/git/refs":
					writes = append(writes, "branch")
					if scenario == "lost_branch_answer" {
						return nil, errors.New("simulated lost answer")
					}
					status, body = 201, `{"ref":"refs/heads/`+branch+`","object":{"type":"commit","sha":"`+tip+`"}}`
				case r.Method == "PUT" && path == "/repos/acme/api/contents/"+factory.PipelineFile:
					writes = append(writes, "commit")
					var ask struct{ Content, Branch, SHA string }
					if json.NewDecoder(r.Body).Decode(&ask) != nil || ask.Branch != branch || ask.Content != base64.StdEncoding.EncodeToString(canonical) {
						t.Error("the commit differed from the reviewed pipeline")
					}
					status, body = 200, `{"commit":{"sha":"`+head+`"}}`
				case r.Method == "POST" && path == "/repos/acme/api/pulls":
					writes = append(writes, "pull_request")
					status, body = 201, `{"number":42,"html_url":"https://github.com/acme/api/pull/42","head":{"ref":"`+branch+`","sha":"`+head+`"},"base":{"ref":"main","sha":"`+tip+`"}}`
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
			var conn appwire.FactoryConnection
			w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
			expected := tip
			if scenario == "stale_base" {
				expected = strings.Repeat("4", 40)
			}
			params := appwire.FactoryPipelinePRParams{Factory: conn.ID, Repo: "acme/api", Text: text, Branch: branch, ExpectedBaseSHA: expected}
			result, code, message := w.call("factory.pipeline_pr", params)
			mu.Lock()
			defer mu.Unlock()
			switch scenario {
			case "opened", "unchanged":
				if code != "" {
					t.Fatalf("real handler outcome: %s: %s", code, message)
				}
				var done appwire.FactoryPipelinePRDone
				if json.Unmarshal(result, &done) != nil || done.Outcome != scenario || done.BaseSHA != tip || done.Factory != conn.ID || done.Repo != "acme/api" {
					t.Fatalf("receipt: %+v", done)
				}
				if scenario == "opened" && (done.PR == nil || done.PR.Number != 42 || strings.Join(writes, ",") != "branch,commit,pull_request") {
					t.Fatalf("opened receipt %+v; writes %v", done, writes)
				}
				if scenario == "unchanged" && (done.PR != nil || len(writes) != 0) {
					t.Fatal("unchanged made a write or claimed a PR")
				}
			default:
				if code == "" {
					t.Fatal("failed flow became a success")
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
				var failure appwire.FactoryPipelinePRFailure
				if json.Unmarshal(frame.Error.Detail, &failure) != nil {
					t.Fatal("missing typed failure")
				}
				if scenario == "stale_base" && (failure.Outcome != "refused" || failure.Stage != "base" || len(writes) != 0) {
					t.Fatalf("stale base: %+v %v", failure, writes)
				}
				if scenario == "lost_branch_answer" && (failure.Outcome != "unknown" || !failure.BranchLeft || failure.Branch != branch || strings.Join(writes, ",") != "branch") {
					t.Fatalf("lost answer: %+v %v", failure, writes)
				}
			}
		})
	}
}
