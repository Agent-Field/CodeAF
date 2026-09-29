package session

// WHAT A PROGRAM LEFT IN ITS COPY IS NEVER DELETED UNSAVED (programcopy.go):
// a lock file a killed git left does not cost the work, a copy git can no
// longer read or whose leftovers cannot be kept stays on disk, every rescue
// branch gets a name of its own, a run nobody saw end credits no model that did
// not answer and keeps the candidate it submitted, the lines codeaf adds to the
// repository's exclude file last only as long as a copy, a project's list of
// links that does not apply refuses the run, and a record is never finished
// twice.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyGitPath is where git keeps name for the copy at dir: its own index and
// HEAD, or the repository's shared refs.
func copyGitPath(t *testing.T, dir, name string) string {
	t.Helper()
	path := strings.TrimSpace(gitOut(t, dir, "rev-parse", "--git-path", name))
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return path
}

// A LOCK A KILLED GIT LEFT IN THE COPY DOES NOT COST THE WORK. A program
// killed in the middle of one of its own commits leaves `index.lock` behind,
// which refused codeaf's `git add` and the patch alike, and the copy — the
// tracked edit and the new file in it — was deleted anyway.
func TestAStaleIndexLockInTheCopyDoesNotCostTheWork(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
	writeFile(t, filepath.Join(folder.Dir, "shared.txt"), "the program's edit\n")
	writeFile(t, filepath.Join(folder.Dir, "new.go"), "package fix\n")
	writeFile(t, copyGitPath(t, folder.Dir, "index.lock"), "")
	end := folder.Finish("done")
	if !end.Kept || end.Refused != "" || end.CopyKept != "" {
		t.Fatalf("the ending = %q, want the work committed", end.Sentence())
	}
	if files := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); !strings.Contains(files, "new.go") {
		t.Fatalf("the branch holds %q, want the new file", files)
	}
	if body := gitOut(t, repo, "show", folder.Branch+":shared.txt"); body != "the program's edit\n" {
		t.Fatalf("the branch's shared.txt = %q, want the program's edit", body)
	}
	if _, err := os.Stat(folder.Dir); !os.IsNotExist(err) {
		t.Fatalf("the copy is still there: %v", err)
	}
}

// A COPY GIT CAN NO LONGER READ IS KEPT, NOT DELETED. The program's shell
// removed the copy's `.git` file: nothing of it can be committed or kept as a
// patch, and git will not remove it either, so it was deleted outright and the
// ending never said so. It stays, and the ending says where and why.
func TestACopyGitCannotReadIsKeptWithTheWorkInIt(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
	writeFile(t, filepath.Join(folder.Dir, "new.go"), "package fix\n")
	if err := os.Remove(filepath.Join(folder.Dir, ".git")); err != nil {
		t.Fatal(err)
	}
	end := folder.Finish("done")
	if end.CopyKept == "" || end.CopyLeft != folder.Dir {
		t.Fatalf("the ending = %+v, want the copy kept", end)
	}
	if body := readFile(t, filepath.Join(folder.Dir, "new.go")); body != "package fix\n" {
		t.Fatalf("the program's file in the kept copy = %q", body)
	}
	said := end.Sentence()
	if !strings.Contains(said, "its copy is kept at "+folder.Dir+", because git can no longer read it") ||
		!strings.Contains(said, "worktree prune` releases "+folder.Branch) || strings.Contains(said, "changed nothing") {
		t.Fatalf("the ending = %q, want the kept copy named", said)
	}
}

// A COPY WHOSE LEFTOVERS CANNOT BE KEPT AS A PATCH STAYS. The program left its
// copy on no branch with a file uncommitted, and the run's record folder cannot
// be written: the file is committed nowhere and patched nowhere, so the copy
// holding it is not removed.
func TestACopyWhoseLeftoversCannotBeKeptAnywhereStays(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
	blocked := filepath.Join(t.TempDir(), "a-file")
	writeFile(t, blocked, "")
	folder.Keep = filepath.Join(blocked, "record")
	mustGit(t, folder.Dir, "checkout", "-q", "--detach")
	writeFile(t, filepath.Join(folder.Dir, "loose.go"), "package loose\n")
	end := folder.Finish("done")
	if end.CopyKept == "" || end.Patch != "" {
		t.Fatalf("the ending = %+v, want the copy kept and no patch", end)
	}
	if _, err := os.Stat(filepath.Join(folder.Dir, "loose.go")); err != nil {
		t.Fatalf("the loose file went with the copy: %v", err)
	}
}

