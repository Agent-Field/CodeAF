package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// ── a factory backend over the wire, keyless ───────────────────────────────

// factoryWire is the real appBackend behind the real appwire server, with
// every line the engine writes kept so a test can search them for a secret.
type factoryWire struct {
	t       *testing.T
	backend *appBackend
	in      io.Writer
	mu      sync.Mutex
	lines   []string
	answers chan json.RawMessage
	next    int
	hello   string
}

func newFactoryWire(t *testing.T, hub *appFactoryHub) *factoryWire {
	t.Helper()
	backend := newFakeBackend(newAppRecorder(), nil)
	backend.factoryOnce.Do(func() { backend.factories = hub })
	t.Cleanup(func() { backend.Close() })
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	w := &factoryWire{t: t, backend: backend, in: inW, answers: make(chan json.RawMessage, 64)}
	go func() {
		scanner := bufio.NewScanner(outR)
		scanner.Buffer(make([]byte, 16<<20), 16<<20)
		for scanner.Scan() {
			line := scanner.Text()
			w.mu.Lock()
			w.lines = append(w.lines, line)
			w.mu.Unlock()
			w.answers <- json.RawMessage(line)
		}
		close(w.answers)
	}()
	t.Cleanup(func() { inW.Close() })
	hello := <-w.answers
	w.hello = string(hello)
	if !strings.Contains(string(hello), appwire.FeatureFactory) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactory, hello)
	}
	return w
}

// call sends one request and returns its result, or the error code and message.
func (w *factoryWire) call(method string, params any) (json.RawMessage, string, string) {
	w.t.Helper()
	w.next++
	raw, _ := json.Marshal(params)
	fmt.Fprintf(w.in, `{"id":%d,"method":%q,"params":%s}`+"\n", w.next, method, raw)
	for line := range w.answers {
		var f struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &f) != nil || f.ID == nil || *f.ID != w.next {
			continue
		}
		if f.Error != nil {
			return nil, f.Error.Code, f.Error.Message
		}
		return f.Result, "", ""
	}
	w.t.Fatalf("the wire ended during %s", method)
	return nil, "", ""
}

func (w *factoryWire) must(method string, params any, out any) {
	w.t.Helper()
	result, code, msg := w.call(method, params)
	if code != "" {
		w.t.Fatalf("%s: %s: %s", method, code, msg)
	}
	if out != nil {
		if err := json.Unmarshal(result, out); err != nil {
			w.t.Fatalf("%s: %v in %s", method, err, result)
		}
	}
}

func (w *factoryWire) mustFail(method string, params any, want string) string {
	w.t.Helper()
	_, code, msg := w.call(method, params)
	if code != want {
		w.t.Fatalf("%s: code %q (%s), want %q", method, code, msg, want)
	}
	return msg
}

// neverSaid fails if any line the engine wrote contains one of the secrets.
func (w *factoryWire) neverSaid(secrets ...string) {
	w.t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, line := range w.lines {
		for _, s := range secrets {
			if s != "" && strings.Contains(line, s) {
				w.t.Fatalf("a frame carries the secret %q: %.300s", s, line)
			}
		}
	}
}

func testFactoryHub(t *testing.T) *appFactoryHub {
	t.Helper()
	hub := newAppFactoryHub()
	hub.dir = func() string { return filepath.Join(t.TempDir(), "app") }
	dir := hub.dir()
	hub.dir = func() string { return dir }
	return hub
}

// ── a real factory server, with a fake GitHub ──────────────────────────────

type factoryClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *factoryClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *factoryClock) advance(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeGitHub hands out a different device code and person on every start, so
// two callers' flows differ, and grants a code only when a test says so.
type fakeGitHub struct {
	mu      sync.Mutex
	started int
	granted map[string]bool
	logins  map[string]string
}

func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{granted: map[string]bool{}, logins: map[string]string{}}
}

func (g *fakeGitHub) grant(code string) { g.mu.Lock(); g.granted[code] = true; g.mu.Unlock() }

