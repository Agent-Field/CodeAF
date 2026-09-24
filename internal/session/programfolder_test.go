package session

// THE CONTRACT OF A PROGRAM'S FOLDER (programfolder.go), in real git in
// temporary repositories: which folder, a branch in a repository and nothing
// of git anywhere else, a checkout that is in the way refused before anything
// starts, one run per folder, and a run whose process went away settled by the
// next codeaf that finds it without a single git write.

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

// A CHECKOUT WITH CHANGES THAT ARE NOT COMMITTED IS REFUSED BEFORE ANYTHING
// STARTS — a modified file, an untracked one, a staged one — naming them, and
// nothing is switched, cut or started. The proposal is refused before its
// card, in the same words.
func TestAProgramIsRefusedACheckoutWithChangesThatAreNotCommitted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		dirty func(t *testing.T, repo string)
		named string
	}{
		{"modified", func(t *testing.T, repo string) {
			writeFile(t, filepath.Join(repo, "shared.txt"), "the person's own line\n")
		}, "(shared.txt)"},
		{"untracked", func(t *testing.T, repo string) { writeFile(t, filepath.Join(repo, "notes", "draft.md"), "draft\n") }, "(notes/draft.md)"},
		{"staged", func(t *testing.T, repo string) {
			writeFile(t, filepath.Join(repo, "new.go"), "package x\n")
			mustGit(t, repo, "add", "new.go")
		}, "(new.go)"},
		{"many", func(t *testing.T, repo string) {
			for _, name := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
				writeFile(t, filepath.Join(repo, name), "x\n")
			}
		}, "(a.go, b.go, c.go and 2 more)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepo(t)
			tc.dirty(t, repo)
			agent, double := programAgent(t, repo)
			want := canonicalPath(repo) + " has changes that are not committed " + tc.named + "; commit or stash them, then ask again"
			_, _, _, err := agent.StartDelegate(context.Background(), "fake", "change the project")
			if err == nil || err.Error() != want {
				t.Fatalf("StartDelegate = %v, want %q", err, want)
			}
			if double.didRun() {
				t.Fatal("a refused checkout started a run")
			}
			if head := currentBranch(repo); head != "work" {
				t.Fatalf("a refused checkout was switched to %q", head)
			}
			if branches := strings.TrimSpace(gitOut(t, repo, "branch", "--list", "task/*")); branches != "" {
				t.Fatalf("a refused checkout was given a branch: %q", branches)
			}
			stand := agent.resolveTaskGround(taskSpec{via: "fake", ground: repo, brief: "b", deliverable: "d", acceptance: "a"})
			if stand.refusal != want {
				t.Fatalf("the proposal's refusal = %q, want %q", stand.refusal, want)
			}
		})
	}
}

