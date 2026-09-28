package session

// WHERE A STOPPED PROGRAM'S WORK GOES.
//
// A stop ends a program's run the way every ending of it ends: its folder
// finished (programfolder.go), with what it had left uncommitted committed on
// its own branch, that branch left checked out, and the person's branch where
// it was. A stop that changed nothing leaves no branch, and the person is told
// at once where the work will be.

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stoppedDelegatedRunThatDid runs one program whose work before the stop is
// play, in a repository newTestRepo makes (prepare may give it branches first),
// stops it, and answers the repository, the run's row and the notes on its page.
func stoppedDelegatedRunThatDid(t *testing.T, prepare func(repo string), play func(t *testing.T, workspace string)) (string, TaskNotice, []string) {
	t.Helper()
	double := newBeltRunDouble("unused")
	double.honoursStop = true
	double.early = func(workspace string) { play(t, workspace) }
	registerBeltRunEngine(t, double)
	conversation := newTestRepo(t)
	if prepare != nil {
		prepare(conversation)
	}
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	id, _, _, err := agent.StartDelegate(context.Background(), "fake", "add files to the project")
	if err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	if _, err := agent.Cancel(CancelTask + ":" + strconv.FormatUint(id, 10)); err != nil {
		t.Fatal(err)
	}
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	var row TaskNotice
	for _, kept := range agent.graph().runRows(id) {
		if kept.ID == id {
			row = kept
		}
	}
	return conversation, row, beltRunNotes(t, filepath.Dir(spec.Store.Path()), spec.Store.RootID())
}

// A STOPPED PROGRAM'S WORK IS COMMITTED ON ITS BRANCH, committed by the
// program or not, and the branch is left checked out: the stop's own words are
// the body of the commit that holds what it left.
func TestAStoppedProgramsWorkIsCommittedOnItsBranchAndLeftCheckedOut(t *testing.T) {
	repo, row, notes := stoppedDelegatedRunThatDid(t, nil, func(t *testing.T, workspace string) {
		commitIn(t, workspace, "one.txt")
		writeFile(t, filepath.Join(workspace, "two.txt"), "two\n")
	})
	branch, log := taskBranchLog(t, repo)
	if head := currentBranch(repo); head != branch {
		t.Fatalf("the checkout is on %q after the stop, want the program's branch %q", head, branch)
	}
	files := gitOut(t, repo, "ls-tree", "--name-only", branch)
	for _, name := range []string{"one.txt", "two.txt"} {
		if !strings.Contains(files, name) {
			t.Fatalf("the program's branch does not hold %s:\n%s", name, files)
		}
	}
	if !strings.Contains(log, "wip(edit): one.txt") {
		t.Fatalf("the program's own commit is gone from its branch:\n%s", log)
	}
	if body := gitOut(t, repo, "log", "-1", "--format=%b", branch); !strings.Contains(body, "stopped") {
		t.Fatalf("the commit of what the stop left does not say it was stopped:\n%s", body)
	}
	if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work")); tip != strings.TrimSpace(gitOut(t, repo, "rev-parse", branch+"~2")) {
		t.Fatalf("the person's branch moved to %s", tip)
	}
	if row.Branch != branch || len(row.Changed) != 2 || row.Merge != mergeKept {
		t.Fatalf("the row names %q with %q (%s), want the program's branch with both files", row.Branch, row.Changed, row.Merge)
	}
	if joined := strings.Join(notes, "\n"); !strings.Contains(joined, "stopped · its work is on the branch "+branch+" in "+canonicalPath(repo)+", 2 files, and that branch is checked out there") {
		t.Fatalf("the stop does not say where the work is: %q", notes)
	}
}

// A STOPPED PROGRAM THAT CHANGED NOTHING LEAVES NO BRANCH, as one that ended
// does, and the person's own branch is checked out again.
func TestAStoppedProgramThatChangedNothingLeavesNoBranch(t *testing.T) {
	repo, row, notes := stoppedDelegatedRunThatDid(t, nil, func(*testing.T, string) {})
	if branches := strings.TrimSpace(gitOut(t, repo, "branch", "--list", "task/*")); branches != "" {
		t.Fatalf("a stopped run that changed nothing left a branch behind: %q", branches)
	}
	if head := currentBranch(repo); head != "work" {
		t.Fatalf("the checkout is on %q, want the person's branch work back", head)
	}
	if row.Branch != "" || !strings.Contains(strings.Join(notes, "\n"), "stopped · it changed nothing, so "+canonicalPath(repo)+" is back on your branch work") {
		t.Fatalf("a stopped run that changed nothing draws %q and says %q", row.Branch, notes)
	}
}

// THE STOP SAYS AT ONCE WHERE THE WORK WILL BE: on the program's branch in the
// person's folder, not on "its branch", which named nothing a person could
// find.
func TestAStoppedProgramSaysWhereItsWorkWillBe(t *testing.T) {
	double := newBeltRunDouble("unused")
	double.honoursStop = true
	registerBeltRunEngine(t, double)
	repo := newTestRepo(t)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	id, _, _, err := agent.StartDelegate(context.Background(), "fake", "add files")
	if err != nil {
		t.Fatal(err)
	}
	<-double.entered
	branch := currentBranch(repo)
	said, err := agent.Cancel(CancelTask + ":" + strconv.FormatUint(id, 10))
	if err != nil {
		t.Fatal(err)
	}
	if want := "its work so far stays on its branch " + branch + ", checked out in " + canonicalPath(repo); !strings.Contains(said, want) {
		t.Fatalf("the stop said %q, want %q", said, want)
	}
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
}