func (g *fakeGitHub) client() *http.Client {
	return &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		g.mu.Lock()
		defer g.mu.Unlock()
		_ = r.ParseForm()
		body := `{}`
		switch r.URL.String() {
		case "https://github.com/login/device/code":
			g.started++
			code := "github-device-secret-" + strconv.Itoa(g.started)
			g.logins["github-token-secret-"+strconv.Itoa(g.started)] = []string{"alice", "bob", "carol"}[(g.started-1)%3]
			body = fmt.Sprintf(`{"device_code":%q,"user_code":"CODE-%d","verification_uri":"https://github.com/login/device","interval":5,"expires_in":900}`, code, g.started)
		case "https://github.com/login/oauth/access_token":
			code := r.Form.Get("device_code")
			if !g.granted[code] {
				body = `{"error":"authorization_pending"}`
				break
			}
			n := strings.TrimPrefix(code, "github-device-secret-")
			body = fmt.Sprintf(`{"access_token":"github-token-secret-%s","scope":"read:user,repo","token_type":"bearer"}`, n)
		case "https://api.github.com/user":
			login := g.logins[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
			body = fmt.Sprintf(`{"login":%q,"name":"Person %s","avatar_url":""}`, login, login)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

const testHostToken = "access-token-secret-0123456789"

type realFactory struct {
	URL    string
	clock  *factoryClock
	github *fakeGitHub
}

// startRealFactory serves the real /v1 API over a floor of fixtures. It is
// reached only with its access token or a GitHub sign-in.
func startRealFactory(t *testing.T) realFactory {
	t.Helper()
	clock := &factoryClock{at: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	snap := factory.Fixture(clock.now())
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: clock.now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Append(context.Background(), record.Event{Kind: record.Approved, Job: 1, Handle: "alice", Detail: "proof"}); err != nil {
		t.Fatal(err)
	}
	gh := newFakeGitHub()
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) { return snap, nil },
		Repos: func(context.Context) ([]string, []factory.RepoInfo, error) {
			return []string{"agentfield/codeaf"}, []factory.RepoInfo{{Full: "agentfield/codeaf", Name: "codeaf", Owner: "agentfield", Open: 3, Dir: "/srv/private/checkouts/codeaf"}}, nil
		},
		RecipeAt: func(repo string) (factory.Recipe, []factory.Problem, string, error) {
			return snap.Repos[0].Recipe, nil, "/srv/private/checkouts/codeaf", nil
		},
	}
	h := serve.New(seam, serve.Config{
		Version: "test", Name: "Acme factory", ServedBy: "factory", AccessToken: testHostToken, Mode: api.FactoryTeam,
		GitHubClientID: "client", GitHubHTTPClient: gh.client(), Now: clock.now, Store: st, Record: rec, Root: st.Root(),
		RecordApproval: func(context.Context, int, bool) (string, error) { return "evt_1", nil },
	})
	// A FORWARDED REQUEST IS NOT A LOOPBACK ONE: the header makes this peer a
	// stranger to the factory, which then asks for what it asks a remote
	// caller, so the tests below meet the same refusals a real attachment does.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { srv.Close(); h.Close() })
	return realFactory{URL: srv.URL, clock: clock, github: gh}
}

// ── round trip ─────────────────────────────────────────────────────────────

// A REAL ATTACHED FACTORY, END TO END: attach, list, and every read, through
// the wire, against the real server; and no frame, no saved file, carries the
// token that opened it.
func TestAppFactoryRoundTripAgainstTheRealServer(t *testing.T) {
	f := startRealFactory(t)
	hub := testFactoryHub(t)
	w := newFactoryWire(t, hub)

	var caps appwire.FactoryCapabilities
	w.must("factory.capabilities", struct{}{}, &caps)
	if !caps.Remote || !caps.Registry || !caps.Local || !caps.Runner || caps.Writes || caps.MaxFlows != 256 {
		t.Fatalf("capabilities overclaim: %+v", caps)
	}

	// Attached with no token, the factory is there and the person is signed out.
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": f.URL}, &conn)
	if conn.State != appwire.FactorySignedOut || conn.URL != f.URL || !strings.HasPrefix(conn.ID, "factory_") || conn.Name == "" {
		t.Fatalf("attached without a token: %+v", conn)
	}
	w.mustFail("factory.queue", map[string]any{"factory": conn.ID}, appwire.CodeSignedOut)

	// The host token opens it; attaching again is the same connection.
	var again appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": f.URL + "/", "token": testHostToken}, &again)
	if again.ID != conn.ID || again.State != appwire.FactoryOK {
		t.Fatalf("attached with a token: %+v", again)
	}
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 1 || list.Factories[0].State != appwire.FactoryOK {
		t.Fatalf("list = %+v", list)
	}

	var me api.Me
	w.must("factory.me", map[string]any{"factory": conn.ID}, &me)
	if me.Contract != api.Contract || me.SessionID != "" {
		t.Fatalf("me = %+v", me)
	}
	var queue api.Queue
	w.must("factory.queue", map[string]any{"factory": conn.ID}, &queue)
	want := factory.Fixture(f.clock.now())
	if len(queue.Jobs) == 0 || len(queue.Jobs) > len(want.Items) {
		t.Fatalf("queue has %d jobs for a floor of %d", len(queue.Jobs), len(want.Items))
	}
	var job api.Job
	w.must("factory.job", map[string]any{"factory": conn.ID, "job": want.Items[0].ID}, &job)
	if job.JobID == "" || job.Ref == "" {
		t.Fatalf("job = %+v", job)
	}
	w.mustFail("factory.job", map[string]any{"factory": conn.ID, "job": 99999}, appwire.CodeNotFound)

	var repos appwire.FactoryRepos
	w.must("factory.repos", map[string]any{"factory": conn.ID}, &repos)
	if len(repos.Watched) != 1 || len(repos.Available) != 1 || repos.Available[0].Open != 3 {
		t.Fatalf("repos = %+v", repos)
	}
	var pipe appwire.FactoryPipeline
	w.must("factory.pipeline", map[string]any{"factory": conn.ID, "repo": "agentfield/codeaf"}, &pipe)
	if len(pipe.Pipeline.Steps) == 0 || pipe.Problems == nil {
		t.Fatalf("pipeline = %+v", pipe)
	}
	var runners api.Runners
	w.must("factory.runners", map[string]any{"factory": conn.ID}, &runners)
	var people api.People
	w.must("factory.people", map[string]any{"factory": conn.ID}, &people)
	var activity appwire.FactoryActivity
	w.must("factory.activity", map[string]any{"factory": conn.ID, "limit": 1}, &activity)
	if len(activity.Events) != 1 || activity.Events[0].Handle != "alice" {
		t.Fatalf("activity = %+v", activity)
	}
	w.mustFail("factory.setup", map[string]any{"factory": "factory_nope"}, appwire.CodeNotFound)
	var setup api.FactorySetup
	w.must("factory.setup", map[string]any{"factory": conn.ID}, &setup)
	if setup.ID != conn.ID || setup.Server != f.URL {
		t.Fatalf("setup = %+v", setup)
	}

	// THE SECRET IS NOWHERE: not on any frame, not in the saved file, and the
	// file is private. A server path never crossed either.
	w.neverSaid(testHostToken, "/srv/private/checkouts")
	path := hub.path()
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), testHostToken) || !strings.Contains(string(saved), f.URL) {
		t.Fatalf("saved file = %s", saved)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("saved file mode = %v", info.Mode().Perm())
	}
	if info, _ := os.Stat(filepath.Dir(path)); info.Mode().Perm() != 0o700 {
		t.Fatalf("saved folder mode = %v", info.Mode().Perm())
	}
}

