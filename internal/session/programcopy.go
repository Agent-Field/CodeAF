package session

// A PROGRAM IN A REPOSITORY WORKS IN A COPY OF ITS OWN.
//
// THE CONTRACT, for a folder that is a git repository (programfolder.go keeps
// the rest — which folder, a plain folder, the refusals):
//
//  1. THE COPY IS A GIT WORKTREE IN A PRIVATE FOLDER OF CODEAF'S — under this
//     account's cache folder ([programCopyRoot]), never inside the person's
//     repository, and never in a shared temporary folder another account can
//     reach first or a restart empties — cut on a branch of the program's own
//     ([taskBranchName]) from the commit the person's checkout stands on. It is
//     not for the person to open: it exists so that several runs can work on
//     one repository at once, and so that the person's own checkout, index and
//     branch are never touched.
//  2. THE COPY STARTS WHERE THE PERSON IS, UNCOMMITTED CHANGES AND ALL. What
//     their checkout tracks but has not committed — edits, staged or not,
//     and staged new files — is written into one commit on top of the one their
//     checkout stands on, through an index of codeaf's own, and the program's
//     branch begins with it ([ProgramFolder.snapshotLeftBehind]). Their index,
//     their files and their branch are only read, and the changes stay
//     uncommitted in their folder as they were. A checkout in the middle of a
//     merge or a rebase is not carried — its files hold conflict markers — and
//     neither is anything when git cannot read it; the run is not refused for
//     either, and the receipt and a shell run's first lines say which it was
//     ([ProgramFolder.LeftBehindWords]). Untracked files are copied as local
//     inputs, excluded from automatic commits and submission snapshots.
//  3. A FEW FOLDERS GIT IGNORES ARE LINKED IN, NOT COPIED ([programCopyLinks]):
//     installed dependencies and environment files the project's own build
//     and tests need and a fresh checkout does not have — `node_modules`, a
//     `.venv`, a `.env`. Build output is never linked, because two runs
//     building into one folder corrupt each other, and neither is anything
//     else: a `bin/` linked in once had a run's `make build` overwrite the
//     binary its person was running. A project names its own list in
//     `.codeaf/config.json` ([config.ProjectProgramLinks]). A link git would
//     not otherwise ignore is named in the repository's exclude file only
//     while a copy of it is on disk ([excludeLinkedNames]).
//  4. WHEN THE PROGRAM EXITS — done, not finished, stopped, crashed, or its
//     process gone — what it left uncommitted on its branch is committed there,
//     the copy is removed and git's record of it pruned, so THE BRANCH IS
//     RELEASED: checked out nowhere, and free for the person, a merge, or the
//     next run of the same work. THE BRANCH IS ALWAYS KEPT, even when the run
//     changed nothing. What cannot be committed on it — the program moved the
//     copy off its branch, or left a merge half done — is kept as a patch in the
//     run's record folder instead of being lost with the copy, and commits it
//     made on no branch are put on one. NOTHING THE PROGRAM LEFT IS DELETED
//     UNSAVED: a copy whose leftovers could be neither committed nor kept is
//     left where it is, and the ending says where ([ProgramFolderEnd.CopyKept]).
//  5. A RUN WHOSE PROCESS WENT AWAY IS FINISHED THE SAME WAY by the next codeaf
//     that finds it: the conversation that reopens it, or the next program run
//     on the same repository ([sweepProgramCopies]). Unlike a folder a person
//     works in, nobody else's edits can be in a copy, so committing what is in
//     it is committing the run's own work. And the program is not still making
//     it: the hold on the copy is handed to the program's own process
//     ([ProgramFolder.Hold]), so a codeaf that died leaves the program its
//     grace to stop, and its copy is finished only once the program is gone.

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/home"
)

// programCopyRoot is the folder every program's copy is made under: this
// account's cache folder (`~/Library/Caches/codeaf/worktrees` on a Mac,
// `~/.cache/codeaf/worktrees` on Linux), which is private to it, out of the
// way, and — unlike the system's temporary folder — neither shared with other
// accounts nor emptied by a restart, so a copy a crash left behind is still
// there to finish. A state root CODEAF_HOME moved takes the copies with it, to
// `worktrees` inside it ([home.Moved]), so a disposable home leaves nothing in
// the person's cache. A variable so a test can put copies in a folder of its
// own.
var programCopyRoot = func() string {
	if !home.Moved() {
		if cache, err := os.UserCacheDir(); err == nil && filepath.IsAbs(cache) {
			return filepath.Join(cache, "codeaf", "worktrees")
		}
	}
	return home.Join("worktrees")
}

// programCopyLinked is the ignored names a copy links in from the person's
// repository when the project names none: installed dependencies and
// environment files, the things a fresh checkout lacks that a project's build
// and tests need, and nothing a build writes. `.env.*` files are taken too
// ([programCopyLinks]).
var programCopyLinked = []string{"node_modules", ".venv", "venv", ".env", ".envrc"}

// programLeftoversFile is the patch in a run's record folder that holds what a
// program left in its copy and codeaf could not commit on its branch.
const programLeftoversFile = "leftovers.patch"

// programCopyExcludeSentinel and programCopyExcludeEnd fence the lines codeaf
// adds to a repository's exclude file for the links in its copies
// ([excludeLinkedNames]), so they can be taken out again whole and nothing
// else of the file is touched.
const (
	programCopyExcludeSentinel = "# codeaf: links in a program's copy of this repository, removed when no copy is left"
	programCopyExcludeEnd      = "# codeaf: end"
	programCopyExcludeCreated  = "# codeaf: exclude file created for this block"
	programCopyExcludeNewline  = "# codeaf: separator newline added for this block"
)

// Copied says the program works in a copy of its own of a repository, rather
// than in a folder itself.
func (f *ProgramFolder) Copied() bool { return f != nil && f.Repo != "" }

// Ground is the folder the run's work is about, as a person names it: the
// person's repository for a run in a copy of it, and the folder itself for
// every other.
func (f *ProgramFolder) Ground() string {
	if f.Copied() {
		return f.Repo
	}
	return f.Dir
}

// Hold is the open file the run's hold on its folder is taken on, for the door
// that starts the program to hand to the program's process
// ([delegate.Launch.Hold]); nil when there is none.
//
// THE HOLD LIVES AS LONG AS THE LAST PROCESS THAT HAS IT. It is a flock, which
// belongs to the open file and not to the process that opened it, so with the
// program holding the same file a codeaf that dies leaves the folder held until
// the program has stopped too: the next codeaf never finishes a copy — commits
// it, removes it — while the program is still writing its last edits there.
func (f *ProgramFolder) Hold() *os.File {
	if f == nil {
		return nil
	}
	return f.lock
}

