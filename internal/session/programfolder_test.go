package session

// THE CONTRACT OF A PROGRAM'S FOLDER (programfolder.go, programcopy.go), in
// real git in temporary repositories: which folder, a copy on a branch of its
// own in a repository and nothing of git anywhere else, the person's checkout
// never touched, runs side by side on one repository, and a run whose process
// went away finished by the next codeaf that finds it.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// programAgent is a conversation on folder carrying the fake program, with a
// run engine double registered and not yet released.
func programAgent(t *testing.T, folder string) (*Agent, *beltRunDouble) {
	t.Helper()
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = folder
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	return agent, double
}

// worktreeCount is how many worktrees git records for repo, its own checkout
// among them.
func worktreeCount(t *testing.T, repo string) int {
	t.Helper()
	return strings.Count(gitOut(t, repo, "worktree", "list", "--porcelain"), "worktree ")
}

// A REPOSITORY'S PROGRAM WORKS IN A COPY OF ITS OWN, AND THE PERSON'S CHECKOUT
// IS NEVER TOUCHED. Uncommitted changes — a modified file, an untracked one, a
// staged one — used to refuse the run; they are left where they are now, not
// in the copy, and named on the receipt. When the run ends its work is on its
// branch, the copy is gone and the branch is checked out nowhere.
func TestAProgramWorksInACopyAndLeavesTheCheckoutAlone(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's own line\n")
	writeFile(t, filepath.Join(repo, "notes", "draft.md"), "draft\n")
	writeFile(t, filepath.Join(repo, "new.go"), "package x\n")
	mustGit(t, repo, "add", "new.go")
	status := gitOut(t, repo, "status", "--porcelain")
	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	double := newBeltRunDouble("done")
	double.work = func(workspace string) { writeFile(t, filepath.Join(workspace, "fix.go"), "package fix\n") }
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	id, _, _, err := agent.StartDelegate(context.Background(), "fake", "change the project")
	if err != nil {
		t.Fatalf("a checkout with uncommitted changes refused the run: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	copyDir := canonicalPath(spec.Workspace)
	if copyDir == canonicalPath(repo) || !strings.HasPrefix(copyDir, canonicalPath(programCopyRoot())) {
		t.Fatalf("the program works in %q, want a copy under %q", copyDir, programCopyRoot())
	}
	if spec.PlainFolder || !strings.HasPrefix(spec.ProgramBranch, "task/") || currentBranch(copyDir) != spec.ProgramBranch {
		t.Fatalf("the copy is on %q (plain %v), want the program's own branch %q", currentBranch(copyDir), spec.PlainFolder, spec.ProgramBranch)
	}
	for _, left := range []string{"notes/draft.md", "new.go"} {
		if _, err := os.Stat(filepath.Join(copyDir, left)); !os.IsNotExist(err) {
			t.Fatalf("the person's uncommitted %s is in the copy: %v", left, err)
		}
	}
	if !strings.Contains(spec.ProgramBriefNote, "private copy of the repository at "+canonicalPath(repo)) || !strings.Contains(spec.ProgramBriefNote, copyDir) {
		t.Fatalf("the brief does not say where the copy is: %q", spec.ProgramBriefNote)
	}
	receipt := delegateReceipt(repo, testPrograms("fake")[0], agent.runRowCopy(id))
	if !strings.Contains(receipt, "a private copy of "+repo) || !strings.Contains(receipt, "Your uncommitted changes (") ||
		strings.Contains(receipt, "codeaf's own tools write nothing") {
		t.Fatalf("the receipt = %q, want the copy and the changes it does not have", receipt)
	}
	endBeltRun(t, agent, double)
	if currentBranch(repo) != "work" || strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")) != head || gitOut(t, repo, "status", "--porcelain") != status {
		t.Fatalf("the person's checkout was touched: on %q\n%s", currentBranch(repo), gitOut(t, repo, "status", "--porcelain"))
	}
	if files := gitOut(t, repo, "ls-tree", "-r", "--name-only", spec.ProgramBranch); !strings.Contains(files, "fix.go") || strings.Contains(files, "draft.md") {
		t.Fatalf("the branch holds %q, want the program's work and none of the person's", files)
	}
	if _, err := os.Stat(copyDir); !os.IsNotExist(err) || worktreeCount(t, repo) != 1 {
		t.Fatalf("the copy was not removed (%v), %d worktrees", err, worktreeCount(t, repo))
	}
}

// A CHECKOUT IN THE MIDDLE OF A MERGE IS NOT IN THE WAY EITHER, and is left as
// it is: the copy is cut from the commit it stands on.
func TestAProgramStartsBesideACheckoutInTheMiddleOfAMerge(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "checkout", "-q", "-b", "other")
	writeFile(t, filepath.Join(repo, "shared.txt"), "theirs\n")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "theirs")
	mustGit(t, repo, "checkout", "-q", "work")
	writeFile(t, filepath.Join(repo, "shared.txt"), "ours\n")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "ours")
	if _, err := git(repo, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "other"); err == nil {
		t.Fatal("the merge did not stop on its conflict")
	}
	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
	folder.Finish("")
	if after := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); after != head {
		t.Fatalf("the person's merge was concluded: HEAD moved from %s to %s", head, after)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); err != nil {
		t.Fatalf("the merge in progress is gone: %v", err)
	}
}