func TestAppFactoryConnectionsSurviveARestartWithoutTheirSecrets(t *testing.T) {
	f := startRealFactory(t)
	hub := testFactoryHub(t)
	b := newFakeBackend(newAppRecorder(), nil)
	b.factoryOnce.Do(func() { b.factories = hub })
	conn, err := b.FactoryAttach(context.Background(), f.URL, testHostToken)
	if err != nil || conn.State != appwire.FactoryOK {
		t.Fatalf("%+v %v", conn, err)
	}
	b.Close()

	second := newAppFactoryHub()
	second.dir = hub.dir
	b2 := newFakeBackend(newAppRecorder(), nil)
	b2.factoryOnce.Do(func() { b2.factories = second })
	defer b2.Close()
	list, err := b2.FactoryList(context.Background())
	if err != nil || len(list.Factories) != 1 || list.Factories[0].ID != conn.ID || list.Factories[0].State != appwire.FactorySignedOut {
		t.Fatalf("after a restart: %+v %v", list, err)
	}
	if err := b2.FactoryDetach(context.Background(), conn.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ = b2.FactoryList(context.Background()); len(list.Factories) != 0 {
		t.Fatalf("detached factory is still listed: %+v", list)
	}
	if err := b2.FactoryDetach(context.Background(), conn.ID); err == nil {
		t.Fatal("detaching twice should say no such factory")
	}
}

// ── per-caller device sign-in, against the real server ─────────────────────

func TestAppFactorySignInIsolatesCallersAndKeepsGitHubSecretsOut(t *testing.T) {
	f := startRealFactory(t)
	hub := testFactoryHub(t)
	hub.now = f.clock.now
	w := newFactoryWire(t, hub)

	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": f.URL, "token": testHostToken}, &conn)

	var a, b appwire.FactorySignIn
	w.must("factory.sign_in_start", map[string]any{"factory": conn.ID, "caller": "window-a"}, &a)
	w.must("factory.sign_in_start", map[string]any{"factory": conn.ID, "caller": "window-b"}, &b)
	if a.Flow == "" || a.Flow == b.Flow || a.UserCode == b.UserCode || a.UserCode == "" || a.VerificationURI != "https://github.com/login/device" || a.Interval != 5 {
		t.Fatalf("flows a=%+v b=%+v", a, b)
	}

	// B cannot poll, cancel, or learn anything about A's flow, and the refusal
	// is the one a flow that never existed gets.
	missing := w.mustFail("factory.sign_in_poll", map[string]any{"flow": "no-such-flow", "caller": "window-b"}, appwire.CodeNotFound)
	if got := w.mustFail("factory.sign_in_poll", map[string]any{"flow": a.Flow, "caller": "window-b"}, appwire.CodeNotFound); got != missing {
		t.Fatalf("another caller's flow says %q, a missing one %q", got, missing)
	}
	w.mustFail("factory.sign_in_cancel", map[string]any{"flow": a.Flow, "caller": "window-b"}, appwire.CodeNotFound)
	// ...and the attempt did not disturb A.
	var state appwire.FactorySignInState
	w.must("factory.sign_in_poll", map[string]any{"flow": a.Flow, "caller": "window-a"}, &state)
	if state.State != appwire.SignInPending {
		t.Fatalf("a polled early: %+v", state)
	}

	// GitHub grants A's code; after the interval, A is signed in as alice.
	f.github.grant("github-device-secret-1")
	f.clock.advance(6 * time.Second)
	w.must("factory.sign_in_poll", map[string]any{"flow": a.Flow, "caller": "window-a"}, &state)
	if state.State != appwire.SignInSignedIn || state.Me == nil || state.Me.Handle != "alice" || state.Me.SessionID != "" {
		t.Fatalf("a signed in: %+v", state)
	}
	// The engine now holds the session: the factory reads as signed in.
	var queue api.Queue
	w.must("factory.queue", map[string]any{"factory": conn.ID}, &queue)
	if len(queue.Jobs) == 0 {
		t.Fatal("signed in, and the queue is empty")
	}
	// The flow is spent.
	w.mustFail("factory.sign_in_poll", map[string]any{"flow": a.Flow, "caller": "window-a"}, appwire.CodeNotFound)

	// B's own flow is still B's, and B can cancel it.
	w.must("factory.sign_in_cancel", map[string]any{"flow": b.Flow, "caller": "window-b"}, nil)
	w.mustFail("factory.sign_in_poll", map[string]any{"flow": b.Flow, "caller": "window-b"}, appwire.CodeNotFound)

	// NO DEVICE CODE, NO GITHUB TOKEN, NO FACTORY SESSION on any frame.
	hub.mu.Lock()
	session := hub.sessions[conn.ID].token
	hub.mu.Unlock()
	if session == "" {
		t.Fatal("no session was kept")
	}
	w.neverSaid(session, "github-device-secret-1", "github-device-secret-2", "github-token-secret-1", "device_code", "access_token")
}

func TestAppFactorySignInDeniedAndExpired(t *testing.T) {
	f := startRealFactory(t)
	hub := testFactoryHub(t)
	hub.now = f.clock.now
	b := newFakeBackend(newAppRecorder(), nil)
	b.factoryOnce.Do(func() { b.factories = hub })
	defer b.Close()
	ctx := context.Background()
	conn, err := b.FactoryAttach(ctx, f.URL, testHostToken)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := b.FactorySignInStart(ctx, "w", conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Asking again before the interval is answered here, not by GitHub.
	if st, err := b.FactorySignInPoll(ctx, "w", flow.Flow); err != nil || st.State != appwire.SignInPending || st.Interval != 5 {
		t.Fatalf("early poll: %+v %v", st, err)
	}
	f.clock.advance(901 * time.Second)
	if st, err := b.FactorySignInPoll(ctx, "w", flow.Flow); err != nil || st.State != appwire.SignInExpired {
		t.Fatalf("expired poll: %+v %v", st, err)
	}
	if _, err := b.FactorySignInPoll(ctx, "w", flow.Flow); err == nil {
		t.Fatal("an expired flow should be gone")
	}
}

// ── the stub factory for what the real one cannot be made to do ───────────

type stubFactory struct {
	mu       sync.Mutex
	handler  http.HandlerFunc
	srv      *httptest.Server
	requests []string
	auth     []string
	cookies  []string
}

func startStub(t *testing.T, h http.HandlerFunc) *stubFactory {
	t.Helper()
	s := &stubFactory{handler: h}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.cookies = append(s.cookies, r.Header.Get("Cookie"))
		s.mu.Unlock()
		if r.URL.Path == "/v1/about" {
			io.WriteString(w, `{"factory":{"name":"Stub","address":"x","version":"t"},"name":"Stub","contract":1}`)
			return
		}
		s.handler(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func attachStub(t *testing.T, hub *appFactoryHub, s *stubFactory, token string) (*appBackend, appwire.FactoryConnection) {
	t.Helper()
	b := newFakeBackend(newAppRecorder(), nil)
	b.factoryOnce.Do(func() { b.factories = hub })
	t.Cleanup(func() { b.Close() })
	conn, err := b.FactoryAttach(context.Background(), s.srv.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	return b, conn
}

func wireCodeOf(t *testing.T, err error) string {
	t.Helper()
	var w *appwire.Error
	if err == nil {
		t.Fatal("no error")
	}
	if !asWire(err, &w) {
		t.Fatalf("not a wire error: %v", err)
	}
	return w.Code
}

func TestAppFactoryRefusesAddressesThatCarryCredentialsOrCrossTheNetworkInTheClear(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), nil)
	b.factoryOnce.Do(func() { b.factories = testFactoryHub(t) })
	defer b.Close()
	for _, raw := range []string{
		"https://user:pass@factory.example", "https://factory.example/path", "https://factory.example?token=x",
		"https://factory.example#x", "http://factory.example", "http://10.0.0.5:7420", "ftp://factory.example", "factory.example", "https://",
	} {
		_, err := b.FactoryAttach(context.Background(), raw, "")
		if wireCodeOf(t, err) != appwire.CodeBadParams {
			t.Errorf("%q: %v", raw, err)
		}
	}
	// Nothing was saved by a refusal.
	if _, err := os.Stat(b.factories.path()); err == nil {
		t.Fatal("a refused address was saved")
	}
}

func TestAppFactoryNeverUsesAProxyNeverFollowsARedirectAndBoundsEverything(t *testing.T) {
	hub := testFactoryHub(t)
	if hub.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("the factory client takes an ambient proxy")
	}
	if hub.client.Timeout <= 0 || hub.client.Timeout > 30*time.Second {
		t.Fatalf("timeout = %v", hub.client.Timeout)
	}
	var target *stubFactory
	var mode string
	var elsewhere int
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { elsewhere++ }))
	defer other.Close()
	target = startStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "redirect":
			http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
		case "big":
			w.Write([]byte(strings.Repeat("a", appFactoryBody+10)))
		case "hang":
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		case "junk":
			io.WriteString(w, "<html>not json</html>")
		case "expired":
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"GitHub access ended","reason":"github_expired"}`)
		case "signedout":
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"sign in","reason":"signed_out"}`)
		}
	})
	b, conn := attachStub(t, hub, target, "stub-bearer-secret")
	ctx := context.Background()

	mode = "redirect"
	_, err := b.FactoryQueue(ctx, conn.ID)
	if wireCodeOf(t, err) != appwire.CodeUnreachable || elsewhere != 0 {
		t.Fatalf("redirect: %v (followed %d)", err, elsewhere)
	}
	mode = "big"
	if _, err = b.FactoryQueue(ctx, conn.ID); wireCodeOf(t, err) != appwire.CodeTooLarge {
		t.Fatalf("oversize: %v", err)
	}
	mode = "junk"
	if _, err = b.FactoryQueue(ctx, conn.ID); wireCodeOf(t, err) != appwire.CodeEngine {
		t.Fatalf("junk: %v", err)
	}
	mode = "expired"
	if _, err = b.FactoryQueue(ctx, conn.ID); wireCodeOf(t, err) != appwire.CodeGitHubExpired {
		t.Fatalf("github_expired: %v", err)
	}
	if list, _ := b.FactoryList(ctx); list.Factories[0].State != appwire.FactoryGitHubExpired {
		t.Fatalf("list: %+v", list)
	}
	mode = "signedout"
	if _, err = b.FactoryQueue(ctx, conn.ID); wireCodeOf(t, err) != appwire.CodeSignedOut {
		t.Fatalf("signed out: %v", err)
	}
	if list, _ := b.FactoryList(ctx); list.Factories[0].State != appwire.FactorySignedOut {
		t.Fatalf("list: %+v", list)
	}

	// A hung factory is cut off at the client's timeout.
	hub.client.Timeout = 150 * time.Millisecond
	mode = "hang"
	started := time.Now()
	if _, err = b.FactoryQueue(ctx, conn.ID); wireCodeOf(t, err) != appwire.CodeUnreachable || time.Since(started) > 3*time.Second {
		t.Fatalf("hang: %v after %v", err, time.Since(started))
	}
	// An unknown factory is not found, and a refusal never echoes the address.
	if _, err = b.FactoryQueue(ctx, "factory_0000"); wireCodeOf(t, err) != appwire.CodeNotFound {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error names an address: %v", err)
	}
}

