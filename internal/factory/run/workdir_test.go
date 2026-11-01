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
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
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
	mustGit(t, checkout, "worktree", "remove", "--force", dir)
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

// A DROP WITH NOTHING TO LOSE DELETES THE FOLDER AND THE LOCAL BRANCH, and
// only those: the checkout keeps its files. The worktree is found from inside
// itself, so a folder made from another checkout than the one the factory
// names (a window opened in the repository) is deleted all the same, and a
// second discard finds nothing left and does nothing.
func TestDiscardWithNoWorkDeletesTheFolderAndTheLocalBranch(t *testing.T) {
	checkout := gitRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := wdItem()
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	if work, err := w.Work(it, checkout); err != nil || work.Any() || work.String() != "" {
		t.Fatalf("a fresh worktree holds work: %+v %v", work, err)
	}
	other := gitRig(t)
	if err := w.Discard(it, other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the dropped job's folder remains")
	}
	if branches := mustGit(t, checkout, "branch", "--list", w.Branch(it)); branches != "" {
		t.Fatalf("the dropped job's branch remains: %q", branches)
	}
	if _, err := os.Stat(filepath.Join(checkout, "ledger.go")); err != nil {
		t.Fatal("the checkout changed")
	}
	if err := w.Discard(it, checkout); err != nil {
		t.Fatal("a second discard is not a no-op:", err)
	}
}

// WORK IS NAMED BEFORE IT IS LOST: commits on the job's branch beyond where it
// started, and changes in its folder that were never committed, untracked
// files among them. Discarding it anyway deletes it all.
func TestWorkNamesCommitsAndUncommittedChanges(t *testing.T) {
	checkout := gitRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := wdItem()
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	it.ChangeBase = mustGit(t, dir, "rev-parse", "HEAD")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "one")
	if work, err := w.Work(it, checkout); err != nil || work.String() != "1 commit" {
		t.Fatalf("one commit: %+v %v", work, err)
	}
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "two")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "three")
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package ledger\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	work, err := w.Work(it, checkout)
	if err != nil || work.Commits != 3 || !work.Uncommitted || work.String() != "3 commits and uncommitted changes" {
		t.Fatalf("work %+v %q %v", work, work.String(), err)
	}
	// With no pinned start the count is from where a new branch would start.
	it.ChangeBase = ""
	if work, err := w.Work(it, checkout); err != nil || work.Commits != 3 {
		t.Fatalf("unpinned %+v %v", work, err)
	}
	mustGit(t, dir, "reset", "-q", "--hard", "HEAD~3")
	if err := os.Remove(filepath.Join(dir, "new.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ledger.go"), []byte("package ledger // edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if work, err := w.Work(it, checkout); err != nil || work.String() != "uncommitted changes" {
		t.Fatalf("an edit alone: %+v %v", work, err)
	}
	if err := w.Discard(it, checkout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the folder with work in it remains")
	}
	if branches := mustGit(t, checkout, "branch", "--list", w.Branch(it)); branches != "" {
		t.Fatalf("the branch remains: %q", branches)
	}
}

// A PULL REQUEST'S JOB KEEPS ITS BRANCH: only its folder goes, so only what
// is uncommitted there is work a drop would lose.
func TestAPullRequestJobsDiscardKeepsItsBranch(t *testing.T) {
	checkout, _ := prRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := prItem()
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "a review note")
	if work, err := w.Work(it, checkout); err != nil || work.Any() {
		t.Fatalf("a commit on a kept branch is counted as lost: %+v %v", work, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if work, err := w.Work(it, checkout); err != nil || work.String() != "uncommitted changes" {
		t.Fatalf("work %+v %v", work, err)
	}
	if err := w.Discard(it, checkout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the pull request job's folder remains")
	}
	mustGit(t, checkout, "rev-parse", "--verify", "refs/heads/"+w.Branch(it))
}

// NOTHING ON THE REMOTE IS TOUCHED: a branch already pushed stays there after
// the local one is deleted.
func TestDiscardLeavesTheRemoteBranch(t *testing.T) {
	checkout := gitRig(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "-q", "--bare", bare)
	mustGit(t, checkout, "remote", "add", "origin", bare)
	mustGit(t, checkout, "push", "-q", "origin", "main")
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := wdItem()
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "the fix")
	mustGit(t, dir, "push", "-q", "origin", w.Branch(it))
	pushed := mustGit(t, bare, "rev-parse", "refs/heads/"+w.Branch(it))
	if err := w.Discard(it, checkout); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, bare, "rev-parse", "refs/heads/"+w.Branch(it)); got != pushed {
		t.Fatalf("the remote branch moved: %s, want %s", got, pushed)
	}
	if got := mustGit(t, checkout, "ls-remote", "--heads", "origin"); !strings.Contains(got, w.Branch(it)) {
		t.Fatalf("the remote lost the branch: %q", got)
	}
}