// EVERY RESCUE BRANCH HAS A NAME OF ITS OWN. Two runs of one line, carried on
// on one branch, both committed on no branch; the second rescue once collided
// with the first's `<branch>-detached`, failed, and its commit went with the
// copy.
func TestTwoRunsOfALineBothKeepTheCommitsTheyMadeOnNoBranch(t *testing.T) {
	repo := newTestRepo(t)
	program := testPrograms("fake")[0]
	first := prepareIn(t, program, repo, "Fix the parser")
	detachedCommit := func(dir, name string) string {
		mustGit(t, dir, "checkout", "-q", "--detach")
		writeFile(t, filepath.Join(dir, name), name+"\n")
		mustGit(t, dir, "add", name)
		mustGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", name)
		return strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	}
	one := detachedCommit(first.Dir, "one.txt")
	firstEnd := first.Finish("")
	carry := &programCarry{Branch: first.Branch, Root: repo, Home: first.Home, Start: first.Start}
	second, err := PrepareProgramFolder(ProgramFolderOrder{Program: program, Dir: repo, Title: "Fix the parser", Holder: "task 8", Keep: t.TempDir(), Carry: carry})
	if err != nil {
		t.Fatal(err)
	}
	two := detachedCommit(second.Dir, "two.txt")
	secondEnd := second.Finish("")
	if firstEnd.Saved != first.Branch+"-detached" || secondEnd.Saved != first.Branch+"-detached-2" {
		t.Fatalf("the rescues are %q and %q", firstEnd.Saved, secondEnd.Saved)
	}
	if branchCommit(repo, firstEnd.Saved) != one || branchCommit(repo, secondEnd.Saved) != two || secondEnd.CopyKept != "" {
		t.Fatalf("a rescue does not hold its run's commit: %+v", secondEnd)
	}
}

// A RUN NOBODY SAW END CREDITS NO MODEL THAT DID NOT ANSWER. The commit that
// finishes a run found gone named the conversation's own model, which answered
// none of its calls; with no record of the calls that answered, it names none.
func TestTheCommitForARunFoundGoneCreditsNoModelThatDidNotAnswer(t *testing.T) {
	repo := newTestRepo(t)
	dead, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "The dead run", Holder: "task 9", Keep: t.TempDir(), SignModel: "the-chat-model"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dead.Dir, "half.txt"), "half done\n")
	dead.release()
	sweepProgramCopies(repo)
	message := gitOut(t, repo, "log", "-1", "--format=%B", dead.Branch)
	if !strings.Contains(message, "The dead run") || strings.Contains(message, "the-chat-model") {
		t.Fatalf("the finishing commit = %q, want no credit for a model that did not answer", message)
	}
}