// A PLAIN FOLDER IS WORKED IN AS IT IS: no git is made there, and the program
// is started with its own flags for it.
func TestAProgramOnAPlainFolderGetsNoGit(t *testing.T) {
	folder := t.TempDir()
	agent, double := programAgent(t, folder)
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "make a thing"); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	endBeltRun(t, agent, double)
	if !spec.PlainFolder || canonicalPath(spec.Workspace) != canonicalPath(folder) {
		t.Fatalf("the program works in %q (plain %v), want the folder itself without git", spec.Workspace, spec.PlainFolder)
	}
	if _, err := os.Stat(filepath.Join(folder, ".git")); !os.IsNotExist(err) {
		t.Fatalf("a plain folder was made a repository: %v", err)
	}
}

// A REPOSITORY AT THE HOME FOLDER IS NOBODY'S PROJECT. A folder under a
// dotfiles repository rooted at home is worked in as a plain folder — no
// branch cut in the person's dotfiles, the program told it works without git
// — and the ending says why nothing was committed.
func TestAFolderInARepositoryAtTheHomeFolderIsWorkedInWithoutGit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustGit(t, home, "init", "-q")
	writeFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	mustGit(t, home, "add", "-A")
	mustGit(t, home, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "dotfiles")
	before := strings.TrimSpace(gitOut(t, home, "rev-parse", "HEAD"))
	project := filepath.Join(home, "Desktop", "pong")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	double := newBeltRunDouble("done")
	double.work = func(workspace string) { writeFile(t, filepath.Join(workspace, "pong.py"), "print('pong')\n") }
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = project
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "build pong"); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	endBeltRun(t, agent, double)
	if !spec.PlainFolder || canonicalPath(spec.Workspace) != canonicalPath(project) {
		t.Fatalf("the program works in %q (plain %v), want %q without git", spec.Workspace, spec.PlainFolder, project)
	}
	if after := strings.TrimSpace(gitOut(t, home, "rev-parse", "HEAD")); after != before || strings.HasPrefix(currentBranch(home), "task/") {
		t.Fatalf("the dotfiles repository moved from %s to %s (on %q)", before, after, currentBranch(home))
	}
	if branches := strings.TrimSpace(gitOut(t, home, "branch", "--list", "task/*")); branches != "" {
		t.Fatalf("a branch was cut in the repository at home: %q", branches)
	}
	notes := beltRunNotes(t, filepath.Dir(spec.Store.Path()), spec.Store.RootID())
	if !strings.Contains(strings.Join(notes, "\n"), "the git repository around it is at "+canonicalPath(home)+", which holds your home folder, so codeaf cut no branch there and committed nothing") {
		t.Fatalf("the page does not say why nothing was committed: %q", notes)
	}
}

// A GROUND THAT IS NOT THERE YET IS MADE WHEN THE RUN STARTS, empty, and the
// program works in it.
func TestAMissingGroundIsMadeWhenTheRunStarts(t *testing.T) {
	parent := t.TempDir()
	fresh := filepath.Join(parent, "pong")
	agent, double := programAgent(t, parent)
	stand := agent.resolveTaskGround(taskSpec{via: "fake", ground: fresh, brief: "b", deliverable: "d", acceptance: "a"})
	if stand.refusal != "" {
		t.Fatalf("a new folder was refused: %q", stand.refusal)
	}
	program := testPrograms("fake")[0]
	if err := agent.startKnownTaskRunVia(context.Background(), agent.graph().reserve(), "Pong", "build pong", nil, stand, "", &program); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	endBeltRun(t, agent, double)
	if info, err := os.Stat(fresh); err != nil || !info.IsDir() || canonicalPath(spec.Workspace) != canonicalPath(fresh) {
		t.Fatalf("the program works in %q, and the new folder is %v (%v)", spec.Workspace, info, err)
	}
}