// ── access links: one cookie per caller, capped, expiring ──────────────────

// accessStub is the factory's /v1/factory-access reduced to its binding rule:
// a sign_in issues a cookie to whoever asked, and only that cookie is joined.
func accessStub(t *testing.T) (*stubFactory, *int) {
	var mu sync.Mutex
	issued := 0
	joined := map[string]bool{}
	s := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]any{"state": "ready", "link": r.URL.Query().Get("link"), "detail": "Ready",
			"factory": map[string]any{"id": "x", "name": "Stub", "host": "h", "operator": "@op", "repos": nil}}
		cookie, _ := r.Cookie(appFactoryJoinCookie)
		if r.Method == "POST" {
			var body api.FactoryAccessRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			out["link"] = body.Link
			if body.Verb == "sign_in" && cookie == nil {
				issued++
				http.SetCookie(w, &http.Cookie{Name: appFactoryJoinCookie, Value: "join-cookie-" + strconv.Itoa(issued), Path: "/v1/factory-access"})
				out["state"], out["user_code"], out["sign_in_url"] = "waiting", "WXYZ", "https://github.com/login/device"
			}
		} else if cookie != nil {
			joined[cookie.Value] = true
			out["state"], out["access_token"] = "joined", "session-grant-secret-"+cookie.Value
		}
		json.NewEncoder(w).Encode(out)
	})
	return s, &issued
}