// A CANDIDATE A RUN SUBMITTED AND NEVER FINISHED IS KEPT. senior-dev keeps it
// at a ref of the copy's own, which goes with the copy; a run found gone puts
// it on a branch first, and says which.
func TestARunFoundGoneKeepsTheCandidateItSubmitted(t *testing.T) {
	repo := newTestRepo(t)
	dead := deadProgramFolder(t, repo, t.TempDir())
	writeFile(t, filepath.Join(dead.Dir, "candidate.go"), "package candidate\n")
	mustGit(t, dead.Dir, "add", "candidate.go")
	tree := strings.TrimSpace(gitOut(t, dead.Dir, "write-tree"))
	candidate := strings.TrimSpace(gitOut(t, dead.Dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", tree, "-p", "HEAD", "-m", "submitted"))
	mustGit(t, dead.Dir, "update-ref", "refs/worktree/senior-dev/submitted", candidate)
	mustGit(t, dead.Dir, "reset", "-q")
	if err := os.Remove(filepath.Join(dead.Dir, "candidate.go")); err != nil {
		t.Fatal(err)
	}
	next := prepareIn(t, testPrograms("fake")[0], repo, "The next run")
	defer next.Finish("")
	kept := dead.Branch + "-submitted"
	if branchCommit(repo, kept) != candidate {
		t.Fatalf("the submitted candidate is not on %s", kept)
	}
	ending, ok := keptProgramFolderEnd(dead.Keep)
	if !ok || !strings.Contains(ending.Sentence(), "what it had saved at refs/worktree/senior-dev/submitted, which its branch does not hold, is kept on the branch "+kept) {
		t.Fatalf("the ending = %q, want the kept candidate named", ending.Sentence())
	}
	start := deadProgramFolder(t, repo, t.TempDir())
	mustGit(t, start.Dir, "update-ref", "refs/worktree/senior-dev/start", "HEAD")
	sweepProgramCopies(repo)
	if branchCommit(repo, start.Branch+"-start") != "" {
		t.Fatal("a ref its branch already holds was put on a branch")
	}
}

// A STARTING TREE ALREADY ON THE BRANCH NEEDS NO RESCUE BRANCH. The saved
// start commit is not an ancestor of the branch tip, but its tree is the base
// tree, so keeping it would claim the run left work its branch lacks.
func TestASweptCopyPutsNoBranchOnItsStartingTree(t *testing.T) {
	repo := newTestRepo(t)
	dead := deadProgramFolder(t, repo, t.TempDir())
	base := dead.base()
	commitIn(t, dead.Dir, "committed.txt")
	tree := strings.TrimSpace(gitOut(t, dead.Dir, "rev-parse", base+"^{tree}"))
	start := strings.TrimSpace(gitOut(t, dead.Dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", tree, "-p", base, "-m", "starting tree"))
	ref := "refs/worktree/senior-dev/start"
	mustGit(t, dead.Dir, "update-ref", ref, start)
	if _, err := git(dead.Dir, "merge-base", "--is-ancestor", start, dead.Branch); err == nil {
		t.Fatal("the saved start commit is an ancestor of the branch")
	}
	sweepProgramCopies(repo)
	if branchCommit(repo, dead.Branch+"-start") != "" {
		t.Fatal("a branch was put on the starting tree already held by the run's branch")
	}
	if _, err := os.Stat(dead.Dir); !os.IsNotExist(err) {
		t.Fatalf("the swept copy is still there: %v", err)
	}
	ending, ok := keptProgramFolderEnd(dead.Keep)
	if !ok || strings.Contains(ending.Sentence(), ref) {
		t.Fatalf("the ending = %q, want no saved start ref named", ending.Sentence())
	}
}

// A RUN FOUND GONE WITH NOTHING LEFT TO COMMIT SAYS SO. Its work is on its
// branch as it committed it itself, and the ending does not claim a commit
// codeaf never made.
func TestARunFoundGoneWithNothingLeftSaysItsOwnCommitsHoldTheWork(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "The dead run")
	commitIn(t, folder.Dir, "done.txt")
	folder.release()
	sweepProgramCopies(repo)
	ending, ok := keptProgramFolderEnd(folder.Keep)
	if !ok || !strings.Contains(ending.Sentence(), "1 file, as its run had committed it before it went away") {
		t.Fatalf("the ending = %q", ending.Sentence())
	}
}

// THE LINES CODEAF ADDS TO THE EXCLUDE FILE LAST ONLY AS LONG AS A COPY. A link
// a `.gitignore` ignores only as a folder is named there while a copy of the
// repository is on disk, a name it already ignores is not, and the file is
// exactly the person's own again once the last copy is gone — so a team that
// later stops ignoring `.env` is never told by a line of codeaf's that it is
// ignored.
func TestTheExcludeLinesLastOnlyAsLongAsACopy(t *testing.T) {
	// THE NETWORK IS OFF, so node_modules is linked, which is when a line is
	// needed for it.
	t.Setenv(programNetworkEnv, "off")
	repo := safetyRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "node_modules/\n.env\n")
	mustGit(t, repo, "add", ".gitignore")
	mustGit(t, repo, "commit", "-q", "-m", "ignore")
	writeFile(t, filepath.Join(repo, "node_modules", "left-pad", "index.js"), "module.exports = 1\n")
	writeFile(t, filepath.Join(repo, ".env"), "A=1\n")
	exclude := copyGitPath(t, repo, "info/exclude")
	own := "# the person's own\n*.log\n"
	writeFile(t, exclude, own)
	first := prepareIn(t, testPrograms("fake")[0], repo, "First")
	second := prepareIn(t, testPrograms("fake")[0], repo, "Second")
	during := readFile(t, exclude)
	if !strings.HasPrefix(during, own) || !strings.Contains(during, "\n/node_modules\n") || strings.Contains(during, "/.env") {
		t.Fatalf("the exclude file during the runs =\n%s", during)
	}
	if strings.Count(during, programCopyExcludeSentinel) != 1 {
		t.Fatalf("two copies wrote two blocks:\n%s", during)
	}
	if status := strings.TrimSpace(gitOut(t, first.Dir, "status", "--porcelain")); status != "" {
		t.Fatalf("the copy's tree is not clean with its links in it:\n%s", status)
	}
	first.Finish("")
	if !strings.Contains(readFile(t, exclude), "/node_modules") {
		t.Fatal("the lines went while a copy still needed them")
	}
	second.Finish("")
	if after := readFile(t, exclude); after != own {
		t.Fatalf("the exclude file after the last copy =\n%q, want the person's own %q", after, own)
	}
}

// A PROJECT'S LIST OF LINKS THAT DOES NOT APPLY REFUSES THE RUN, naming the
// file, rather than quietly linking the defaults; a list written as a JSON list
// is taken, as the text is.
func TestAProjectsListOfLinksThatDoesNotApplyRefusesTheRun(t *testing.T) {
	repo := safetyRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "bin/\n")
	mustGit(t, repo, "add", ".gitignore")
	mustGit(t, repo, "commit", "-q", "-m", "ignore")
	writeFile(t, filepath.Join(repo, "bin", "tool"), "a tool\n")
	settings := filepath.Join(repo, ".codeaf", "config.json")
	writeFile(t, settings, `{"program.links": ["bin"]}`)
	listed := prepareIn(t, testPrograms("fake")[0], repo, "Listed")
	if _, err := os.Lstat(filepath.Join(listed.Dir, "bin", "tool")); err != nil {
		t.Fatalf("a list written as a JSON list was not carried in: %v", err)
	}
	listed.Finish("")
	for body, want := range map[string]string{
		`{"program.links": 5}`:           "program.links wants comma-separated text, or a list of names",
		`{"program.links": "tools/bin"}`: `names "tools/bin", which is not a folder or file at the top of the repository`,
		`{"program.links": "bin",}`:      "read project settings",
	} {
		writeFile(t, settings, body)
		err := prepareErr(t, repo)
		if !strings.Contains(err, want) || !strings.Contains(err, "fix it, then ask again") {
			t.Fatalf("%s refused with %q, want %q", body, err, want)
		}
		if worktreeCount(t, repo) != 1 {
			t.Fatalf("a refused run left a copy behind for %s", body)
		}
	}
}

// A RUN THAT ENDED WHILE A SWEEP ASKED FOR IT IS NOT FINISHED TWICE. The sweep
// read its record as owed and waited for its hold; the run removed its record
// before letting go, and the sweep reads it again once it has the hold. A run
// whose ending is in its record folder — its codeaf gone between writing it and
// letting go — is let go, never ended again over the top of it.
func TestARunEndedWhileASweepAskedIsNotFinishedTwice(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
	record := programFolderRecord(folder.key)
	owed, ok := readProgramFolderAt(record)
	if !ok {
		t.Fatal("no record for a live run")
	}
	said := folder.Finish("done").Sentence()
	if claimOwedProgramFolder(record, owed) != nil {
		t.Fatal("a sweep claimed a run that had ended")
	}
	if ending, _ := keptProgramFolderEnd(folder.Keep); ending.Sentence() != said {
		t.Fatalf("the ending was written over: %q, want %q", ending.Sentence(), said)
	}

	crashed := prepareIn(t, testPrograms("fake")[0], repo, "Crash while letting go")
	record = programFolderRecord(crashed.key)
	owed, _ = readProgramFolderAt(record)
	end := crashed.settle("done")
	end.keepEnding()
	crashed.release()
	if claimOwedProgramFolder(record, owed) != nil {
		t.Fatal("a sweep settled a run whose ending was already written")
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("the ended run's record is still owed: %v", err)
	}
	if ending, _ := keptProgramFolderEnd(crashed.Keep); ending.Sentence() != end.Sentence() {
		t.Fatalf("the ending was written over: %q", ending.Sentence())
	}
}

// A RUN WAITING ITS TURN IN A REPOSITORY IS TOLD IT WILL WORK IN A COPY. It
// has no record yet to read a branch off, and its receipt once said the
// repository "has no git history" and that codeaf's tools would write nothing
// in it.
func TestAQueuedRunsReceiptInARepositorySaysItWillWorkInACopy(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's edit\n")
	said := delegateReceipt(repo, testPrograms("fake")[0], nil)
	if !strings.Contains(said, "when it starts it works alone in a private copy of "+repo) ||
		!strings.Contains(said, "Your uncommitted changes (shared.txt) go into its copy as they are when it starts, as the first commit on its branch") ||
		strings.Contains(said, "no git history") || strings.Contains(said, "write nothing") {
		t.Fatalf("the queued run's receipt = %q", said)
	}
}

// A RUN THAT NEVER STARTED SAYS WHAT BECAME OF ITS BRANCH. Its empty branch is
// deleted, and the ending written down says so — not that the branch is kept.
func TestARunThatNeverStartedSaysItsEmptyBranchWasDeleted(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Never started")
	folder.abandon()
	if branchCommit(repo, folder.Branch) != "" || worktreeCount(t, repo) != 1 {
		t.Fatal("a run that never started left its branch or its copy")
	}
	ending, ok := keptProgramFolderEnd(folder.Keep)
	if !ok || !strings.Contains(ending.Sentence(), "it never started, so its copy is removed and its empty branch "+folder.Branch+" deleted") {
		t.Fatalf("the ending = %q", ending.Sentence())
	}
}

// A SECOND HAND-OFF IN ONE CONVERSATION IS REFUSED WITH WHERE THE WORK IS: in a
// copy of the repository when the program works in one, and in the folder
// itself only when it works there.
func TestASecondHandOffIsRefusedWithWhereTheFirstWorks(t *testing.T) {
	program := testPrograms("fake")[0]
	copied := &beltRun{delegate: &program, ground: "/r/repo", folder: &ProgramFolder{Repo: "/r/repo", Dir: "/cache/repo-1"}}
	if err := programJoinRefusal(&program, copied); err == nil || !strings.HasPrefix(err.Error(), "work is already underway in a copy of /r/repo; fake runs alone in a conversation") {
		t.Fatalf("the refusal = %v", err)
	}
	plain := &beltRun{delegate: &program, ground: "/r/notes", folder: &ProgramFolder{Dir: "/r/notes"}}
	if err := programJoinRefusal(nil, plain); err == nil || !strings.HasPrefix(err.Error(), "work is already underway in /r/notes;") {
		t.Fatalf("the refusal = %v", err)
	}
}

// A BRANCH WHOSE WORK PASSED IS NEVER WRITTEN AGAIN. The next run of the line
// starts from its tip on a new branch of its own, counts only its own files,
// and says the branch it was cut from; the passed branch keeps exactly what
// passed.
func TestTheRunAfterAPassStartsOnANewBranchOnTopOfIt(t *testing.T) {
	repo := newTestRepo(t)
	program := testPrograms("fake")[0]
	first := prepareIn(t, program, repo, "Build the parser")
	commitIn(t, first.Dir, "parser.go")
	first.Finish("passed")
	passed := branchCommit(repo, first.Branch)
	carry := &programCarry{Branch: first.Branch, Root: repo, Home: first.Home, Start: first.Start, Fresh: true}
	next, err := PrepareProgramFolder(ProgramFolderOrder{Program: program, Dir: repo, Title: "Build the lexer", Holder: "task 2", Keep: t.TempDir(), Carry: carry})
	if err != nil {
		t.Fatal(err)
	}
	if next.Branch == first.Branch || next.Continues || next.From != first.Branch || next.Start != passed {
		t.Fatalf("the next run = %+v, want a new branch cut from %s", next, first.Branch)
	}
	if receipt := delegateReceipt(repo, program, runCopyOf(next.tree())); !strings.Contains(receipt, "on a new branch "+next.Branch+" cut from "+first.Branch+", whose work passed") {
		t.Fatalf("the receipt = %q", receipt)
	}
	if _, err := os.Stat(filepath.Join(next.Dir, "parser.go")); err != nil {
		t.Fatalf("the passed work is not in the next run's copy: %v", err)
	}
	writeFile(t, filepath.Join(next.Dir, "lexer.go"), "package lexer\n")
	said := next.Finish("done").Sentence()
	if branchCommit(repo, first.Branch) != passed {
		t.Fatal("the passed branch was written again")
	}
	if !strings.Contains(said, "its work is on the branch "+next.Branch+" in "+repo+", 1 file, on top of "+first.Branch+", whose work it holds too") {
		t.Fatalf("the ending = %q", said)
	}
}

// A FOLDER FOR COPIES THAT IS NOT THIS ACCOUNT'S OWN IS REFUSED: a link is
// never followed, and a folder open to others is closed to them.
func TestTheFolderForCopiesIsPrivate(t *testing.T) {
	base := t.TempDir()
	open := filepath.Join(base, "open")
	if err := os.Mkdir(open, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := privateCopyRoot(open); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(open); info.Mode().Perm() != 0o700 {
		t.Fatalf("the folder for copies is %v, want 0700", info.Mode().Perm())
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(open, link); err != nil {
		t.Fatal(err)
	}
	if err := privateCopyRoot(link); err == nil || !strings.Contains(err.Error(), "is a link or a file") {
		t.Fatalf("a link for the folder for copies = %v, want it refused", err)
	}
}

// A CUT GIT LFS REFUSED SAYS WHY. A copy is a fresh checkout, and a PATH with
// no git-lfs fails it with a line that names the filter and nothing else.
func TestACutWithNoGitLFSNamesTheCause(t *testing.T) {
	said := copyCutWords("git-lfs filter-process: git-lfs: command not found\nfatal: the remote end hung up unexpectedly\n")
	if !strings.HasPrefix(said, "git-lfs filter-process: git-lfs: command not found; this repository keeps files in Git LFS") {
		t.Fatalf("the refusal = %q", said)
	}
	if said := copyCutWords("fatal: invalid reference: nope"); said != "fatal: invalid reference: nope" {
		t.Fatalf("an ordinary refusal = %q", said)
	}
}

// A RUN THAT ADDS NOTHING TO THE PERSON'S UNCOMMITTED WORK CHANGED NOTHING.
// Their changes are the first commit on its branch and are never counted as
// its work; the program's notes folder is never carried in; and a run that
// never started deletes its branch, snapshot and all, leaving the person's
// changes where they always were.
func TestUncommittedWorkCarriedInIsNeverCountedAsTheProgramsOwn(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "half.go"), "package half\n")
	writeFile(t, filepath.Join(repo, ".fake-notes", "old.md"), "a notes folder of the person's\n")
	program := notesProgram()
	folder := prepareIn(t, program, repo, "Finish it")
	if folder.Snapshot == "" || folder.base() != folder.Snapshot {
		t.Fatalf("the person's work was not carried in: %+v", folder)
	}
	if files := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Snapshot); strings.Contains(files, ".fake-notes") || !strings.Contains(files, "half.go") {
		t.Fatalf("the carried-in commit holds %q", files)
	}
	end := folder.Finish("")
	if end.Kept || len(end.Changed) != 0 || !strings.HasPrefix(end.Sentence(), "it changed nothing") {
		t.Fatalf("a run that added nothing = %q", end.Sentence())
	}

	never := prepareIn(t, program, repo, "Never started")
	never.abandon()
	if branchCommit(repo, never.Branch) != "" {
		t.Fatal("a run that never started kept its branch")
	}
	if body := readFile(t, filepath.Join(repo, "half.go")); body != "package half\n" {
		t.Fatalf("the person's file = %q", body)
	}
}

