//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	livePublishOptIn = "CODEAF_LIVE_PUBLISH_BRANCH"
	livePublishIssue = "CODEAF_LIVE_PUBLISH_ISSUE"
)

// THE LIVE PUBLISH BESIDE AN OLDER BRANCH. A new home runs an issue whose branch
// name already holds an older run's commits on GitHub, with one real model and
// the signed-in account. The person's Approve opens a draft PR of exactly the
// approved commit, and the older branch does not move.
//
// It OPENS ONE REAL PULL REQUEST, so it runs only when pointed at a repository
// and an issue whose branch already exists. Opt-in, a few cents.
func TestLivePublishOpensThePRBesideAnOlderBranch(t *testing.T) {
	number, _ := strconv.Atoi(os.Getenv(livePublishIssue))
	if os.Getenv(livePublishOptIn) != "1" || number <= 0 {
		t.Skipf("opt-in live lane: set %s=1, %s=<number> and %s=<owner/name>; it opens one real pull request", livePublishOptIn, livePublishIssue, liveSetupRepoEnv)
	}
	if liveFactoryKey == "" || liveSetupGH == "" || liveSetupGHConfig == "" || strings.Count(liveSetupRepo, "/") != 1 {
		t.Fatal("BLOCKED: this needs an OpenRouter key, the signed-in gh and a repository; not faking a pass")
	}
	repo := liveSetupRepo
	checkout := filepath.Join(t.TempDir(), "clone")
	if out, err := livePublishGH("repo", "clone", repo, checkout, "--", "-q"); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}

	appHome, userHome, decoy := useLocalHome(t)
	_, _, _ = appHome, userHome, decoy
	git, _ := exec.LookPath("git")
	t.Setenv("PATH", strings.Join([]string{filepath.Dir(git), filepath.Dir(liveSetupGH), "/usr/bin", "/bin"}, string(os.PathListSeparator)))
	t.Setenv("GH_CONFIG_DIR", liveSetupGHConfig)
	t.Setenv("GH_PROMPT_DISABLED", "1")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv(factoryPostureEnv, "")
	keys := config.NewAppCredentials()
	t.Cleanup(config.UseAppCredentials(keys))
	keys.Set(modelsource.DefaultID, liveFactoryKey)
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)

	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.closeAll)
	launch, err := openV3Launch(proc, v3Options{Model: liveFactoryModel, OneModel: true, Workspace: checkout})
	if err != nil {
		t.Fatal(err)
	}
	assertFactoryPin(t, launch.Config, liveFactoryModel)
	stageSettle = 300 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })

	w := newFactoryWire(t, testFactoryHub(t))
	// This wire opens no agents, so the background brief writer stays off.
	w.backend.briefs.stop()
	w.backend.briefs = newBriefDebounce(time.Hour, func(int, string) {})
	w.backend.local.parent = func() session.Config { return launch.Config }
	s := &localSetup{factoryWire: w, home: appHome, userHm: userHome}
	st := s.start()
	v := s.setup("factory.setup_connect", map[string]any{"source": "gh"})
	if !v.GitHub.Connected {
		t.Fatalf("connect: %q", v.GitHub.Error)
	}
	s.setup("factory.setup_select", map[string]any{"repos": []string{repo}})
	floor := w.backend.local.host.floor
	if err := floor.SetCheckout(repo, checkout); err != nil {
		t.Fatal(err)
	}
	var id int
	for end := time.Now().Add(2 * time.Minute); id == 0; time.Sleep(250 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("issue #%d never reached Backlog", number)
		}
		items, _ := floor.List()
		for _, it := range items {
			if it.Num == number && it.Kind == factory.KindIssue {
				id = it.ID
			}
		}
	}
	if err := floor.Update(id, func(it *factory.Item) error {
		it.Autonomy, it.Cap, it.Assignee = factory.AutonomyManual, 2, v.GitHub.Handle
		it.Stages = []factory.Stage{
			{Name: "write", Kind: factory.StageChat, On: true, Until: "done", Ask: "Create the file c48-note.txt with the write tool, its content exactly `published beside the older branch`. Then call stage_result with done true, findings 0 and a one-line output. Do nothing else."},
			{Name: "verify", Kind: factory.StageCheck, On: true, Ask: "grep -qx 'published beside the older branch' c48-note.txt"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it, _ := floor.Get(id)
	branch := publishBranchForLive(it)
	older := livePublishTip(t, repo, branch)
	if older == "" {
		t.Fatalf("BLOCKED: %s is not on the remote, so this would prove nothing", branch)
	}

	job := func() api.Job {
		var j api.Job
		w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &j)
		return j
	}
	act := func(method string, extra map[string]any) {
		t.Helper()
		params := map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job().Version}}
		for k, v := range extra {
			params[k] = v
		}
		var acted appwire.FactoryJobActed
		w.must(method, params, &acted)
	}
	waitFor := func(what string, ok func(factory.Item) bool) factory.Item {
		t.Helper()
		for end := time.Now().Add(5 * time.Minute); ; time.Sleep(250 * time.Millisecond) {
			it, err := floor.Get(id)
			if err == nil && ok(it) {
				return it
			}
			if time.Now().After(end) {
				t.Fatalf("never %s: %s %q failure %+v publish %+v", what, it.State, it.Question, it.Failure, it.PublishStatus)
			}
		}
	}
	// The factory learns who the person is from GitHub behind its first read.
	for end := time.Now().Add(2 * time.Minute); ; time.Sleep(time.Second) {
		var me api.Me
		if err := w.backend.factoryHub().read(context.Background(), st.Factory, "/v1/me", appFactorySmall, &me); err == nil && strings.TrimSpace(me.Handle) != "" {
			break
		}
		if time.Now().After(end) {
			t.Fatal("the factory never learned who the person is after connecting GitHub")
		}
	}
	var run appwire.FactoryLocalStatus
	w.must("factory.local_run", map[string]any{"caller": "w1"}, &run)
	act("factory.job_start", nil)
	landed := waitFor("landed", func(it factory.Item) bool { return it.State == factory.StateLanded })
	act("factory.job_approve", map[string]any{"commit": landed.HeadCommit})
	done := waitFor("published", func(it factory.Item) bool {
		return it.PublishStatus != nil && (it.PublishStatus.State == "opened" || it.PublishStatus.State == "failed")
	})
	wire := job()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	summary := map[string]any{
		"requested_model": liveFactoryModel, "request_models": recorder.requests, "response_models": recorder.replies, "calls": recorder.calls, "cost_usd": recorder.cost, "problems": recorder.problems,
		"branch": branch, "older_before": older, "older_after": livePublishTip(t, repo, branch), "head": landed.HeadCommit,
		"publishing": done.PublishStatus, "publish_error": wire.PublishError, "pr": done.PullRequest,
	}
	writeLiveFactoryLog(t, summary)
	if done.PublishStatus.State != "opened" || done.PullRequest == nil {
		t.Fatalf("no PR: %v", summary)
	}
	if got := livePublishTip(t, repo, branch); got != older {
		t.Fatalf("the older branch moved from %s to %s", older, got)
	}
	head := done.PullRequest.HeadBranch
	if head == branch || livePublishTip(t, repo, head) != landed.HeadCommit {
		t.Fatalf("the approved commit is not on the PR's own branch %q: %v", head, summary)
	}
	if recorder.calls == 0 || len(recorder.problems) > 0 || recorder.requests[liveFactoryModel] != recorder.calls || len(recorder.replies) != 1 {
		t.Fatalf("model policy: %v", summary)
	}
}

func livePublishGH(args ...string) ([]byte, error) {
	cmd := exec.Command(liveSetupGH, args...)
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+liveSetupGHConfig, "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	return cmd.CombinedOutput()
}

// livePublishTip is the branch's commit on GitHub, "" when it has none.
func livePublishTip(t *testing.T, repo, branch string) string {
	t.Helper()
	out, err := livePublishGH("api", "repos/"+repo+"/git/matching-refs/heads/"+branch)
	if err != nil {
		t.Fatalf("read %s: %v", branch, err)
	}
	var refs []struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(out, &refs); err != nil {
		t.Fatalf("read %s: %v", branch, err)
	}
	for _, r := range refs {
		if r.Ref == "refs/heads/"+branch {
			return r.Object.SHA
		}
	}
	return ""
}

// publishBranchForLive is the name the job's new worktree will take.
func publishBranchForLive(it factory.Item) string {
	return factory.NormalizeHandle(it.Person()) + "/" + strconv.Itoa(it.Num) + "-" + liveSlug(it.Title)
}

func liveSlug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if b.Len() >= 40 {
			break
		}
		if r < 128 && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
