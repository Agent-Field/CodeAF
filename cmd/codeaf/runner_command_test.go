package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

func TestRunnerJoinUsesTheFactoryAttachmentDoor(t *testing.T) {
	attachLab(t)
	if err := runRunner([]string{"join", "https://factory.example", "--name", "ci-2", "--token", "session-fixture", "--attach-only"}); err != nil {
		t.Fatal(err)
	}
	got, ok := readFactoryAttachment()
	if !ok || got.URL != "https://factory.example" || got.Name != "ci-2" || got.Token != "session-fixture" {
		t.Fatalf("runner attachment: %+v %t", got, ok)
	}
}

func TestRunnerJoinRefusesAnythingButJoin(t *testing.T) {
	attachLab(t)
	for _, args := range [][]string{nil, {"leave"}, {"join"}, {"join", "a", "b"}} {
		if err := runRunner(args); err == nil {
			t.Fatalf("runner %v was accepted", args)
		}
	}
}

// runnerLab is a real factory service (internal/factory/serve over a real
// store) whose GitHub is faked at the transport, with one access link minted.
type runnerLab struct {
	server *httptest.Server
	link   string // the codeaf://factory/join link
	secret string
	row    tokens.Token
	login  string
	store  *store.Store // the factory's own jobs
}

type githubStub func(*http.Request) (*http.Response, error)

func (f githubStub) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newRunnerLab(t *testing.T, login string, expires time.Time) *runnerLab {
	t.Helper()
	// The GitHub poll is rate-limited by the service's own clock, so this one
	// runs ahead of the wall clock by five seconds on every read.
	var mu sync.Mutex
	clock := time.Now()
	return newRunnerLabAt(t, login, expires, func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(6 * time.Second)
		return clock
	})
}

// newRunnerLabAt is [newRunnerLab] on the service clock now. A lab that leases
// a job needs the wall clock: on the racing one every runner reads offline.
func newRunnerLabAt(t *testing.T, login string, expires time.Time, now func() time.Time) *runnerLab {
	t.Helper()
	attachLab(t)
	factoryJoinEvery = 10 * time.Millisecond
	t.Cleanup(func() { factoryJoinEvery = 5 * time.Second })
	lab := &runnerLab{login: login}
	body := func(s string) *http.Response {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(s)), Header: http.Header{}}
	}
	github := &http.Client{Transport: githubStub(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.String() == "https://github.com/login/device/code":
			return body(`{"device_code":"gh-device-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","interval":5,"expires_in":900}`), nil
		case r.URL.String() == "https://github.com/login/oauth/access_token":
			return body(`{"access_token":"gh-user-secret","scope":"read:user,repo","token_type":"bearer"}`), nil
		case r.URL.String() == "https://api.github.com/user":
			return body(`{"login":"` + lab.login + `","name":"Priya","avatar_url":""}`), nil
		case strings.HasPrefix(r.URL.String(), "https://api.github.com/repos/"):
			return body(`{}`), nil
		}
		t.Errorf("unexpected GitHub request %s", r.URL)
		return nil, io.EOF
	})}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	row.Created, row.Expires, row.AccessLink = time.Now(), expires, true
	tokensPath := filepath.Join(t.TempDir(), "tokens.json")
	if err := tokens.Save(tokensPath, []tokens.Token{row}); err != nil {
		t.Fatal(err)
	}
	seam := factory.FixtureSeam(time.Now())
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "operator"}, nil }
	lab.server = httptest.NewUnstartedServer(nil)
	handler := serve.New(seam, serve.Config{Version: "test", Address: "http://" + lab.server.Listener.Addr().String(), AccessToken: "floor-secret", TokensPath: tokensPath,
		GitHubClientID: "client", GitHubHTTPClient: github, Store: st, Root: st.Root(), Record: history, Now: now, Mode: api.FactoryTeam})
	lab.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) })
	lab.server.Start()
	t.Cleanup(func() { lab.server.Close(); handler.Close() })
	lab.row, lab.secret, lab.store = row, row.Token, st
	lab.link = "codeaf://factory/join?url=" + lab.server.URL + "#token=" + row.Token
	return lab
}

func TestParseFactoryJoinLink(t *testing.T) {
	for _, c := range []struct {
		raw, base, secret string
		bad               bool
	}{
		{raw: "codeaf://factory/join?url=https%3A%2F%2Ffactory.example#token=abc", base: "https://factory.example", secret: "abc"},
		{raw: "codeaf://factory/join?url=http://127.0.0.1:7420/#token=a%2Bb", base: "http://127.0.0.1:7420", secret: "a+b"},
		{raw: "codeaf://join?url=https://factory.example#token=abc", base: "https://factory.example", secret: "abc"},
		{raw: "codeaf://factory/join?url=https://factory.example", bad: true},
		{raw: "codeaf://factory/join?url=ftp://factory.example#token=abc", bad: true},
		{raw: "codeaf://factory/open?url=https://factory.example#token=abc", bad: true},
		{raw: "https://factory.example/join#token=abc", bad: true},
	} {
		got, err := parseFactoryJoinLink(c.raw)
		if c.bad {
			if err == nil {
				t.Errorf("%q was accepted: %+v", c.raw, got)
			}
			continue
		}
		if err != nil || got.base != c.base || got.secret != c.secret {
			t.Errorf("%q = %+v, %v; want %s %s", c.raw, got, err, c.base, c.secret)
		}
	}
}

