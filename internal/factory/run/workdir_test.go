package run

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// gitRig is a real checkout with one commit on main, made with the git on
// the PATH. A machine with no git skips: worktrees are git's to make.
func gitRig(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this machine")
	}
	dir := filepath.Join(t.TempDir(), "api")
	mustGit(t, "", "init", "-q", "-b", "main", dir)
	if err := os.WriteFile(filepath.Join(dir, "ledger.go"), []byte("package ledger\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "ledger.go")
	mustGit(t, dir, "commit", "-q", "-m", "first")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	if dir != "" {
		full = append([]string{"-C", dir}, full...)
	}
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func wdItem() factory.Item {
	return factory.Item{ID: 1, Num: 1, Repo: "acme/api", Title: "Total double-counts!"}
}

// TestForMakesAWorktreeOnItsOwnBranchAndReusesIt is the defect's fix: the
// item's work is in a folder of its own on a branch of its own, the line
// that names the branch is logged once, and a second round finds the same
// folder rather than making another.
func TestForMakesAWorktreeOnItsOwnBranchAndReusesIt(t *testing.T) {
	checkout := gitRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	var mu sync.Mutex
	var logs []string
	w.Log = func(_ factory.Item, line string) { mu.Lock(); logs = append(logs, line); mu.Unlock() }
	it := wdItem()

	if b := w.Branch(it); b != "factory/1-total-double-counts" {
		t.Fatalf("branch = %q", b)
	}
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "api-1" {
		t.Errorf("the worktree is %q, want <root>/api-1", dir)
	}
	if got := mustGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "factory/1-total-double-counts" {
		t.Errorf("the worktree is on %q", got)
	}
	if got := mustGit(t, checkout, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("the person's checkout moved to %q", got)
	}
	again, err := w.For(it, checkout)
	if err != nil || again != dir {
		t.Fatalf("a second round got %q, %v; want the same %q", again, err, dir)
	}
	if len(logs) != 1 || logs[0] != "branch: factory/1-total-double-counts" {
		t.Errorf("logs = %q, want the branch line once", logs)
	}

	// A second item has a folder and a branch of its own.
	other := factory.Item{ID: 2, Num: 7, Repo: "api", Title: "Second thing"}
	d2, err := w.For(other, checkout)
	if err != nil || d2 == dir {
		t.Fatalf("the second item got %q, %v", d2, err)
	}
}

// TestForStartsFromACommitOnADirtyCheckout: uncommitted changes in the
// person's checkout neither stop the worktree nor follow it, and stay put.
func TestForStartsFromACommitOnADirtyCheckout(t *testing.T) {
	checkout := gitRig(t)
	if err := os.WriteFile(filepath.Join(checkout, "ledger.go"), []byte("package ledger // half done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "scratch.txt"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	dir, err := w.For(wdItem(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ledger.go")); string(b) != "package ledger\n" {
		t.Errorf("the worktree started from the dirty file: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch.txt")); err == nil {
		t.Error("an untracked file followed into the worktree")
	}
	if b, _ := os.ReadFile(filepath.Join(checkout, "ledger.go")); string(b) != "package ledger // half done\n" {
		t.Errorf("the person's edit was touched: %q", b)
	}
}

// TestForStartsFromOriginHeadWhenKnown: the base is the remote's default
// branch, so the person's unpushed commit is not on the item's branch.
func TestForStartsFromOriginHeadWhenKnown(t *testing.T) {
	checkout := gitRig(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "-q", "--bare", bare)
	mustGit(t, checkout, "remote", "add", "origin", bare)
	mustGit(t, checkout, "push", "-q", "origin", "main")
	mustGit(t, checkout, "remote", "set-head", "origin", "main")
	pushed := mustGit(t, checkout, "rev-parse", "HEAD")
	mustGit(t, checkout, "commit", "-q", "--allow-empty", "-m", "local only")
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	dir, err := w.For(wdItem(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != pushed {
		t.Errorf("the item starts at %s, want origin/main %s", got, pushed)
	}
}

// TestForChecksOutAnExistingBranch: a branch left from an earlier worktree
// that was removed is checked out again, with its work on it.
func TestForChecksOutAnExistingBranch(t *testing.T) {
	checkout := gitRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := wdItem()
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "the fix")
	tip := mustGit(t, dir, "rev-parse", "HEAD")
	if err := w.Remove(it, checkout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("Remove left the folder")
	}
	mustGit(t, checkout, "rev-parse", "--verify", "refs/heads/factory/1-total-double-counts")
	dir, err = w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != tip {
		t.Errorf("the branch came back at %s, want its own tip %s", got, tip)
	}
}

// TestForNamesItsErrors: the two sentences a person can be shown.
func TestForNamesItsErrors(t *testing.T) {
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := factory.Item{ID: 3, Num: 12, Repo: "acme/api", Title: "x"}
	if _, err := w.For(it, ""); err == nil || err.Error() != "codeaf does not know where acme/api is checked out" {
		t.Errorf("no checkout: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this machine")
	}
	notRepo := t.TempDir()
	_, err := w.For(it, notRepo)
	if err == nil || !strings.HasPrefix(err.Error(), "could not make a worktree for #12: ") || strings.Contains(err.Error(), "\n") {
		t.Errorf("not a repository: %v", err)
	}
}

// TestJobDirAndRunIn pin what loop.go is handed: Workdir wins over RepoDir,
// RepoDir stands when there is no Workdir, and a folder that could not be
// made fails the round without calling the executor.
func TestJobDirAndRunIn(t *testing.T) {
	it := wdItem()
	lp := &floorLoop{r: New(Options{RepoDir: func(string) string { return "/checkout" }})}
	if d, err := lp.jobDir(it); d != "/checkout" || err != nil {
		t.Errorf("RepoDir alone: %q %v", d, err)
	}
	lp.r.opts.Workdir = func(factory.Item) (string, error) { return "/work/api-1", nil }
	if d, err := lp.jobDir(it); d != "/work/api-1" || err != nil {
		t.Errorf("with Workdir: %q %v", d, err)
	}
	called := false
	exec := ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
		called = true
		return factory.StageResult{Done: true}, nil
	})
	if _, err := runIn(context.Background(), exec, Job{dirErr: errors.New("could not make a worktree for #1: boom")}); err == nil || called {
		t.Errorf("a failed folder ran the round: %v %v", err, called)
	}
	if res, err := runIn(context.Background(), exec, Job{Dir: "/work/api-1"}); err != nil || !res.Done || !called {
		t.Errorf("a good folder: %+v %v", res, err)
	}
}

// TestAPrPushesItsBranchToTheRemote is the post's half: the branch the
// worktree is on reaches the bare remote, and the pull request is opened
// from that same branch.
func TestAPrPushesItsBranchToTheRemote(t *testing.T) {
	checkout := gitRig(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "-q", "--bare", bare)
	mustGit(t, checkout, "remote", "add", "origin", bare)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := wdItem()
	var line string
	w.Log = func(_ factory.Item, l string) { line = l }
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "the fix")
	it.Stream = &factory.Stream{Log: []factory.LogLine{{Text: line}, {Text: "read ledger.go"}}}

	f := &fakeSource{}
	ex := NewPostExecutorWith(PostOptions{Source: func(string) factory.Source { return f }, Push: GitPush(ExecGit{})})
	res, err := ex.Run(context.Background(), Job{Item: it, Dir: dir, Stage: factory.Stage{Name: "post", Kind: factory.StagePost, Ask: "pr"}})
	if err != nil || !res.Done {
		t.Fatalf("%+v %v", res, err)
	}
	if f.got[0].Branch != "factory/1-total-double-counts" {
		t.Errorf("the pr is from %q", f.got[0].Branch)
	}
	want := mustGit(t, dir, "rev-parse", "HEAD")
	if got := mustGit(t, bare, "rev-parse", "refs/heads/factory/1-total-double-counts"); got != want {
		t.Errorf("the remote has %s, want %s", got, want)
	}
}

// TestAPrWithNoRemoteSaysSoAndPostsNothing, and a failed push says git's
// last line and posts nothing either.
func TestAPrWithNoRemoteSaysSoAndPostsNothing(t *testing.T) {
	checkout := gitRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := factory.Item{ID: 3, Num: 12, Repo: "api", Title: "Fix it"}
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSource{}
	ex := NewPostExecutorWith(PostOptions{Source: func(string) factory.Source { return f }, Push: GitPush(ExecGit{})})
	job := Job{Item: it, Dir: dir, Stage: factory.Stage{Name: "post", Kind: factory.StagePost, Ask: "pr"}}
	if _, err := ex.Run(context.Background(), job); err == nil || err.Error() != "#12 has no remote to push to" {
		t.Errorf("no remote: %v", err)
	}
	mustGit(t, checkout, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	_, err = ex.Run(context.Background(), job)
	if err == nil || !strings.HasPrefix(err.Error(), "could not push factory/12-fix-it: ") || strings.Contains(err.Error(), "\n") {
		t.Errorf("a failed push: %v", err)
	}
	if len(f.got) != 0 {
		t.Errorf("a pr was opened after the push failed: %+v", f.got)
	}
}
