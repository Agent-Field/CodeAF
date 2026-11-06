package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

type repoIssuesFactory struct {
	mu   sync.Mutex
	asks []string
}

func attachRepoIssuesFactory(t *testing.T, status int, body string) (*factoryWire, string, *repoIssuesFactory) {
	t.Helper()
	f := &repoIssuesFactory{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/about", "/v1/me":
			_ = json.NewEncoder(w).Encode(api.Me{Contract: api.Contract, Factory: api.Factory{Name: "acme"}, Mode: api.FactoryTeam})
		case "/v1/repos/acme/api/issues":
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer "+testHostToken {
				t.Errorf("issue request = %s, authorization %q", r.Method, r.Header.Get("Authorization"))
			}
			f.mu.Lock()
			f.asks = append(f.asks, r.URL.RawQuery)
			f.mu.Unlock()
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	wire := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	wire.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	return wire, conn.ID, f
}

// C1, C2, C3, C4: The real wire advertises the read and carries its fields,
// caller credential, default, cursor, row count and PR-inclusive newest.
func TestAppFactoryRepoIssuesAdvertisesAndReadsThroughTheConnection(t *testing.T) {
	body := `{"repo":"acme/api","issues":[{"id":9007199254740993,"number":7,"title":"Retry","body":"details","body_cut":true,"url":"https://github.com/acme/api/issues/7","state":"closed","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-11-01T01:00:00Z"}],"rows":2,"newest":"2026-11-01T02:00:00Z"}`
	wire, id, f := attachRepoIssuesFactory(t, 200, body)
	if !strings.Contains(wire.hello, appwire.FeatureFactoryRepoIssues) {
		t.Fatalf("hello = %s", wire.hello)
	}
	var caps appwire.FactoryCapabilities
	wire.must("factory.capabilities", map[string]any{}, &caps)
	if !caps.RepoIssues {
		t.Fatalf("capabilities = %+v", caps)
	}
	const since = "2026-11-01T01:02:03.123+05:30"
	var got appwire.FactoryRepoIssues
	wire.must("factory.repo_issues", map[string]any{"factory": id, "repo": " acme/api ", "since": since, "page": 10}, &got)
	if got.Factory != id || got.Repo != "acme/api" || got.Rows != 2 || got.Newest == nil || len(got.Issues) != 1 {
		t.Fatalf("result = %+v", got)
	}
	if i := got.Issues[0]; i.ID != 9007199254740993 || i.Number != 7 || i.Title != "Retry" || i.Body != "details" || !i.BodyCut ||
		i.State != "closed" || i.URL != "https://github.com/acme/api/issues/7" || i.CreatedAt.Format(time.RFC3339) != "2026-10-01T00:00:00Z" ||
		i.UpdatedAt.Format(time.RFC3339) != "2026-11-01T01:00:00Z" || got.Newest.Format(time.RFC3339) != "2026-11-01T02:00:00Z" {
		t.Fatalf("issue = %+v, newest = %v", i, got.Newest)
	}
	wire.must("factory.repo_issues", map[string]any{"factory": id, "repo": "acme/api"}, &got)
	f.mu.Lock()
	defer f.mu.Unlock()
	want := url.Values{"since": {since}, "page": {"10"}}.Encode() + " page=1"
	if strings.Join(f.asks, " ") != want {
		t.Fatalf("queries = %v, want %s", f.asks, want)
	}
}

// C4: Invalid params never reach the Factory's HTTP route.
func TestAppFactoryRepoIssuesBadParamsSendNothing(t *testing.T) {
	wire, id, f := attachRepoIssuesFactory(t, 200, `{"repo":"acme/api","issues":[],"rows":0}`)
	for _, bad := range []map[string]any{
		{"factory": id, "repo": "acme/api", "page": 11},
		{"factory": id, "repo": "acme/api", "page": 0},
		{"factory": id, "repo": "acme/api", "since": "yesterday"},
		{"factory": id, "repo": "acme"},
		{"factory": id, "repo": "acme/api", "state": "open"},
	} {
		wire.mustFail("factory.repo_issues", bad, appwire.CodeBadParams)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.asks) != 0 {
		t.Fatalf("refused reads sent %v", f.asks)
	}
}

func repoIssuesErrorDetail(t *testing.T, wire *factoryWire, id, want string) json.RawMessage {
	t.Helper()
	wire.mustFail("factory.repo_issues", map[string]any{"factory": id, "repo": "acme/api"}, want)
	wire.mu.Lock()
	defer wire.mu.Unlock()
	for i := len(wire.lines) - 1; i >= 0; i-- {
		var frame struct {
			ID    int `json:"id"`
			Error *struct {
				Detail json.RawMessage `json:"detail"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(wire.lines[i]), &frame) == nil && frame.ID == wire.next && frame.Error != nil {
			return frame.Error.Detail
		}
	}
	t.Fatal("no error frame")
	return nil
}

// C6: The wire's busy refusal preserves the Factory's RFC 3339 retry time.
func TestAppFactoryRepoIssuesPreservesRateLimitRetryAt(t *testing.T) {
	wire, id, _ := attachRepoIssuesFactory(t, 429, `{"error":"GitHub rate limit","retry_at":"2026-11-01T03:00:00Z"}`)
	raw := repoIssuesErrorDetail(t, wire, id, appwire.CodeBusy)
	var detail api.RepoIssuesRetry
	if json.Unmarshal(raw, &detail) != nil || detail.RetryAt.Format(time.RFC3339) != "2026-11-01T03:00:00Z" {
		t.Fatalf("detail = %s", raw)
	}
}

// C5, C7: A missing route is too old; a repository's 404 remains not_found.
func TestAppFactoryRepoIssuesDistinguishesAnOlderFactoryFromAMissingRepository(t *testing.T) {
	for _, tc := range []struct{ body, code, reason string }{
		{`{"error":"factory endpoint not found"}`, appwire.CodeConflict, appwire.FactoryReasonTooOld},
		{`{"error":"repository not found"}`, appwire.CodeNotFound, ""},
	} {
		t.Run(tc.code, func(t *testing.T) {
			wire, id, _ := attachRepoIssuesFactory(t, 404, tc.body)
			raw := repoIssuesErrorDetail(t, wire, id, tc.code)
			var detail appwire.FactoryRefusal
			if len(raw) > 0 && json.Unmarshal(raw, &detail) != nil {
				t.Fatalf("detail = %s", raw)
			}
			if detail.Reason != tc.reason {
				t.Fatalf("reason = %q, want %q", detail.Reason, tc.reason)
			}
		})
	}
}

// The existing Factory authentication reasons remain the wire's sign-in codes.
func TestAppFactoryRepoIssuesKeepsSignInRefusals(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{`{"error":"sign in"}`, appwire.CodeSignedOut},
		{`{"error":"reconnect","reason":"github_expired"}`, appwire.CodeGitHubExpired},
	} {
		t.Run(tc.code, func(t *testing.T) {
			wire, id, _ := attachRepoIssuesFactory(t, 401, tc.body)
			wire.mustFail("factory.repo_issues", map[string]any{"factory": id, "repo": "acme/api"}, tc.code)
		})
	}
}