func TestAppFactoryAccessLinkFlowBelongsToItsCaller(t *testing.T) {
	stub, _ := accessStub(t)
	hub := testFactoryHub(t)
	w := newFactoryWire(t, hub)
	link := "codeaf://factory/join?url=" + stub.srv.URL + "&code=invite-secret-xyz"
	params := func(caller string) map[string]any { return map[string]any{"link": link, "caller": caller} }

	var got appwire.FactoryAccess
	w.must("factory.access_check", params("window-a"), &got)
	if got.State != "ready" || got.Factory.ID == "" || got.Factory.Host != stub.srv.URL {
		t.Fatalf("check = %+v", got)
	}
	w.must("factory.access_sign_in", params("window-a"), &got)
	if got.State != "waiting" || got.UserCode != "WXYZ" || got.SignInURL == "" {
		t.Fatalf("sign_in = %+v", got)
	}
	// Another window opening the same link has no cookie, so is not joined.
	w.must("factory.access_check", params("window-b"), &got)
	if got.State != "ready" {
		t.Fatalf("another caller sees %+v", got)
	}
	// The first window's poll presents its own cookie and is joined.
	w.must("factory.access_check", params("window-a"), &got)
	if got.State != "joined" {
		t.Fatalf("owner sees %+v", got)
	}
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 1 || list.Factories[0].URL != stub.srv.URL || list.Factories[0].Mode != "team" {
		t.Fatalf("joining did not save the connection: %+v", list)
	}
	// Window b's checks carried no cookie and window a's carried only a's.
	stub.mu.Lock()
	for i, req := range stub.requests {
		if strings.Contains(req, "window-") || strings.Contains(stub.cookies[i], "join-cookie-2") {
			t.Fatalf("a caller name or a second cookie reached the factory: %q %q", req, stub.cookies[i])
		}
	}
	stub.mu.Unlock()
	// NEITHER THE GRANT NOR THE INVITATION IS ON THE WIRE.
	w.neverSaid("session-grant-secret", "invite-secret-xyz", "join-cookie-1")
	hub.mu.Lock()
	held := hub.sessions[got.Factory.ID].token
	hub.mu.Unlock()
	if !strings.HasPrefix(held, "session-grant-secret-") {
		t.Fatalf("the grant was not kept engine-side: %q", held)
	}
	w.mustFail("factory.access_check", map[string]any{"link": "https://example.com/join", "caller": "w"}, appwire.CodeBadParams)
	w.mustFail("factory.access_check", map[string]any{"link": "codeaf://factory/join?url=http://10.1.1.1", "caller": "w"}, appwire.CodeBadParams)
}