// prepareProgramCopy readies a copy of the repository at folder.Dir for a
// program's run, per the contract at the top of this file: the copy cut on
// the program's branch (or on the branch it carries on), its linked folders in
// place, the hold on it taken, and its record written before git is touched.
func prepareProgramCopy(order ProgramFolderOrder, folder *ProgramFolder) (*ProgramFolder, error) {
	repo := folder.Dir
	folder.Repo = repo
	// A FOLDER ANOTHER PROGRAM'S RUN WORKS IN ITSELF IS STILL REFUSED. A run
	// from an older build, or a plain-folder run around this repository, is
	// switching or rewriting the person's checkout, and a copy cut from it now
	// would be cut from whatever that run left there.
	if hold, busy := programHoldNear(canonicalPath(repo), ""); busy {
		return nil, fmt.Errorf("%s", programFolderBusy(repo, hold))
	}
	links, projectListed, err := programCopyLinks(repo)
	if err != nil {
		return nil, err
	}
	start, err := git(repo, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%s has no commit to cut a branch from: %s", repo, firstLine(start))
	}
	folder.Start, folder.Home = strings.TrimSpace(start), currentBranch(repo)
	folder.takeCarry(order.Carry)
	folder.snapshotLeftBehind()
	sweepProgramCopies(repo)
	dir, err := newProgramCopyDir(repo)
	if err != nil {
		return nil, fmt.Errorf("make a folder for %s's copy of %s: %w", folder.Program, repo, err)
	}
	folder.Dir, folder.key = dir, canonicalPath(dir)
	lock, hold, busy := claimProgramFolder(folder.key, folder.Program+", "+order.Holder)
	if busy {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("%s", programFolderBusy(dir, hold))
	}
	folder.lock = lock
	// EVERY INPUT IS KEPT OUT UNTIL ITS FINGERPRINT IS TAKEN, so a codeaf that
	// goes away while it is copying them leaves no half-copied input for the
	// next one to read as the run's work ([gitidentity.Inputs.LeftAlone]).
	folder.Inputs = gitidentity.Inputs{}
	for _, name := range folder.Untracked {
		folder.Inputs[name] = ""
	}
	// THE RECORD IS WRITTEN BEFORE THE COPY IS CUT, so a process that goes
	// away between the two leaves a record the next codeaf can finish from
	// rather than a worktree nothing knows about.
	folder.write()
	if refusal := folder.cutCopy(); refusal != "" {
		folder.forget()
		return nil, fmt.Errorf("%s", refusal)
	}
	if err := folder.copyUntracked(); err != nil {
		folder.forget()
		return nil, err
	}
	offline := programNetworkOff()
	folder.Linked, folder.Carried = carryIgnored(repo, dir, folder.Notes, links, offline, projectListed)
	excludeLinkedNames(dir, folder.Linked, programEnvironmentFolders(offline))
	ignored, err := git(dir, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		folder.forget()
		return nil, fmt.Errorf("read paths ignored at the start in %s: %s", dir, firstLine(ignored))
	}
	listed := strings.Split(strings.TrimSuffix(ignored, "\x00"), "\x00")
	folder.IgnoredAtStart = append(listed, folder.Linked...)
	if err := folder.writeIgnoredAtStart([]byte(strings.Join(folder.IgnoredAtStart, "\x00"))); err != nil {
		folder.forget()
		return nil, err
	}
	if file := folder.InputsFile(); file != "" {
		if err := gitidentity.WriteInputs(file, folder.Inputs); err != nil {
			folder.forget()
			return nil, err
		}
	}
	folder.write()
	return folder, nil
}

// takeCarry names the branch the run works on: the one an earlier run of its
// line left, when it carries that on in this repository ([programCarry]), and
// otherwise a new one of its own, cut from the person's checkout or — after a
// run whose work passed — from that run's branch.
func (f *ProgramFolder) takeCarry(carry *programCarry) {
	f.Branch = taskBranchName(f.Title)
	if carry == nil || canonicalPath(carry.Root) != canonicalPath(f.Repo) {
		return
	}
	tip := branchCommit(f.Repo, carry.Branch)
	switch {
	case tip == "":
	case carry.Fresh:
		f.Untracked = slices.Clone(carry.Untracked)
		// THIS RUN'S START IS THE PASSED WORK'S TIP, so the files its ending
		// counts are its own; its branch holds the passed work too, and says so.
		f.Home, f.Start, f.From = carry.Home, tip, carry.Branch
	default:
		f.Untracked = slices.Clone(carry.Untracked)
		// THE LINE'S START, NOT THIS RUN'S. "Changed nothing" and the files the
		// ending counts are measured from where the first run of the line began,
		// so a sent-back run can never read the earlier runs' work as none.
		f.Branch, f.Home, f.Start, f.Snapshot, f.Continues = carry.Branch, carry.Home, carry.Start, carry.Snapshot, true
	}
}

// base is the commit the program's own work is counted from: the one carrying
// the person's uncommitted changes when its branch begins with one, and the
// one it was cut from otherwise. What the person had not committed is never
// counted as the program's work, and a run that added nothing to it changed
// nothing.
func (f *ProgramFolder) base() string {
	if f.Snapshot != "" {
		return f.Snapshot
	}
	return f.Start
}

// snapshotLeftBehind carries what the person's checkout has not committed
// into the run's copy, per the second point of the contract at the top of this
// file: the paths read, and — unless the checkout is in the middle of a merge
// or a rebase — the commit that holds them made, for the branch to begin with.
// A run that carries on an earlier run's branch carries nothing new: its
// branch already begins where the line's first run found the person.
func (f *ProgramFolder) snapshotLeftBehind() {
	if f.Continues || f.From != "" {
		return
	}
	f.LeftBehind = uncommittedPaths(f.Repo, f.Notes)
	if len(f.LeftBehind) == 0 {
		return
	}
	if half := halfDone(f.Repo); half != "" {
		f.LeftBehindWhy = "your checkout is in the middle of a " + half
		return
	}
	untracked, err := git(f.Repo, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		f.LeftBehindWhy = "they could not be read (" + firstLine(untracked) + ")"
		return
	}
	for _, name := range strings.Split(untracked, "\x00") {
		if name != "" && slices.Contains(f.LeftBehind, name) {
			f.Untracked = append(f.Untracked, name)
		}
	}
	f.LeftBehind = withoutUntracked(f.LeftBehind, f.Untracked)
	snapshot, err := snapshotCheckout(f.Repo, f.Start, f.Notes, f.Program)
	if err != nil {
		f.LeftBehindWhy = "they could not be read (" + err.Error() + ")"
		return
	}
	f.Snapshot = snapshot
}

// copyUntracked carries the person's untracked files into the copy as its
// inputs — as files, never as git objects — and takes each one's fingerprint
// as it lands ([ProgramFolder.Inputs]). A retry copies only its original input
// list, and never overwrites a path the earlier run already put on its branch:
// that one is the earlier run's work now, not an input.
func (f *ProgramFolder) copyUntracked() error {
	var copied []string
	inputs := gitidentity.Inputs{}
	for _, name := range f.Untracked {
		source, target := filepath.Join(f.Repo, name), filepath.Join(f.Dir, name)
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		parent := canonicalPath(filepath.Dir(target))
		if parent != canonicalPath(f.Dir) && !strings.HasPrefix(parent, canonicalPath(f.Dir)+string(filepath.Separator)) {
			return fmt.Errorf("copy untracked %s: its parent is outside the run's copy", name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyPath(source, target); err != nil {
			return fmt.Errorf("copy untracked %s: %w", name, err)
		}
		copied = append(copied, name)
		inputs[name], _ = gitidentity.Fingerprint(target)
	}
	f.Untracked, f.Inputs = copied, inputs
	return nil
}

// withoutUntracked separates the tracked snapshot's receipt from the local
// inputs that are copied without entering history.
func withoutUntracked(paths, untracked []string) []string {
	var tracked []string
	for _, name := range paths {
		if !slices.Contains(untracked, name) {
			tracked = append(tracked, name)
		}
	}
	return tracked
}

// snapshotCheckout writes what the checkout at repo has not committed into one
// commit whose parent is start, and answers it; "" when there is nothing, and
// git's line when git would not.
//
// NOTHING OF THE PERSON'S IS WRITTEN. The staging happens in an index of
// codeaf's own (`GIT_INDEX_FILE`), read from start's tree, beside the real one
// and removed after; the tree is written from it and the commit with
// `commit-tree`, which moves no ref. Their index, their files, HEAD and every
// branch are as they were: what is new is objects, and the program's branch
// once the copy is cut from the commit. It is [sealGroundWork]'s way, which a
// task grounded on a checkout with work in it has used since #578; the
// program's notes are what it leaves out instead of a task's droppings.
//
// ONLY WHAT GIT STATUS NAMES IS STAGED: tracked edits, staged or not,
// deletions, and explicitly staged new files. Untracked files are copied in
// as the run's inputs without entering this commit ([ProgramFolder.copyUntracked]). A path hidden by the
// person's index must stay at its committed bytes in the copy. What git
// ignores is not in its world, and the copy carries the few such folders a
// build needs instead ([programCopyLinks]).
func snapshotCheckout(repo, start, notes, program string) (string, error) {
	paths, err := uncommittedSnapshotList(repo, notes)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	gitDir, err := git(repo, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", errors.New(firstLine(gitDir))
	}
	index := filepath.Join(strings.TrimSpace(gitDir), "codeaf-program-index-"+shortID())
	defer func() { _ = os.Remove(index) }()
	pathspec := index + ".paths"
	defer func() { _ = os.Remove(pathspec) }()
	withIndex := func(args ...string) (string, error) {
		out, err := gitWith(repo, []string{"GIT_INDEX_FILE=" + index, "GIT_LITERAL_PATHSPECS=1"}, args...)
		if err != nil {
			return "", errors.New(firstLine(out))
		}
		return strings.TrimSpace(out), nil
	}
	if _, err := withIndex("read-tree", start); err != nil {
		return "", err
	}
	if paths, err = snapshotPathsWithSomething(repo, index, paths); err != nil || len(paths) == 0 {
		return "", err
	}
	if err := os.WriteFile(pathspec, []byte(strings.Join(paths, "\x00")+"\x00"), 0o600); err != nil {
		return "", err
	}
	if _, err := withIndex("add", "-A", "--pathspec-from-file="+pathspec, "--pathspec-file-nul"); err != nil {
		return "", err
	}
	if notes = strings.Trim(notes, "/"); notes != "" {
		if _, err := withIndex("rm", "-r", "-q", "--cached", "--ignore-unmatch", "--", notes); err != nil {
			return "", err
		}
	}
	tree, err := withIndex("write-tree")
	if err != nil {
		return "", err
	}
	if was, err := git(repo, "rev-parse", start+"^{tree}"); err == nil && strings.TrimSpace(was) == tree {
		return "", nil
	}
	message := "Your uncommitted changes when " + program + "'s run began\n\n" +
		"codeaf carried them into " + program + "'s copy of this repository, as they were, so its work starts where yours stood; they are still uncommitted in your own folder."
	commit, err := git(repo, append(append([]string{"-c", "commit.gpgsign=false"}, codeafGitIdentity()...),
		"commit-tree", tree, "-p", start, "-m", message)...)
	if err != nil {
		return "", errors.New(firstLine(commit))
	}
	return strings.TrimSpace(commit), nil
}

// snapshotPathsWithSomething keeps the paths git status named that are on disk
// or in the private index read from the starting tree, in their order.
//
// A PATH ON NEITHER SIDE HAS NOTHING TO CARRY, AND GIT REFUSES IT. A file added
// to the index and deleted since ("AD"), or the new name of a staged rename
// deleted since ("RD"), is named by git status but matches nothing, and `git
// add` fails the whole list on one such pathspec — which left every other
// uncommitted change out of the copy. Only the paths missing from disk are
// asked about, a bounded number at a time so no argument list outgrows the
// system's.
func snapshotPathsWithSomething(repo, index string, paths []string) ([]string, error) {
	const perAsk = 512
	var missing []string
	for _, path := range paths {
		if _, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(path))); err != nil {
			missing = append(missing, path)
		}
	}
	if len(missing) == 0 {
		return paths, nil
	}
	indexed := map[string]bool{}
	for len(missing) > 0 {
		ask := missing[:min(perAsk, len(missing))]
		missing = missing[len(ask):]
		out, err := gitWith(repo, []string{"GIT_INDEX_FILE=" + index, "GIT_LITERAL_PATHSPECS=1"},
			append([]string{"ls-files", "-z", "--"}, ask...)...)
		if err != nil {
			return nil, errors.New(firstLine(out))
		}
		for _, path := range strings.Split(out, "\x00") {
			indexed[path] = true
		}
	}
	kept := paths[:0:0]
	for _, path := range paths {
		if _, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(path))); err == nil || indexed[path] {
			kept = append(kept, path)
		}
	}
	return kept, nil
}

