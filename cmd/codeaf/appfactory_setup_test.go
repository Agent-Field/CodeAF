package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/home"
)

const setupSecret = "setup-secret-token-0123456789"

// setupGitHub is a GitHub that answers the three reads first-run setup makes
// and records every request, so a test can say nothing was written to it.
type setupGitHub struct {
	mu       sync.Mutex
	requests []string
}

func (g *setupGitHub) client(t *testing.T) *http.Client {
	return &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		g.mu.Lock()
		g.requests = append(g.requests, r.Method+" "+r.URL.Path)
		g.mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("setup wrote to GitHub: %s %s", r.Method, r.URL)
		}
		if r.URL.Host != "api.github.com" {
			t.Errorf("setup sent a credential to %s", r.URL.Host)
		}
		body, status := `{}`, 200
		switch r.URL.Path {
		case "/user":
			body = `{"login":"owner"}`
		case "/user/repos":
			body = `[{"full_name":"acme/api","open_issues_count":2},{"full_name":"acme/web","open_issues_count":0},{"full_name":"acme/old","archived":true}]`
		case "/repos/acme/api/issues":
			body = `[{"number":1,"title":"First","body":"b","html_url":"https://github.com/acme/api/issues/1"},{"number":2,"title":"Second"},{"number":3,"title":"A pull","pull_request":{"url":"x"}}]`
		case "/repos/acme/web/issues":
			body = `[]`
		default:
			status = 404
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

func (g *setupGitHub) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.requests...)
}

// offlineLocal keeps the local factory off the real forge: GitHub is the fake,
// no pipeline file is read from it, and the floor's link to GitHub is a fixed
// login instead of a call to /user.
func offlineLocal(b *appBackend, g *setupGitHub, t *testing.T) {
	b.local.githubHTTP = g.client(t)
	b.local.rules = func(context.Context, string, string) (pipeline.File, error) { return pipeline.File{}, os.ErrNotExist }
	b.local.seamHook = func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
		return seam
	}
}

// localSetup is a real appBackend behind the real wire, over a throwaway app
// home, with the app's own credential store installed and a fake GitHub.
type localSetup struct {
	*factoryWire
	home   string
	userHm string
	github *setupGitHub
}

// useLocalHome points the app home, the person's home and the profile at
// folders of this test, keeps the ambient profile and tokens out, and gives
// the process only git on its PATH.
func useLocalHome(t *testing.T) (appHome, userHome, decoy string) {
	t.Helper()
	appHome, userHome, decoy = t.TempDir(), t.TempDir(), t.TempDir()
	// The unix socket of a solo host is short-pathed; t.TempDir can be long.
	t.Setenv(home.EnvVar, appHome)
	t.Setenv("HOME", userHome)
	t.Setenv(config.ProfileDirEnv, decoy)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if git, err := exec.LookPath("git"); err == nil {
		t.Setenv("PATH", filepath.Dir(git))
	}
	restore := config.UseAppCredentials(config.NewAppCredentials())
	t.Cleanup(restore)
	return appHome, userHome, decoy
}

func newLocalSetup(t *testing.T) *localSetup {
	t.Helper()
	appHome, userHome, _ := useLocalHome(t)
	w := newFactoryWire(t, testFactoryHub(t))
	gh := &setupGitHub{}
	offlineLocal(w.backend, gh, t)
	return &localSetup{factoryWire: w, home: appHome, userHm: userHome, github: gh}
}

func (s *localSetup) start() appwire.FactoryLocalStatus {
	s.t.Helper()
	var st appwire.FactoryLocalStatus
	s.must("factory.local_start", map[string]any{"caller": "w1"}, &st)
	return st
}

func (s *localSetup) setup(method string, params map[string]any) appwire.FactorySetupView {
	s.t.Helper()
	params["caller"] = "w1"
	var v appwire.FactorySetupView
	s.must(method, params, &v)
	return v
}

