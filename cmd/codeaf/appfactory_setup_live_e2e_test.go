//go:build e2e

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE LIVE LOCAL SETUP. The app's own door, end to end, against a real GitHub
// repository: factory.local_start serves this machine's factory, setup_connect
// takes the signed-in `gh`, setup_select picks ONE repository, the factory's own
// setup worker reads its open issues into Backlog, setup_clone makes a real
// checkout in a temporary folder, and setup_done ends the walk.
//
// SETUP STARTS NOTHING, and this is where that is proved against the real
// forge: no model request, no runner, no job leaving Backlog, and no GitHub
// request that is not a GET. The repository's issues and pull requests are
// read through `gh` before and after and must be byte-identical.
//
// NO FAKE LAUNCH, NO FAKE GITHUB: the factory's GitHub client is the real
// transport behind a recorder that refuses (and counts) anything but a GET.
// Its seam is the product's own: nothing is hooked, nothing is stubbed.
//
// THE TOKEN IS NEVER READ HERE. The product reads it itself through
// `gh auth token` and keeps it in the app's credential memory; this test reads
// only the public profile `gh api user` answers, and afterwards scans the app
// home, the profile and the person's home for the shapes a GitHub token has.
//
// Opt-in: CODEAF_LIVE_FACTORY_SETUP=1 and CODEAF_LIVE_FACTORY_REPO=<owner/name>,
// a repository of the signed-in account with at least one open issue. It reads a
// real account; the repository is named by the person running it, never here.
// The guard counts the factory's API requests: only GETs pass. (`git clone`
// speaks to github.com with a POST to git-upload-pack, which is a read and is
// not an API request, so this guard does not claim to cover it.)

const (
	liveSetupOptIn   = "CODEAF_LIVE_FACTORY_SETUP"
	liveSetupRepoEnv = "CODEAF_LIVE_FACTORY_REPO"
	liveSetupWait    = 90 * time.Second
)

// liveSetupRepo is the repository the run was pointed at.
var liveSetupRepo = strings.TrimSpace(os.Getenv(liveSetupRepoEnv))

// The signed-in gh's configuration folder and binary are read when the test
// binary starts, before TestMain moves HOME, and handed to the product
// explicitly: the one thing of this machine the test lets in.
var (
	liveSetupGHConfig = realGHConfigDir()
	liveSetupGH, _    = exec.LookPath("gh")
)

func realGHConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("GH_CONFIG_DIR")); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "gh")
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".config", "gh")
	}
	return ""
}

// readOnlyGitHub passes every GET to the real GitHub and refuses the rest.
type readOnlyGitHub struct {
	mu      sync.Mutex
	reads   int
	refused []string
}

func (g *readOnlyGitHub) client() *http.Client {
	return &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		g.mu.Lock()
		if r.Method != http.MethodGet {
			g.refused = append(g.refused, r.Method+" "+r.URL.Path)
			g.mu.Unlock()
			return nil, errors.New("the live setup lane makes no GitHub write")
		}
		g.reads++
		g.mu.Unlock()
		return http.DefaultTransport.RoundTrip(r)
	})}
}

// liveGH runs the real gh with the signed-in configuration, read-only verbs.
func liveGH(t *testing.T, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(liveSetupGH, args...)
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+liveSetupGHConfig, "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gh %s: %v: %.200s", args[0], err, stderr.String())
	}
	return out
}