// cutCopy adds the copy as a worktree of the person's repository, on the
// program's branch, and answers git's line when it would not go.
//
// A BRANCH CHECKED OUT SOMEWHERE ELSE CANNOT BE A SECOND WORKTREE'S. A carried
// branch the person has since checked out in their own folder, to look at it,
// is carried on on a new branch cut from its tip instead, so the run still
// starts from the work it was sent back to.
func (f *ProgramFolder) cutCopy() string {
	if strings.TrimSpace(f.place.Dir) != "" {
		defer lockGitRoot(f.place, f.Repo)()
	}
	head := []string{"-c", "core.hooksPath=" + os.DevNull, "worktree", "add", "-q"}
	if f.Continues {
		out, err := git(f.Repo, append(head, f.Dir, f.Branch)...)
		if err == nil {
			return ""
		}
		taken := f.Branch
		f.Branch = taskBranchName(f.Title)
		f.write()
		if out2, err := git(f.Repo, append(head, "-b", f.Branch, f.Dir, taken)...); err != nil {
			return fmt.Sprintf("could not cut %s's copy of %s: %s (and carrying on on %s: %s)", f.Program, f.Repo, copyCutWords(out2), taken, firstLine(out))
		}
		return ""
	}
	if out, err := git(f.Repo, append(head, "-b", f.Branch, f.Dir, f.base())...); err != nil {
		// A BRANCH MADE BY A CUT THAT FAILED IS DELETED, so nothing is left in
		// the person's repository that nothing knows about.
		if tip := branchCommit(f.Repo, f.Branch); tip != "" && tip == f.base() {
			_, _ = git(f.Repo, "branch", "-q", "-D", f.Branch)
		}
		return fmt.Sprintf("could not cut %s's copy of %s: %s", f.Program, f.Repo, copyCutWords(out))
	}
	return ""
}

// copyCutWords is git's line for a cut that failed, with the cause named when it
// is one a person cannot read off that line.
//
// A REPOSITORY THAT KEEPS FILES IN GIT LFS NEEDS git-lfs TO CHECK ONE OUT. The
// person's own checkout was filled when they cloned it, so a run that worked in
// it never needed the filter; a copy is a fresh checkout, and a PATH without
// git-lfs fails it with "command not found". Emptying the filter for the cut is
// no cure: git then fails every `git status` in the copy instead, the program's
// own among them.
func copyCutWords(out string) string {
	said := firstLine(out)
	if strings.Contains(out, "git-lfs") && strings.Contains(out, "not found") {
		said += "; this repository keeps files in Git LFS, and git-lfs is not on the PATH codeaf runs with: install it, or start codeaf where it is on the PATH, then ask again"
	}
	return said
}

// forget undoes a copy's preparation that failed part way: the copy and git's
// record of it gone, a branch it cut that holds nothing deleted, the record
// and the hold let go.
func (f *ProgramFolder) forget() {
	f.unlinkCopy()
	f.removeCopy()
	if !f.Continues {
		if tip := branchCommit(f.Repo, f.Branch); tip != "" && tip == f.base() {
			_, _ = git(f.Repo, "branch", "-q", "-D", f.Branch)
		}
	}
	f.releaseCopy()
}

// newProgramCopyDir makes the empty folder one copy is cut into, named for the
// repository so a person who does come across it can tell whose it is.
func newProgramCopyDir(repo string) (string, error) {
	root := programCopyRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if err := privateCopyRoot(root); err != nil {
		return "", err
	}
	name := programCopyName.ReplaceAllString(filepath.Base(repo), "-")
	dir, err := os.MkdirTemp(root, strings.Trim(name, "-")+"-")
	if err != nil {
		return "", err
	}
	return canonicalPath(dir), nil
}

var programCopyName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// privateCopyRoot refuses a folder for copies that is not this account's own:
// a link, a file, or a folder another account made. One that is this
// account's but open to others is closed to them.
//
// A COPY HOLDS THE PERSON'S CODE AND LINKS TO THEIR `.env`. A folder somebody
// else made first, at the name codeaf would use, would have every run's copy
// cut where that somebody can read it — or, as a link, somewhere else entirely.
func privateCopyRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s is a link or a file, not a folder of codeaf's own, so no copy is made there", root)
	}
	if !ownedByThisAccount(info) {
		return fmt.Errorf("%s belongs to another account, so no copy is made there", root)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return os.Chmod(root, 0o700)
	}
	return nil
}

