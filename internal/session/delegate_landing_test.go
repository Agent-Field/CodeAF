package session

// WHERE A PROGRAM'S WORK IS WHEN IT ENDS, WHATEVER IT DID IN THE FOLDER.
//
// A program in a repository works in a copy of its own, on a branch codeaf cut
// for it (programcopy.go). These pin what the person finds when it ends: its
// branch holding everything it left, checked out nowhere, their own checkout
// untouched, the branch kept even when it changed nothing, what a HEAD its
// shell moved left kept rather than lost, and its own notes moved out and
// never committed.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
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

// commitIn writes each file and commits it, the way a program that commits
// despite its brief leaves a commit of its own on its branch.
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

// taskBranchLog answers the task's one branch and the subjects of its history,
// newest first.
func taskBranchLog(t *testing.T, repo string) (string, string) {
	t.Helper()
	branches := strings.Fields(gitOut(t, repo, "branch", "--format=%(refname:short)", "--list", "task/*"))
	if len(branches) != 1 {
		t.Fatalf("want the task's one branch in the repository, got %q", branches)
	}
	return branches[0], gitOut(t, repo, "log", "--format=%s", branches[0])
}

// A PROGRAM RUN THAT CHANGED NOTHING KEEPS ITS BRANCH AND TOUCHES NOTHING: the
// person's checkout is where it was, the copy is gone, the branch stays where
// it was cut, and the row names no branch over no work.
func TestADelegatedRunThatChangedNothingKeepsItsBranchAndTouchesNothing(t *testing.T) {
	repo, base, row, notes := delegatedRunThatDid(t, nil, func(*testing.T, string) {})
	branch, _ := taskBranchLog(t, repo)
	if branchCommit(repo, branch) != base {
		t.Fatalf("a run that changed nothing moved its branch off %s", base)
	}
	if head := currentBranch(repo); head != "work" || strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")) != base || worktreeCount(t, repo) != 1 {
		t.Fatalf("the checkout is on %q after a run that changed nothing, want the person's branch work at %s and no copy", head, base)
	}
	if row.Branch != "" {
		t.Fatalf("the row names a branch %q over no work", row.Branch)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "it changed nothing; its branch "+branch+" in ") {
		t.Fatalf("the page does not say the run changed nothing: %q", notes)
	}
}

// A PERSON WHOSE CHECKOUT WAS ON NO BRANCH has the program's branch cut from
// that commit, and their checkout left on it.
func TestADelegatedRunFromADetachedCheckoutCutsItsBranchFromThatCommit(t *testing.T) {
	repo, base, row, notes := delegatedRunThatDid(t, func(repo string) {
		mustGit(t, repo, "checkout", "-q", "--detach")
	}, func(t *testing.T, workspace string) {
		writeFile(t, filepath.Join(workspace, "one.txt"), "one\n")
	})
	branch, log := taskBranchLog(t, repo)
	if currentBranch(repo) != "" || strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")) != base || row.Branch != branch {
		t.Fatalf("the checkout is on %q and the row names %q, want the checkout left detached and the program's branch %q", currentBranch(repo), row.Branch, branch)
	}
	if strings.TrimSpace(gitOut(t, repo, "merge-base", branch, base)) != base || !strings.Contains(log, "first") {
		t.Fatalf("the program's branch was not cut from the detached commit:\n%s", log)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "its work is on the branch "+branch+" in ") {
		t.Fatalf("the page does not say where the work is: %q", notes)
	}
}

