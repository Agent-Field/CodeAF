package session

// WHERE A PROGRAM'S WORK IS WHEN IT ENDS, WHATEVER IT DID IN THE FOLDER.
//
// A program works in the person's folder itself, on a branch codeaf cut for
// it when the folder is a repository (programfolder.go). These pin what the
// person finds when it ends: its branch checked out with everything it left
// committed there, their own branch untouched, nothing at all when it changed
// nothing, a HEAD its shell moved left exactly where it was, and its own notes
// moved out of the folder and never committed.

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

// A PROGRAM RUN THAT CHANGED NOTHING LEAVES NOTHING: the person's own branch is
// checked out again and the empty branch is gone, so every look-only, failed or
// crashed run does not leave one more `task/*` in the person's repository.
func TestADelegatedRunThatChangedNothingGoesBackAndLeavesNoBranch(t *testing.T) {
	repo, base, row, notes := delegatedRunThatDid(t, nil, func(*testing.T, string) {})
	if branches := strings.TrimSpace(gitOut(t, repo, "branch", "--list", "task/*")); branches != "" {
		t.Fatalf("a run that changed nothing left a branch behind: %q", branches)
	}
	if head := currentBranch(repo); head != "work" || strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")) != base {
		t.Fatalf("the checkout is on %q after a run that changed nothing, want the person's branch work at %s", head, base)
	}
	if row.Branch != "" {
		t.Fatalf("the row names a branch %q over no work", row.Branch)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "it changed nothing, so "+canonicalPath(repo)+" is back on your branch work and its branch task/") {
		t.Fatalf("the page does not say the run changed nothing and went back: %q", notes)
	}
}

// A PERSON WHOSE CHECKOUT WAS ON NO BRANCH GETS THAT COMMIT BACK, and is told
// how to go back to it when the run leaves work.
func TestADelegatedRunFromADetachedCheckoutNamesTheCommitToGoBackTo(t *testing.T) {
	repo, base, row, notes := delegatedRunThatDid(t, func(repo string) {
		mustGit(t, repo, "checkout", "-q", "--detach")
	}, func(t *testing.T, workspace string) {
		writeFile(t, filepath.Join(workspace, "one.txt"), "one\n")
	})
	branch, _ := taskBranchLog(t, repo)
	if head := currentBranch(repo); head != branch || row.Branch != branch {
		t.Fatalf("the checkout is on %q and the row names %q, want the program's branch %q", head, row.Branch, branch)
	}
	want := "your checkout was on no branch, at " + shortSha(base) + ", and `git -C '" + canonicalPath(repo) + "' switch --detach " + shortSha(base) + "` goes back to it"
	if !strings.Contains(strings.Join(notes, "\n"), want) {
		t.Fatalf("the page does not say how to go back to the commit: %q, want %q", notes, want)
	}
}

// A HEAD THE PROGRAM'S SHELL MOVED IS LEFT WHERE IT IS. senior-dev's shell can
// run `git checkout`, and it did, four times in one run. codeaf then commits
// nothing and switches nothing: committing where HEAD is would put codeaf's
// commit on a branch that may be the person's own, and switching would carry
// whatever is in the folder somewhere nobody chose. It says where HEAD is.
func TestAProgramThatMovedHeadOffItsBranchIsLeftWhereItIs(t *testing.T) {
	var left string
	repo, base, row, notes := delegatedRunThatDid(t, nil, func(t *testing.T, workspace string) {
		commitIn(t, workspace, "one.txt")
		mustGit(t, workspace, "checkout", "-q", "work")
		writeFile(t, filepath.Join(workspace, "loose.txt"), "loose\n")
		left = strings.TrimSpace(gitOut(t, workspace, "rev-parse", "HEAD"))
	})
	branch, log := taskBranchLog(t, repo)
	if head := currentBranch(repo); head != "work" || left != base {
		t.Fatalf("the checkout is on %q at %s, want it left on work where the program put it", head, left)
	}
	if !strings.Contains(log, "wip(edit): one.txt") || strings.Count(log, "\n") != 2 {
		t.Fatalf("the program's branch holds:\n%s\nwant its own commit and nothing of codeaf's", log)
	}
	if status := gitOut(t, repo, "status", "--porcelain"); !strings.Contains(status, "loose.txt") {
		t.Fatalf("codeaf committed what the program left while HEAD was elsewhere:\n%s", status)
	}
	want := "fake left " + canonicalPath(repo) + " on the branch work instead of its own branch " + branch +
		", so codeaf changed nothing there: nothing was committed and nothing was switched; " + branch + " holds 1 file"
	if !strings.Contains(strings.Join(notes, "\n"), want) {
		t.Fatalf("the page does not say where HEAD was left: %q, want %q", notes, want)
	}
	if row.Branch != branch {
		t.Fatalf("the row names %q, want the program's branch %q, which holds its commit", row.Branch, branch)
	}
}