// programCopyLinks is the ignored names a copy of repo links in: the
// project's own list when its `.codeaf/config.json` names one — an empty one
// links nothing — and [programCopyLinked] with every `.env.*` file otherwise.
//
// A LIST THAT DOES NOT APPLY IS SAID, NOT SKIPPED (projectconfig.go's third
// law): a project file that cannot be read, a list in a shape codeaf does not
// take, or a name that is not at the top of the repository refuses the run
// with the file named, rather than quietly linking the defaults instead.
func programCopyLinks(repo string) ([]string, bool, error) {
	project, err := config.LoadProjectConfig(repo)
	if err != nil {
		return nil, false, fmt.Errorf("%s; codeaf reads which ignored folders to link into the program's copy there, so fix it, then ask again", err)
	}
	listed, found, err := project.Names(config.ProjectProgramLinks)
	if err != nil {
		return nil, false, fmt.Errorf("%s; fix it, then ask again", err)
	}
	if found {
		names := make([]string, 0, len(listed))
		for _, name := range listed {
			name = strings.Trim(name, "/")
			if name == "" || strings.Contains(name, "/") || name == "." || name == ".." || name == ".git" {
				return nil, false, fmt.Errorf("%s: %s names %q, which is not a folder or file at the top of the repository, and a copy links only those; fix it, then ask again",
					project.Path(), config.ProjectProgramLinks, name)
			}
			names = append(names, name)
		}
		return names, true, nil
	}
	names := append([]string(nil), programCopyLinked...)
	if matches, err := filepath.Glob(filepath.Join(repo, ".env.*")); err == nil {
		for _, match := range matches {
			names = append(names, filepath.Base(match))
		}
	}
	return names, false, nil
}

// programNetworkOff says a program runs with its network off: SENIOR_DEV_NET
// set to anything but "allow", read the way senior-dev reads it
// (internal/seniordev/netpolicy), which fails closed on a word it does not
// know. codeaf leaves the variable to the environment it was started in and
// sets it on no run, so this is on unless somebody turned it off.
func programNetworkOff() bool {
	switch strings.ToLower(strings.TrimSpace(env.Value(programNetworkEnv))) {
	case "", "allow":
		return false
	}
	return true
}

// programNetworkEnv is senior-dev's network switch (netpolicy.EnvMode), which
// its process inherits from codeaf's.
const programNetworkEnv = "SENIOR_DEV_NET"

// programEnvironmentFolders is the folders a program with its network on may
// install a project's dependencies into in its copy ([soloDependenciesSection]
// in internal/seniordev), which must stay out of git there whatever the
// project's .gitignore says ([excludeLinkedNames]); none with the network off,
// when nothing can be installed.
func programEnvironmentFolders(offline bool) []string {
	if offline {
		return nil
	}
	return []string{".venv", "venv", "node_modules"}
}

// carryIgnored carries into dir each of names that is in repo, at its top
// level, and that git ignores there, and answers the ones it linked and the
// ones it put there of the copy's own. A name git does not ignore is the
// repository's own file and is in the copy already; one the program keeps its
// notes under is never carried, because its notes are its own run's.
//
// A LINK SHARES THE PERSON'S FOLDER, WRITES AND ALL. An `npm install` through a
// linked node_modules, or a `pip install` into a linked .venv, changes the
// environment the person works in, and an editable install in a linked .venv
// points the copy's tests at the person's own source rather than the copy's.
// So with the network on — the program can install what it lacks — nothing
// that can be had another way is linked ([carryOne]); with it off nothing can
// be installed, and linking is the only way the copy has what the project's
// build and tests need.
func carryIgnored(repo, dir, notes string, names []string, offline, projectListed bool) (linked, carried []string) {
	for _, name := range names {
		if name == "" || strings.Contains(name, "/") || name == ".git" || name == "." || name == ".." ||
			(notes != "" && name == strings.Trim(notes, "/")) {
			continue
		}
		source := filepath.Join(repo, name)
		if _, err := os.Lstat(source); err != nil {
			continue
		}
		if _, err := git(repo, "check-ignore", "-q", "--", name); err != nil {
			continue
		}
		target := filepath.Join(dir, name)
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		switch carryOne(source, target, name, offline, projectListed) {
		case carryLinked:
			linked = append(linked, name)
		case carryOwn:
			carried = append(carried, name)
		}
	}
	return linked, carried
}

// carryWay is how one name reached a copy.
type carryWay int

const (
	carryNone carryWay = iota
	carryLinked
	carryOwn
)

// carryOne puts one ignored name into a copy, and answers how:
//
//   - with the network off, every name is linked;
//   - a file (an `.env`) is copied: it is small, and a link would let the
//     program's edits land in the person's;
//   - a Python virtual environment is left out: an editable install in it
//     points at the person's source, so the copy's tests would import the
//     person's code, and the program builds one of its own;
//   - a folder that is itself a link is left out for dependencies, or linked
//     only when the project explicitly listed its name;
//   - any other folder is cloned copy-on-write where the disk can (APFS, a
//     reflinking Linux filesystem), which costs no space and no time;
//   - where it cannot, `node_modules` is left out for the program to install,
//     and a folder the project listed itself is linked, as the project asked.
func carryOne(source, target, name string, offline, projectListed bool) carryWay {
	if offline {
		return linkInto(source, target)
	}
	link, err := os.Lstat(source)
	if err != nil {
		return carryNone
	}
	info, err := os.Stat(source)
	if link.Mode()&os.ModeSymlink != 0 {
		if err != nil {
			return carryNone
		}
		if info.IsDir() {
			if name == "node_modules" || isVirtualEnv(source) || !projectListed {
				return carryNone
			}
			return linkInto(source, target)
		}
	}
	switch {
	case err != nil:
		return carryNone
	case info.Mode().IsRegular():
		if copyFileInto(source, target, info.Mode().Perm()) == nil {
			return carryOwn
		}
		return carryNone
	case !info.IsDir():
		return carryNone
	case isVirtualEnv(source):
		return carryNone
	case cloneTree(source, target) == nil:
		return carryOwn
	case name == "node_modules":
		return carryNone
	}
	return linkInto(source, target)
}

// isVirtualEnv says dir is a Python virtual environment: it holds pyvenv.cfg.
func isVirtualEnv(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "pyvenv.cfg"))
	return err == nil
}

// linkInto links target to source, answering carryLinked when it went.
func linkInto(source, target string) carryWay {
	if os.Symlink(source, target) == nil {
		return carryLinked
	}
	return carryNone
}

// copyFileInto copies one file, its mode kept.
func copyFileInto(source, target string, mode os.FileMode) error {
	body, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, body, mode)
}

// excludeLinkedNames names, in the exclude file git reads for the copy at dir,
// each of the links in it that git would not otherwise ignore, anchored at the
// top and fenced by codeaf's own two lines.
//
// A LINK IS NOT A DIRECTORY TO GIT. `node_modules/` in a .gitignore matches a
// directory and not a link named node_modules, so without this the link is an
// untracked file in the copy: the program is told its tree is not clean, and a
// commit of it would put a link to the person's disk in their history. The
// file is the repository's shared one — git reads no other for a worktree —
// so ONLY WHAT IS NEEDED GOES IN, AND ONLY FOR AS LONG AS A COPY IS ON DISK: a
// name git already ignores as a link (`.env`, `node_modules` without the
// slash) is left out, and the lines come back out when the last copy of the
// repository is removed ([dropProgramExclude]). A team that stops ignoring
// `.envrc` to commit it is then never told by a line codeaf left behind that
// it is ignored.
//
// The file is rewritten whole, under codeaf's own lock on it, and put in place
// by a rename, so git never reads half of it and two copies starting at once
// never write over each other's lines.
//
// AND THE FOLDERS A PROGRAM MAY INSTALL INTO ARE KEPT OUT OF GIT THE SAME WAY
// (folders, [programEnvironmentFolders]): a project that does not ignore .venv
// would otherwise have the program's new environment in its answer and its
// finishing commit. They are named as folders (`/.venv/`), and only when git
// in the copy would not already ignore a folder of that name.
func excludeLinkedNames(dir string, names, folders []string) {
	if len(names) == 0 && len(folders) == 0 {
		return
	}
	exclude := gitExcludeFile(dir)
	if exclude == "" {
		return
	}
	// THE LOOK IS TAKEN UNDER THE LOCK: a copy of the same repository removed
	// at this moment takes the lines out under it too, and a look taken before
	// it would read them as already there.
	defer lockProgramExclude(exclude)()
	var need []string
	for _, name := range names {
		if _, err := git(dir, "check-ignore", "-q", "--", name); err != nil {
			need = append(need, "/"+name)
		}
	}
	for _, name := range folders {
		if _, err := git(dir, "check-ignore", "-q", "--", name+"/"); err != nil {
			need = append(need, "/"+name+"/")
		}
	}
	if len(need) == 0 {
		return
	}
	current, err := os.ReadFile(exclude)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	outside, ours, created := splitProgramExclude(string(current))
	created = created || errors.Is(err, os.ErrNotExist)
	for _, line := range need {
		if !slices.Contains(ours, line) {
			ours = append(ours, line)
		}
	}
	writeProgramExclude(exclude, outside, ours, created)
}