// until reads setup until ok, for the factory's own worker finishing a step.
func (s *localSetup) until(what string, ok func(appwire.FactorySetupView) bool) appwire.FactorySetupView {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		v := s.setup("factory.setup_read", map[string]any{})
		if ok(v) {
			return v
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("setup never reached %s: %+v", what, v)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func repoOf(v appwire.FactorySetupView, id string) appwire.FactorySetupRepo {
	for _, r := range v.Repos {
		if r.ID == id {
			return r
		}
	}
	return appwire.FactorySetupRepo{}
}

// gitCheckout makes a folder whose origin is the GitHub repository, the way a
// clone would leave it, without touching a network.
func gitCheckout(t *testing.T, dir, repo string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for _, args := range [][]string{{"init", "-q", dir}, {"-C", dir, "remote", "add", "origin", "https://github.com/" + repo + ".git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// ── nothing exists until a window asks ─────────────────────────────────────

func TestLocalFactoryIsAbsentUntilAsked(t *testing.T) {
	s := newLocalSetup(t)
	var caps appwire.FactoryCapabilities
	s.must("factory.capabilities", struct{}{}, &caps)
	if !caps.Local || !caps.Runner || caps.Writes {
		t.Fatalf("capabilities: %+v", caps)
	}
	if !strings.Contains(s.hello, appwire.FeatureFactoryLocal) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactoryLocal)
	}
	var st appwire.FactoryLocalStatus
	s.must("factory.local_status", map[string]any{"caller": "w1"}, &st)
	if st.State != appwire.LocalStopped || st.Factory != "" || st.URL != "" || st.Attachment.State != appwire.AttachmentNone {
		t.Fatalf("status before start: %+v", st)
	}
	// Reading status serves nothing, and setup before start is not_found.
	if _, err := os.Stat(filepath.Join(v3FactoryRoot(), "serve.json")); err == nil {
		t.Fatal("a factory was served without being asked")
	}
	s.mustFail("factory.setup_read", map[string]any{"caller": "w1"}, appwire.CodeNotFound)
	s.mustFail("factory.setup_discover", map[string]any{"caller": "w1"}, appwire.CodeNotFound)
	if len(s.github.seen()) != 0 {
		t.Fatalf("GitHub was asked: %v", s.github.seen())
	}
}

// ── the whole first run, and nothing started ───────────────────────────────

func TestLocalFactorySetupWalksGitHubReposIssuesAndCheckoutsStartingNothing(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	if st.State != appwire.LocalRunning || st.Authority != appwire.LocalAuthorityApp || !strings.HasPrefix(st.Factory, "factory_") || !strings.HasPrefix(st.URL, "http://127.0.0.1:") {
		t.Fatalf("start: %+v", st)
	}
	host := s.backend.local.host
	if host == nil || !host.quiet {
		t.Fatal("the setup host is not quiet")
	}
	// Starting again is the same factory.
	if again := s.start(); again.Factory != st.Factory || again.URL != st.URL {
		t.Fatalf("second start: %+v", again)
	}
	// The registry sees it, signed in, as a solo factory.
	var list appwire.FactoryList
	s.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 1 || list.Factories[0].ID != st.Factory || list.Factories[0].State != appwire.FactoryOK || list.Factories[0].Mode != "solo" {
		t.Fatalf("list: %+v", list)
	}

	// 1. GitHub.
	v := s.setup("factory.setup_read", map[string]any{})
	if v.State != "github" || v.GitHub.Connected {
		t.Fatalf("first step: %+v", v)
	}
	v = s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret})
	if v.State != "repos" || !v.GitHub.Connected || v.GitHub.VisibleRepos != 3 || v.GitHub.Organization != "acme" || len(v.Repos) != 3 {
		t.Fatalf("connected: %+v", v)
	}
	if key, _ := config.AppCredentialSource().Key("github"); key != setupSecret {
		t.Fatal("GitHub custody moved: the app's credential store does not hold the token")
	}
	if !repoOf(v, "acme/old").Archived || repoOf(v, "acme/api").Clone != "missing" || repoOf(v, "acme/api").Pipeline != "defaults" {
		t.Fatalf("repos: %+v", v.Repos)
	}
	if sug := repoOf(v, "acme/api").SuggestedFolder; sug != "~/code/acme-api" {
		t.Fatalf("suggested folder %q", sug)
	}

	// 2. Visible, unarchived repos only.
	s.mustFail("factory.setup_select", map[string]any{"caller": "w1", "repos": []string{"acme/old"}}, appwire.CodeEngine)
	s.mustFail("factory.setup_select", map[string]any{"caller": "w1", "repos": []string{"other/repo"}}, appwire.CodeEngine)
	s.setup("factory.setup_select", map[string]any{"repos": []string{"acme/api", "acme/web"}})

	// 3. The factory's own worker reads the issues into Backlog.
	v = s.until("checkouts", func(v appwire.FactorySetupView) bool { return v.State == "checkouts" })
	if r := repoOf(v, "acme/api"); r.Read != "done" || r.ReadCount != 2 || !r.Selected {
		t.Fatalf("api read: %+v", r)
	}
	if r := repoOf(v, "acme/web"); r.Read != "empty" || r.ReadCount != 0 {
		t.Fatalf("web read: %+v", r)
	}

	// 4. Nothing started: no job is running or queued, no runner exists, no
	// runner election was entered, and GitHub was only read.
	var q api.Queue
	s.must("factory.queue", map[string]any{"factory": st.Factory}, &q)
	for _, j := range q.Jobs {
		if j.State != "backlog" {
			t.Fatalf("setup left a job %d in %q", j.ID, j.State)
		}
	}
	if len(q.Jobs) != 2 {
		t.Fatalf("expected the two issues as Backlog jobs: %+v", q.Jobs)
	}
	var runners api.Runners
	s.must("factory.runners", map[string]any{"factory": st.Factory}, &runners)
	if len(runners.Runners) != 0 {
		t.Fatalf("setup registered a runner: %+v", runners.Runners)
	}
	if release, ok := host.floor.TryWorker(); !ok {
		t.Fatal("a runner was elected")
	} else {
		release()
	}
	for _, r := range s.github.seen() {
		if !strings.HasPrefix(r, "GET ") {
			t.Fatalf("GitHub request %q", r)
		}
	}

	// 5. A checkout: keep one that is there, or leave it for later. The clone
	// of a folder that already holds the repository keeps it (no network).
	existing := filepath.Join(s.userHm, "code", "acme-api")
	gitCheckout(t, existing, "acme/api")
	v = s.setup("factory.setup_clone", map[string]any{"repo": "acme/api", "folder": "~/code/acme-api"})
	v = s.until("api found", func(v appwire.FactorySetupView) bool { return repoOf(v, "acme/api").Clone == "found" })
	if r := repoOf(v, "acme/api"); r.Folder != "~/code/acme-api" || r.CloneError != "" {
		t.Fatalf("api checkout: %+v", r)
	}
	v = s.setup("factory.setup_skip", map[string]any{"repo": "acme/web"})
	if repoOf(v, "acme/web").Clone != "skipped" {
		t.Fatalf("web: %+v", repoOf(v, "acme/web"))
	}
	v = s.setup("factory.setup_done", map[string]any{})
	if v.State != "done" {
		t.Fatalf("done: %+v", v)
	}

	// Still nothing running, and the secrets are nowhere a person or a log
	// could read them: not in a frame, not on disk.
	s.must("factory.queue", map[string]any{"factory": st.Factory}, &q)
	for _, j := range q.Jobs {
		if j.State != "backlog" {
			t.Fatalf("a job moved to %q", j.State)
		}
	}
	s.neverSaid(setupSecret, host.token)
	for _, root := range []string{s.home} {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err == nil && info.Mode().IsRegular() {
				if data, _ := os.ReadFile(path); strings.Contains(string(data), setupSecret) || strings.Contains(string(data), host.token) {
					t.Errorf("%s holds a secret", path)
				}
			}
			return nil
		})
	}
}

