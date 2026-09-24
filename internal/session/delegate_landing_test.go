package session

// WHERE A PROGRAM'S WORK LANDS, WHATEVER IT DID WITH HEAD.
//
// A program's shell can switch branches in its copy, and senior-dev did, four
// times in one run. The landing squashed onto whatever branch HEAD was on while
// the row and the note named codeaf's task branch, which then held nothing; and
// where the program checked out one of the person's own branches, the squash
// reset that branch and took the person's commits off it. These pin the work to
// the task's branch and the person's branches to their own history.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// delegatedRunThatDid runs one program whose work is play, in a repository
// newTestRepo makes (prepare may give it branches first), and answers the
// repository, its first commit, the run's row and the notes on its page.
func delegatedRunThatDid(t *testing.T, prepare func(repo string), play func(t *testing.T, workspace string)) (string, string, TaskNotice, []string) {
	t.Helper()
	double := newBeltRunDouble("done")
	double.work = func(workspace string) { play(t, workspace) }
	registerBeltRunEngine(t, double)
	conversation := newTestRepo(t)
	if prepare != nil {
		prepare(conversation)
	}
	base := strings.TrimSpace(gitOut(t, conversation, "rev-parse", "HEAD"))
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
	endBeltRun(t, agent, double)
	var row TaskNotice
	for _, kept := range agent.graph().runRows(id) {
		if kept.ID == id {
			row = kept
		}
	}
	return conversation, base, row, beltRunNotes(t, filepath.Dir(spec.Store.Path()), spec.Store.RootID())
}

// commitIn writes each file and commits it the way senior-dev commits an edit.
func commitIn(t *testing.T, workspace string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, workspace, "add", name)
		mustGit(t, workspace, "-c", "user.name=p", "-c", "user.email=p@p", "commit", "-q", "-m", "wip(edit): "+name)
	}
}

// taskBranchHolds asserts the task's one branch holds exactly one `task:`
// commit above base carrying every named file, and answers the branch.
func taskBranchHolds(t *testing.T, repo, base string, names ...string) string {
	t.Helper()
	branches := strings.Fields(gitOut(t, repo, "branch", "--format=%(refname:short)", "--list", "task/*"))
	if len(branches) != 1 {
		t.Fatalf("want the task's one branch in the repository, got %q", branches)
	}
	commits := strings.Fields(gitOut(t, repo, "rev-list", base+".."+branches[0]))
	subject := strings.TrimSpace(gitOut(t, repo, "log", "-1", "--format=%s", branches[0]))
	if len(commits) != 1 || !strings.HasPrefix(subject, "task: ") {
		t.Fatalf("the task's branch holds %d commits above the base (last %q), want one `task:` commit", len(commits), subject)
	}
	files := gitOut(t, repo, "ls-tree", "--name-only", branches[0])
	for _, name := range names {
		if !strings.Contains(files, name) {
			t.Fatalf("the task's branch does not hold %s:\n%s", name, files)
		}
	}
	return branches[0]
}

// A PROGRAM THAT SWITCHED TO A BRANCH OF ITS OWN still lands on the task's
// branch, and the note names the branch it had moved to.
func TestADelegatedRunThatSwitchedBranchLandsOnTheTaskBranch(t *testing.T) {
	repo, base, row, notes := delegatedRunThatDid(t, nil, func(t *testing.T, workspace string) {
		mustGit(t, workspace, "checkout", "-q", "-b", "senior-own")
		commitIn(t, workspace, "one.txt", "two.txt")
	})
	branch := taskBranchHolds(t, repo, base, "one.txt", "two.txt")
	if row.Branch != branch || row.Merge != mergeKept {
		t.Fatalf("the row names branch %q (%s), want the task's %q, kept", row.Branch, row.Merge, branch)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "landed on "+branch+": 2 files · fake had moved its copy to the branch senior-own; its work was committed on "+branch) {
		t.Fatalf("the page's notes do not say where the program had moved: %q", notes)
	}
	if strings.Contains(joined, "may also undo") {
		t.Fatalf("work built on the copy's first commit was warned about: %q", notes)
	}
}

