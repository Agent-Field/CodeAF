package session

// WHERE A STOPPED PROGRAM'S WORK GOES, WHATEVER IT DID WITH HEAD.
//
// The landing puts a program's copy back on the task's own branch before its
// work is committed; the stop did not. A stopped program's work was committed
// on whatever branch HEAD was on, one of the person's own included, while the
// report named the task's branch, which held nothing; a stop that changed
// nothing left an empty branch; and a run that had committed every write, the
// way senior-dev does, was told as having changed nothing at all.

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

// A STOPPED PROGRAM THAT HAD CHECKED OUT THE PERSON'S OWN BRANCH never has
// codeaf commit on it: the person's branch is exactly as the program left it,
// and the work, committed and not, is on the task's branch the report names.
func TestAStoppedProgramOnThePersonsBranchLeavesItAndKeepsItsWorkOnTheTaskBranch(t *testing.T) {
	var left string
	repo, row, notes := stoppedDelegatedRunThatDid(t, func(repo string) {
		mustGit(t, repo, "checkout", "-q", "-b", "persons-feature")
		commitIn(t, repo, "mine.txt")
		mustGit(t, repo, "checkout", "-q", "-")
	}, func(t *testing.T, workspace string) {
		mustGit(t, workspace, "checkout", "-q", "persons-feature")
		commitIn(t, workspace, "one.txt")
		writeFile(t, filepath.Join(workspace, "two.txt"), "two\n")
		left = strings.TrimSpace(gitOut(t, workspace, "rev-parse", "HEAD"))
	})
	if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", "persons-feature")); tip != left {
		t.Fatalf("codeaf's stop moved the person's branch from %s to %s:\n%s", left, tip, gitOut(t, repo, "log", "--format=%s", "persons-feature"))
	}
	branch, _ := taskBranchLog(t, repo)
	files := gitOut(t, repo, "ls-tree", "--name-only", branch)
	for _, name := range []string{"one.txt", "two.txt"} {
		if !strings.Contains(files, name) {
			t.Fatalf("the task's branch does not hold %s:\n%s", name, files)
		}
	}
	if row.Branch != branch {
		t.Fatalf("the row names %q, want the task's branch %q", row.Branch, branch)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "its work so far is kept on "+branch) || !strings.Contains(joined, "fake had moved its copy to the branch persons-feature") {
		t.Fatalf("the stop does not say where the work is and where the program had moved: %q", notes)
	}
}

// A STOPPED PROGRAM THAT COMMITTED EVERY WRITE is told as having changed what
// it changed. senior-dev commits each write on the task's branch, so nothing
// is left uncommitted at a stop, and the stop read that as "it had changed
// nothing" and named no branch.
func TestAStoppedProgramThatCommittedEveryWriteReportsItsFiles(t *testing.T) {
	repo, row, notes := stoppedDelegatedRunThatDid(t, nil, func(t *testing.T, workspace string) {
		commitIn(t, workspace, "one.txt", "two.txt")
	})
	branch, _ := taskBranchLog(t, repo)
	if row.Branch != branch || len(row.Changed) != 2 {
		t.Fatalf("the row names %q with %q, want the task's branch %q with both files", row.Branch, row.Changed, branch)
	}
	joined := strings.Join(notes, "\n")
	if strings.Contains(joined, "it had changed nothing") || !strings.Contains(joined, "its work so far is kept on "+branch) {
		t.Fatalf("a stopped run that committed its writes says: %q", notes)
	}
}

// A STOPPED PROGRAM THAT CHANGED NOTHING LEAVES NO BRANCH, as one that ended
// does.
func TestAStoppedProgramThatChangedNothingLeavesNoBranch(t *testing.T) {
	repo, row, notes := stoppedDelegatedRunThatDid(t, nil, func(*testing.T, string) {})
	if branches := strings.TrimSpace(gitOut(t, repo, "branch", "--list", "task/*")); branches != "" {
		t.Fatalf("a stopped run that changed nothing left a branch behind: %q", branches)
	}
	if row.Branch != "" || !strings.Contains(strings.Join(notes, "\n"), "stopped · it had changed nothing") {
		t.Fatalf("a stopped run that changed nothing draws %q and says %q", row.Branch, notes)
	}
}