// ── the app's home decides, not the environment's profile ──────────────────

func TestLocalFactoryUsesTheAppHomeNotTheAmbientProfile(t *testing.T) {
	appHome, _, decoy := useLocalHome(t)
	w := newFactoryWire(t, testFactoryHub(t))
	gh := &setupGitHub{}
	offlineLocal(w.backend, gh, t)
	s := &localSetup{factoryWire: w, home: appHome, github: gh}
	s.start()
	s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret})
	host := s.backend.local.host
	if host.profile != home.Join() {
		t.Fatalf("profile %q is not the app home %q", host.profile, home.Join())
	}
	if !strings.HasPrefix(host.floor.Root(), appHome) {
		t.Fatalf("floor %q is outside the app home", host.floor.Root())
	}
	// The app's credential store keeps its own bookkeeping in the process's
	// profile (config.AppCredentials, unchanged); the factory puts nothing of
	// its own there, and no secret.
	entries, _ := os.ReadDir(decoy)
	for _, e := range entries {
		if e.Name() != "config.json" && e.Name() != "config.json.lock" {
			t.Fatalf("the ambient profile was written: %v", entries)
		}
		if data, _ := os.ReadFile(filepath.Join(decoy, e.Name())); strings.Contains(string(data), setupSecret) {
			t.Fatalf("%s holds the token", e.Name())
		}
	}
}