// A FILE THE PERSON'S INDEX HIDES IS NOT PART OF THE SNAPSHOT. A fresh private
// index does not inherit skip-worktree, so staging the whole checkout used to
// include local configuration that the person's git status did not name.
func TestTheSnapshotLeavesOutALocalFileTheIndexHides(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "local.cfg"), "committed\n")
	mustGit(t, repo, "add", "local.cfg")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "local config")
	mustGit(t, repo, "update-index", "--skip-worktree", "local.cfg")
	writeFile(t, filepath.Join(repo, "local.cfg"), "private edit\n")
	writeFile(t, filepath.Join(repo, "shared.txt"), "visible edit\n")
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Read the visible work")
	defer folder.Finish("")
	if changes := strings.TrimSpace(gitOut(t, repo, "diff-tree", "--no-commit-id", "--name-status", "-r", folder.Snapshot)); changes != "M\tshared.txt" {
		t.Fatalf("the snapshot changed %q, want only shared.txt", changes)
	}
	if got := readFile(t, filepath.Join(folder.Dir, "local.cfg")); got != "committed\n" {
		t.Fatalf("the copy's local.cfg = %q, want committed bytes", got)
	}
	if got := readFile(t, filepath.Join(repo, "local.cfg")); got != "private edit\n" {
		t.Fatalf("the person's local.cfg = %q", got)
	}
}

