package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// prRig is a remote with main and a pull request #7 whose head is a commit
// main does not have, served at refs/pull/7/head the way GitHub serves a
// fork's pull request too, and a clone of it as the person's checkout. main
// moves on after the pull request was opened, so a branch cut from main is
// not the pull request. It answers the checkout and the head's commit.
func prRig(t *testing.T) (checkout, head string) {
	t.Helper()
	remote := gitRig(t)
	mustGit(t, remote, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(remote, "ledger.go"), []byte("package ledger\n\nfunc Total() int { return 1 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, remote, "commit", "-q", "-am", "the change")
	head = mustGit(t, remote, "rev-parse", "HEAD")
	mustGit(t, remote, "update-ref", "refs/pull/7/head", head)
	mustGit(t, remote, "checkout", "-q", "main")
	mustGit(t, remote, "branch", "-q", "-D", "feature")
	checkout = filepath.Join(t.TempDir(), "clone")
	mustGit(t, "", "clone", "-q", remote, checkout)
	mustGit(t, remote, "commit", "-q", "--allow-empty", "-m", "main moves on")
	return checkout, head
}

func prItem() factory.Item {
	return factory.Item{ID: 2, Num: 7, Kind: factory.KindPR, Repo: "api", Title: "total counts once", Base: "main", Head: "alice:feature"}
}

// A PULL REQUEST'S WORKTREE IS ITS HEAD, with its base beside it, so the
// step reads the change with `git diff origin/main...HEAD` and never goes to
// the network for it.
func TestAPullRequestsWorktreeIsCutAtItsHead(t *testing.T) {
	checkout, head := prRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	dir, err := w.For(prItem(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatalf("the worktree is at %s, want the pull request's head %s", got, head)
	}
	if got := mustGit(t, dir, "diff", "--stat", PullBase(prItem())+"...HEAD"); !strings.Contains(got, "ledger.go") {
		t.Fatalf("the change is not read against the base: %q", got)
	}
}

// A WORKTREE CUT BEFORE THIS LAW, from the default branch, is brought to the
// head the next round, because nothing of its own is on it; one a step wrote
// into stays where it is.
func TestAnOldPullRequestWorktreeIsBroughtToItsHead(t *testing.T) {
	checkout, head := prRig(t)
	it := prItem()
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	dir := w.Dir(it)
	mustGit(t, checkout, "worktree", "add", "-q", "-b", w.Branch(it), dir, "origin/main")
	var logs []string
	w.Log = func(_ factory.Item, line string) { logs = append(logs, line) }
	if _, err := w.For(it, checkout); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatalf("the old worktree stayed at %s, want %s", got, head)
	}
	if len(logs) != 1 || !strings.HasPrefix(logs[0], "at the head of #7: ") {
		t.Fatalf("logs = %q", logs)
	}

	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "a step's own fix")
	own := mustGit(t, dir, "rev-parse", "HEAD")
	it.HeadSHA = "0000000000000000000000000000000000000000"
	if _, err := w.For(it, checkout); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != own {
		t.Fatalf("a worktree with a step's commit was moved to %s", got)
	}
}

// A repository with no remote has nowhere to fetch a pull request from: its
// worktree is cut the way an issue's is, and the round still runs.
func TestAPullRequestWithNoRemoteIsCutFromTheCheckout(t *testing.T) {
	checkout := gitRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	dir, err := w.For(prItem(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mustGit(t, dir, "rev-parse", "HEAD"), mustGit(t, checkout, "rev-parse", "HEAD"); got != want {
		t.Fatalf("at %s, want %s", got, want)
	}
}

func prJob() Job {
	it := prItem()
	it.Product, it.Author = "acme", "alice"
	it.Body = "Total counted refunds twice.\n\nFixes #3.\n\n## How it was checked\n\ngo test ./ledger"
	it.Diff = "+2 −1"
	it.Files = []factory.FileChange{{Path: "ledger.go", Added: 2, Removed: 1}, {Path: "ledger_test.go", Added: 9}}
	it.CheckRuns = []factory.CheckRun{{Name: "test", State: "success"}, {Name: "lint", State: "failure"}}
	it.Comments = []factory.Comment{{Author: "bob", Body: "does this\ncover credits?"}}
	it.Stages = []factory.Stage{{Name: "read", Ask: "the diff and its claims", On: true}, {Name: "review", Ask: "findings as a comment", On: true}}
	it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "read"}, {Name: "review"}}}
	return Job{Item: it, Stage: it.Stages[0], Round: 1, Dir: "/work/api-7"}
}

// THE BRIEF CARRIES WHAT THE STEP WENT TO THE WEB FOR: base and head, the
// files with their counts, each check, the issue it closes, the last
// comments, and how to read the change in the work tree.
func TestAPullRequestsBriefCarriesTheChangeAndWhereToReadIt(t *testing.T) {
	brief := stageBrief(prJob())
	for _, want := range []string{
		"read: the diff and its claims\n\nYou are the read step of a codeaf run on pull request #7.",
		"pull request #7 · total counts once · acme/api\nby alice · into main from alice:feature · +2 −1 · checks: test success, lint failure\nlinked: #3\nTotal counted refunds twice.",
		"## How it was checked\n\ngo test ./ledger",
		"changed files (2):\nledger.go +2 −1\nledger_test.go +9 −0",
		"the last comments, oldest first:\n- bob: does this cover credits?",
		"`git diff origin/main...HEAD` is the whole change",
		"You need neither the network nor the forge",
		"stages: read › review · this is read",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
	if !strings.HasSuffix(brief, briefClosing) {
		t.Errorf("the brief does not close with %q", briefClosing)
	}
}

// An issue's brief has no files or checks to name, and says its work tree is
// the item's own branch off the default one.
func TestAnIssuesBriefNamesItsLabelsCommentsAndBranch(t *testing.T) {
	job := Job{Item: factory.Item{ID: 4, Num: 12, Kind: factory.KindIssue, Repo: "api", Title: "Total double-counts",
		Author: "carol", Labels: []string{"bug"}, Body: "refunds count twice",
		Comments: []factory.Comment{{Author: "dan", Body: "seen on March"}}},
		Stage: factory.Stage{Name: "plan", Ask: "read the issue and say how"}, Round: 1, Dir: "/work/api-12"}
	brief := stageBrief(job)
	for _, want := range []string{
		"You are the plan step of a codeaf run on issue #12.",
		"issue #12 · Total double-counts · api\nby carol · labels: bug\nrefunds count twice",
		"- dan: seen on March",
		"on the item's own branch, factory/12-total-double-counts, cut from the default branch",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
	for _, gone := range []string{"changed files", "checks:", "linked:", "git diff origin"} {
		if strings.Contains(brief, gone) {
			t.Errorf("an issue's brief says %q:\n%s", gone, brief)
		}
	}
}