// ── one authority per machine, and nothing left behind ─────────────────────

func TestLocalFactoryElectsOneAuthorityAndQuitCleansUp(t *testing.T) {
	useLocalHome(t)
	hubA := testFactoryHub(t)
	a := newFactoryWire(t, hubA)
	offlineLocal(a.backend, &setupGitHub{}, t)
	var first appwire.FactoryLocalStatus
	a.must("factory.local_start", map[string]any{"caller": "a"}, &first)
	if first.Authority != appwire.LocalAuthorityApp {
		t.Fatalf("first: %+v", first)
	}

	// A second engine on the same machine joins the first; it serves nothing.
	b := newFactoryWire(t, testFactoryHub(t))
	var second appwire.FactoryLocalStatus
	b.must("factory.local_start", map[string]any{"caller": "b"}, &second)
	if second.Authority != appwire.LocalAuthorityOther || second.URL != first.URL || b.backend.local.host != nil || b.backend.local.server != nil {
		t.Fatalf("second: %+v", second)
	}
	// ... and reads the one factory through it, signed in by the discovery.
	var queue api.Queue
	b.must("factory.queue", map[string]any{"factory": second.Factory}, &queue)

	// Quitting the joiner leaves the authority serving.
	b.backend.closeLocalFactory()
	var st appwire.FactoryLocalStatus
	b.must("factory.local_status", map[string]any{"caller": "b"}, &st)
	if st.State != appwire.LocalStopped {
		t.Fatalf("closed joiner: %+v", st)
	}
	if _, ok := liveFactoryDiscovery(); !ok {
		t.Fatal("the joiner's quit stopped the authority")
	}
	b.mustFail("factory.local_start", map[string]any{"caller": "b"}, appwire.CodeClosed)

	// Quitting the authority takes down the listener, the discovery record
	// and the lock, and forgets its row.
	host := a.backend.local.host
	a.backend.closeLocalFactory()
	if _, ok := liveFactoryDiscovery(); ok {
		t.Fatal("discovery outlived the authority")
	}
	if _, err := os.Stat(filepath.Join(v3FactoryRoot(), "serve.json")); err == nil {
		t.Fatal("serve.json outlived the authority")
	}
	if _, err := http.Get(first.URL + "/v1/about"); err == nil {
		t.Fatal("the listener outlived the authority")
	}
	if release, ok := host.floor.TryWorker(); ok {
		release()
	}
	hubA.mu.Lock()
	rows := len(hubA.rows)
	hubA.mu.Unlock()
	if rows != 0 {
		t.Fatalf("the list kept %d rows", rows)
	}
	data, _ := os.ReadFile(hubA.path())
	if strings.Contains(string(data), host.token) {
		t.Fatal("the registry file holds the bearer")
	}

	// The next engine is elected again.
	c := newFactoryWire(t, testFactoryHub(t))
	offlineLocal(c.backend, &setupGitHub{}, t)
	var third appwire.FactoryLocalStatus
	c.must("factory.local_start", map[string]any{"caller": "c"}, &third)
	if third.Authority != appwire.LocalAuthorityApp {
		t.Fatalf("third: %+v", third)
	}
}