// A named namespace is pinned by the created branch record, even if the job
// is reassigned before it is dropped.
func TestWorktreeUsesItsPersonsNamespaceAndKeepsItsRecordedBranch(t *testing.T) {
	checkout := gitRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	w.Prefix = func(it factory.Item) string { return it.Assignee }
	it := wdItem()
	it.Assignee = "owner"
	var made string
	w.Log = func(_ factory.Item, line string) { made = strings.TrimPrefix(line, "branch: ") }
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, dir, "branch", "--show-current"); got != "owner/1-total-double-counts" {
		t.Fatal(got)
	}
	it.Change = &factory.Change{Branch: made}
	it.Stream = &factory.Stream{Log: []factory.LogLine{{Text: "branch: " + made}}}
	it.Assignee = "someone-else"
	it.Title = "Renamed issue"
	if !strings.Contains(workTree(Job{Item: it, Dir: dir}), made) {
		t.Fatal("brief lost recorded branch")
	}
	mustGit(t, checkout, "worktree", "remove", dir)
	if _, err := w.For(it, checkout); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, dir, "branch", "--show-current"); got != made {
		t.Fatal("recovery changed branch: " + got)
	}
	if w.ownBranch(it) != made {
		t.Fatal("lost recorded branch")
	}
	if err := w.Discard(it, checkout); err != nil {
		t.Fatal(err)
	}
	if _, err := w.git.Run(checkout, "show-ref", "--verify", "refs/heads/"+made); err == nil {
		t.Fatal("branch was not discarded")
	}
}

// The first brief reads the branch recorded while preparing its worktree.
func TestFirstBriefNamesTheCreatedBranch(t *testing.T) {
	checkout := gitRig(t)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := wdItem()
	original.ID = 0
	original.Assignee = "owner"
	original.Stream = &factory.Stream{}
	it, err := st.Create(original)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorkdirs(filepath.Join(st.Root(), "work"), ExecGit{})
	w.Prefix = func(it factory.Item) string { return it.Person() }
	w.Log = StoreLog(st, time.Now)
	r := New(Options{Store: st, Workdir: func(it factory.Item) (string, error) { return w.For(it, checkout) }})
	lp := &floorLoop{r: r}
	job := lp.job(&loopCtl{id: it.ID, ctx: context.Background()}, factory.Stage{Name: "plan"}, 0, 0, 1)
	if !strings.Contains(workTree(job), "owner/1-total-double-counts") {
		t.Fatalf("first brief: %s", workTree(job))
	}
}

func TestBranchRecordsCannotNameUnrelatedLocalWork(t *testing.T) {
	w := NewWorkdirs(t.TempDir(), ExecGit{})
	w.Prefix = func(factory.Item) string { return "owner" }
	it := wdItem()
	it.Stream = &factory.Stream{Log: []factory.LogLine{{Text: "branch: main"}, {Text: "branch: owner/2-other-job"}}}
	if got := w.ownBranch(it); got != "owner/1-total-double-counts" {
		t.Fatalf("unrelated branch: %s", got)
	}
	it.Stream.Log = append(it.Stream.Log, factory.LogLine{Text: "branch: previous-owner/1-original"})
	it.Change = &factory.Change{Branch: "factory/1-old-record"}
	if got := w.ownBranch(it); got != "previous-owner/1-original" {
		t.Fatalf("ignored current record: %s", got)
	}
}

// A WORKTREE AN EARLIER RELEASE NAMED DIFFERENTLY still holds the job's
// branch: the folder once took the repository's display name ("CodeAF-1796"),
// now its slug. Discard finds it by the branch it holds inside this work root
// and deletes both; once, the branch alone was refused as "used by worktree",
// at every launch.
func TestDiscardFindsTheJobsWorktreeUnderAnEarlierName(t *testing.T) {
	checkout := gitRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := wdItem()
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	earlier := filepath.Join(filepath.Dir(dir), "Earlier-"+filepath.Base(dir))
	mustGit(t, checkout, "worktree", "move", dir, earlier)
	if err := w.Discard(it, checkout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(earlier); !os.IsNotExist(err) {
		t.Fatal("the job's worktree under its earlier name remains")
	}
	if branches := mustGit(t, checkout, "branch", "--list", w.Branch(it)); branches != "" {
		t.Fatalf("the job's branch remains: %q", branches)
	}
}