// A PROGRAM THAT LEFT HEAD ON NO BRANCH still lands on the task's branch.
func TestADelegatedRunOnADetachedHeadLandsOnTheTaskBranch(t *testing.T) {
	repo, base, row, notes := delegatedRunThatDid(t, nil, func(t *testing.T, workspace string) {
		mustGit(t, workspace, "checkout", "-q", "--detach")
		commitIn(t, workspace, "one.txt")
	})
	branch := taskBranchHolds(t, repo, base, "one.txt")
	if row.Branch != branch {
		t.Fatalf("the row names %q, want the task's branch %q", row.Branch, branch)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "fake had left its copy on no branch; its work was committed on "+branch) {
		t.Fatalf("the page's notes do not say HEAD was on no branch: %q", notes)
	}
}

// A PROGRAM THAT CHECKED OUT THE PERSON'S OWN BRANCH never has codeaf rewrite
// it: the person's commit is still on their branch afterwards, and the work
// lands on the task's branch.
func TestADelegatedRunThatCheckedOutThePersonsBranchLeavesItsHistory(t *testing.T) {
	repo, base, _, notes := delegatedRunThatDid(t, func(repo string) {
		mustGit(t, repo, "checkout", "-q", "-b", "persons-feature")
		commitIn(t, repo, "mine.txt")
		mustGit(t, repo, "-c", "user.name=p", "-c", "user.email=p@p", "commit", "-q", "--amend", "-m", "the person's own commit")
		mustGit(t, repo, "checkout", "-q", "-")
	}, func(t *testing.T, workspace string) {
		mustGit(t, workspace, "checkout", "-q", "persons-feature")
		commitIn(t, workspace, "one.txt")
	})
	if log := gitOut(t, repo, "log", "--format=%s", "persons-feature"); !strings.Contains(log, "the person's own commit") {
		t.Fatalf("codeaf's landing took the person's own commit off their branch:\n%s", log)
	}
	branch := taskBranchHolds(t, repo, base, "one.txt")
	if !strings.Contains(strings.Join(notes, "\n"), "fake had moved its copy to the branch persons-feature; its work was committed on "+branch) {
		t.Fatalf("the page's notes do not name the person's branch the program moved to: %q", notes)
	}
}

// WORK NOT BUILT ON THE COPY'S FIRST COMMIT is warned about, because its squash
// may also undo what that commit had.
func TestADelegatedRunBuiltOnAnotherCommitIsWarnedAbout(t *testing.T) {
	_, _, _, notes := delegatedRunThatDid(t, nil, func(t *testing.T, workspace string) {
		mustGit(t, workspace, "checkout", "-q", "--orphan", "fresh")
		commitIn(t, workspace, "one.txt")
	})
	if !strings.Contains(strings.Join(notes, "\n"), "its work was not built on the commit its copy started from") {
		t.Fatalf("work built on another commit landed without a word: %q", notes)
	}
}

// A PROGRAM RUN THAT CHANGED NOTHING LEAVES NO BRANCH. There is nothing on an
// empty branch to merge, and every look-only, failed or crashed run used to
// leave one more `task/*` in the person's repository.
func TestADelegatedRunThatChangedNothingLeavesNoBranch(t *testing.T) {
	repo, _, row, notes := delegatedRunThatDid(t, nil, func(*testing.T, string) {})
	if branches := strings.TrimSpace(gitOut(t, repo, "branch", "--list", "task/*")); branches != "" {
		t.Fatalf("a run that changed nothing left a branch behind: %q", branches)
	}
	if row.Branch != "" {
		t.Fatalf("the row names a branch %q over no work", row.Branch)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "nothing to land: the run's working copy holds no change") {
		t.Fatalf("the page does not say there was nothing to land: %q", notes)
	}
}