// A CHECKOUT IN THE MIDDLE OF A MERGE IS REFUSED, and says what to do.
func TestAProgramIsRefusedACheckoutInTheMiddleOfAMerge(t *testing.T) {
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
	agent, double := programAgent(t, repo)
	_, _, _, err := agent.StartDelegate(context.Background(), "fake", "change the project")
	if err == nil || err.Error() != canonicalPath(repo)+" is in the middle of a merge; finish it or abort it, then ask again" {
		t.Fatalf("StartDelegate = %v, want the merge named", err)
	}
	if double.didRun() {
		t.Fatal("a checkout in the middle of a merge started a run")
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

// ONE RUN PER FOLDER. A second program run on a folder one is working in —
// from another conversation here — is refused, naming the run that holds it,
// at its card and at its start alike; once the first has ended the folder is
// free again.
func TestASecondProgramRunOnTheSameFolderIsRefusedWhileTheFirstRuns(t *testing.T) {
	repo := newTestRepo(t)
	first, double := programAgent(t, repo)
	id, title, _, err := first.StartDelegate(context.Background(), "fake", "the first piece of work")
	if err != nil {
		t.Fatal(err)
	}
	<-double.entered
	second, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	want := canonicalPath(repo) + " is busy: fake, " + taskStopName(id, title) + ", is working in it, and one folder takes one program run at a time; ask again when that run has ended"
	if _, _, _, err := second.StartDelegate(context.Background(), "fake", "a second piece"); err == nil || err.Error() != want {
		t.Fatalf("the second run's start = %v, want %q", err, want)
	}
	if stand := second.resolveTaskGround(taskSpec{via: "fake", ground: repo, brief: "b", deliverable: "d", acceptance: "a"}); stand.refusal != want {
		t.Fatalf("the second run's proposal = %q, want %q", stand.refusal, want)
	}
	endBeltRun(t, first, double)
	if holder := programFolderHolder(canonicalPath(repo)); holder != "" {
		t.Fatalf("the folder is still held by %q after its run ended", holder)
	}
}

// deadProgramFolder readies repo for a program's run the way a run that went
// away leaves it: its branch cut, a file of its work left uncommitted, and its
// hold dropped by a process that is gone, with its record folder keep.
func deadProgramFolder(t *testing.T, repo, keep string) *ProgramFolder {
	t.Helper()
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "The dead run", Holder: "task 9 (The dead run)", Keep: keep})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "half.txt"), "half done\n")
	folder.release()
	return folder
}

// A RUN FOUND INTERRUPTED AT A REOPEN IS SETTLED WITHOUT A COMMIT. Its process
// went away, so what is uncommitted in its folder may be its last edits or the
// person's own made on its branch since — here both — and codeaf commits
// neither: the checkout stays on the run's branch as it was left, and the row
// and page say where the work is and how much of it is not committed.
func TestAProgramRunFoundInterruptedAtAReopenIsSettledWithoutACommit(t *testing.T) {
	repo := newTestRepo(t)
	var dead *ProgramFolder
	agent, id, _ := reopenedWith(t, func(_ *plandb.Store, taskDir string, _ time.Time) {
		dead = deadProgramFolder(t, repo, taskDir)
		writeFile(t, filepath.Join(repo, "shared.txt"), "the person's own edit, made after codeaf closed\n")
	})
	row := reopenedRow(t, agent, id)
	if head := currentBranch(repo); head != dead.Branch {
		t.Fatalf("the checkout is on %q after the reopen, want the dead run's branch %q", head, dead.Branch)
	}
	if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", dead.Branch)); tip != dead.Start {
		t.Fatalf("the reopen committed on the dead run's branch: %s, want it still at %s", tip, dead.Start)
	}
	if status := gitOut(t, repo, "status", "--porcelain"); !strings.Contains(status, " M shared.txt") || !strings.Contains(status, "?? half.txt") {
		t.Fatalf("what was uncommitted was not left as it was:\n%s", status)
	}
	want := "its work so far is on its branch " + dead.Branch + " in " + repo + ", which is checked out there, as it left it, with 2 files not committed; commit or stash them there before you go back to your branch work"
	if !strings.Contains(row.Report, want) || TaskReasonOf(row.Ending, row.Report) != "codeaf closed while fake was running" {
		t.Fatalf("the reopened row = %+v, want its ending and %q", row, want)
	}
	if again, ok := readProgramFolder(canonicalPath(repo)); !ok || again.Ended != want {
		t.Fatalf("the folder's record = %+v, want it ended with %q", again, want)
	}
	if holder := programFolderHolder(canonicalPath(repo)); holder != "" {
		t.Fatalf("the reopen still holds the folder: %q", holder)
	}
}