// dropProgramExclude takes codeaf's lines back out of repo's exclude file when
// no copy of the repository is left on disk ([excludeLinkedNames]).
func dropProgramExclude(repo string) {
	exclude := gitExcludeFile(repo)
	if exclude == "" {
		return
	}
	if current, err := os.ReadFile(exclude); err != nil || !strings.Contains(string(current), programCopyExcludeSentinel) {
		return
	}
	defer lockProgramExclude(exclude)()
	if programCopiesOnDisk(repo) {
		return
	}
	current, err := os.ReadFile(exclude)
	if err != nil {
		return
	}
	if outside, ours, created := splitProgramExclude(string(current)); len(ours) > 0 || outside != string(current) {
		writeProgramExclude(exclude, outside, nil, created)
	}
}

// gitExcludeFile is the exclude file git reads for the checkout at dir: the
// repository's shared one, for a worktree too. "" when git cannot say.
func gitExcludeFile(dir string) string {
	out, err := git(dir, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return ""
	}
	exclude := strings.TrimSpace(out)
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(dir, exclude)
	}
	return exclude
}

// lockProgramExclude takes codeaf's lock on one repository's exclude file, and
// answers the release. It is a file of codeaf's own beside the holds, never a
// lock of git's, so the person's git is never kept waiting on it; a
// filesystem that takes no locks goes unlocked.
func lockProgramExclude(exclude string) func() {
	directory := home.Join("v3", programFolderDir)
	if os.MkdirAll(directory, 0o700) != nil {
		return func() {}
	}
	// THE NAME DOES NOT END IN .lock, which is how the holds are listed
	// ([programHoldNear]); this is no program's hold on a folder.
	file, err := os.OpenFile(filepath.Join(directory, programFolderName(exclude)+".exclude"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	if filelock.Lock(file, true, false) != nil {
		_ = file.Close()
		return func() {}
	}
	return func() {
		_ = filelock.Unlock(file)
		_ = file.Close()
	}
}

// splitProgramExclude parts an exclude file into everything that is not
// codeaf's, as it was written, and the lines between codeaf's two fences. The
// block also records whether the file and its separator belong to codeaf, so
// cleanup after a restart can restore an absent file or an unterminated line.
func splitProgramExclude(text string) (string, []string, bool) {
	start := strings.Index(text, programCopyExcludeSentinel+"\n")
	if start < 0 || (start > 0 && text[start-1] != '\n') {
		return text, nil, false
	}
	content := text[start+len(programCopyExcludeSentinel)+1:]
	end := strings.Index(content, programCopyExcludeEnd+"\n")
	if end < 0 || (end > 0 && content[end-1] != '\n') {
		return text, nil, false
	}
	before, after := text[:start], content[end+len(programCopyExcludeEnd)+1:]
	var ours []string
	created, separator := false, false
	for _, line := range strings.Split(content[:end], "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == programCopyExcludeCreated:
			created = true
		case trimmed == programCopyExcludeNewline:
			separator = true
		case trimmed != "":
			ours = append(ours, trimmed)
		}
	}
	// A NEW RULE AFTER THE BLOCK STILL NEEDS A LINE OF ITS OWN. The separator
	// becomes part of that edit; removing it would weld two ignore patterns.
	if separator && after == "" {
		before = strings.TrimSuffix(before, "\n")
	}
	return before + after, ours, created
}

// writeProgramExclude writes an exclude file as outside with codeaf's lines
// fenced at its end, none when ours is empty, through a temporary file and a
// rename. A file codeaf created is removed only when nobody added rules to it.
func writeProgramExclude(exclude, outside string, ours []string, created bool) {
	if len(ours) == 0 && outside == "" && created {
		_ = os.Remove(exclude)
		return
	}
	text := outside
	if len(ours) > 0 {
		metadata := ""
		if created {
			metadata += programCopyExcludeCreated + "\n"
		}
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
			metadata += programCopyExcludeNewline + "\n"
		}
		text += programCopyExcludeSentinel + "\n" + metadata + strings.Join(ours, "\n") + "\n" + programCopyExcludeEnd + "\n"
	}
	if os.MkdirAll(filepath.Dir(exclude), 0o755) != nil {
		return
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(exclude); err == nil {
		mode = info.Mode().Perm()
	}
	temporary, err := os.CreateTemp(filepath.Dir(exclude), ".exclude-codeaf-*")
	if err != nil {
		return
	}
	_, werr := temporary.WriteString(text)
	cerr := temporary.Close()
	if werr != nil || cerr != nil || os.Chmod(temporary.Name(), mode) != nil || os.Rename(temporary.Name(), exclude) != nil {
		_ = os.Remove(temporary.Name())
	}
}

// programCopiesOnDisk says a worktree of repo is still on disk under
// [programCopyRoot]: a copy of it some run is working in, or one left for a
// sweep.
func programCopiesOnDisk(repo string) bool {
	out, err := git(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return true
	}
	root := canonicalPath(programCopyRoot())
	for _, line := range strings.Split(out, "\n") {
		dir, ok := strings.CutPrefix(line, "worktree ")
		if !ok {
			continue
		}
		if dir = canonicalPath(dir); strictlyInside(dir, root) {
			if _, err := os.Stat(dir); err == nil {
				return true
			}
		}
	}
	return false
}

// unlinkCopy takes the linked folders back out of a copy before anything is
// committed or removed there, so nothing that follows can reach through a link
// into the person's own folder.
func (f *ProgramFolder) unlinkCopy() {
	for _, name := range f.Linked {
		target := filepath.Join(f.Dir, name)
		if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(target)
		}
	}
}

// programKeptRef is one of a copy's own refs that codeaf put a branch on,
// because the copy that held it was about to go ([ProgramFolder.keepOwnRefs]).
type programKeptRef struct {
	Ref    string
	Branch string
}

// settleCopy ends a program's run in its copy, per the fourth and fifth points
// of the contract at the top of this file: its notes kept, what it left
// committed on its branch (or kept as a patch when that cannot be), and the
// copy removed so the branch is released — or, when what it left could not be
// kept anywhere else, the copy left where it is. gone says the run's process
// went away before it could end the run itself.
func (f *ProgramFolder) settleCopy(result string, gone bool) ProgramFolderEnd {
	end := ProgramFolderEnd{Folder: *f, Gone: gone}
	end.Notes = f.keepNotes()
	f.unlinkCopy()
	before := branchCommit(f.Repo, f.Branch)
	stays := ""
	if _, err := os.Stat(f.Dir); err == nil {
		stays = f.settleCopyWork(&end, result, gone)
	}
	if tip := branchCommit(f.Repo, f.Branch); tip != "" {
		end.Changed = changedBetween(f.Repo, f.base(), tip)
		end.Kept = tip != f.base()
		end.Committed = tip != before
	}
	if stays != "" {
		end.CopyKept, end.CopyLeft = stays, f.Dir
		return end
	}
	end.CopyLeft = f.removeCopy()
	return end
}

// settleCopyWork puts what the program left in its copy somewhere that
// outlives the copy — its branch, a patch, a branch on the commits it made on
// none, a branch on a record of its own the branch does not hold — and answers
// why the copy has to stay instead, "" when it may go.
func (f *ProgramFolder) settleCopyWork(end *ProgramFolderEnd, result string, gone bool) string {
	if !f.copyReadable() {
		return "git can no longer read it as a checkout, so what " + f.Program + " left there could be neither committed nor kept"
	}
	f.clearStaleLocks()
	var stays string
	if head := currentBranch(f.Dir); head != f.Branch {
		stays = f.settleMovedCopy(end, head)
	} else if refused := f.commitLeftovers(result); refused != "" {
		end.Refused = refused
		stays = f.keepWhatIsLeft(end)
	}
	if gone {
		kept, failed := f.keepOwnRefs()
		end.Frozen = kept
		if stays == "" && len(failed) > 0 {
			stays = failed[0]
		}
	}
	return stays
}