// AND A HEAD LEFT ON NO BRANCH IS SAID WITH ITS COMMIT.
func TestAProgramThatDetachedHeadIsLeftWhereItIs(t *testing.T) {
	var at string
	repo, _, _, notes := delegatedRunThatDid(t, nil, func(t *testing.T, workspace string) {
		mustGit(t, workspace, "checkout", "-q", "--detach")
		commitIn(t, workspace, "one.txt")
		at = strings.TrimSpace(gitOut(t, workspace, "rev-parse", "HEAD"))
	})
	if head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); head != at || currentBranch(repo) != "" {
		t.Fatalf("codeaf moved a detached HEAD from %s to %s", at, head)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "fake left "+canonicalPath(repo)+" on no branch, at "+shortSha(at)+" instead of its own branch task/") {
		t.Fatalf("the page does not say HEAD was left on no branch: %q", notes)
	}
}

// THE RECEIPT PROMISES NO MERGE. An approved hand-off to a program says who has
// the work and where it will be: on a new branch in the folder itself, left
// checked out, with the person's branch named as the one that does not move;
// in the folder itself for a folder with no history; in the conversation for
// a program that only answers.
func TestAProgramsReceiptSaysWhereTheWorkWillBeAndPromisesNoMerge(t *testing.T) {
	tree := testPrograms("fake")[0]
	repo, plain := "/r/repo", "/r/plain"
	record := &TaskCopyRecord{Dir: repo, Branch: "task/pong-abc123", Home: "main", HomeSha: "0123456789abcdef"}
	if got, want := delegateReceipt(repo, tree, record), "It is fake's: it works alone in /r/repo itself, on a new branch task/pong-abc123; your branch main does not move, and when it ends task/pong-abc123 stays checked out there with its work."; got != want {
		t.Fatalf("the receipt for a repository = %q, want %q", got, want)
	}
	detached := &TaskCopyRecord{Dir: repo, Branch: "task/pong-abc123", HomeSha: "0123456789abcdef"}
	if got := delegateReceipt(repo, tree, detached); !strings.Contains(got, "; the commit 0123456789ab does not move") {
		t.Fatalf("the receipt for a detached checkout = %q", got)
	}
	if got := delegateReceipt(plain, tree, &TaskCopyRecord{Dir: plain}); got != "It is fake's: it works alone in /r/plain itself, which has no git history, so its changes are there as it makes them." {
		t.Fatalf("the receipt for a plain folder = %q", got)
	}
	reader := tree
	reader.Lands = delegate.LandsText
	if got := delegateReceipt(plain, reader, nil); got != "It is fake's: it works alone, and its answer arrives when it ends." {
		t.Fatalf("the receipt for a program that answers = %q", got)
	}
	for _, got := range []string{delegateReceipt(repo, tree, record), delegateReceipt(plain, tree, nil), delegateReceipt(plain, reader, nil)} {
		if strings.Contains(got, "lands") || strings.Contains(got, "copy") {
			t.Fatalf("a receipt promises a landing or a copy: %q", got)
		}
	}
}

// A PROGRAM HANDED A FOLDER INSIDE A REPOSITORY WORKS AT THE REPOSITORY'S ROOT,
// which is where its branch is, and its brief reaches it as it was written:
// there is no copy for a path to be rewritten into.
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
	if canonicalPath(spec.Workspace) != canonicalPath(repo) || spec.Brief != brief {
		t.Fatalf("the program works in %q on %q, want the repository's root %q and the brief as written", spec.Workspace, spec.Brief, repo)
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