func TestLiveLocalSetupPublishedFactoryAgainstRealRepo(t *testing.T) {
	if os.Getenv(liveSetupOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 and %s=<owner/name> (reads that repository through the signed-in gh)", liveSetupOptIn, liveSetupRepoEnv)
	}
	if !strings.Contains(liveSetupRepo, "/") {
		t.Fatalf("BLOCKED: set %s to the owner/name of a repository with an open issue", liveSetupRepoEnv)
	}
	if liveSetupGH == "" || liveSetupGHConfig == "" {
		t.Fatal("BLOCKED: no gh on PATH or no gh configuration; not faking a pass")
	}

	// ── what GitHub says before: the account, the issues, the pull requests ──
	var me struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(liveGH(t, "api", "user"), &me); err != nil || me.Login == "" {
		t.Fatalf("gh api user: %v (login %q)", err, me.Login)
	}
	snapshot := func() (issues, pulls []byte) {
		issues = liveGH(t, "issue", "list", "-R", liveSetupRepo, "--state", "all", "--limit", "5000", "--json", "number,state,updatedAt,title")
		pulls = liveGH(t, "pr", "list", "-R", liveSetupRepo, "--state", "all", "--limit", "5000", "--json", "number,state,updatedAt,title")
		return
	}
	issuesBefore, pullsBefore := snapshot()
	var open []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(liveGH(t, "issue", "list", "-R", liveSetupRepo, "--state", "open", "--limit", "5000", "--json", "number"), &open); err != nil {
		t.Fatal(err)
	}
	if len(open) == 0 {
		t.Fatalf("BLOCKED: %s has no open issue to read into Backlog", liveSetupRepo)
	}

	// ── isolation: the app home, HOME and profile are this test's own ────────
	appHome, userHome, decoy := useLocalHome(t)
	var dirs []string
	for _, tool := range []string{"git", "gh"} {
		if p, err := exec.LookPath(tool); err == nil {
			dirs = append(dirs, filepath.Dir(p))
		}
	}
	if gh := liveSetupGH; gh != "" {
		dirs = append(dirs, filepath.Dir(gh))
	}
	t.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
	t.Setenv("GH_CONFIG_DIR", liveSetupGHConfig)
	t.Setenv("GH_PROMPT_DISABLED", "1")
	t.Setenv(factoryPostureEnv, "")
	// NO MODEL: any request to a model endpoint is counted, and refused.
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	var modelMu sync.Mutex
	modelHits := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelMu.Lock()
		modelHits++
		modelMu.Unlock()
		http.Error(w, "setup makes no model call", http.StatusServiceUnavailable)
	}))
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)

	w := newFactoryWire(t, testFactoryHub(t))
	gh := &readOnlyGitHub{}
	w.backend.local.githubHTTP = gh.client()
	s := &localSetup{factoryWire: w, home: appHome, userHm: userHome}

	// ── 1. start: this engine serves a quiet factory, with no runner ─────────
	st := s.start()
	if st.State != appwire.LocalRunning || st.Authority != appwire.LocalAuthorityApp || st.Runner {
		t.Fatalf("start: %+v", st)
	}
	host := w.backend.local.host
	if host == nil || !host.quiet {
		t.Fatal("the setup host is not quiet")
	}

	// ── 2. connect with gh, and compare with the account GitHub names ────────
	v := s.setup("factory.setup_read", map[string]any{})
	if v.State != "github" || v.GitHub.Connected {
		t.Fatalf("first step: state %q connected %v", v.State, v.GitHub.Connected)
	}
	v = s.setup("factory.setup_connect", map[string]any{"source": "gh"})
	if v.State != "repos" || !v.GitHub.Connected {
		t.Fatalf("connected: state %q connected %v error %q", v.State, v.GitHub.Connected, v.GitHub.Error)
	}
	if v.GitHub.Handle != factory.NormalizeHandle(me.Login) {
		t.Fatalf("handle %q is not the authenticated login %q", v.GitHub.Handle, factory.NormalizeHandle(me.Login))
	}
	if v.GitHub.Name != me.Name || v.GitHub.AvatarURL != me.AvatarURL || v.GitHub.AvatarURL == "" {
		t.Fatalf("name/avatar differ from /user: name equal %v, avatar equal %v, avatar empty %v", v.GitHub.Name == me.Name, v.GitHub.AvatarURL == me.AvatarURL, v.GitHub.AvatarURL == "")
	}
	if v.GitHub.GHLogin != "" && factory.NormalizeHandle(v.GitHub.GHLogin) != factory.NormalizeHandle(me.Login) {
		t.Fatalf("gh login %q is not %q", v.GitHub.GHLogin, me.Login)
	}
	visible := len(v.Repos)
	if r := repoOf(v, liveSetupRepo); r.ID == "" || r.Archived {
		t.Fatalf("BLOCKED: %s is not a visible, unarchived repository for this account (visible %d)", liveSetupRepo, visible)
	}

	// ── 3. select only the chosen repository; the factory's worker reads its issues ───
	s.setup("factory.setup_select", map[string]any{"repos": []string{liveSetupRepo}})
	deadline := time.Now().Add(liveSetupWait)
	until := func(what string, ok func(appwire.FactorySetupView) bool) appwire.FactorySetupView {
		for {
			cur := s.setup("factory.setup_read", map[string]any{})
			if ok(cur) {
				return cur
			}
			if time.Now().After(deadline) {
				t.Fatalf("setup never reached %s: state %q repo %+v", what, cur.State, repoOf(cur, liveSetupRepo))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	v = until("checkouts", func(c appwire.FactorySetupView) bool { return c.State == "checkouts" })
	selected := 0
	for _, r := range v.Repos {
		if r.Selected {
			selected++
		}
	}
	demo := repoOf(v, liveSetupRepo)
	if selected != 1 || !demo.Selected || demo.Read != "done" || demo.ReadCount != len(open) {
		t.Fatalf("read: selected %d, demo %+v, open issues %d", selected, demo, len(open))
	}

	// The source was unknown at discovery; only the selected repo learned it.
	known := 0
	for _, r := range v.Repos {
		if r.Pipeline != "" {
			known++
		}
	}
	if demo.Pipeline == "" || known != 1 {
		t.Fatalf("pipeline source: selected %q, repos with a known source %d (want 1)", demo.Pipeline, known)
	}

	// Backlog now holds exactly the open issues, and nothing else moved.
	var q api.Queue
	s.must("factory.queue", map[string]any{"factory": st.Factory}, &q)
	backlog := func(q api.Queue) int {
		n := 0
		for _, j := range q.Jobs {
			if j.State != api.StateBacklog {
				t.Fatalf("job %s is %q, not backlog", j.Ref, j.State)
			}
			n++
		}
		return n
	}
	if n := backlog(q); n != len(open) {
		t.Fatalf("backlog holds %d jobs, GitHub has %d open issues", n, len(open))
	}
	revisionAfterRead := q.Factory.Version

	// ── 4. a real clone into an isolated folder ──────────────────────────────
	folder := filepath.Join(resolvedTempDir(t), "clones", "selected")
	s.setup("factory.setup_clone", map[string]any{"repo": liveSetupRepo, "folder": folder})
	v = until("clone found", func(c appwire.FactorySetupView) bool {
		r := repoOf(c, liveSetupRepo)
		if r.Clone == "failed" {
			t.Fatalf("clone failed: %q", r.CloneError)
		}
		return r.Clone == "found"
	})
	if r := repoOf(v, liveSetupRepo); r.CloneError != "" {
		t.Fatalf("clone error %q", r.CloneError)
	}
	if out, err := exec.Command("git", "-C", folder, "rev-parse", "--is-inside-work-tree").Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("the clone is not a work tree: %v", err)
	}
	if out, _ := exec.Command("git", "-C", folder, "remote", "get-url", "origin").Output(); !strings.EqualFold(strings.TrimSpace(string(out)), "https://github.com/"+liveSetupRepo+".git") {
		t.Fatalf("origin is %q", strings.TrimSpace(string(out)))
	}

	// ── 5. done ──────────────────────────────────────────────────────────────
	v = s.setup("factory.setup_done", map[string]any{})
	if v.State != "done" {
		t.Fatalf("done: %q", v.State)
	}

	// ── 6. the real reads, and what setup started: nothing ───────────────────
	s.must("factory.queue", map[string]any{"factory": st.Factory}, &q)
	if n := backlog(q); n != len(open) {
		t.Fatalf("after done, backlog holds %d jobs, want %d", n, len(open))
	}
	var repos appwire.FactoryRepos
	s.must("factory.repos", map[string]any{"factory": st.Factory}, &repos)
	if !reflect.DeepEqual(repos.Watched, []string{liveSetupRepo}) {
		t.Fatalf("watched repositories: %v", repos.Watched)
	}
	var runners api.Runners
	s.must("factory.runners", map[string]any{"factory": st.Factory}, &runners)
	if len(runners.Runners) != 0 || w.backend.local.runner || w.backend.local.runnerDone != nil {
		t.Fatalf("a runner exists: registered %d, flag %v", len(runners.Runners), w.backend.local.runner)
	}
	if release, ok := host.floor.TryWorker(); !ok {
		t.Fatal("a runner was elected")
	} else {
		release()
	}
	modelMu.Lock()
	calls := modelHits
	modelMu.Unlock()
	if calls != 0 {
		t.Fatalf("%d model requests were made", calls)
	}
	gh.mu.Lock()
	reads, refused := gh.reads, append([]string(nil), gh.refused...)
	gh.mu.Unlock()
	if len(refused) != 0 {
		t.Fatalf("setup tried GitHub writes: %v", refused)
	}
	if reads == 0 {
		t.Fatal("the factory made no GitHub read: the lane did not go through the real transport")
	}
	issuesAfter, pullsAfter := snapshot()
	if !bytes.Equal(issuesBefore, issuesAfter) || !bytes.Equal(pullsBefore, pullsAfter) {
		t.Fatal("the repository's issues or pull requests changed while setup ran")
	}

	// ── 7. no credential reached a file the product or the profile keeps ─────
	shape := regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}`)
	for _, dir := range []string{appHome, userHome, decoy} {
		for path, data := range snapshotTree(t, dir) {
			if shape.MatchString(data) {
				t.Fatalf("%s holds something shaped like a GitHub token", path)
			}
		}
	}
	for _, line := range w.lines {
		if shape.MatchString(line) {
			t.Fatal("a wire frame carries something shaped like a GitHub token")
		}
	}

	// Safe counts only: no login, name, avatar or path.
	t.Logf("live setup: visible_repos=%d selected=%d open_issues=%d backlog_jobs=%d queue_version_after_read=%q queue_version_end=%q github_reads=%d github_writes_refused=%d model_calls=%d runners_registered=%d runner_started=%v clone=found watched=%d",
		visible, selected, len(open), len(q.Jobs), revisionAfterRead, q.Factory.Version, reads, len(refused), calls, len(runners.Runners), w.backend.local.runner, len(repos.Watched))

	// Cleanup is the test's own: the engine closes (stopping the loopback
	// service and forgetting its row) before the temporary folders go.
	w.backend.Close()
	if _, err := os.Stat(filepath.Join(v3FactoryRoot(), "serve.json")); err == nil {
		t.Fatal("serve.json survived the engine closing")
	}
}