// A HEAD THE PROGRAM'S SHELL MOVED IS NOT COMMITTED ON. senior-dev's shell can
// run `git checkout`, and it did, four times in one run. Its branch keeps what
// it committed there, what it left loose is kept as a patch, and nothing of
// codeaf's is committed onto a branch nobody chose.
func TestAProgramThatMovedHeadOffItsBranchIsLeftWhereItIs(t *testing.T) {
	repo, base, row, notes := delegatedRunThatDid(t, nil, func(t *testing.T, workspace string) {
		commitIn(t, workspace, "one.txt")
		mustGit(t, workspace, "checkout", "-q", "-b", "elsewhere")
		writeFile(t, filepath.Join(workspace, "loose.txt"), "loose\n")
	})
	branch, log := taskBranchLog(t, repo)
	if head := currentBranch(repo); head != "work" || strings.TrimSpace(gitOut(t, repo, "rev-parse", "work")) != base {
		t.Fatalf("the person's checkout is on %q, want work at %s", head, base)
	}
	if !strings.Contains(log, "wip(edit): one.txt") || strings.Count(log, "\n") != 2 {
		t.Fatalf("the program's branch holds:\n%s\nwant its own commit and nothing of codeaf's", log)
	}
	said := strings.Join(notes, "\n")
	for _, want := range []string{
		"fake left its copy on the branch elsewhere instead of its own branch " + branch + ", so codeaf committed nothing there",
		branch + " in ", "holds 1 file", "what it left uncommitted is kept as a patch at ",
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("the page does not say %q: %q", want, notes)
		}
	}
	if row.Branch != branch {
		t.Fatalf("the row names %q, want the program's branch %q, which holds its commit", row.Branch, branch)
	}
}

// AND WHAT IT COMMITTED ON NO BRANCH IS KEPT ON ONE, because the copy that was
// the only thing holding it goes when the run ends.
func TestAProgramThatDetachedHeadIsLeftWhereItIs(t *testing.T) {
	var at string
	repo, base, _, notes := delegatedRunThatDid(t, nil, func(t *testing.T, workspace string) {
		mustGit(t, workspace, "checkout", "-q", "--detach")
		commitIn(t, workspace, "one.txt")
		at = strings.TrimSpace(gitOut(t, workspace, "rev-parse", "HEAD"))
	})
	if head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); head != base || currentBranch(repo) != "work" {
		t.Fatalf("codeaf moved the person's checkout to %s", head)
	}
	said := strings.Join(notes, "\n")
	if !strings.Contains(said, "fake left its copy on no branch, at "+shortSha(at)+" instead of its own branch task/") {
		t.Fatalf("the page does not say HEAD was left on no branch: %q", notes)
	}
	saved := strings.TrimSpace(gitOut(t, repo, "branch", "--list", "task/*-detached", "--format=%(refname:short)"))
	if saved == "" || branchCommit(repo, saved) != at || !strings.Contains(said, "what it committed there is kept on the branch "+saved) {
		t.Fatalf("the commit made on no branch was not kept: %q at %s, page %q", saved, branchCommit(repo, saved), notes)
	}
}

// THE RECEIPT PROMISES NO MERGE. An approved hand-off to a program says who has
// the work and where it will be: in a private copy on a new branch, cut from
// the person's branch (or the commit their checkout is on), which holds the
// work when it ends; in the folder itself for a folder with no history; in the
// conversation for a program that only answers.
func TestAProgramsReceiptSaysWhereTheWorkWillBeAndPromisesNoMerge(t *testing.T) {
	tree := testPrograms("fake")[0]
	repo, plain := "/r/repo", "/r/plain"
	record := &TaskCopyRecord{Dir: "/tmp/copy", Root: repo, Branch: "task/pong-abc123", Home: "main", HomeSha: "0123456789abcdef"}
	if got, want := delegateReceipt(repo, tree, record), "It is fake's: it works alone in a private copy of /r/repo, on a new branch task/pong-abc123 cut from your branch main as last committed; your checkout is not touched, and when it ends task/pong-abc123 holds its work, checked out nowhere."; got != want {
		t.Fatalf("the receipt for a repository = %q, want %q", got, want)
	}
	detached := &TaskCopyRecord{Dir: "/tmp/copy", Root: repo, Branch: "task/pong-abc123", HomeSha: "0123456789abcdef"}
	if got := delegateReceipt(repo, tree, detached); !strings.Contains(got, " cut from the commit 0123456789ab;") {
		t.Fatalf("the receipt for a detached checkout = %q", got)
	}
	if got := delegateReceipt(plain, tree, &TaskCopyRecord{Dir: plain}); got != "It is fake's: it works alone in /r/plain itself, which has no git history, so its changes are there as it makes them. Until it ends, codeaf's own tools write nothing in /r/plain." {
		t.Fatalf("the receipt for a plain folder = %q", got)
	}
	reader := tree
	reader.Lands = delegate.LandsText
	if got := delegateReceipt(plain, reader, nil); got != "It is fake's: it works alone, and its answer arrives when it ends." {
		t.Fatalf("the receipt for a program that answers = %q", got)
	}
	for _, got := range []string{delegateReceipt(repo, tree, record), delegateReceipt(plain, tree, nil), delegateReceipt(plain, reader, nil)} {
		if strings.Contains(got, "lands") || strings.Contains(got, "merge") {
			t.Fatalf("a receipt promises a landing or a merge: %q", got)
		}
	}
}

