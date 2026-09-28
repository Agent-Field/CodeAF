package session

// A PROGRAM IN A REPOSITORY WORKS IN A COPY OF ITS OWN.
//
// THE CONTRACT, for a folder that is a git repository (programfolder.go keeps
// the rest — which folder, a plain folder, the refusals):
//
//  1. THE COPY IS A GIT WORKTREE IN A PRIVATE TEMPORARY FOLDER — under the
//     system's temporary folder ([programCopyRoot]), never inside the person's
//     repository — cut on a branch of the program's own ([taskBranchName]) from
//     the commit the person's checkout stands on. It is not for the person to
//     open: it exists so that several runs can work on one repository at once,
//     and so that the person's own checkout, index and branch are never touched.
//  2. WHAT IS NOT COMMITTED IN THE PERSON'S CHECKOUT IS NOT IN THE COPY. A copy
//     is cut from a commit, so their uncommitted changes stay theirs, unread,
//     and the run is not refused for them: the card and the receipt say they
//     were left behind ([ProgramFolder.LeftBehind]).
//  3. A FEW FOLDERS GIT IGNORES ARE LINKED IN, NOT COPIED ([programCopyLinks]):
//     installed dependencies and environment files the project's own build
//     and tests need and a fresh checkout does not have — `node_modules`, a
//     `.venv`, a `.env`. Build output is never linked, because two runs
//     building into one folder corrupt each other, and neither is anything
//     else: a `bin/` linked in once had a run's `make build` overwrite the
//     binary its person was running. A project names its own list in
//     `.codeaf/config.json` ([config.ProjectProgramLinks]).
//  4. WHEN THE PROGRAM EXITS — done, not finished, stopped, crashed, or its
//     process gone — what it left uncommitted on its branch is committed there,
//     the copy is removed and git's record of it pruned, so THE BRANCH IS
//     RELEASED: checked out nowhere, and free for the person, a merge, or the
//     next run of the same work. THE BRANCH IS ALWAYS KEPT, even when the run
//     changed nothing. What cannot be committed on it — the program moved the
//     copy off its branch, or left a merge half done — is kept as a patch in the
//     run's record folder instead of being lost with the copy.
//  5. A RUN WHOSE PROCESS WENT AWAY IS FINISHED THE SAME WAY by the next codeaf
//     that finds it: the conversation that reopens it, or the next program run
//     on the same repository ([sweepProgramCopies]). Unlike a folder a person
//     works in, nobody else's edits can be in a copy, so committing what is in
//     it is committing the run's own work.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// programCopyRoot is the folder every program's copy is made under: private
// to this user and temporary by nature. A variable so a test can put copies in
// a folder of its own.
var programCopyRoot = func() string {
	return filepath.Join(os.TempDir(), "codeaf-worktrees")
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

// programCopyExcludeSentinel heads the lines codeaf adds to the repository's
// exclude file for the names it links into a copy ([excludeLinkedNames]).
const programCopyExcludeSentinel = "# codeaf: folders linked into a program's copy"

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
	start, err := git(repo, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%s has no commit to cut a branch from: %s", repo, firstLine(start))
	}
	folder.Start, folder.Home = strings.TrimSpace(start), currentBranch(repo)
	folder.LeftBehind = uncommittedPaths(repo, "")
	if carry := order.Carry; carry != nil && canonicalPath(carry.Root) == canonicalPath(repo) && branchCommit(repo, carry.Branch) != "" {
		// THE LINE'S START, NOT THIS RUN'S. "Changed nothing" and the files the
		// ending counts are measured from where the first run of the line began,
		// so a sent-back run can never read the earlier runs' work as none.
		folder.Branch, folder.Home, folder.Start, folder.Continues = carry.Branch, carry.Home, carry.Start, true
	} else {
		folder.Branch = taskBranchName(folder.Title)
	}
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
	// THE RECORD IS WRITTEN BEFORE THE COPY IS CUT, so a process that goes
	// away between the two leaves a record the next codeaf can finish from
	// rather than a worktree nothing knows about.
	folder.write()
	if refusal := folder.cutCopy(); refusal != "" {
		folder.forget()
		return nil, fmt.Errorf("%s", refusal)
	}
	folder.Linked = linkIgnored(repo, dir, folder.Notes, programCopyLinks(repo))
	excludeLinkedNames(dir, folder.Linked)
	ignored, err := git(dir, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		folder.settleCopy("", false)
		folder.forget()
		return nil, fmt.Errorf("read paths ignored at the start in %s: %s", dir, firstLine(ignored))
	}
	listed := strings.Split(strings.TrimSuffix(ignored, "\x00"), "\x00")
	folder.IgnoredAtStart = append(listed, folder.Linked...)
	if err := folder.writeIgnoredAtStart([]byte(strings.Join(folder.IgnoredAtStart, "\x00"))); err != nil {
		folder.settleCopy("", false)
		folder.forget()
		return nil, err
	}
	folder.write()
	return folder, nil
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
			return fmt.Sprintf("could not cut %s's copy of %s: %s (and carrying on on %s: %s)", f.Program, f.Repo, firstLine(out2), taken, firstLine(out))
		}
		return ""
	}
	if out, err := git(f.Repo, append(head, "-b", f.Branch, f.Dir, f.Start)...); err != nil {
		// A BRANCH MADE BY A CUT THAT FAILED IS DELETED, so nothing is left in
		// the person's repository that nothing knows about.
		if tip := branchCommit(f.Repo, f.Branch); tip != "" && tip == f.Start {
			_, _ = git(f.Repo, "branch", "-q", "-D", f.Branch)
		}
		return fmt.Sprintf("could not cut %s's copy of %s: %s", f.Program, f.Repo, firstLine(out))
	}
	return ""
}

