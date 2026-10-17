package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── a real team factory whose operator is alice, and a log of what it was sent ─

// adminFactoryAddress is the public address the factory believes it has, which
// an invitation embeds. It is not where the test dials.
const adminFactoryAddress = "http://factory.example"

type adminFactory struct {
	URL    string
	clock  *factoryClock
	github *fakeGitHub
	mu     sync.Mutex
	log    []string // "METHOD path" of every request except /v1/about
	// gate, when set, holds a request at the door until it returns, so a test
	// can change the caller's credential while a read is in flight.
	gate func(path string)
	// st is the factory's own store, for the tests that read what was kept.
	st *store.Store
	// afterStore, when set, is returned by the repository switch AFTER the list
	// was stored, as a failed event would; noRepoSwitch leaves the factory with
	// no way to save the list at all.
	afterStore   error
	noRepoSwitch bool
}

func startAdminFactory(t *testing.T) *adminFactory { return startAdminFactoryAt(t, true) }

// startAdminFactoryAt is the same team factory. forwarded makes every request a
// stranger's; false leaves the real loopback peer, whose no-bearer request the
// factory answers as its host.
func startAdminFactoryAt(t *testing.T, forwarded bool) *adminFactory {
	t.Helper()
	return startAdminFactoryWith(t, forwarded, false)
}