// RUNS ON ONE REPOSITORY WORK SIDE BY SIDE. Each has a copy and a branch of
// its own, the repository is held by neither, and each copy goes when its run
// ends while the other's stays.
func TestTwoProgramRunsWorkOnOneRepositoryAtOnce(t *testing.T) {
	repo := newTestRepo(t)
	first, double := programAgent(t, repo)
	if _, _, _, err := first.StartDelegate(context.Background(), "fake", "the first piece of work"); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	if holder := programFolderHolder(canonicalPath(repo)); holder != "" {
		t.Fatalf("a run in a copy holds the repository: %q", holder)
	}
	second, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	if stand := second.resolveTaskGround(taskSpec{via: "fake", ground: repo, brief: "b", deliverable: "d", acceptance: "a"}); stand.refusal != "" {
		t.Fatalf("the second run's proposal was refused: %q", stand.refusal)
	}
	other := prepareIn(t, testPrograms("fake")[0], repo, "A second piece")
	if canonicalPath(other.Dir) == canonicalPath(spec.Workspace) || other.Branch == spec.ProgramBranch {
		t.Fatalf("the second run shares the first's copy %q or branch %q", other.Dir, other.Branch)
	}
	if worktreeCount(t, repo) != 3 {
		t.Fatalf("%d worktrees, want the checkout and two copies", worktreeCount(t, repo))
	}
	commitIn(t, other.Dir, "second.txt")
	if end := other.Finish("done"); !end.Kept || end.CopyLeft != "" {
		t.Fatalf("the second run ended %+v, want its work kept and its copy gone", end)
	}
	if _, err := os.Stat(spec.Workspace); err != nil {
		t.Fatalf("the first run's copy went with the second's: %v", err)
	}
	endBeltRun(t, first, double)
	if worktreeCount(t, repo) != 1 {
		t.Fatalf("%d worktrees after both runs ended, want the checkout alone", worktreeCount(t, repo))
	}
	for _, branch := range []string{spec.ProgramBranch, other.Branch} {
		if branchCommit(repo, branch) == "" {
			t.Fatalf("the branch %s was not kept", branch)
		}
	}
}

// deadProgramFolder readies repo for a program's run the way a run that went
// away leaves it: its copy cut, a file of its work left uncommitted there, and
// its hold dropped by a process that is gone, with its record folder keep.
func deadProgramFolder(t *testing.T, repo, keep string) *ProgramFolder {
	t.Helper()
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "The dead run", Holder: "task 9 (The dead run)", Keep: keep})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(folder.Dir, "half.txt"), "half done\n")
	folder.release()
	return folder
}

// A RUN FOUND INTERRUPTED AT A REOPEN IS FINISHED LIKE ONE THAT ENDED. Nothing
// but its own work can be in its copy, so what it left there is committed on
// its branch and the copy removed, which releases the branch; the person's
// checkout, and their edit in it, are not touched; the row says where the work
// is.
func TestAProgramRunFoundInterruptedAtAReopenIsCommittedAndItsCopyRemoved(t *testing.T) {
	repo := newTestRepo(t)
	var dead *ProgramFolder
	agent, id, _ := reopenedWith(t, func(_ *plandb.Store, taskDir string, _ time.Time) {
		dead = deadProgramFolder(t, repo, taskDir)
		writeFile(t, filepath.Join(repo, "shared.txt"), "the person's own edit, made after codeaf closed\n")
	})
	row := reopenedRow(t, agent, id)
	if files := gitOut(t, repo, "ls-tree", "-r", "--name-only", dead.Branch); !strings.Contains(files, "half.txt") {
		t.Fatalf("the dead run's work was not committed on its branch:\n%s", files)
	}
	if status := gitOut(t, repo, "status", "--porcelain"); strings.TrimSpace(status) != "M shared.txt" || currentBranch(repo) != "work" {
		t.Fatalf("the person's checkout was touched: on %q\n%s", currentBranch(repo), status)
	}
	if _, err := os.Stat(dead.Dir); !os.IsNotExist(err) || worktreeCount(t, repo) != 1 {
		t.Fatalf("the dead run's copy is still there: %v", err)
	}
	want := "its work so far is on the branch " + dead.Branch + " in " + repo + ", 1 file, committed when codeaf found its run had gone"
	if !strings.Contains(row.Report, want) || TaskReasonOf(row.Ending, row.Report) != "codeaf closed while fake was running" {
		t.Fatalf("the reopened row = %+v, want its ending and %q", row, want)
	}
	if row.Branch != dead.Branch {
		t.Fatalf("the reopened row names the branch %q, want %q", row.Branch, dead.Branch)
	}
}