// AN ABSENT FILE THAT THE PERSON'S INDEX HIDES IS NOT A DELETION. Neither
// index flag should let the private snapshot remove a file git status omits.
func TestTheSnapshotRecordsNoDeletionTheIndexHides(t *testing.T) {
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		t.Run(flag, func(t *testing.T) {
			repo := newTestRepo(t)
			writeFile(t, filepath.Join(repo, "hidden.txt"), "committed\n")
			mustGit(t, repo, "add", "hidden.txt")
			mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "hidden file")
			mustGit(t, repo, "update-index", flag, "hidden.txt")
			if err := os.Remove(filepath.Join(repo, "hidden.txt")); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(repo, "shared.txt"), "visible edit\n")
			folder := prepareIn(t, testPrograms("fake")[0], repo, "Read the visible work")
			defer folder.Finish("")
			if changes := strings.TrimSpace(gitOut(t, repo, "diff-tree", "--no-commit-id", "--name-status", "-r", folder.Snapshot)); changes != "M\tshared.txt" {
				t.Fatalf("the snapshot changed %q, want only shared.txt", changes)
			}
			if got := readFile(t, filepath.Join(folder.Dir, "hidden.txt")); got != "committed\n" {
				t.Fatalf("the copy's hidden.txt = %q, want committed bytes", got)
			}
		})
	}
}