// A RUN THAT WENT AWAY IN A FOLDER IS SETTLED BEFORE THE NEXT ONE STARTS THERE,
// AND NOTHING OF IT IS COMMITTED: the next run meets what is uncommitted the
// way it meets anybody's changes — refused, naming them — and is told whose
// they may be. A person's edits made on the dead run's branch used to be swept
// into a commit under codeaf's name, and the next run cut on top of it.
func TestTheNextRunInAFolderIsRefusedWhatTheOneThatWentAwayLeft(t *testing.T) {
	repo := newTestRepo(t)
	dead := deadProgramFolder(t, repo, t.TempDir())
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's own edit, made after codeaf closed\n")
	_, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "The next run", Holder: "task 10 (The next run)", Keep: t.TempDir()})
	want := repo + " has changes that are not committed (shared.txt, half.txt); commit or stash them, then ask again; they may be an earlier fake run's, which codeaf could not finish: its branch " + dead.Branch + " is checked out there"
	if err == nil || err.Error() != want {
		t.Fatalf("the next run = %v, want %q", err, want)
	}
	if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", dead.Branch)); tip != dead.Start {
		t.Fatalf("the next run committed on the dead run's branch: %s, want it still at %s", tip, dead.Start)
	}
	if head := currentBranch(repo); head != dead.Branch {
		t.Fatalf("the checkout is on %q, want the dead run's branch", head)
	}
	if again, ok := readProgramFolder(canonicalPath(repo)); !ok || !strings.HasPrefix(again.Ended, "its work so far is on its branch "+dead.Branch) {
		t.Fatalf("the dead run's record = %+v, want it settled", again)
	}
	if holder := programFolderHolder(canonicalPath(repo)); holder != "" {
		t.Fatalf("the refused run still holds the folder: %q", holder)
	}
}

// A MERGE THE PERSON IS RESOLVING ON A DEAD RUN'S BRANCH IS NOT CONCLUDED: the
// next run is refused over it, and MERGE_HEAD and the conflict are left alone.
func TestTheNextRunDoesNotConcludeAMergeOnTheBranchAnEarlierRunLeft(t *testing.T) {
	repo := newTestRepo(t)
	dead := deadProgramFolder(t, repo, t.TempDir())
	if err := os.Remove(filepath.Join(repo, "half.txt")); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "checkout", "-q", "-b", "other", "work")
	writeFile(t, filepath.Join(repo, "shared.txt"), "theirs\n")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "theirs")
	mustGit(t, repo, "checkout", "-q", dead.Branch)
	writeFile(t, filepath.Join(repo, "shared.txt"), "ours\n")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "ours")
	if _, err := git(repo, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "other"); err == nil {
		t.Fatal("the merge did not stop on its conflict")
	}
	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	_, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "The next run", Holder: "task 10 (The next run)", Keep: t.TempDir()})
	if err == nil || !strings.HasPrefix(err.Error(), repo+" is in the middle of a merge; finish it or abort it, then ask again") {
		t.Fatalf("the next run = %v, want the merge named", err)
	}
	if after := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); after != head {
		t.Fatalf("the person's merge was concluded: HEAD moved from %s to %s", head, after)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); err != nil {
		t.Fatalf("the merge in progress is gone: %v", err)
	}
}

// A CLEAN FOLDER A RUN WENT AWAY IN IS WORKED IN AGAIN: its committed work
// stays on its branch, which the next run is cut from, as it would be from
// any branch the person had checked out.
func TestTheNextRunCarriesOnInAFolderTheOneThatWentAwayLeftClean(t *testing.T) {
	repo := newTestRepo(t)
	dead := deadProgramFolder(t, repo, t.TempDir())
	if err := os.Remove(filepath.Join(repo, "half.txt")); err != nil {
		t.Fatal(err)
	}
	commitIn(t, repo, "done.txt")
	next, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "The next run", Holder: "task 10 (The next run)", Keep: t.TempDir()})
	if err != nil {
		t.Fatalf("the next run was refused a clean folder: %v", err)
	}
	defer next.Finish("")
	if next.Home != dead.Branch {
		t.Fatalf("the next run was cut from %q, want the dead run's branch %q, which was left checked out", next.Home, dead.Branch)
	}
	if files := gitOut(t, repo, "ls-tree", "--name-only", dead.Branch); !strings.Contains(files, "done.txt") {
		t.Fatalf("the dead run's committed work is not on its branch:\n%s", files)
	}
}