// A PROGRAM HANDED A FOLDER INSIDE A REPOSITORY WORKS ON THE WHOLE REPOSITORY,
// in a copy of its root, and its brief reaches it as it was written with one
// line ahead of it saying where the copy is.
func TestAProgramHandedASubfolderWorksAtTheRepositorysRoot(t *testing.T) {
	double := newBeltRunDouble("")
	registerBeltRunEngine(t, double)
	repo := newTestRepo(t)
	sub := filepath.Join(repo, "packages", "foo")
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
	if _, err := os.Stat(filepath.Join(spec.Workspace, "packages", "foo", "src", "a.ts")); err != nil || spec.Brief != brief {
		t.Fatalf("the program works in %q on %q, want a copy of the repository's root and the brief as written: %v", spec.Workspace, spec.Brief, err)
	}
	if !strings.Contains(spec.ProgramBriefNote, "private copy of the repository at "+canonicalPath(repo)+", checked out at "+spec.Workspace) {
		t.Fatalf("the line ahead of the brief = %q, want the repository's root and its copy", spec.ProgramBriefNote)
	}
	endBeltRun(t, agent, double)
}

// A PROGRAM'S NOTES IN A REPOSITORY ARE MOVED OUT AND NEVER COMMITTED. They
// are the program's records — its database and its whole conversation — and a
// commit of what the run left would otherwise have taken them onto the branch.
func TestARepositoryRunsNotesAreMovedOutAndNeverCommitted(t *testing.T) {
	double := newBeltRunDouble("done")
	double.work = func(workspace string) {
		writeFile(t, filepath.Join(workspace, ".fake", "spec.md"), "the brief\n")
		writeFile(t, filepath.Join(workspace, "made.txt"), "made\n")
	}
	registerBeltRunEngine(t, double)
	repo := newTestRepo(t)
	programs := testPrograms("fake")
	programs[0].Notes = ".fake"
	place := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: place}
		config.AskConsent = false
		config.Delegates = programs
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "make a file"); err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	endBeltRun(t, agent, double)
	branch, _ := taskBranchLog(t, repo)
	files := gitOut(t, repo, "ls-tree", "-r", "--name-only", branch)
	if !strings.Contains(files, "made.txt") || strings.Contains(files, ".fake") {
		t.Fatalf("the program's branch holds:\n%s\nwant its work and none of its notes", files)
	}
	taskDir := plandb.TaskDir(place, spec.Store.RootID())
	if _, err := os.Stat(filepath.Join(taskDir, "fake", "spec.md")); err != nil {
		t.Fatalf("the notes are not in the task's record folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".fake")); !os.IsNotExist(err) {
		t.Fatalf("the notes were left in the person's folder: %v", err)
	}
}