// A RUN THAT WENT AWAY IS FINISHED BEFORE THE NEXT ONE ON ITS REPOSITORY
// STARTS, and the next one is cut from the person's checkout, not from the
// branch the dead run left.
func TestTheNextRunOnARepositoryFinishesTheCopyTheOneThatWentAwayLeft(t *testing.T) {
	repo := newTestRepo(t)
	dead := deadProgramFolder(t, repo, t.TempDir())
	next := prepareIn(t, testPrograms("fake")[0], repo, "The next run")
	defer next.Finish("")
	if files := gitOut(t, repo, "ls-tree", "-r", "--name-only", dead.Branch); !strings.Contains(files, "half.txt") {
		t.Fatalf("the dead run's work was not committed on its branch:\n%s", files)
	}
	if _, err := os.Stat(dead.Dir); !os.IsNotExist(err) {
		t.Fatalf("the dead run's copy is still there: %v", err)
	}
	if next.Branch == dead.Branch || next.Continues || next.Start != strings.TrimSpace(gitOut(t, repo, "rev-parse", "work")) {
		t.Fatalf("the next run is on %q (continues %v), want a new branch cut from the person's checkout", next.Branch, next.Continues)
	}
	if kept, ok := keptProgramFolderEnd(dead.Keep); !ok || !strings.Contains(kept.Sentence(), "its work so far is on the branch "+dead.Branch) {
		t.Fatalf("the dead run's record folder = %+v, want where its work went", kept)
	}
}

// A RUN SENT BACK CARRIES ON ON THE EARLIER RUN'S BRANCH, in a copy of its
// own, and counts what the line did from where the line began. When the person
// has since checked that branch out themselves, a copy cannot have it too, so
// the run carries on on a new branch cut from its tip.
func TestARunSentBackCarriesOnOnTheEarlierRunsBranch(t *testing.T) {
	repo := newTestRepo(t)
	first := prepareIn(t, testPrograms("fake")[0], repo, "The first run")
	commitIn(t, first.Dir, "done.txt")
	first.Finish("done")
	carry := &programCarry{Branch: first.Branch, Root: repo, Home: first.Home, Start: first.Start}
	again, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "The first run, again", Holder: "task 8", Keep: t.TempDir(), Carry: carry})
	if err != nil {
		t.Fatal(err)
	}
	if again.Branch != first.Branch || !again.Continues || again.Start != first.Start || currentBranch(again.Dir) != first.Branch {
		t.Fatalf("the sent-back run is on %q (continues %v, start %s), want %q from %s", again.Branch, again.Continues, shortSha(again.Start), first.Branch, shortSha(first.Start))
	}
	if _, err := os.Stat(filepath.Join(again.Dir, "done.txt")); err != nil {
		t.Fatalf("the earlier run's work is not in the copy: %v", err)
	}
	if end := again.Finish(""); !end.Kept || len(end.Changed) != 1 {
		t.Fatalf("a sent-back run that added nothing ended %+v, want the line's work still counted", end)
	}

	mustGit(t, repo, "checkout", "-q", first.Branch)
	taken, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "The first run, once more", Holder: "task 9", Keep: t.TempDir(), Carry: carry})
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Finish("")
	if taken.Branch == first.Branch || strings.TrimSpace(gitOut(t, taken.Dir, "rev-parse", "HEAD")) != branchCommit(repo, first.Branch) {
		t.Fatalf("a run carrying on a branch the person has checked out is on %q at %s, want a new branch at its tip", taken.Branch, gitOut(t, taken.Dir, "rev-parse", "HEAD"))
	}
}