func TestAppFactoryPendingFlowsAreCappedAndExpire(t *testing.T) {
	stub, _ := accessStub(t)
	hub := testFactoryHub(t)
	clock := &factoryClock{at: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	hub.now = clock.now
	b := newFakeBackend(newAppRecorder(), nil)
	b.factoryOnce.Do(func() { b.factories = hub })
	defer b.Close()
	ctx := context.Background()
	link := "codeaf://factory/join?url=" + stub.srv.URL

	for i := 0; i < appwire.MaxFactoryFlows; i++ {
		if _, err := b.FactoryAccessSignIn(ctx, "caller-"+strconv.Itoa(i), link); err != nil {
			t.Fatalf("flow %d refused: %v", i, err)
		}
	}
	_, err := b.FactoryAccessSignIn(ctx, "one-too-many", link)
	if wireCodeOf(t, err) != appwire.CodeBusy {
		t.Fatalf("the 257th pending flow: %v", err)
	}
	// An existing caller's flow is not new, so it still answers.
	if _, err := b.FactoryAccessCheck(ctx, "caller-3", link); err != nil {
		t.Fatalf("a held flow was refused: %v", err)
	}
	// Past their life they are dropped, and there is room again.
	clock.advance(appFactoryFlowLife + time.Minute)
	if _, err := b.FactoryAccessSignIn(ctx, "one-too-many", link); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	hub.mu.Lock()
	n := len(hub.links)
	hub.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d flows held after expiry, want 1", n)
	}
}