// THE CONVERSATION IS TOLD NOTHING WAS MERGED, WHERE THE BRANCH IS, AND HOW TO
// BRING IT IN. The line a landing delivers is the one account the chat's model
// gets, and it read like a merged run's: `landed on task/x: 2 files`.
func TestTheConversationIsToldABranchOnlyLandingWasNotMerged(t *testing.T) {
	double := newBeltRunDouble("done")
	double.work = func(workspace string) { commitIn(t, workspace, "one.txt", "two.txt") }
	registerBeltRunEngine(t, double)
	conversation := newTestRepo(t)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "add two files"); err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	endBeltRun(t, agent, double)
	branches := strings.Fields(gitOut(t, conversation, "branch", "--format=%(refname:short)", "--list", "task/*"))
	if len(branches) != 1 {
		t.Fatalf("want the task's one branch, got %q", branches)
	}
	root := canonicalPath(conversation)
	want := "its work is on the branch " + branches[0] + " in " + root + ", 2 files; nothing was merged into your checkout, and `git -C '" +
		root + "' merge " + branches[0] + "` brings it in"
	if got := conversationJournalLines(agent, want); got != 1 {
		t.Fatalf("the conversation was told %d times %q", got, want)
	}
	if got := conversationJournalLines(agent, "landed on "+branches[0]); got != 0 {
		t.Fatal("the conversation was told the work landed, the shape of a merged run")
	}
}

// THE RECEIPT PROMISES NO MERGE. An approved hand-off to a program says who has
// the work and where it will be: on the task's own branch for a copy, in the
// folder itself for a folder with no history, in the conversation for one that
// only answers.
func TestAProgramsReceiptSaysWhereTheWorkWillBeAndPromisesNoMerge(t *testing.T) {
	agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, nil)
	tree := testPrograms("fake")[0]
	if got := agent.delegateReceipt(4, tree); got != "It is fake's: it works alone in a copy, and when it ends its work is left on the task's own branch; nothing is merged into the checkout." {
		t.Fatalf("the receipt for a copy = %q", got)
	}
	agent.beltMu.Lock()
	agent.beltRun = &beltRun{row: 4, plain: true}
	agent.beltMu.Unlock()
	if got := agent.delegateReceipt(4, tree); !strings.Contains(got, "in the folder itself, which has no git history") {
		t.Fatalf("the receipt for a plain folder = %q", got)
	}
	agent.beltMu.Lock()
	agent.beltRun = nil
	agent.beltMu.Unlock()
	reader := tree
	reader.Lands = delegate.LandsText
	if got := agent.delegateReceipt(4, reader); got != "It is fake's: it works alone, and its answer arrives when it ends." {
		t.Fatalf("the receipt for a program that answers = %q", got)
	}
	for _, got := range []string{agent.delegateReceipt(4, tree), agent.delegateReceipt(4, reader)} {
		if strings.Contains(got, "lands") {
			t.Fatalf("a receipt promises a landing: %q", got)
		}
	}
}

// A PROGRAM HANDED A SUBFOLDER'S TASK READS PATHS THAT EXIST IN ITS COPY. The
// copy is of the whole repository, so the subfolder is the same subfolder in
// it and the repository is the copy's root; mapping the subfolder to the copy's
// root sent every path to a file that is not there, and left the repository's
// own spelling pointing at the person's checkout.
func TestAProgramHandedASubfoldersTaskReadsPathsThatExistInItsCopy(t *testing.T) {
	double := newBeltRunDouble("")
	registerBeltRunEngine(t, double)
	repo := newTestRepo(t)
	sub := filepath.Join(repo, "packages", "foo")
	if err := os.MkdirAll(filepath.Join(sub, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, "src", "a.ts"), "export {}\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "the package")
	agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
		config.Workspace = sub
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	brief := "fix " + sub + "/src/a.ts, then run git -C " + repo + " status"
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", brief); err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	copyRoot := spec.Workspace
	want := "fix " + copyRoot + "/packages/foo/src/a.ts, then run git -C " + copyRoot + " status"
	if got := delegate.RehomeBrief(spec.Brief, spec.Ground); got != want {
		t.Fatalf("the program would read %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(copyRoot, "packages", "foo", "src", "a.ts")); err != nil {
		t.Fatalf("the path the program reads is not in its copy: %v", err)
	}
	endBeltRun(t, agent, double)
}