// settleMovedCopy settles a copy whose HEAD the program moved off its branch.
//
// A HEAD THE PROGRAM MOVED IS NOT COMMITTED ON. Its branch holds what it
// committed there; whatever is loose in the copy is kept as a patch rather
// than committed onto a branch nobody chose, and commits it made on no branch
// are put on one ([ProgramFolder.keepDetached]).
func (f *ProgramFolder) settleMovedCopy(end *ProgramFolderEnd, head string) string {
	end.Moved, end.HeadOn = true, head
	if head == "" {
		end.At = shortCommit(f.Dir, "HEAD")
		saved, ok := f.keepDetached()
		end.Saved = saved
		if !ok {
			return "the commits " + f.Program + " made there on no branch could not be kept on one"
		}
	}
	return f.keepWhatIsLeft(end)
}

// keepWhatIsLeft keeps what is still uncommitted in the copy as a patch
// ([ProgramFolder.keepLeftovers]), and answers why the copy has to stay when
// git could not say what that is or the patch could not be written.
func (f *ProgramFolder) keepWhatIsLeft(end *ProgramFolderEnd) string {
	left, err := uncommittedList(f.Dir, f.Notes)
	if err != nil {
		return "git could not say what " + f.Program + " left there (" + err.Error() + ")"
	}
	end.Uncommitted = len(left)
	if len(left) == 0 {
		return ""
	}
	patch, refused := f.keepLeftovers()
	end.Patch = patch
	if refused == "" {
		return ""
	}
	if end.Refused == "" {
		end.Refused = refused
	}
	return "what " + f.Program + " left there could be neither committed nor kept as a patch"
}