// A STAGED RENAME NEEDS BOTH PATHS STAGED IN THE PRIVATE INDEX. The receipt
// names only its destination, but omitting its source leaves the old file too.
func TestTheSnapshotCarriesAStagedRenameAsARename(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "a.txt"), "rename me\n")
	mustGit(t, repo, "add", "a.txt")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "file to rename")
	mustGit(t, repo, "mv", "a.txt", "b.txt")
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Read the rename")
	defer folder.Finish("")
	if changes := strings.TrimSpace(gitOut(t, repo, "diff-tree", "--no-commit-id", "--name-status", "-r", "-M", folder.Snapshot)); changes != "R100\ta.txt\tb.txt" {
		t.Fatalf("the snapshot changed %q, want the staged rename", changes)
	}
}

// A LINKED DEPENDENCY FOLDER CANNOT BE CLONED SAFELY. With the network on,
// the program installs into its own copy and never writes through that link.
func TestANetworkOnCopyNeverLinksToAPersonsLinkedNodeModules(t *testing.T) {
	t.Setenv(programNetworkEnv, "allow")
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "node_modules\n")
	mustGit(t, repo, "add", ".gitignore")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "ignore dependencies")
	personDeps := t.TempDir()
	writeFile(t, filepath.Join(personDeps, "installed.txt"), "original\n")
	if err := os.Symlink(personDeps, filepath.Join(repo, "node_modules")); err != nil {
		t.Fatal(err)
	}
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Read dependencies")
	copyDeps := filepath.Join(folder.Dir, "node_modules")
	if info, err := os.Lstat(copyDeps); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	} else if err == nil && info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the copy's node_modules is a link to the person's folder")
	}
	folder.Finish("")
	if got := readFile(t, filepath.Join(personDeps, "installed.txt")); got != "original\n" {
		t.Fatalf("the person's installed.txt = %q", got)
	}
}