// ── the CLI's attachment ───────────────────────────────────────────────────

func TestLocalFactoryAdoptsAnExplicitCLIAttachmentWithoutSavingItsBearer(t *testing.T) {
	useLocalHome(t)
	const cliBearer = "cli-attach-bearer-secret-9876543210"
	var sawBearer sync.Map
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			sawBearer.Store(got, true)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/about":
			fmt.Fprint(w, `{"contract":`+fmt.Sprint(api.Contract)+`,"name":"Team box","factory":{"name":"Team box"}}`)
		case "/v1/me":
			if r.Header.Get("Authorization") != "Bearer "+cliBearer {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"contract":`+fmt.Sprint(api.Contract)+`,"mode":"team","factory":{"name":"Team box"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	hub := testFactoryHub(t)
	w := newFactoryWire(t, hub)
	offlineLocal(w.backend, &setupGitHub{}, t)

	// No attach.json: nothing to adopt.
	w.mustFail("factory.local_adopt", map[string]any{"caller": "w"}, appwire.CodeNotFound)

	if err := writeFactoryAttachment(factoryAttachment{URL: remote.URL, Name: "laptop", Token: cliBearer}); err != nil {
		t.Fatal(err)
	}
	var st appwire.FactoryLocalStatus
	w.must("factory.local_status", map[string]any{"caller": "w"}, &st)
	if st.Attachment.State != appwire.AttachmentAvailable || st.Attachment.URL != remote.URL || st.Attachment.Name != "laptop" {
		t.Fatalf("attachment before: %+v", st.Attachment)
	}
	// The attachment coexists with the local factory: starting does not
	// adopt it, and adopting does not start anything.
	w.must("factory.local_start", map[string]any{"caller": "w"}, &st)
	if st.State != appwire.LocalRunning || st.Attachment.State != appwire.AttachmentAvailable {
		t.Fatalf("start with an attachment: %+v", st)
	}
	local := st.Factory
	w.must("factory.local_adopt", map[string]any{"caller": "w"}, &st)
	if st.Attachment.State != appwire.AttachmentAdopted || st.Factory != local || st.State != appwire.LocalRunning {
		t.Fatalf("adopted: %+v", st)
	}
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	states := map[string]string{}
	for _, f := range list.Factories {
		states[f.ID] = f.State
	}
	if len(list.Factories) != 2 || states[local] != appwire.FactoryOK || states[st.Attachment.Factory] != appwire.FactoryOK {
		t.Fatalf("list: %+v", list)
	}
	// The bearer reached only the factory it belongs to, and is in no file of
	// the registry and no frame.
	data, _ := os.ReadFile(hub.path())
	if strings.Contains(string(data), cliBearer) {
		t.Fatalf("the registry saved the CLI bearer: %s", data)
	}
	w.neverSaid(cliBearer)
	// The CLI's own file is untouched by the app.
	if a, ok := readFactoryAttachment(); !ok || a.Token != cliBearer {
		t.Fatalf("attach.json changed: %+v", a)
	}
}

func TestLocalFactoryNeverAdoptsAnUnsafeAttachment(t *testing.T) {
	useLocalHome(t)
	w := newFactoryWire(t, testFactoryHub(t))
	// Plain http to another machine would put a bearer on the network.
	if err := writeFactoryAttachment(factoryAttachment{URL: "http://team.example.com", Token: "never-sent-bearer-value"}); err != nil {
		t.Fatal(err)
	}
	var st appwire.FactoryLocalStatus
	w.must("factory.local_status", map[string]any{"caller": "w"}, &st)
	if st.Attachment.State != appwire.AttachmentUnsafe || st.Attachment.URL != "" {
		t.Fatalf("unsafe attachment: %+v", st.Attachment)
	}
	w.mustFail("factory.local_adopt", map[string]any{"caller": "w"}, appwire.CodeForbidden)
	w.neverSaid("never-sent-bearer-value")
}

// ── refusals ───────────────────────────────────────────────────────────────

func TestLocalFactoryRefusesBadCallersAndForbiddenFolders(t *testing.T) {
	s := newLocalSetup(t)
	for _, m := range []string{"factory.local_start", "factory.local_status", "factory.local_adopt", "factory.setup_read", "factory.setup_discover"} {
		s.mustFail(m, map[string]any{}, appwire.CodeBadParams)
		s.mustFail(m, map[string]any{"caller": "   "}, appwire.CodeBadParams)
		s.mustFail(m, map[string]any{"caller": strings.Repeat("c", 129)}, appwire.CodeBadParams)
	}
	// A field the wire does not carry is refused and nothing starts.
	s.mustFail("factory.local_start", map[string]any{"caller": "w1", "url": "http://elsewhere"}, appwire.CodeBadParams)
	if s.backend.local.running {
		t.Fatal("a refused start started")
	}
	s.start()
	s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret})

	// Out of order: the factory's own sentence, with no secret in it.
	msg := s.mustFail("factory.setup_done", map[string]any{"caller": "w1"}, appwire.CodeEngine)
	s.mustFail("factory.setup_connect", map[string]any{"caller": "w1", "source": "token", "token": "another-secret"}, appwire.CodeEngine)
	if strings.Contains(msg, setupSecret) {
		t.Fatalf("a refusal carries the token: %q", msg)
	}
	s.setup("factory.setup_select", map[string]any{"repos": []string{"acme/api"}})
	s.until("checkouts", func(v appwire.FactorySetupView) bool { return v.State == "checkouts" })

	inside := filepath.Join(s.home, "v3", "factory", "checkouts", "api")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(s.home, link); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{inside, s.home, filepath.Join(s.home, "anything"), "/", filepath.Join(link, "api"), filepath.Join(s.home, "..", filepath.Base(s.home), "x")} {
		for _, m := range []string{"factory.setup_clone", "factory.setup_use_folder"} {
			s.mustFail(m, map[string]any{"caller": "w1", "repo": "acme/api", "folder": folder}, appwire.CodeForbidden)
		}
	}
	s.mustFail("factory.setup_clone", map[string]any{"caller": "w1", "repo": "acme/api", "folder": "code/api"}, appwire.CodeBadParams)
	// A folder that holds some other repository is refused by the factory.
	other := filepath.Join(s.userHm, "other")
	gitCheckout(t, other, "someone/else")
	s.mustFail("factory.setup_use_folder", map[string]any{"caller": "w1", "repo": "acme/api", "folder": other}, appwire.CodeEngine)
	// Nothing was cloned or kept by any of that.
	v := s.setup("factory.setup_read", map[string]any{})
	if r := repoOf(v, "acme/api"); r.Clone != "missing" || r.Folder != "" {
		t.Fatalf("a refused folder changed the repo: %+v", r)
	}
	s.neverSaid(setupSecret, "another-secret")
}