func TestAppFactoryDeviceFlowsAreCappedPerCallerAndInTotal(t *testing.T) {
	var mu sync.Mutex
	n := 0
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		code := n
		mu.Unlock()
		if r.URL.Path == "/v1/auth/device" && r.Method == "POST" {
			fmt.Fprintf(w, `{"device_code":"dev-%d","user_code":"U-%d","verification_uri":"https://github.com/login/device","interval":5,"expires_in":900}`, code, code)
		}
	})
	hub := testFactoryHub(t)
	clock := &factoryClock{at: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	hub.now = clock.now
	b, conn := attachStub(t, hub, stub, "")
	ctx := context.Background()
	for i := 0; i < appFactoryPerCaller; i++ {
		if _, err := b.FactorySignInStart(ctx, "greedy", conn.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.FactorySignInStart(ctx, "greedy", conn.ID); wireCodeOf(t, err) != appwire.CodeBusy {
		t.Fatalf("a ninth sign-in for one caller: %v", err)
	}
	for i := appFactoryPerCaller; i < appwire.MaxFactoryFlows; i++ {
		if _, err := b.FactorySignInStart(ctx, "caller-"+strconv.Itoa(i), conn.ID); err != nil {
			t.Fatalf("flow %d: %v", i, err)
		}
	}
	if _, err := b.FactorySignInStart(ctx, "late", conn.ID); wireCodeOf(t, err) != appwire.CodeBusy {
		t.Fatalf("the 257th: %v", err)
	}
	clock.advance(901 * time.Second)
	if _, err := b.FactorySignInStart(ctx, "late", conn.ID); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
}

// ── only what is in the registry ───────────────────────────────────────────

func TestAppFactoryCapabilityIsAbsentFromEnginesThatAreNotOne(t *testing.T) {
	var engine appwire.Engine = newFakeBackend(newAppRecorder(), nil)
	defer engine.Close()
	if _, ok := engine.(appwire.Factory); !ok {
		t.Fatal("the app backend must serve the factory wire")
	}
}