// A SUBMITTED CANDIDATE WHOSE RESCUE BRANCH IS LOCKED STAYS IN ITS COPY.
// Removing that copy would also remove the only ref that still holds the work.
func TestASweptCopyStaysWhenItsCandidateCannotBeKept(t *testing.T) {
	repo := newTestRepo(t)
	dead := deadProgramFolder(t, repo, t.TempDir())
	writeFile(t, filepath.Join(dead.Dir, "candidate.go"), "package candidate\n")
	mustGit(t, dead.Dir, "add", "candidate.go")
	tree := strings.TrimSpace(gitOut(t, dead.Dir, "write-tree"))
	candidate := strings.TrimSpace(gitOut(t, dead.Dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", tree, "-p", "HEAD", "-m", "submitted"))
	ref := "refs/worktree/senior-dev/submitted"
	mustGit(t, dead.Dir, "update-ref", ref, candidate)
	writeFile(t, copyGitPath(t, repo, "refs/heads/"+dead.Branch+"-submitted.lock"), "other git writer\n")
	sweepProgramCopies(repo)
	if _, err := os.Stat(dead.Dir); err != nil {
		t.Fatalf("the copy holding the candidate was removed: %v", err)
	}
	ending, ok := keptProgramFolderEnd(dead.Keep)
	if !ok || !strings.Contains(ending.Sentence(), ref) ||
		!strings.Contains(ending.Sentence(), "its copy is kept at "+dead.Dir) {
		t.Fatalf("the ending = %+v, want the copy kept and the ref named", ending)
	}
}

// A RUN SENT BACK COUNTS FROM WHERE THE LINE'S FIRST RUN FOUND THE PERSON. It
// takes the branch up, the person's changes already its first commit, and
// neither those changes nor the person's newer ones are carried again.
func TestARunSentBackCountsFromThePersonsCarriedInWork(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "half.go"), "package half\n")
	program := testPrograms("fake")[0]
	first := prepareIn(t, program, repo, "Finish it")
	commitIn(t, first.Dir, "done.go")
	first.Finish("")
	writeFile(t, filepath.Join(repo, "later.go"), "package later\n")
	carry := &programCarry{Branch: first.Branch, Root: repo, Home: first.Home, Start: first.Start, Snapshot: first.Snapshot}
	second, err := PrepareProgramFolder(ProgramFolderOrder{Program: program, Dir: repo, Title: "Finish it", Holder: "task 2", Keep: t.TempDir(), Carry: carry})
	if err != nil {
		t.Fatal(err)
	}
	if second.Snapshot != first.Snapshot || len(second.LeftBehind) != 0 {
		t.Fatalf("the sent-back run = snapshot %q left %q, want the first run's %q and nothing new", second.Snapshot, second.LeftBehind, first.Snapshot)
	}
	if _, err := os.Stat(filepath.Join(second.Dir, "later.go")); !os.IsNotExist(err) {
		t.Fatal("the person's newer change was carried into a sent-back run")
	}
	if end := second.Finish(""); !end.Kept || strings.Join(end.Changed, " ") != "done.go" {
		t.Fatalf("the sent-back run counts %q, want the line's own done.go", end.Changed)
	}
}