func TestRunnerJoinSignsInAsThePersonAndKeepsOnlyTheirSession(t *testing.T) {
	lab := newRunnerLab(t, "Priya", time.Now().Add(time.Hour))
	var out strings.Builder
	got, err := joinFactory(context.Background(), lab.link, "", "ci-box", "", &out)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != lab.server.URL || got.Name != "ci-box" || got.Token == "" {
		t.Fatalf("attachment: %+v", got)
	}
	if got.Token == lab.secret || strings.Contains(got.Token, "gh-") {
		t.Fatal("the kept session is the link's secret or a GitHub token")
	}
	// THE PERSON SEES WHERE TO GO AND WHAT TO TYPE, and no secret.
	if !strings.Contains(out.String(), "ABCD-EFGH") || !strings.Contains(out.String(), "https://github.com/login/device") {
		t.Fatalf("sign-in prompt: %q", out.String())
	}
	for _, secret := range []string{lab.secret, got.Token, "gh-device-secret", "gh-user-secret"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("a secret was printed: %q", out.String())
		}
	}
	// The session is the PERSON'S: the factory knows the caller as @priya.
	var me api.Me
	if err := appFactoryRequest(context.Background(), factoryTarget{lab.server.URL, got.Token}, "GET", "/v1/me", nil, &me); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(strings.TrimPrefix(me.Handle, "@")) != "priya" {
		t.Fatalf("the session is %q's, want priya's", me.Handle)
	}
	// It is on disk once, privately, and the file is attach.json.
	info, err := os.Stat(factoryAttachPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("attach.json: %v %v", info, err)
	}
	saved, ok := readFactoryAttachment()
	if !ok || saved.Token != got.Token {
		t.Fatalf("saved attachment: %+v %v", saved, ok)
	}
	// And the link is used up: a second person gets one plain sentence.
	_, err = joinFactory(context.Background(), lab.link, "", "other", "", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "already been used") {
		t.Fatalf("a used link answered %v", err)
	}
}

func TestRunnerJoinAcceptsABareSecretWithTheFactoryAddress(t *testing.T) {
	lab := newRunnerLab(t, "sam", time.Now().Add(time.Hour))
	got, err := joinFactory(context.Background(), lab.secret, lab.server.URL, "", "", io.Discard)
	if err != nil || got.URL != lab.server.URL || got.Token == "" {
		t.Fatalf("bare secret: %+v %v", got, err)
	}
	if _, err := joinFactory(context.Background(), "just-a-secret", "", "", "", io.Discard); err == nil {
		// attachLab leaves no attachment, so there is no factory to read the secret against.
		if _, ok := readFactoryAttachment(); !ok {
			t.Fatal("a bare secret with no factory was accepted")
		}
	}
}

func TestRunnerJoinRefusesAnExpiredLinkWithOneSentenceAndKeepsNothing(t *testing.T) {
	lab := newRunnerLab(t, "Priya", time.Now().Add(-time.Minute))
	_, err := joinFactory(context.Background(), lab.link, "", "", "", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired link answered %v", err)
	}
	if _, ok := readFactoryAttachment(); ok {
		t.Fatal("a refused link left an attachment")
	}
}

func TestRunnerJoinStopsWaitingWhenInterrupted(t *testing.T) {
	lab := newRunnerLab(t, "Priya", time.Now().Add(time.Hour))
	// A GitHub that never completes: the poll keeps answering 202.
	lab.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.FactoryAccess{State: "waiting", UserCode: "WAIT-CODE", SignInURL: "https://github.com/login/device"})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := joinFactory(ctx, lab.link, "", "", "", io.Discard)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("an interrupted join answered %v", err)
	}
	if _, ok := readFactoryAttachment(); ok {
		t.Fatal("an interrupted join left an attachment")
	}
}

// A join RUNS THE FACTORY'S JOBS HERE unless it is told to only attach, and
// the worker is handed the person's session and nothing else.
func TestRunnerJoinRunsTheWorkerByDefaultAndNotWhenAttachOnly(t *testing.T) {
	attachLab(t)
	var got []factoryAttachment
	stock := runFactoryRunner
	t.Cleanup(func() { runFactoryRunner = stock })
	runFactoryRunner = func(_ context.Context, a factoryAttachment, _ io.Writer) error {
		got = append(got, a)
		return nil
	}
	if err := runRunner([]string{"join", "https://factory.example", "--token", "session-fixture", "--name", "box"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://factory.example" || got[0].Token != "session-fixture" || got[0].Name != "box" {
		t.Fatalf("the worker was started with %+v", got)
	}
	if err := runRunner([]string{"join", "https://factory.example", "--token", "session-fixture", "--attach-only"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatal("--attach-only started a worker")
	}
}

// THE WORKER LIFECYCLE, AGAINST A REAL SERVICE: joined with a session, the
// runner registers as that person, shows on the factory's runner list, and
// leaves when it is stopped. No model is asked and no job is run.
func TestRunnerJoinRegistersTheMachineAsThePersonsRunnerAndStopsWithItsContext(t *testing.T) {
	lab := newRunnerLab(t, "Priya", time.Now().Add(time.Hour))
	got, err := joinFactory(context.Background(), lab.link, "", "ci-box", "", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var out strings.Builder
	go func() { done <- runFactoryRunner(ctx, got, &out) }()
	target := factoryTarget{lab.server.URL, got.Token}
	deadline := time.Now().Add(20 * time.Second)
	var runners api.Runners
	for time.Now().Before(deadline) {
		runners = api.Runners{}
		if appFactoryRequest(ctx, target, "GET", "/v1/runners", nil, &runners) == nil && len(runners.Runners) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(runners.Runners) != 1 {
		t.Fatalf("runners after joining: %+v", runners)
	}
	if owner := strings.ToLower(strings.TrimPrefix(runners.Runners[0].Owner, "@")); owner != "priya" {
		t.Fatalf("the runner belongs to %q, want priya", runners.Runners[0].Owner)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the runner ended with %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the runner did not stop with its context")
	}
	if strings.Contains(out.String(), got.Token) {
		t.Fatal("the session was printed")
	}
}