// forget undoes a copy's preparation that failed part way: the copy and git's
// record of it gone, a branch it cut that holds nothing deleted, the record
// and the hold let go.
func (f *ProgramFolder) forget() {
	f.removeCopy()
	if !f.Continues {
		if tip := branchCommit(f.Repo, f.Branch); tip != "" && tip == f.Start {
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
	name := programCopyName.ReplaceAllString(filepath.Base(repo), "-")
	dir, err := os.MkdirTemp(root, strings.Trim(name, "-")+"-")
	if err != nil {
		return "", err
	}
	return canonicalPath(dir), nil
}

var programCopyName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// programCopyLinks is the ignored names a copy of repo links in: the
// project's own list when its `.codeaf/config.json` names one — an empty one
// links nothing — and [programCopyLinked] with every `.env.*` file otherwise.
func programCopyLinks(repo string) []string {
	if project, err := config.LoadProjectConfig(repo); err == nil {
		if listed, found, err := project.String(config.ProjectProgramLinks); err == nil && found {
			var names []string
			for _, name := range strings.Split(listed, ",") {
				if name = strings.Trim(strings.TrimSpace(name), "/"); name != "" {
					names = append(names, name)
				}
			}
			return names
		}
	}
	names := append([]string(nil), programCopyLinked...)
	if matches, err := filepath.Glob(filepath.Join(repo, ".env.*")); err == nil {
		for _, match := range matches {
			names = append(names, filepath.Base(match))
		}
	}
	return names
}

// linkIgnored links into dir each of names that is in repo, at its top level,
// and that git ignores there, and answers the ones it linked. A name git does
// not ignore is the repository's own file and is in the copy already; one the
// program keeps its notes under is never linked, because its notes are its own
// run's.
func linkIgnored(repo, dir, notes string, names []string) []string {
	var linked []string
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
		if os.Symlink(source, target) == nil {
			linked = append(linked, name)
		}
	}
	return linked
}

// excludeLinkedNames writes the names linked into a copy into the exclude
// file git reads for it, anchored at the top, once each.
//
// A LINK IS NOT A DIRECTORY TO GIT. `node_modules/` in a .gitignore matches a
// directory and not a link named node_modules, so without this the link is an
// untracked file in the copy: the program is told its tree is not clean, and a
// commit of it would put a link to the person's disk in their history. The file
// is the repository's shared one — git reads no other for a worktree — and an
// anchored name that is already ignored there changes nothing for the person.
func excludeLinkedNames(dir string, names []string) {
	if len(names) == 0 {
		return
	}
	out, err := git(dir, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return
	}
	path := strings.TrimSpace(out)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	current, _ := os.ReadFile(path)
	have := map[string]bool{}
	for _, line := range strings.Split(string(current), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var add []string
	for _, name := range names {
		if line := "/" + name; !have[line] {
			add = append(add, line)
		}
	}
	if len(add) == 0 {
		return
	}
	text := string(current)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if !have[programCopyExcludeSentinel] {
		text += programCopyExcludeSentinel + "\n"
	}
	text += strings.Join(add, "\n") + "\n"
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(text), 0o644)
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

// settleCopy ends a program's run in its copy, per the fourth and fifth points
// of the contract at the top of this file: its notes kept, what it left
// committed on its branch (or kept as a patch when that cannot be), and the
// copy removed so the branch is released. gone says the run's process went
// away before it could end the run itself.
func (f *ProgramFolder) settleCopy(result string, gone bool) ProgramFolderEnd {
	end := ProgramFolderEnd{Folder: *f, Gone: gone}
	end.Notes = f.keepNotes()
	f.unlinkCopy()
	if _, err := os.Stat(f.Dir); err == nil {
		if head := currentBranch(f.Dir); head != f.Branch {
			// A HEAD THE PROGRAM MOVED IS NOT COMMITTED ON. Its branch holds
			// what it committed there; whatever is loose in the copy is kept as
			// a patch rather than committed onto a branch nobody chose.
			end.Moved, end.HeadOn = true, head
			if head == "" {
				end.At = shortCommit(f.Dir, "HEAD")
				end.Saved = f.keepDetached()
			}
			end.Uncommitted = uncommittedCount(f.Dir, f.Notes)
			if end.Uncommitted > 0 {
				end.Patch, end.Refused = f.keepLeftovers()
			}
		} else if refused := f.commitLeftovers(result); refused != "" {
			end.Refused = refused
			end.Patch, _ = f.keepLeftovers()
		}
	}
	if tip := branchCommit(f.Repo, f.Branch); tip != "" {
		end.Changed = changedBetween(f.Repo, f.Start, tip)
		end.Kept = tip != f.Start
	}
	end.CopyLeft = f.removeCopy()
	return end
}

// keepDetached puts a branch on a detached HEAD the program committed on in
// its copy, when no branch holds that commit, and answers the branch.
//
// A COMMIT NO BRANCH HOLDS GOES WITH THE COPY. In the person's own checkout a
// detached HEAD kept it reachable; a copy is removed when the run ends, and
// what was only its HEAD would be left for git's garbage collection.
func (f *ProgramFolder) keepDetached() string {
	// `git branch --contains` counts the detached HEAD itself as a holder, so
	// the branches are read as refs, which it is not.
	if holders, err := git(f.Dir, "for-each-ref", "--contains", "HEAD", "--format=%(refname)", "refs/heads/"); err != nil || strings.TrimSpace(holders) != "" {
		return ""
	}
	name := f.Branch + "-detached"
	if _, err := git(f.Dir, "branch", "-q", name, "HEAD"); err != nil {
		return ""
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
// that would not go is left ("" when it went, or was already gone).
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
	return ""
}

// releaseCopy lets a copy's hold go and removes its hold file and record: a
// copy's folder is never asked for again, so neither is kept once its run's
// ending is in the run's own record folder ([programFolderEndFile]).
func (f *ProgramFolder) releaseCopy() {
	f.release()
	_ = os.Remove(programHoldFile(f.key))
	_ = os.Remove(programFolderRecord(f.key))
}

// sweepProgramCopies finishes the copies of repo whose runs' processes went
// away without finishing them — a crash, a kill, codeaf closed — and prunes
// git's record of any copy that is no longer on disk, so a branch left checked
// out in a copy nobody holds is released before the next run starts.
func sweepProgramCopies(repo string) {
	repo = canonicalPath(repo)
	records, _ := filepath.Glob(filepath.Join(home.Join("v3", programFolderDir), "*.json"))
	for _, path := range records {
		owed, ok := readProgramFolderAt(path)
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
		lock, _, busy := claimProgramFolder(owed.key, owed.Program+", settling a run codeaf closed under")
		if busy || lock == nil {
			continue
		}
		owed.lock = lock
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
}

// uncommittedPaths is every path a checkout has not committed, the program's
// notes left out, read without taking or writing any of git's locks — the
// person's checkout is never so much as refreshed.
func uncommittedPaths(dir, notes string) []string {
	out, err := git(dir, "--no-optional-locks", "status", "--porcelain", "--untracked-files=all", "-z")
	if err != nil {
		return nil
	}
	var paths []string
	for _, path := range porcelainZPaths(out) {
		if notes != "" && (path == notes || strings.HasPrefix(path, strings.TrimSuffix(notes, "/")+"/")) {
			continue
		}
		paths = append(paths, path)
	}
	return paths
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
	return "You work in a private copy of the repository at " + f.Repo + ", checked out at " + f.Dir +
		" on the branch " + f.Branch + ". A path this brief names under " + f.Repo + " is the same file under " +
		f.Dir + ": read and change it there, and nowhere else."
}

// copySentence is how a run left its copy, in the sentence every surface says
// ([ProgramFolderEnd.Sentence]).
func (e ProgramFolderEnd) copySentence() string {
	f := e.Folder
	repo := shellQuoted(f.Repo)
	merge := "`git -C " + repo + " merge " + f.Branch + "` brings it in"
	var said string
	switch {
	case e.Moved:
		where := "the branch " + e.HeadOn
		if e.HeadOn == "" {
			where = "no branch, at " + e.At
		}
		said = f.Program + " left its copy on " + where + " instead of its own branch " + f.Branch +
			", so codeaf committed nothing there"
		if e.Saved != "" {
			said += "; what it committed there is kept on the branch " + e.Saved
		}
		if e.Kept {
			said += "; " + f.Branch + " in " + f.Repo + " holds " + fileCount(len(e.Changed)) + ", and " + merge
		}
	case e.Kept && e.Gone:
		said = "its work so far is on the branch " + f.Branch + " in " + f.Repo + ", " + fileCount(len(e.Changed)) +
			", committed when codeaf found its run had gone; " + merge
	case e.Kept:
		said = "its work is on the branch " + f.Branch + " in " + f.Repo + ", " + fileCount(len(e.Changed)) +
			"; your checkout was not touched, and " + merge
	case e.Refused != "":
		said = "it committed nothing on its branch " + f.Branch + " in " + f.Repo + ", and what it left could not be committed (" + e.Refused + ")"
	default:
		said = "it changed nothing; its branch " + f.Branch + " in " + f.Repo + " is kept where it began, and your checkout was not touched"
	}
	if e.Refused != "" && e.Kept && !e.Moved {
		said += "; what it left uncommitted could not be committed (" + e.Refused + ")"
	}
	if e.Patch != "" {
		said += "; what it left uncommitted is kept as a patch at " + e.Patch
	} else if e.Moved && e.Refused != "" {
		said += "; what it left uncommitted could not be kept (" + e.Refused + ")"
	}
	if e.CopyLeft != "" {
		said += "; its copy at " + e.CopyLeft + " could not be removed, so " + f.Branch + " is still checked out there"
	}
	return said
}