// A REOPEN THAT SETTLES A FINISHED PROGRAM'S FOLDER SETTLES ITS ROW DONE. The
// program finished and codeaf closed before the row was published; the row was
// left `interrupted` for ever, with no branch, while the page said where the
// work was. It now reads done, with the program's result and the folder's
// sentence, and names the branch that holds the work.
func TestAReopenSettlesTheRowOfAProgramThatFinished(t *testing.T) {
	repo := newTestRepo(t)
	var dead *ProgramFolder
	agent, id, _ := reopenedWith(t, func(store *plandb.Store, taskDir string, _ time.Time) {
		dead = deadProgramFolder(t, repo, taskDir)
		if err := os.Remove(filepath.Join(dead.Dir, "half.txt")); err != nil {
			t.Fatal(err)
		}
		commitIn(t, dead.Dir, "done.txt")
		if err := store.CompleteRoot("finished: its tests pass"); err != nil {
			t.Fatal(err)
		}
	})
	row := reopenedRow(t, agent, id)
	if row.State != TaskDone || !strings.HasPrefix(row.Report, "finished: its tests pass") ||
		!strings.Contains(row.Report, "its work so far is on the branch "+dead.Branch) {
		t.Fatalf("the reopened row = %+v, want it done, with its result and where its work is", row)
	}
	if row.Branch != dead.Branch || row.Merge != mergeKept || row.EndedAt.IsZero() {
		t.Fatalf("the reopened row = %+v, want it to name the branch that holds the work", row)
	}
}

// A FOLDER ANOTHER PROCESS ENDED IS READ BACK FROM THE RUN'S RECORD FOLDER.
// Its folder was finished — by the run itself before codeaf closed, or by the
// next run on that repository — and the reopen found nothing owed, so the row
// said only that codeaf closed. It now says where the work went and names the
// branch.
func TestAReopenReadsTheEndingOfAFolderAnotherProcessEnded(t *testing.T) {
	t.Run("ended by the run", func(t *testing.T) {
		repo := newTestRepo(t)
		var said, branch string
		agent, id, _ := reopenedWith(t, func(_ *plandb.Store, taskDir string, _ time.Time) {
			folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "The ended run", Holder: "task 9 (The ended run)", Keep: taskDir})
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
			said, branch = folder.Finish("done").Sentence(), folder.Branch
		})
		row := reopenedRow(t, agent, id)
		if !strings.Contains(row.Report, said) || row.Branch != branch || TaskReasonOf(row.Ending, row.Report) != "codeaf closed while fake was running" {
			t.Fatalf("the reopened row = %+v, want %q and the branch %s", row, said, branch)
		}
	})
	t.Run("settled by the next run", func(t *testing.T) {
		repo := newTestRepo(t)
		var dead *ProgramFolder
		agent, id, _ := reopenedWith(t, func(_ *plandb.Store, taskDir string, _ time.Time) {
			dead = deadProgramFolder(t, repo, taskDir)
			next := prepareIn(t, testPrograms("fake")[0], repo, "The next run")
			next.Finish("")
		})
		row := reopenedRow(t, agent, id)
		if !strings.Contains(row.Report, "its work so far is on the branch "+dead.Branch) || row.Branch != dead.Branch {
			t.Fatalf("the reopened row = %+v, want where the dead run's work is", row)
		}
	})
}

// THE RECEIPT OF A FOLDER INSIDE A REPOSITORY AT THE HOME FOLDER NAMES THAT
// REPOSITORY. It said the folder "has no git history", which the chat repeated
// to the person, or answered with a `git init` inside their dotfiles.
func TestTheReceiptOfAFolderUnderARepositoryAtHomeNamesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustGit(t, home, "init", "-q")
	writeFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	mustGit(t, home, "add", "-A")
	mustGit(t, home, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "dotfiles")
	project := filepath.Join(home, "Desktop", "pong")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	want := "It is fake's: it works alone in " + project + " itself, inside the git repository at " + canonicalPath(home) +
		", which holds your home folder, so codeaf cuts no branch there and commits nothing; its changes are there as it makes them."
	if got := delegateReceipt(project, testPrograms("fake")[0], &TaskCopyRecord{Dir: project}); !strings.HasPrefix(got, want) || strings.Contains(got, "no git history") {
		t.Fatalf("the receipt = %q, want %q", got, want)
	}
}