// copyReadable says git can still read the copy as the checkout codeaf cut:
// its `.git` file names a worktree record that is there, and HEAD resolves.
//
// THE COPY'S OWN `.git` FILE IS READ, NOT ASKED OF GIT. git asked where the
// checkout is climbs out of a folder whose `.git` is gone, and answers
// whatever repository is above it — the person's home, when that is one — so
// only the one asker is allowed that question ([repositoryRoot]).
func (f *ProgramFolder) copyReadable() bool {
	body, err := os.ReadFile(filepath.Join(f.Dir, ".git"))
	if err != nil {
		return false
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(body)), "gitdir: ")
	if !ok {
		return false
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(f.Dir, gitdir)
	}
	if _, err := os.Stat(filepath.Join(gitdir, "HEAD")); err != nil {
		return false
	}
	_, err = git(f.Dir, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// clearStaleLocks removes the lock files a git process killed in the copy
// leaves behind — the copy's own index and HEAD, and its branch — which would
// refuse every commit after them.
//
// THEY ARE STALE BY THE TIME THIS RUNS: codeaf holds the copy, and the hold is
// the program's too while it lives ([ProgramFolder.Hold]), so nothing that
// could still be writing them is left. A program killed in the middle of one
// of its own commits once had its last edits deleted with its copy because of
// the `index.lock` it left.
func (f *ProgramFolder) clearStaleLocks() {
	for _, name := range []string{"index.lock", "HEAD.lock", "refs/heads/" + f.Branch + ".lock"} {
		out, err := git(f.Dir, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		stale := strings.TrimSpace(out)
		if !filepath.IsAbs(stale) {
			stale = filepath.Join(f.Dir, stale)
		}
		_ = os.Remove(stale)
	}
}

// keepDetached puts a branch on a detached HEAD the program committed on in
// its copy, when no branch holds that commit, and answers the branch; false
// when one was needed and could not be made.
//
// A COMMIT NO BRANCH HOLDS GOES WITH THE COPY. In the person's own checkout a
// detached HEAD kept it reachable; a copy is removed when the run ends, and
// what was only its HEAD would be left for git's garbage collection. The name
// is the first free one ([freeBranchName]), because a line of runs carried on
// on one branch can each leave commits on none.
func (f *ProgramFolder) keepDetached() (string, bool) {
	// `git branch --contains` counts the detached HEAD itself as a holder, so
	// the branches are read as refs, which it is not.
	holders, err := git(f.Dir, "for-each-ref", "--contains", "HEAD", "--format=%(refname)", "refs/heads/")
	if err != nil {
		return "", false
	}
	if strings.TrimSpace(holders) != "" {
		return "", true
	}
	name := freeBranchName(f.Repo, f.Branch+"-detached")
	if _, err := git(f.Dir, "branch", "-q", name, "HEAD"); err != nil {
		return "", false
	}
	return name, true
}

// keepOwnRefs puts a branch on each of the copy's own refs (`refs/worktree/`)
// that holds something its branch does not, and answers what it kept and
// what could not be saved before the copy goes away.
//
// A COPY'S OWN REFS GO WITH IT. senior-dev keeps the candidate it submitted at
// `refs/worktree/senior-dev/submitted`, so a run killed between submitting and
// finishing still has it to restore; per worktree, so two runs never write
// over each other's, and so removed with the copy. A run whose end nobody saw
// is the one that can have stopped in that gap, and its candidate is kept on a
// branch of its own (`<branch>-submitted`) rather than lost with the copy.
func (f *ProgramFolder) keepOwnRefs() ([]programKeptRef, []string) {
	out, err := git(f.Dir, "for-each-ref", "--format=%(refname)", "refs/worktree/")
	if err != nil {
		return nil, []string{"the copy's saved refs could not be read (" + firstLine(out) + ")"}
	}
	tip := branchCommit(f.Repo, f.Branch)
	var kept []programKeptRef
	var failed []string
	for _, ref := range nonEmptyLines(out) {
		if f.branchHolds(ref, tip) {
			continue
		}
		name := freeBranchName(f.Repo, f.Branch+"-"+path.Base(ref))
		if out, err := git(f.Dir, "branch", "-q", name, ref); err == nil {
			kept = append(kept, programKeptRef{Ref: ref, Branch: name})
		} else {
			failed = append(failed, fmt.Sprintf("what %s had saved at %s could not be kept on a branch (%s)", f.Program, ref, firstLine(out)))
		}
	}
	return kept, failed
}

// branchHolds says the branch whose tip is tip already holds what ref names:
// the commit itself, or a tree matching any commit from the run's base through
// the tip, because a saved ref with that tree adds no work to the branch.
func (f *ProgramFolder) branchHolds(ref, tip string) bool {
	if tip == "" {
		return false
	}
	if _, err := git(f.Dir, "merge-base", "--is-ancestor", ref, tip); err == nil {
		return true
	}
	theirs, err := git(f.Dir, "rev-parse", ref+"^{tree}")
	if err != nil {
		return false
	}
	ours, err := git(f.Dir, "rev-parse", tip+"^{tree}")
	theirs = strings.TrimSpace(theirs)
	if err == nil && theirs == strings.TrimSpace(ours) {
		return true
	}
	base := f.base()
	if base == "" {
		return false
	}
	baseTree, err := git(f.Dir, "rev-parse", base+"^{tree}")
	if err == nil && theirs == strings.TrimSpace(baseTree) {
		return true
	}
	trees, err := git(f.Dir, "log", "--format=%T", base+".."+tip)
	if err != nil {
		return false
	}
	for _, tree := range nonEmptyLines(trees) {
		if theirs == tree {
			return true
		}
	}
	return false
}

// freeBranchName is base, or base with the first number after it that no
// branch in repo has.
func freeBranchName(repo, base string) string {
	name := base
	for n := 2; branchCommit(repo, name) != ""; n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	return name
}

// keepLeftovers writes everything loose in the copy into a patch in the run's
// record folder ([programLeftoversFile]), staged through the copy's own index,
// which nothing else reads and which goes with the copy. It answers the
// patch's path, and git's line when there is none.
func (f *ProgramFolder) keepLeftovers() (string, string) {
	if strings.TrimSpace(f.Keep) == "" {
		return "", "there is no record folder to keep them in"
	}
	var toAdd []string
	for _, path := range uncommittedPaths(f.Dir, f.Notes) {
		if !f.excludedFromCommit(path) {
			toAdd = append(toAdd, path)
		}
	}
	if len(toAdd) == 0 {
		return "", ""
	}
	if out, err := git(f.Dir, append([]string{"add", "-A", "--"}, toAdd...)...); err != nil {
		return "", "git add: " + firstLine(out)
	}
	patch, err := git(f.Dir, "diff", "--cached", "--binary", "HEAD")
	if err != nil {
		return "", "git diff: " + firstLine(patch)
	}
	if os.MkdirAll(f.Keep, 0o700) != nil {
		return "", "its record folder could not be made"
	}
	path := filepath.Join(f.Keep, programLeftoversFile)
	if err := os.WriteFile(path, []byte(patch), 0o600); err != nil {
		return "", err.Error()
	}
	return path, ""
}

// removeCopy removes the copy and git's record of it, and answers where a copy
// that would not go is left ("" when it went, or was already gone). The
// repository's exclude file loses codeaf's lines with the last copy
// ([dropProgramExclude]).
func (f *ProgramFolder) removeCopy() string {
	if !f.Copied() || strings.TrimSpace(f.Dir) == "" {
		return ""
	}
	if strings.TrimSpace(f.place.Dir) != "" {
		defer lockGitRoot(f.place, f.Repo)()
	}
	if _, err := os.Stat(f.Dir); err == nil {
		// THE FORCE IS TWICE, the way git spells "and a locked one too": nothing
		// in a copy is the person's, and a copy left behind keeps its branch
		// checked out, which is the one thing this is here to end.
		if _, err := git(f.Repo, "worktree", "remove", "--force", "--force", f.Dir); err != nil {
			f.unlinkCopy()
			_ = os.RemoveAll(f.Dir)
		}
	}
	_, _ = git(f.Repo, "worktree", "prune")
	if _, err := os.Stat(f.Dir); err == nil {
		return f.Dir
	}
	dropProgramExclude(f.Repo)
	return ""
}

// releaseCopy removes a copy's record and hold file and then lets its hold go:
// a copy's folder is never asked for again, so neither is kept once its run's
// ending is in the run's own record folder ([programFolderEndFile]).
//
// THE RECORD GOES BEFORE THE HOLD. A sweep that read the record while this run
// was ending waits for the hold, and reads the record again once it has it
// ([claimOwedProgramFolder]); gone by then, it is never finished twice.
func (f *ProgramFolder) releaseCopy() {
	_ = os.Remove(programFolderRecord(f.key))
	_ = os.Remove(programHoldFile(f.key))
	f.release()
}

// sweepProgramCopies finishes the copies of repo whose runs' processes went
// away without finishing them — a crash, a kill, codeaf closed — and prunes
// git's record of any copy that is no longer on disk, so a branch left checked
// out in a copy nobody holds is released before the next run starts.
func sweepProgramCopies(repo string) {
	repo = canonicalPath(repo)
	records, _ := filepath.Glob(filepath.Join(home.Join("v3", programFolderDir), "*.json"))
	for _, record := range records {
		owed, ok := readProgramFolderAt(record)
		if !ok || owed.Ended != "" {
			continue
		}
		// A RUN FROM A BUILD THAT WORKED IN THE CHECKOUT ITSELF is settled the
		// way that build settled it, reading and writing nothing of git's
		// ([ProgramFolder.settleGone]), so its record stops being owed.
		legacy := !owed.Copied() && owed.Branch != "" && owed.key == repo
		if !legacy && (!owed.Copied() || canonicalPath(owed.Repo) != repo) {
			continue
		}
		if owed = claimOwedProgramFolder(record, owed); owed == nil {
			continue
		}
		end := owed.settleGone()
		owed.Ended = end.Sentence()
		end.keepEnding()
		if legacy {
			owed.write()
			owed.release()
			continue
		}
		owed.releaseCopy()
	}
	_, _ = git(repo, "worktree", "prune")
	dropProgramExclude(repo)
}

// claimOwedProgramFolder takes the hold on the folder of a run read as owed
// from record, and answers that run as its record says now, held; nil when
// somebody holds the folder — the run itself, or its program still stopping —
// or when the run was finished while this asked.
//
// THE RECORD IS READ AGAIN UNDER THE HOLD. A run that was ending when the
// record was first read lets its hold go only after removing it
// ([ProgramFolder.releaseCopy]); read once, before the hold, its ending would
// be written over with a second one. A run whose ending is in its record
// folder already — its codeaf gone between writing the ending and letting go —
// is let go, not settled again.
func claimOwedProgramFolder(record string, owed *ProgramFolder) *ProgramFolder {
	lock, _, busy := claimProgramFolder(owed.key, owed.Program+", settling a run codeaf closed under")
	if busy || lock == nil {
		return nil
	}
	now, ok := readProgramFolderAt(record)
	if !ok || now.Ended != "" || now.key != owed.key {
		_ = filelock.Unlock(lock)
		_ = lock.Close()
		return nil
	}
	now.lock = lock
	if _, ended := keptProgramFolderEnd(now.Keep); ended && now.Copied() {
		now.releaseCopy()
		return nil
	}
	return now
}

// uncommittedPaths is every path a checkout has not committed, the program's
// notes left out, read without taking or writing any of git's locks — the
// person's checkout is never so much as refreshed. Nil when git cannot say.
func uncommittedPaths(dir, notes string) []string {
	paths, _ := uncommittedList(dir, notes)
	return paths
}

// uncommittedList is [uncommittedPaths] with git's line when git cannot say,
// for the reader that must not take "cannot say" for "nothing".
func uncommittedList(dir, notes string) ([]string, error) {
	return uncommittedStatusList(dir, notes, false)
}

// uncommittedSnapshotList adds each rename or copy source because a fresh
// index must stage its removal as well as the destination the receipt names.
func uncommittedSnapshotList(dir, notes string) ([]string, error) {
	return uncommittedStatusList(dir, notes, true)
}

// uncommittedStatusList reads the same status for the receipt and snapshot;
// only the snapshot needs both paths from a rename or copy record.
func uncommittedStatusList(dir, notes string, snapshot bool) ([]string, error) {
	untracked := "--untracked-files=all"
	if snapshot {
		untracked = "--untracked-files=no"
	}
	out, err := git(dir, "--no-optional-locks", "status", "--porcelain", untracked, "-z")
	if err != nil {
		return nil, errors.New(firstLine(out))
	}
	paths := porcelainZPaths(out)
	if snapshot {
		fields := strings.Split(out, "\x00")
		for i := 0; i < len(fields); i++ {
			entry := fields[i]
			if len(entry) < 4 || (entry[0] != 'R' && entry[0] != 'C' && entry[1] != 'R' && entry[1] != 'C') {
				continue
			}
			// A rename into the program's notes is absent from the receipt,
			// so its source must not become a snapshot deletion on its own.
			newPath := entry[3:]
			intoNotes := notes != "" && (newPath == notes || strings.HasPrefix(newPath, strings.TrimSuffix(notes, "/")+"/"))
			if i+1 < len(fields) && !intoNotes {
				paths = append(paths, fields[i+1])
			}
			i++
		}
	}
	var visible []string
	for _, path := range paths {
		if notes != "" && (path == notes || strings.HasPrefix(path, strings.TrimSuffix(notes, "/")+"/")) {
			continue
		}
		visible = append(visible, path)
	}
	return visible, nil
}

// LeftBehindWords is what a run in a copy says, as it starts, about the
// changes the person's checkout had not committed: carried into its copy, or
// not and why; "" when there were none, and for every run not in a copy.
func (f *ProgramFolder) LeftBehindWords() string {
	if !f.Copied() {
		return ""
	}
	var said string
	if f.Snapshot != "" {
		said = carriedInWords(f.LeftBehind)
	} else {
		said = leftBehindWords(f.LeftBehind, f.LeftBehindWhy)
	}
	return strings.TrimSpace(said + " " + copiedUntrackedWords(f.Untracked))
}

// copiedUntrackedWords says what becomes of the person's untracked files in
// the copy, so a receipt neither promises to merge a file the branch will not
// hold nor hides that an edited one will be on it.
func copiedUntrackedWords(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return "Your untracked files (" + namedFew(paths, programFolderShown) + ") are copied in as they are: any it changes are committed as its work, and the rest stay off its branch."
}

// carriedInWords is the sentence for uncommitted changes a copy begins with
// ([ProgramFolder.Snapshot]).
func carriedInWords(paths []string) string {
	named := "Your uncommitted changes"
	if len(paths) > 0 {
		named += " (" + namedFew(paths, programFolderShown) + ")"
	}
	return named + " are in its copy, as the first commit on its branch; in your folder they stay uncommitted, as they are."
}

// leftBehindWords is the sentence for uncommitted changes a copy does not
// have, and why when there is a reason to say; "" for none.
func leftBehindWords(paths []string, why string) string {
	if len(paths) == 0 {
		return ""
	}
	said := "Your uncommitted changes (" + namedFew(paths, programFolderShown) + ") are not in its copy"
	if why != "" {
		said += ": " + why
	}
	return said + "."
}

// BriefNote is the line a program's brief opens with when it works in a copy:
// where the copy is, and that a path the brief names under the person's
// repository is the same file in the copy. "" for every other run.
//
// A BRIEF IS WRITTEN ABOUT THE PERSON'S FOLDER, because that is the one the
// conversation can see. A program that took its paths literally would read
// the person's files and have its writes refused, or — through its shell — make
// them in the person's checkout, outside the copy its work is committed from.
func (f *ProgramFolder) BriefNote() string {
	if !f.Copied() {
		return ""
	}
	said := "You work in a private copy of the repository at " + f.Repo + ", checked out at " + f.Dir +
		" on the branch " + f.Branch + ". A path this brief names under " + f.Repo + " is the same file under " +
		f.Dir + ": read and change it there, and nowhere else."
	if f.Snapshot != "" {
		// A BRIEF THAT SAYS "FINISH IT" MEANS WHAT THE PERSON HAS SO FAR, and a
		// program that read the first commit as somebody else's to undo would
		// start again from the last one.
		said += " Its branch begins with the person's own uncommitted work, committed there as it stood when this run began: it is the work so far, yours to build on."
	}
	if len(f.Untracked) > 0 {
		said += " Files that were untracked in the person's checkout are copied here as they were, uncommitted: edit any the work needs, and those you change are committed as your work; those you leave as they are stay off the branch."
	}
	return said
}

// copySentence is how a run left its copy, in the sentence every surface says
// ([ProgramFolderEnd.Sentence]): where its work is, then what else was kept
// and where ([ProgramFolderEnd.copyAfterwords]).
func (e ProgramFolderEnd) copySentence() string {
	f := e.Folder
	merge := e.mergeWords()
	var said string
	switch {
	case e.Dropped:
		said = "it never started, so its copy is removed and its empty branch " + f.Branch + " deleted; your checkout was not touched"
	case e.Moved:
		said = e.movedCopyWords(merge)
	case e.Kept && e.Gone && e.Committed:
		said = "its work so far is on the branch " + f.Branch + " in " + f.Repo + ", " + fileCount(len(e.Changed)) + e.fromWords() +
			", committed when codeaf found its run had gone; " + merge
	case e.Kept && e.Gone:
		said = "its work so far is on the branch " + f.Branch + " in " + f.Repo + ", " + fileCount(len(e.Changed)) + e.fromWords() +
			", as its run had committed it before it went away; " + merge
	case e.Kept:
		said = "its work is on the branch " + f.Branch + " in " + f.Repo + ", " + fileCount(len(e.Changed)) + e.fromWords() +
			"; your checkout was not touched, and " + merge
	case e.Refused != "":
		said = "it committed nothing on its branch " + f.Branch + " in " + f.Repo + ", and what it left could not be committed (" + e.Refused + ")"
	case e.CopyKept != "":
		said = "nothing of its work is on its branch " + f.Branch + " in " + f.Repo + " yet, and your checkout was not touched"
	default:
		said = "it changed nothing; its branch " + f.Branch + " in " + f.Repo + " is kept where it began, and your checkout was not touched"
	}
	return said + e.copyAfterwords()
}

// mergeWords is how the person brings the branch in.
//
// A MERGE REFUSES A CHECKOUT WHOSE FILES IT WOULD WRITE, and the branch can hold
// two kinds of them. It begins with the person's uncommitted tracked changes
// ([ProgramFolder.Snapshot]), which a plain `git stash` puts aside; and it holds
// any untracked file of theirs the run changed ([ProgramFolder.Inputs]), which
// only a stash naming it with `-u` moves out of the way. Put aside, both come
// back through the branch itself — the run began from them. The inputs it left
// alone are not on the branch, so no stash touches them. THE STASH MUST TAKE
// ONLY THE INPUTS IT NAMES: shell quotes keep each path one word, but git still
// reads pathspec patterns, so a name with pattern characters, or a leading
// colon git would read as magic, needs literal magic.
func (e ProgramFolderEnd) mergeWords() string {
	f := e.Folder
	repo := shellQuoted(f.Repo)
	merge := "`git -C " + repo + " merge " + f.Branch + "`"
	var why, aside []string
	if f.Snapshot != "" {
		why = append(why, "its branch begins with your uncommitted changes as they were when it started")
		aside = append(aside, "`git -C "+repo+" stash`")
	}
	if changed := e.changedInputs(); len(changed) > 0 {
		quoted := make([]string, len(changed))
		for i, path := range changed {
			if strings.ContainsAny(path, "*?[\\") || strings.HasPrefix(path, ":") {
				path = ":(literal)" + path
			}
			quoted[i] = shellQuoted(path)
		}
		why = append(why, "it holds its changes to your untracked "+namedFew(changed, programFolderShown))
		aside = append(aside, "`git -C "+repo+" stash push -u -- "+strings.Join(quoted, " ")+"`")
	}
	if len(aside) == 0 {
		return merge + " brings it in"
	}
	return strings.Join(why, ", and ") + ", so put yours aside with " + strings.Join(aside, " then ") + ", and " + merge + " brings in both"
}

// changedInputs is the person's untracked files the branch holds its changes
// to: the inputs among the paths it changed.
func (e ProgramFolderEnd) changedInputs() []string {
	var changed []string
	for _, path := range e.Changed {
		if slices.Contains(e.Folder.Untracked, path) {
			changed = append(changed, path)
		}
	}
	return changed
}

// fromWords says a branch cut from an earlier run's holds that run's work too,
// which a merge of it brings in with this one's ([ProgramFolder.From]).
func (e ProgramFolderEnd) fromWords() string {
	if e.Folder.From == "" {
		return ""
	}
	return ", on top of " + e.Folder.From + ", whose work it holds too"
}

// movedCopyWords is where the work is when the program left its copy off its
// own branch ([ProgramFolder.settleMovedCopy]).
func (e ProgramFolderEnd) movedCopyWords(merge string) string {
	f := e.Folder
	where := "the branch " + e.HeadOn
	if e.HeadOn == "" {
		where = "no branch, at " + e.At
	}
	said := f.Program + " left its copy on " + where + " instead of its own branch " + f.Branch +
		", so codeaf committed nothing there"
	if e.Saved != "" {
		said += "; what it committed there is kept on the branch " + e.Saved
	}
	if e.Kept {
		said += "; " + f.Branch + " in " + f.Repo + " holds " + fileCount(len(e.Changed)) + ", and " + merge
	}
	return said
}

// copyAfterwords is what follows where a run's work is: what could not be
// committed, the patch, a record of the program's own put on a branch, and a
// copy that stayed on disk — left because it would not go, or kept because
// what is in it is kept nowhere else.
func (e ProgramFolderEnd) copyAfterwords() string {
	f := e.Folder
	var said string
	if e.Refused != "" && e.Kept && !e.Moved {
		said += "; what it left uncommitted could not be committed (" + e.Refused + ")"
	}
	if e.Patch != "" {
		said += "; what it left uncommitted is kept as a patch at " + e.Patch
	} else if e.Moved && e.Refused != "" {
		said += "; what it left uncommitted could not be kept (" + e.Refused + ")"
	}
	for _, kept := range e.Frozen {
		said += "; what it had saved at " + kept.Ref + ", which its branch does not hold, is kept on the branch " + kept.Branch
	}
	switch {
	case e.CopyKept != "":
		said += "; its copy is kept at " + e.CopyLeft + ", because " + e.CopyKept + ": take what you need from it, then delete that folder, and `git -C " +
			shellQuoted(f.Repo) + " worktree prune` releases " + f.Branch
	case e.CopyLeft != "":
		said += "; its copy at " + e.CopyLeft + " could not be removed, so " + f.Branch + " is still checked out there"
	}
	return said
}
