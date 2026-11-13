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
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

type repoIssuesFactory struct {
	mu   sync.Mutex
	asks []string
}

func attachRepoIssuesFactory(t *testing.T, status int, body string) (*factoryWire, string, *repoIssuesFactory) {
	t.Helper()
	return attachRepoIssuesFactoryHeader(t, status, nil, body)
}

func attachRepoIssuesFactoryHeader(t *testing.T, status int, header http.Header, body string) (*factoryWire, string, *repoIssuesFactory) {
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
			for key, values := range header {
				w.Header()[key] = values
			}
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

// The Factory's sign-in gate refusing the bearer stays signed_out and forgets
// a restored sign-in. Its gate always challenges with WWW-Authenticate. A 401
// with no challenge, or with github_expired, is the route's own GitHub step: the
// sign-in has no GitHub access the route can use. That is github_expired and the
// sign-in is kept, including from a Factory that sent that step without a reason.
func TestAppFactoryRepoIssuesKeepsSignInRefusals(t *testing.T) {
	challenge := http.Header{"Www-Authenticate": {`Bearer realm="factory"`}}
	for _, tc := range []struct {
		name   string
		header http.Header
		body   string
		code   string
		forget bool
	}{
		{"gate refuses the bearer", challenge, `{"error":"this link is not recognized","reason":"link_unknown"}`, appwire.CodeSignedOut, true},
		{"gate refuses without a reason", challenge, `{"error":"sign in"}`, appwire.CodeSignedOut, true},
		{"GitHub sign-in expired", challenge, `{"error":"reconnect","reason":"github_expired"}`, appwire.CodeGitHubExpired, false},
		{"no GitHub credential", nil, `{"error":"Reconnect GitHub to read repository issues","reason":"github_expired"}`, appwire.CodeGitHubExpired, false},
		{"no GitHub credential without a reason", nil, `{"error":"Reconnect GitHub to read repository issues"}`, appwire.CodeGitHubExpired, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, id, _ := attachRepoIssuesFactoryHeader(t, 401, tc.header, tc.body)
			first := &person{w: wire, conn: appwire.FactoryConnection{ID: id}}
			kept := first.take(t)
			if len(kept) != 1 {
				t.Fatalf("attach kept %+v", kept)
			}
			second := testFactoryHub(t)
			second.dir = wire.backend.factoryHub().dir
			after := &person{w: newFactoryWire(t, second), conn: first.conn}
			var restored appwire.FactorySessionRestored
			after.w.must("factory.session_restore", map[string]any{"factory": id, "url": kept[0].URL, "token": kept[0].Token}, &restored)
			if !restored.Kept {
				t.Fatal("the kept sign-in was not taken back")
			}
			after.w.mustFail("factory.repo_issues", map[string]any{"factory": id, "repo": "acme/api"}, tc.code)
			gone := after.take(t)
			if forgot := len(gone) == 1 && gone[0].Removed && after.held() == ""; forgot != tc.forget || (!tc.forget && (len(gone) != 0 || after.held() != testHostToken)) {
				t.Fatalf("forget = %t, want %t: changes %+v, held %t", forgot, tc.forget, gone, after.held() != "")
			}
		})
	}
}

// A KEPT TOKEN SIGN-IN WITHOUT GITHUB SURVIVES A BACKGROUND ISSUE READ. A team
// Factory attached with a plain access token (the host's access token, or a
// row minted as `codeaf factory token add` does) and never signed in with
// GitHub is restored after a restart. The Factory cannot read issues for that
// sign-in, so the read is github_expired; the sign-in is still held, nothing is
// queued for removal, and the connection still reads ok and reads its queue.
func TestAppFactoryRepoIssuesWithoutGitHubKeepsARestoredTokenSignIn(t *testing.T) {
	for _, kind := range []string{"host access token", "minted access token"} {
		t.Run(kind, func(t *testing.T) {
			f := startAdminFactory(t)
			secret := testHostToken
			if kind == "minted access token" {
				row, err := tokens.Mint()
				if err != nil {
					t.Fatal(err)
				}
				now := f.clock.now()
				row.Created, row.Expires = now, now.Add(7*24*time.Hour)
				secret = row.Token
				if err := tokens.Add(f.tokens, row); err != nil {
					t.Fatal(err)
				}
			}
			hub := testFactoryHub(t)
			hub.now = f.clock.now
			before := &person{w: newFactoryWire(t, hub)}
			before.w.must("factory.attach", map[string]any{"url": f.URL, "token": secret}, &before.conn)
			kept := before.take(t)
			if len(kept) != 1 || kept[0].Token != secret || kept[0].Removed {
				t.Fatalf("attach kept %+v", kept)
			}

			// The restart: a new engine over the same saved connections, handed
			// the kept token back as the app does at launch.
			second := testFactoryHub(t)
			second.dir = hub.dir
			second.now = f.clock.now
			after := &person{w: newFactoryWire(t, second), conn: before.conn}
			var restored appwire.FactorySessionRestored
			after.w.must("factory.session_restore", map[string]any{"factory": kept[0].Factory, "url": kept[0].URL, "token": kept[0].Token, "expires": kept[0].Expires}, &restored)
			if !restored.Kept {
				t.Fatal("the kept token was not taken back")
			}
			var queue api.Queue
			after.w.must("factory.queue", map[string]any{"factory": after.conn.ID}, &queue)

			if _, code, msg := after.w.call("factory.repo_issues", map[string]any{"factory": after.conn.ID, "repo": "acme/api"}); code != appwire.CodeGitHubExpired {
				t.Errorf("issue read without GitHub = %q (%s), want %q", code, msg, appwire.CodeGitHubExpired)
			}
			if after.held() != secret {
				t.Error("an issue read without GitHub dropped the held sign-in")
			}
			if changes := after.take(t); len(changes) != 0 {
				t.Errorf("an issue read without GitHub changed the kept sign-in: %+v", changes)
			}
			if c := after.listed(t); c.State != appwire.FactoryOK {
				t.Fatalf("after the issue read the connection reads %s", c.State)
			}
			after.w.must("factory.queue", map[string]any{"factory": after.conn.ID}, &queue)
		})
	}
}

// A RESTORED SIGN-IN THE FACTORY ITSELF REFUSES IS STILL FORGOTTEN by an issue
// read: the Factory's sign-in gate answers it, not the route's GitHub step.
func TestAppFactoryRepoIssuesForgetsARestoredSignInTheFactoryRefuses(t *testing.T) {
	f := startAdminFactory(t)
	bob := f.signIn(t, 1, "alice")
	bob.take(t)
	second := testFactoryHub(t)
	second.dir = bob.w.backend.factoryHub().dir
	second.now = f.clock.now
	after := &person{w: newFactoryWire(t, second), handle: "alice", conn: bob.conn}
	var restored appwire.FactorySessionRestored
	after.w.must("factory.session_restore", map[string]any{"factory": bob.conn.ID, "url": f.URL, "token": "dead-session-token-0123456789"}, &restored)
	if !restored.Kept {
		t.Fatal("a hand-back for a saved connection was refused before it was tried")
	}
	after.w.mustFail("factory.repo_issues", map[string]any{"factory": bob.conn.ID, "repo": "acme/api"}, appwire.CodeSignedOut)
	gone := after.take(t)
	if len(gone) != 1 || !gone[0].Removed || gone[0].Factory != bob.conn.ID || after.held() != "" {
		t.Fatalf("the refused sign-in was not removed: %+v, held %q", gone, after.held())
	}
}