// THE FOLDERS A PROGRAM MAY INSTALL INTO STAY OUT OF GIT IN ITS COPY. With the
// network on it may make a .venv, and a project that does not ignore one would
// have the program's whole environment in its answer and its finishing commit;
// the copy names it as a folder while it is on disk, and not with the network
// off, when nothing is installed.
func TestAnEnvironmentTheProgramMakesStaysOutOfItsCommit(t *testing.T) {
	t.Setenv(programNetworkEnv, "")
	repo := newTestRepo(t)
	exclude := copyGitPath(t, repo, "info/exclude")
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Make an environment")
	if during := readFile(t, exclude); !strings.Contains(during, "\n/.venv/\n") || !strings.Contains(during, "\n/node_modules/\n") {
		t.Fatalf("the exclude file during the run =\n%s", during)
	}
	writeFile(t, filepath.Join(folder.Dir, ".venv", "pyvenv.cfg"), "home = /usr/bin\n")
	writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
	folder.Finish("done")
	if paths := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); strings.Contains(paths, ".venv") || !strings.Contains(paths, "fix.go") {
		t.Fatalf("the branch holds %q", paths)
	}
	if after, err := os.ReadFile(exclude); err == nil && strings.Contains(string(after), "/.venv/") {
		t.Fatalf("the line outlived the copy:\n%s", after)
	}

	t.Setenv(programNetworkEnv, "off")
	offline := prepareIn(t, testPrograms("fake")[0], repo, "No network")
	defer offline.Finish("")
	if during, _ := os.ReadFile(exclude); strings.Contains(string(during), "/.venv/") {
		t.Fatalf("a run with the network off was given environment lines:\n%s", during)
	}
}