func startAdminFactoryWith(t *testing.T, forwarded, noRepoSwitch bool) *adminFactory {
	t.Helper()
	f := &adminFactory{clock: &factoryClock{at: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}, github: newFakeGitHub(), noRepoSwitch: noRepoSwitch}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: f.clock.now})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	claim := `{"login":"alice","claimed_at":"2026-10-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "operator.json"), []byte(claim), 0o600); err != nil {
		t.Fatal(err)
	}
	f.st = st
	seam := factory.Seam{Load: func() (factory.Snapshot, error) { return factory.Snapshot{}, nil }}
	if !f.noRepoSwitch {
		seam.SetRepos = func(repos []string) error {
			if err := st.SetRepos(repos); err != nil {
				return err
			}
			return f.afterStore
		}
	}
	h := serve.New(seam, serve.Config{
		Version: "test", Name: "Acme factory", ServedBy: "factory", Address: adminFactoryAddress, AccessToken: testHostToken, Mode: api.FactoryTeam,
		GitHubClientID: "client", GitHubHTTPClient: f.github.client(), Now: f.clock.now, Store: st, Record: rec, Root: st.Root(),
		TokensPath: filepath.Join(dir, "tokens.json"),
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/about" {
			f.mu.Lock()
			f.log = append(f.log, r.Method+" "+r.URL.Path)
			f.mu.Unlock()
		}
		if f.gate != nil {
			f.gate(r.URL.Path)
		}
		// A forwarded request is not a loopback one: no ambient owner fallback.
		if forwarded {
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { srv.Close(); h.Close() })
	f.URL = srv.URL
	return f
}

// since is what the factory was sent after the last call to since.
func (f *adminFactory) since() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.log
	f.log = nil
	return out
}

// who asks the factory directly, with a bearer, who it is.
func (f *adminFactory) who(t *testing.T, token string) (int, api.Me) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, f.URL+"/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var me api.Me
	_ = json.NewDecoder(resp.Body).Decode(&me)
	f.since() // reading is not what the test is measuring
	return resp.StatusCode, me
}

func (f *adminFactory) signIn(t *testing.T, n int, handle string) *person {
	t.Helper()
	hub := testFactoryHub(t)
	hub.now = f.clock.now
	w := newFactoryWire(t, hub)
	p := &person{w: w, handle: handle}
	w.must("factory.attach", map[string]any{"url": f.URL, "token": testHostToken}, &p.conn)
	var flow appwire.FactorySignIn
	w.must("factory.sign_in_start", map[string]any{"factory": p.conn.ID, "caller": "w"}, &flow)
	f.github.grant("github-device-secret-" + string(rune('0'+n)))
	f.clock.advance(6 * time.Second)
	var state appwire.FactorySignInState
	w.must("factory.sign_in_poll", map[string]any{"flow": flow.Flow, "caller": "w"}, &state)
	if state.State != appwire.SignInSignedIn || state.Me == nil || state.Me.Handle != handle {
		t.Fatalf("sign in as %s: %+v", handle, state)
	}
	f.since()
	return p
}

func (p *person) held() string { return p.w.backend.factoryHub().tokenFor(p.conn.ID) }

func (p *person) listed(t *testing.T) appwire.FactoryConnection {
	t.Helper()
	var list appwire.FactoryList
	p.w.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 1 {
		t.Fatalf("list = %+v", list)
	}
	return list.Factories[0]
}

// ── the real server ────────────────────────────────────────────────────────

// A MEMBER SIGNS OUT: GET /v1/me, then DELETE of exactly their own session,
// once; the session is dead at the factory, the row stays, nobody else is
// touched, and no frame carries the session id or any credential.
func TestAppFactorySignOutMemberAgainstTheRealServer(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	bob := f.signIn(t, 2, "bob")
	bobToken, aliceToken := bob.held(), alice.held()
	_, bobMe := f.who(t, bobToken)
	if !strings.HasPrefix(bobMe.SessionID, "session_") {
		t.Fatalf("a sign-in's id: %q", bobMe.SessionID)
	}

	if !strings.Contains(bob.w.hello, appwire.FeatureFactoryAdmin) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactoryAdmin)
	}
	var caps appwire.FactoryCapabilities
	bob.w.must("factory.capabilities", struct{}{}, &caps)
	if !caps.Admin || caps.Writes {
		t.Fatalf("capabilities: %+v", caps)
	}

	var done appwire.FactoryAdminDone
	bob.w.must("factory.sign_out", map[string]any{"factory": bob.conn.ID}, &done)
	if done.Factory != bob.conn.ID || done.Action != appwire.AdminActSignOut || !done.SignedOut {
		t.Fatalf("done = %+v", done)
	}
	if got, want := f.since(), []string{"GET /v1/me", "DELETE /v1/me/sessions/" + bobMe.SessionID}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the factory was sent %v, want %v", got, want)
	}
	if status, _ := f.who(t, bobToken); status != http.StatusUnauthorized {
		t.Fatalf("bob's session still works at the factory: %d", status)
	}
	if status, me := f.who(t, aliceToken); status != http.StatusOK || me.Handle != "alice" {
		t.Fatalf("alice was signed out too: %d %+v", status, me)
	}
	if bob.held() != "" {
		t.Fatal("the signed-out session is still held")
	}
	if c := bob.listed(t); c.ID != bob.conn.ID || c.State != appwire.FactorySignedOut {
		t.Fatalf("the row after sign-out: %+v", c)
	}
	f.since()
	bob.w.mustFail("factory.queue", map[string]any{"factory": bob.conn.ID}, appwire.CodeSignedOut)

	// Nothing held, so a second press sends nothing at all.
	f.since()
	bob.w.mustFail("factory.sign_out", map[string]any{"factory": bob.conn.ID}, appwire.CodeSignedOut)
	if got := f.since(); len(got) != 0 {
		t.Fatalf("signing out of nothing sent %v", got)
	}
	bob.w.neverSaid(bobToken, bobMe.SessionID, testHostToken, "github-device-secret-2", "github-token-secret-2")
}

// A MEMBER CANNOT REVOKE EVERYONE, AND IS TOLD SO WITHOUT A REQUEST: the factory
// would answer a non-operator 401, which reads as "signed out".
func TestAppFactoryRevokeSessionsRefusedForAMember(t *testing.T) {
	f := startAdminFactory(t)
	f.signIn(t, 1, "alice")
	bob := f.signIn(t, 2, "bob")
	msg := bob.w.mustFail("factory.revoke_sessions", map[string]any{"factory": bob.conn.ID}, appwire.CodeForbidden)
	if !strings.Contains(msg, "operator") {
		t.Fatalf("words: %q", msg)
	}
	if got := f.since(); strings.Join(got, "|") != "GET /v1/me" {
		t.Fatalf("a refusal sent %v", got)
	}
	if bob.held() == "" {
		t.Fatal("a refused revoke dropped the sign-in")
	}
}

// THE OPERATOR REVOKES EVERYONE, ONCE, WITH THE CREDENTIAL THEY HOLD: every
// session at the factory ends, theirs is dropped here, and no secret leaves.
func TestAppFactoryRevokeSessionsByTheOperatorAgainstTheRealServer(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	bob := f.signIn(t, 2, "bob")
	aliceToken, bobToken := alice.held(), bob.held()
	_, aliceMe := f.who(t, aliceToken)

	var done appwire.FactoryAdminDone
	alice.w.must("factory.revoke_sessions", map[string]any{"factory": alice.conn.ID}, &done)
	if done.Action != appwire.AdminActRevokeSessions || !done.SignedOut {
		t.Fatalf("done = %+v", done)
	}
	if got := f.since(); strings.Join(got, "|") != "GET /v1/me|POST /v1/access/revoke-sessions" {
		t.Fatalf("the factory was sent %v", got)
	}
	for who, token := range map[string]string{"alice": aliceToken, "bob": bobToken} {
		if status, _ := f.who(t, token); status != http.StatusUnauthorized {
			t.Fatalf("%s still has a session at the factory: %d", who, status)
		}
	}
	if alice.held() != "" {
		t.Fatal("the operator's own ended session is still held")
	}
	if c := alice.listed(t); c.State != appwire.FactorySignedOut {
		t.Fatalf("the row after revoke-all: %+v", c)
	}
	alice.w.neverSaid(aliceToken, bobToken, aliceMe.SessionID, testHostToken)
}

// A HOST KEY IS NOT A SIGN-IN. Signing out of it is refused (it has no session
// to end and the key is not ours to throw away); revoking sessions with it is
// the factory's own operator door, sent once, and the key keeps working.
func TestAppFactoryHostKeyConnectionSignOutAndRevoke(t *testing.T) {
	f := startAdminFactory(t)
	hub := testFactoryHub(t)
	w := newFactoryWire(t, hub)
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": f.URL, "token": testHostToken}, &conn)
	// The fake GitHub hands the first sign-in to alice, whose session this key can revoke.
	other := f.signIn(t, 1, "alice")
	otherToken := other.held()
	f.since()

	msg := w.mustFail("factory.sign_out", map[string]any{"factory": conn.ID}, appwire.CodeConflict)
	if !strings.Contains(msg, "key") {
		t.Fatalf("words: %q", msg)
	}
	if got := f.since(); strings.Join(got, "|") != "GET /v1/me" {
		t.Fatalf("a refusal sent %v", got)
	}
	if hub.tokenFor(conn.ID) != testHostToken {
		t.Fatal("the key was dropped by a refused sign-out")
	}

	var done appwire.FactoryAdminDone
	w.must("factory.revoke_sessions", map[string]any{"factory": conn.ID}, &done)
	if done.SignedOut || done.Action != appwire.AdminActRevokeSessions {
		t.Fatalf("done = %+v", done)
	}
	if got := f.since(); strings.Join(got, "|") != "GET /v1/me|POST /v1/access/revoke-sessions" {
		t.Fatalf("the factory was sent %v", got)
	}
	if status, _ := f.who(t, otherToken); status != http.StatusUnauthorized {
		t.Fatalf("a session survived the revoke: %d", status)
	}
	if hub.tokenFor(conn.ID) != testHostToken {
		t.Fatal("revoking sessions threw the key away")
	}
	w.must("factory.me", map[string]any{"factory": conn.ID}, nil)
	w.neverSaid(testHostToken)
}

// THE MACHINE'S OWN FACTORY IS NEVER SIGNED OUT OF, DETACHED OR STOPPED by
// these methods; it is refused, and it keeps running with its key held.
func TestAppFactoryAdminRefusesTheMachinesOwnFactory(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	if st.State != appwire.LocalRunning || st.Authority != appwire.LocalAuthorityApp {
		t.Fatalf("local factory: %+v", st)
	}
	hub := s.backend.factoryHub()
	key := hub.tokenFor(st.Factory)
	if key == "" {
		t.Fatal("the local factory holds no key")
	}
	for _, method := range []string{"factory.sign_out", "factory.revoke_sessions"} {
		s.mustFail(method, map[string]any{"factory": st.Factory}, appwire.CodeConflict)
	}
	if hub.tokenFor(st.Factory) != key {
		t.Fatal("a refusal let go of the local factory's key")
	}
	var again appwire.FactoryLocalStatus
	s.must("factory.local_status", map[string]any{"caller": "w1"}, &again)
	if again.State != appwire.LocalRunning || again.Factory != st.Factory {
		t.Fatalf("the local factory after the refusal: %+v", again)
	}
	var list appwire.FactoryList
	s.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 1 || list.Factories[0].State != appwire.FactoryOK {
		t.Fatalf("list: %+v", list)
	}
	s.neverSaid(key)
}

// A row marked local is refused whatever its id, and the refusal wakes
// nothing: the local factory is not resumed to answer it.
func TestAppFactoryAdminLocalGuardNeverResumesOrSendsAnything(t *testing.T) {
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("a request reached the factory: %s", r.URL) })
	hub := testFactoryHub(t)
	b := newFakeBackend(newAppRecorder(), nil)
	b.factoryOnce.Do(func() { b.factories = hub })
	t.Cleanup(func() { b.Close() })
	hub.resume = func(context.Context) { t.Error("the local factory was resumed") }
	if _, err := hub.snapshot(); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	hub.rows = append(hub.rows, appFactoryRow{ID: appLocalID, URL: stub.srv.URL, Local: true, Resume: true}, appFactoryRow{ID: "factory_marked", URL: "http://127.0.0.1:1", Local: true})
	hub.sessions[appLocalID] = appFactorySession{token: "local-host-key-fixture"}
	hub.sessions["factory_marked"] = appFactorySession{token: "marked-host-key-fixture"}
	hub.mu.Unlock()
	for _, id := range []string{appLocalID, "factory_marked"} {
		for _, call := range []func(context.Context, appwire.FactoryAdminParams) (appwire.FactoryAdminDone, error){b.FactorySignOut, b.FactoryRevokeSessions} {
			if _, err := call(context.Background(), appwire.FactoryAdminParams{Factory: id}); wireCodeOf(t, err) != appwire.CodeConflict {
				t.Fatalf("%s: %v", id, err)
			}
		}
		if hub.tokenFor(id) == "" {
			t.Fatalf("%s lost its key", id)
		}
	}
	if len(stub.requests) != 0 {
		t.Fatalf("requests: %v", stub.requests)
	}
	if _, err := b.FactorySignOut(context.Background(), appwire.FactoryAdminParams{Factory: "factory_nope"}); wireCodeOf(t, err) != appwire.CodeNotFound {
		t.Fatalf("unknown factory: %v", err)
	}
}

// ── scripted factories, for what the real one cannot be made to do ────────

const adminHeld = "held-bearer-fixture-for-admin"

// adminStub answers GET /v1/me from me, and every other request from other.
func adminStub(t *testing.T, me string, other http.HandlerFunc) (*appBackend, *stubFactory, appwire.FactoryConnection) {
	t.Helper()
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/me" {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasPrefix(me, "!") { // "!401 body"
				code, body, _ := strings.Cut(me[1:], " ")
				status, err := strconv.Atoi(code)
				if err != nil || status < 100 || status > 599 {
					t.Errorf("invalid scripted HTTP status %q", code)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
				return
			}
			_, _ = w.Write([]byte(me))
			return
		}
		other(w, r)
	})
	b, conn := attachStub(t, testFactoryHub(t), stub, adminHeld)
	return b, stub, conn
}

func meBody(session string, operator bool) string {
	raw, _ := json.Marshal(map[string]any{"factory": map[string]any{"name": "Stub"}, "handle": "bob", "session_id": session, "operator": operator, "mode": "team", "contract": 1})
	return string(raw)
}

func (s *stubFactory) seen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.requests, "|")
}

func (s *stubFactory) allAuth(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.auth {
		if a != "" && a != "Bearer "+adminHeld {
			t.Errorf("request %d (%s) carried %q, not the held credential", i, s.requests[i], a)
		}
		if s.requests[i] != "GET /v1/about" && a == "" {
			t.Errorf("request %d (%s) carried no credential", i, s.requests[i])
		}
	}
	for i, c := range s.cookies {
		if c != "" {
			t.Errorf("request %d carried a cookie", i)
		}
	}
}

func admin(b *appBackend, which string, id string) (appwire.FactoryAdminDone, error) {
	p := appwire.FactoryAdminParams{Factory: id}
	if which == "revoke" {
		return b.FactoryRevokeSessions(context.Background(), p)
	}
	return b.FactorySignOut(context.Background(), p)
}

func TestAppFactorySignOutOutcomesScripted(t *testing.T) {
	const session = "session_0123abcd"
	reply := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); _, _ = w.Write([]byte(body)) }
	}
	// refusal is a refusal the factory marked as made before its first change.
	refusal := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	cases := []struct {
		name     string
		me       string
		other    http.HandlerFunc
		code     string // "" for success
		held     bool   // the credential is still held after
		requests string
	}{
		{"confirmed", meBody(session, false), reply(200, `{"factory":{}}`), "", false, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		{"already signed out at the factory", "!401 {\"error\":\"sign in\"}", reply(500, ``), appwire.CodeSignedOut, true, "GET /v1/me"},
		{"github expired", "!401 {\"error\":\"x\",\"reason\":\"github_expired\"}", reply(500, ``), appwire.CodeGitHubExpired, true, "GET /v1/me"},
		{"identity unreadable", "!500 {}", reply(500, ``), appwire.CodeEngine, true, "GET /v1/me"},
		{"session already gone is not claimed as done", meBody(session, false), refusal(404, `{"error":"session not found"}`), appwire.CodeNotFound, true, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		{"forbidden", meBody(session, false), refusal(403, `{"error":"no"}`), appwire.CodeForbidden, true, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		// A refusal without the marker may follow the change (it is deleted, then the save or the event fails).
		{"404 without the marker is uncertain", meBody(session, false), reply(404, `{"error":"session not found"}`), appwire.CodeUncertain, true, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		{"403 without the marker is uncertain", meBody(session, false), reply(403, `{"error":"no"}`), appwire.CodeUncertain, true, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		{"409 without the marker is uncertain", meBody(session, false), reply(409, ``), appwire.CodeUncertain, true, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		{"factory failure is uncertain", meBody(session, false), reply(500, `{"error":"boom"}`), appwire.CodeUncertain, true, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		{"bad gateway is uncertain", meBody(session, false), reply(502, ``), appwire.CodeUncertain, true, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		{"redirect is not followed", meBody(session, false), func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://127.0.0.1:1/elsewhere", http.StatusTemporaryRedirect)
		}, appwire.CodeUncertain, true, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		{"connection cut after the request left is uncertain", meBody(session, false), func(w http.ResponseWriter, r *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
		}, appwire.CodeUncertain, true, "GET /v1/me|DELETE /v1/me/sessions/" + session},
		{"a key", meBody("", true), reply(200, ``), appwire.CodeConflict, true, "GET /v1/me"},
		{"an access link", meBody("tok_abc123", false), reply(200, ``), appwire.CodeConflict, true, "GET /v1/me"},
		{"a path in the id", meBody("session_a/../../v1/access/revoke-sessions", true), reply(200, ``), appwire.CodeConflict, true, "GET /v1/me"},
		{"a query in the id", meBody("session_a?x=1", true), reply(200, ``), appwire.CodeConflict, true, "GET /v1/me"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, stub, conn := adminStub(t, tc.me, tc.other)
			stub.mu.Lock()
			stub.requests = nil // attach's own probe is not the subject
			stub.auth, stub.cookies = nil, nil
			stub.mu.Unlock()
			done, err := admin(b, "sign_out", conn.ID)
			if tc.code == "" {
				if err != nil || !done.SignedOut || done.Action != appwire.AdminActSignOut {
					t.Fatalf("done = %+v, %v", done, err)
				}
			} else {
				if got := wireCodeOf(t, err); got != tc.code {
					t.Fatalf("code %q (%v), want %q", got, err, tc.code)
				}
				if done != (appwire.FactoryAdminDone{}) {
					t.Fatalf("a failure answered %+v", done)
				}
			}
			if got := stub.seen(); got != tc.requests {
				t.Fatalf("the factory was sent %q, want %q", got, tc.requests)
			}
			if held := b.factoryHub().tokenFor(conn.ID) == adminHeld; held != tc.held {
				t.Fatalf("credential held = %v, want %v", held, tc.held)
			}
			if err != nil {
				for _, secret := range []string{session, adminHeld, "0123abcd", stub.srv.URL} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("the error carries %q: %v", secret, err)
					}
				}
			}
			stub.allAuth(t)
		})
	}
}

// A TRANSPORT FAILURE IS NEVER REPORTED WITH THE ADDRESS OR THE SESSION ID, and
// only a refused connection (nothing was sent) is told apart from the rest.
func TestAppFactoryAdminTransportFailures(t *testing.T) {
	const session = "session_feedbeef"
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"connection refused", &url.Error{Op: "Delete", URL: "http://x/v1/me/sessions/" + session, Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}, appwire.CodeUnreachable},
		{"reset while reading", &url.Error{Op: "Delete", URL: "http://x/v1/me/sessions/" + session, Err: errors.New("read: connection reset by peer")}, appwire.CodeUncertain},
		{"timeout", &url.Error{Op: "Post", URL: "http://x/v1/access/revoke-sessions", Err: context.DeadlineExceeded}, appwire.CodeUncertain},
	} {
		for _, which := range []string{"sign_out", "revoke"} {
			t.Run(tc.name+"/"+which, func(t *testing.T) {
				b, stub, conn := adminStub(t, meBody(session, true), func(http.ResponseWriter, *http.Request) {})
				hub := b.factoryHub()
				real := hub.client.Transport
				var sent int
				hub.client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
					if r.Method == http.MethodGet {
						return real.RoundTrip(r)
					}
					sent++
					return nil, tc.err
				})
				_, err := admin(b, which, conn.ID)
				if got := wireCodeOf(t, err); got != tc.code {
					t.Fatalf("code %q (%v), want %q", got, err, tc.code)
				}
				if sent != 1 {
					t.Fatalf("the write was attempted %d times", sent)
				}
				for _, leak := range []string{session, adminHeld, stub.srv.URL, "x/v1"} {
					if strings.Contains(err.Error(), leak) {
						t.Fatalf("the error carries %q: %v", leak, err)
					}
				}
				if hub.tokenFor(conn.ID) != adminHeld {
					t.Fatal("an unconfirmed write dropped the credential")
				}
			})
		}
	}
}

func TestAppFactoryRevokeSessionsOutcomesScripted(t *testing.T) {
	const session = "session_0123abcd"
	reply := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); _, _ = w.Write([]byte(body)) }
	}
	refusal := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	cases := []struct {
		name     string
		me       string
		other    http.HandlerFunc
		code     string
		signedIn bool // the credential is still held after
		out      bool // done.SignedOut
		requests string
	}{
		{"operator session", meBody(session, true), reply(200, `{"revoked":true}`), "", false, true, "GET /v1/me|POST /v1/access/revoke-sessions"},
		{"operator key", meBody("", true), reply(200, `{"revoked":true}`), "", true, false, "GET /v1/me|POST /v1/access/revoke-sessions"},
		{"not the operator, nothing sent", meBody(session, false), reply(200, ``), appwire.CodeForbidden, true, false, "GET /v1/me"},
		{"signed out in between", meBody(session, true), refusal(401, `{"error":"a bearer access token is required"}`), appwire.CodeSignedOut, true, false, "GET /v1/me|POST /v1/access/revoke-sessions"},
		{"forbidden", meBody(session, true), refusal(403, `{"error":"no"}`), appwire.CodeForbidden, true, false, "GET /v1/me|POST /v1/access/revoke-sessions"},
		{"an older factory", meBody(session, true), refusal(404, `404 page not found`), appwire.CodeNotFound, true, false, "GET /v1/me|POST /v1/access/revoke-sessions"},
		// Without the marker a refusal may follow the change; it is never claimed as "nothing happened".
		{"403 without the marker is uncertain", meBody(session, true), reply(403, `{"error":"no"}`), appwire.CodeUncertain, true, false, "GET /v1/me|POST /v1/access/revoke-sessions"},
		{"404 without the marker is uncertain", meBody(session, true), reply(404, `404 page not found`), appwire.CodeUncertain, true, false, "GET /v1/me|POST /v1/access/revoke-sessions"},
		{"409 without the marker is uncertain", meBody(session, true), reply(409, ``), appwire.CodeUncertain, true, false, "GET /v1/me|POST /v1/access/revoke-sessions"},
		{"failed part-way is uncertain and keeps the credential", meBody(session, true), reply(500, `{"error":"boom"}`), appwire.CodeUncertain, true, false, "GET /v1/me|POST /v1/access/revoke-sessions"},
		{"identity unreadable", "!401 {}", reply(200, ``), appwire.CodeSignedOut, true, false, "GET /v1/me"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body string
			other := func(w http.ResponseWriter, r *http.Request) {
				raw := make([]byte, 16)
				n, _ := r.Body.Read(raw)
				body = string(raw[:n])
				tc.other(w, r)
			}
			b, stub, conn := adminStub(t, tc.me, other)
			stub.mu.Lock()
			stub.requests, stub.auth, stub.cookies = nil, nil, nil
			stub.mu.Unlock()
			done, err := admin(b, "revoke", conn.ID)
			if tc.code == "" {
				if err != nil || done.SignedOut != tc.out || done.Action != appwire.AdminActRevokeSessions {
					t.Fatalf("done = %+v, %v", done, err)
				}
			} else if got := wireCodeOf(t, err); got != tc.code {
				t.Fatalf("code %q (%v), want %q", got, err, tc.code)
			}
			if got := stub.seen(); got != tc.requests {
				t.Fatalf("the factory was sent %q, want %q", got, tc.requests)
			}
			if body != "" {
				t.Fatalf("revoke-all sent a body: %q", body)
			}
			if held := b.factoryHub().tokenFor(conn.ID) == adminHeld; held != tc.signedIn {
				t.Fatalf("credential held = %v, want %v", held, tc.signedIn)
			}
			stub.allAuth(t)
		})
	}
}

// ONE PRESS IS ONE REQUEST: a second call for the same connection while the
// first is in flight is refused as busy and sends nothing; a new sign-in that
// lands while the old one is being ended is not forgotten by the old answer.
func TestAppFactoryAdminOneInFlightAndNewerSignInSurvives(t *testing.T) {
	const session = "session_0123abcd"
	entered, release := make(chan struct{}), make(chan struct{})
	var deletes int
	var mu sync.Mutex
	var once sync.Once
	b, stub, conn := adminStub(t, meBody(session, true), func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deletes++
		mu.Unlock()
		once.Do(func() { close(entered) })
		<-release
		w.WriteHeader(200)
	})
	result := make(chan error, 1)
	go func() { _, err := admin(b, "sign_out", conn.ID); result <- err }()
	<-entered
	for _, which := range []string{"sign_out", "revoke"} {
		if _, err := admin(b, which, conn.ID); wireCodeOf(t, err) != appwire.CodeBusy {
			t.Fatalf("%s while one is in flight: %v", which, err)
		}
	}
	// A sign-in completes meanwhile and holds a different credential.
	b.factoryHub().hold(conn.ID, "newer-bearer-fixture", 0)
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if b.factoryHub().tokenFor(conn.ID) != "newer-bearer-fixture" {
		t.Fatal("the old sign-out's answer forgot a newer sign-in")
	}
	mu.Lock()
	defer mu.Unlock()
	if deletes != 1 || strings.Count(stub.seen(), "DELETE") != 1 {
		t.Fatalf("DELETE sent %d times: %s", deletes, stub.seen())
	}
	// Free again once done.
	if _, err := admin(b, "sign_out", conn.ID); wireCodeOf(t, err) == appwire.CodeBusy {
		t.Fatal("the connection stayed busy")
	}
}

// NOTHING HELD, NOTHING SENT: a connection with no credential is refused before
// any request, so no ambient owner or key can be used to act for the person.
func TestAppFactoryAdminWithNoCredentialSendsNothing(t *testing.T) {
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	b, conn := attachStub(t, testFactoryHub(t), stub, "")
	stub.mu.Lock()
	stub.requests = nil
	stub.mu.Unlock()
	for _, which := range []string{"sign_out", "revoke"} {
		if _, err := admin(b, which, conn.ID); wireCodeOf(t, err) != appwire.CodeSignedOut {
			t.Fatalf("%s: %v", which, err)
		}
	}
	if stub.seen() != "" {
		t.Fatalf("requests: %s", stub.seen())
	}
}

// After a confirmed sign-out nothing the engine does on its own gives the
// credential back: listing, reading and a resume check leave it dropped.
func TestAppFactorySignOutStaysSignedOutAcrossListsAndReads(t *testing.T) {
	const session = "session_0123abcd"
	b, _, conn := adminStub(t, meBody(session, false), func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	if _, err := admin(b, "sign_out", conn.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := b.FactoryList(context.Background()); err != nil {
			t.Fatal(err)
		}
		b.resumeLocal(context.Background())
		_, _ = b.FactoryQueue(context.Background(), conn.ID)
	}
	if b.factoryHub().tokenFor(conn.ID) != "" {
		t.Fatal("a credential came back after sign-out")
	}
}