// NOTES THAT WERE THERE BEFORE A REPOSITORY RUN are not this run's to take, do
// not refuse the run as changes of the person's, and are not committed.
func TestNotesThatWereThereBeforeARepositoryRunStayAndAreNotCommitted(t *testing.T) {
	double := newBeltRunDouble("done")
	double.work = func(workspace string) { writeFile(t, filepath.Join(workspace, "made.txt"), "made\n") }
	registerBeltRunEngine(t, double)
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".fake", "old.md"), "an earlier run's\n")
	programs := testPrograms("fake")
	programs[0].Notes = ".fake"
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = programs
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "make a file"); err != nil {
		t.Fatalf("a folder whose only untracked files are the program's own notes was refused: %v", err)
	}
	<-double.entered
	endBeltRun(t, agent, double)
	branch, _ := taskBranchLog(t, repo)
	if files := gitOut(t, repo, "ls-tree", "-r", "--name-only", branch); strings.Contains(files, ".fake") || !strings.Contains(files, "made.txt") {
		t.Fatalf("the program's branch holds:\n%s", files)
	}
	if _, err := os.Stat(filepath.Join(repo, ".fake", "old.md")); err != nil {
		t.Fatalf("notes that were there before the run were taken: %v", err)
	}
}

// plainFolderRun runs a program that keeps notes in `.fake` on a folder with
// no git history, playing a run that writes its work and its notes there, and
// answers the folder and the task's record folder.
func plainFolderRun(t *testing.T, before func(folder string)) (string, string, []string) {
	t.Helper()
	double := newBeltRunDouble("done")
	double.work = func(workspace string) {
		if err := os.MkdirAll(filepath.Join(workspace, ".fake", "storage"), 0o755); err != nil {
			t.Error(err)
			return
		}
		for name, body := range map[string]string{
			"made.txt":                   "made\n",
			".fake/spec.md":              "the brief\n",
			".fake/storage/session.json": "{}\n",
		} {
			if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o644); err != nil {
				t.Error(err)
			}
		}
	}
	registerBeltRunEngine(t, double)
	folder := t.TempDir()
	if before != nil {
		before(folder)
	}
	programs := testPrograms("fake")
	programs[0].Notes = ".fake"
	place := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = folder
		config.Place = Place{Dir: place}
		config.AskConsent = false
		config.Delegates = programs
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "make a file in this folder"); err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	endBeltRun(t, agent, double)
	root := spec.Store.RootID()
	return folder, plandb.TaskDir(place, root), beltRunNotes(t, place, root)
}

// A PROGRAM THAT WORKED IN A PLAIN FOLDER LEAVES ONLY ITS WORK THERE. Its own
// records (a session database and its whole model conversation, for
// senior-dev) are moved into the task's record folder, where the page says
// they are, instead of waiting in the person's folder for a `git add -A`.
func TestAPlainFolderRunsNotesAreMovedIntoTheTasksRecordFolder(t *testing.T) {
	folder, taskDir, notes := plainFolderRun(t, nil)
	if _, err := os.Stat(filepath.Join(folder, "made.txt")); err != nil {
		t.Fatalf("the work is not in the folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, ".fake")); !os.IsNotExist(err) {
		t.Fatalf("the program's notes were left in the person's folder: %v", err)
	}
	for _, name := range []string{"spec.md", filepath.Join("storage", "session.json")} {
		if _, err := os.Stat(filepath.Join(taskDir, "fake", name)); err != nil {
			t.Fatalf("the program's %s is not in the task's record folder: %v", name, err)
		}
	}
	if !strings.Contains(strings.Join(notes, "\n"), "its notes (.fake/) are kept in "+filepath.Join(taskDir, "fake")) {
		t.Fatalf("the page does not say where the notes went: %q", notes)
	}
}

// NOTES THAT WERE THERE BEFORE THE RUN ARE NOT THIS RUN'S TO TAKE.
func TestAPlainFolderRunLeavesNotesThatWereThereBeforeIt(t *testing.T) {
	folder, taskDir, _ := plainFolderRun(t, func(folder string) {
		if err := os.MkdirAll(filepath.Join(folder, ".fake"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(folder, ".fake", "old.md"), "an earlier run's\n")
	})
	if _, err := os.Stat(filepath.Join(folder, ".fake", "old.md")); err != nil {
		t.Fatalf("notes that were there before the run were taken: %v", err)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "fake")); !os.IsNotExist(err) {
		t.Fatalf("notes that were not this run's alone were moved: %v", err)
	}
}
