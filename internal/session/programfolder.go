package session

// A PROGRAM WORKS IN THE FOLDER IT IS GIVEN, ON A BRANCH OF ITS OWN WHEN THAT
// FOLDER IS A GIT REPOSITORY.
//
// THE CONTRACT. This is the whole of what codeaf does to the folder a program
// that edits files works in (senior-dev first), for a run a conversation hands
// off and for one a person starts at a shell alike; senior-dev.md and
// delegates.md say it in a person's words.
//
//  1. WHICH FOLDER. The folder the proposal names as `ground`, or the
//     conversation's own when it names none (a typed `/senior-dev` names none),
//     or the one a shell run was started in or named with `--dir` — THAT FOLDER
//     ITSELF, never a copy of it. Inside a git repository it is the
//     repository's root. It is never the home folder or a folder holding it
//     ([programHomeRefusal]). A folder that is not there yet is made, empty,
//     when the folder it would be made in is there.
//  2. A GIT REPOSITORY — history, a commit, and a root below the home folder.
//     The person's branch (or the commit their checkout is on) is written
//     down, `git switch -c` cuts the program's own branch ([taskBranchName]),
//     and the program works there in its own git mode. THE PERSON'S BRANCH
//     NEVER MOVES. A checkout with changes that are not committed, or in the
//     middle of a merge, a rebase or a cherry-pick, is refused before anything
//     starts, with what is in the way named ([programCheckoutInTheWay]).
//  3. ANYTHING ELSE — no history, no commit yet, or a repository whose root is
//     the home folder or above it: the program works in the folder as it is,
//     started with its own flags for that ([delegate.Delegate.PlainFolder];
//     senior-dev's `--in-place`). codeaf passes them whenever it decided so,
//     because the program's own reading of a folder climbs to any repository
//     around it.
//  4. WHEN IT ENDS — done, not finished, stopped, or crashed, an end the
//     process holding the run saw — in a repository, what the program left
//     uncommitted is committed onto its branch in one commit (the task's
//     title, the result under it) and the branch is LEFT CHECKED OUT, so the
//     person sees the work in their folder. A run that changed nothing is
//     undone: the person's branch is checked out again and the empty branch
//     deleted. A HEAD the program's shell moved off its branch is left exactly
//     where it is, and said, and so is a branch of the person's that moved.
//     A RUN WHOSE PROCESS WENT AWAY — codeaf closed, crashed or killed — is
//     settled by the next codeaf that finds it WITHOUT A SINGLE GIT WRITE
//     ([ProgramFolder.settleGone]): its work stays as it left it, and the
//     person is told where and in what state. In either kind of folder the
//     program's notes ([delegate.Delegate.Notes]) are moved into the run's
//     record folder unless they were there before the run.
//  5. ONE RUN PER FOLDER. codeaf starts and stops the run and keeps its money,
//     its time and its screen, and nothing else. A second program run on a
//     folder one is working in — from any conversation, any window, or a
//     shell — is refused, naming the run that holds it; the hold is a file
//     lock, which dies with the process that took it ([claimProgramFolder]).
//
// WHY THERE IS SO LITTLE HERE. Until 2026-09-24 a program ran through the
// general task machinery: a copy of the folder cut for every run, the brief's
// paths rewritten to name the copy, the program's commits squashed and its
// HEAD put back at the landing, and a ladder of placement rules, each layer
// patching the one before it. The owner asked why it was so hard to have
// senior-dev just work on the problem — "if it's in a git repo, great - if
// not, just do it" — and the answer was that codeaf had made it hard. The
// copy, the rewriting, the squash and the ladder went, and this is what
// stayed.
//
// ONE ROAD FOR BOTH DOORS. The conversation's run (task_run_belt.go) and the
// shell's `codeaf senior-dev` (cmd/codeaf/carried.go) prepare a folder with
// [PrepareProgramFolder] and finish it with [ProgramFolder.Finish], so a
// person at a shell and a person in the chat get the same folder, the same
// branch, the same refusals and the same last sentence.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

// programFolderDir is where the hold on each folder a program works in, and
// the record of that run's folder, live: under the state root, keyed by the
// folder, and never inside the person's folder.
const programFolderDir = "program-folders"

// programFolderShown is how many of the paths in the way a refused checkout
// names before it counts the rest.
const programFolderShown = 3

// ProgramFolderOrder is what a door hands [PrepareProgramFolder].
type ProgramFolderOrder struct {
	// Program is the program that will work in the folder.
	Program delegate.Delegate
	// Dir is the folder asked for, absolute.
	Dir string
	// Title is the run's title: the program's branch is named from it, and
	// the commit that finishes the run carries it. Empty is the first words of
	// Brief, the way a task names itself from its brief ([taskPersonTitle]).
	Title string
	Brief string
	// Holder is how a second run on the folder is told whose run holds it:
	// `task 4 (Fix the parser)`, or `a run started at a shell`.
	Holder string
	// Keep is the run's record folder. The program's notes are moved into it
	// when the run ends, and it is the name a reopen finds the run's folder by
	// ([settleOwedProgramFolder]).
	Keep string
	// Instead is what a folder that is the home folder is answered with,
	// after the refusal itself ([programHomeRefusal]).
	Instead string
	// Place is the conversation's session folder, which says where the
	// repository's git lock lives ([lockGitRoot]); zero for a shell run,
	// which takes none.
	Place Place
	// Sign puts the attribution trailer on the commit that finishes the run
	// ([signed]).
	Sign bool
}

// ProgramFolder is one program run's folder as [PrepareProgramFolder] readied
// it. It is also the record a later process settles the run's folder from
// when the process that started it went away first
// ([settleOwedProgramFolder]), which is why its fields are written down.
type ProgramFolder struct {
	// Program is the program's name, and Title is the run's.
	Program string `json:"program"`
	Title   string `json:"title"`
	// Dir is the folder the program works in, spelled the way the door asked
	// for it when it asked for the folder itself.
	Dir string `json:"dir"`
	// Branch is the program's own branch, cut by codeaf; empty for a folder the
	// program works in without git. Home is the branch the person had checked
	// out, empty when their checkout was on no branch, and Start is the commit
	// it stood on: together they are where going back goes.
	Branch string `json:"branch,omitempty"`
	Home   string `json:"home,omitempty"`
	Start  string `json:"start,omitempty"`
	// Outer is a repository around a folder worked in without git, which codeaf
	// cut no branch in because its root holds the home folder.
	Outer string `json:"outer,omitempty"`
	// Notes is the program's notes folder inside Dir, and NotesWereThere says
	// it was already there when the run began, which leaves it where it is.
	Notes          string `json:"notes,omitempty"`
	NotesWereThere bool   `json:"notesWereThere,omitempty"`
	// Keep is the run's record folder ([ProgramFolderOrder.Keep]) and Sign is
	// its attribution ([ProgramFolderOrder.Sign]).
	Keep string `json:"keep,omitempty"`
	Sign bool   `json:"sign,omitempty"`
	// Ended is the sentence the run's folder was finished with. Empty is a
	// folder still owed its ending.
	Ended string `json:"ended,omitempty"`

	key   string
	place Place
	lock  *os.File
}

// Plain says the program works in its folder without git.
func (f *ProgramFolder) Plain() bool { return f == nil || f.Branch == "" }

// PrepareProgramFolder readies the folder a program was asked to work in, per
// the contract at the top of this file, and holds it for the run: the folder
// resolved and made when it must be, the hold taken, a run that went away in
// it finished first, and in a repository the checkout read and the program's
// branch cut. The refusal is a sentence a person can act on, and nothing has
// been changed when there is one.
func PrepareProgramFolder(order ProgramFolderOrder) (*ProgramFolder, error) {
	if strings.TrimSpace(order.Dir) == "" {
		return nil, errors.New(order.Program.Name + " was handed no folder to work in")
	}
	asked := absolutePath(filepath.Clean(strings.TrimSpace(order.Dir)))
	dir, repo, outer, refusal := programFolderAt(order.Program, asked, order.Instead)
	if refusal != "" {
		return nil, errors.New(refusal)
	}
	title := strings.TrimSpace(order.Title)
	if title == "" {
		title = taskPersonTitle(order.Brief)
	}
	folder := &ProgramFolder{
		Program: order.Program.Name, Title: title, Dir: dir, Outer: outer,
		Notes: order.Program.Notes, Keep: order.Keep, Sign: order.Sign,
		key: canonicalPath(dir), place: order.Place,
	}
	lock, holder := claimProgramFolder(folder.key, order.Program.Name+", "+order.Holder)
	if holder != "" {
		return nil, errors.New(programFolderBusy(dir, holder))
	}
	folder.lock = lock
	// A RUN THAT WENT AWAY IN THIS FOLDER IS SETTLED BEFORE THE NEXT ONE STARTS,
	// AND NOTHING OF IT IS COMMITTED ([ProgramFolder.settleGone]): its record
	// ended with where its work is and its notes moved, so the next run is
	// never handed the last one's checklist as its own. What it left
	// uncommitted stays exactly where it was, and the next run meets it the way
	// it meets anybody's changes — refused, and told whose they may be. Nothing
	// else holds the folder, because this does; on a filesystem that takes no
	// locks nothing can say so, and an owed run there is left alone.
	var earlier *ProgramFolderEnd
	if owed, ok := readProgramFolder(folder.key); ok && owed.Ended == "" && lock != nil {
		owed.key = folder.key
		end := owed.settleGone()
		owed.Ended = end.Sentence()
		owed.write()
		earlier = &end
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.Mkdir(dir, 0o755); err != nil {
			folder.release()
			return nil, fmt.Errorf("make the folder %s: %w", dir, err)
		}
	}
	if folder.Notes != "" {
		_, err := os.Lstat(filepath.Join(dir, folder.Notes))
		folder.NotesWereThere = err == nil
	}
	if !repo {
		folder.write()
		return folder, nil
	}
	if err := folder.cutBranch(); err != nil {
		folder.release()
		if earlier != nil && earlier.Folder.Branch != "" && !earlier.Moved {
			// AND THE REFUSAL SAYS WHOSE THE CHANGES MAY BE. codeaf cannot tell a
			// run's last edits from the person's own made on its branch since, so
			// it commits neither and says both.
			return nil, fmt.Errorf("%w; they may be an earlier %s run's, which codeaf could not finish: its branch %s is checked out there",
				err, earlier.Folder.Program, earlier.Folder.Branch)
		}
		return nil, err
	}
	return folder, nil
}

// cutBranch reads the person's checkout and cuts the program's branch in it,
// under the repository's git lock when there is a session to keep one.
func (f *ProgramFolder) cutBranch() error {
	if strings.TrimSpace(f.place.Dir) != "" {
		defer lockGitRoot(f.place, f.key)()
	}
	if refusal := programCheckoutInTheWay(f.Dir, f.Notes); refusal != "" {
		return errors.New(refusal)
	}
	start, err := git(f.Dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return fmt.Errorf("%s has no commit to cut a branch from: %s", f.Dir, firstLine(start))
	}
	f.Start, f.Home = strings.TrimSpace(start), currentBranch(f.Dir)
	f.Branch = taskBranchName(f.Title)
	// THE RECORD IS WRITTEN BEFORE THE BRANCH IS CUT, so a process that goes
	// away between the two leaves a record a later one can finish from rather
	// than a branch nothing knows about.
	f.write()
	out, err := git(f.Dir, append(switchWithoutHooks(), "-c", f.Branch)...)
	if err == nil {
		return nil
	}
	// A SWITCH THAT FAILED IS READ AGAIN BEFORE IT IS ANSWERED. git's exit says
	// the command failed, not that nothing happened: a lock it could not take
	// after the branch was made leaves the branch behind, and whatever else can
	// go wrong once HEAD has moved leaves the checkout on it. A cut that did
	// happen is carried on with, because the record written above is exactly
	// what it needs; a branch made without the checkout following is deleted,
	// so nothing is left in the person's repository that nothing knows about.
	if currentBranch(f.Dir) == f.Branch {
		return nil
	}
	if tip := branchCommit(f.Dir, f.Branch); tip != "" && tip == f.Start {
		_, _ = git(f.Dir, "branch", "-q", "-D", f.Branch)
	}
	_ = os.Remove(programFolderRecord(f.key))
	refusal := fmt.Sprintf("could not cut %s's branch in %s: %s", f.Program, f.Dir, firstLine(out))
	if !f.onHome() {
		refusal += "; the checkout is now on " + checkoutWords(f.Dir) + ", not " + f.homeWords()
	}
	return errors.New(refusal)
}

// switchWithoutHooks is the head of every `git switch` codeaf runs in a
// program's folder: quiet, and with the repository's hooks turned off.
//
// BOTH SWITCHES GO BETWEEN TWO NAMES FOR ONE COMMIT — the program's branch cut
// where the checkout stands, and the person's own checked out again over a
// branch that holds nothing past it — so there is no checkout work a hook
// could have to do. And a hook that fails is the one way a switch that moved
// HEAD still exits non-zero: an LFS post-checkout hook with no git-lfs on
// codeaf's PATH left the checkout on a branch the refusal said was never cut.
func switchWithoutHooks() []string {
	return []string{"-c", "core.hooksPath=" + os.DevNull, "switch", "-q"}
}

// onHome says the checkout is where the person had it before the run: on
// their branch, or at the commit when it was on none.
func (f *ProgramFolder) onHome() bool {
	head := currentBranch(f.Dir)
	if f.Home != "" {
		return head == f.Home
	}
	at, err := git(f.Dir, "rev-parse", "--verify", "-q", "HEAD")
	return head == "" && err == nil && strings.TrimSpace(at) == f.Start
}

// checkoutWords names where a checkout is now, as a person reads it: the
// branch, or the commit when it is on none.
func checkoutWords(dir string) string {
	if head := currentBranch(dir); head != "" {
		return "the branch " + head
	}
	return "no branch, at " + shortCommit(dir, "HEAD")
}

// programFolderAt is the folder a program asked to work in asked works in,
// whether it works there on a branch, the repository around it codeaf will
// not cut one in, and the refusal when there is nowhere it may work.
func programFolderAt(program delegate.Delegate, asked, instead string) (string, bool, string, string) {
	dir, repo, outer, refusal := programFolderOf(asked)
	if refusal != "" {
		return "", false, "", refusal
	}
	if refusal := programHomeRefusal(program, dir, instead); refusal != "" {
		return "", false, "", refusal
	}
	return dir, repo, outer, ""
}

// programFolderOf is [programFolderAt] without the home folder's refusal,
// which is the program's to say: the repository's root when asked is in a
// repository with a commit whose root is below the home folder, and asked
// itself otherwise. A folder that is not there yet is read by the folder it
// would be made in, and refused when that is not there either.
func programFolderOf(asked string) (dir string, repo bool, outer string, refusal string) {
	probe := asked
	if info, err := os.Stat(asked); err != nil {
		parent := filepath.Dir(asked)
		if info, err := os.Stat(parent); err != nil || !info.IsDir() {
			return "", false, "", asked + " is not there, and neither is " + parent + ", the folder it would be made in"
		}
		probe = parent
	} else if !info.IsDir() {
		return "", false, "", asked + " is a file, not a folder"
	}
	root, ok := repositoryRoot(probe)
	switch {
	case !ok || !hasCommit(root):
		return asked, false, "", ""
	case holdsHomeFolder(root):
		// A REPOSITORY AT THE HOME FOLDER IS NOBODY'S PROJECT. A dotfiles
		// repository there would otherwise have every folder under home read as
		// its subfolder, and the program's branch cut in the person's dotfiles.
		return asked, false, root, ""
	case canonicalPath(asked) == root:
		return asked, true, "", ""
	}
	return root, true, "", ""
}

// programCheckoutInTheWay is why a repository's checkout cannot take a
// program's branch now, in a sentence that says what to do; empty when nothing
// is in the way. The program's own notes folder is never in the way: it is
// the program's, and it is kept out of every commit.
//
// A CHANGE THAT IS NOT COMMITTED IS THE PERSON'S, AND A BRANCH CUT OVER IT
// TAKES IT ALONG. The program would count it as its own work, commit it with
// its first write, or — restoring a tree whose tests cannot start — put the
// file back to its last commit. So nothing starts until the person has put it
// somewhere of their own.
func programCheckoutInTheWay(dir, notes string) string {
	if half := halfDone(dir); half != "" {
		return dir + " is in the middle of a " + half + "; finish it or abort it, then ask again"
	}
	out, err := git(dir, "status", "--porcelain", "--untracked-files=all", "-z")
	if err != nil {
		return "git could not read " + dir + ": " + firstLine(out)
	}
	var paths []string
	for _, path := range porcelainZPaths(out) {
		if notes != "" && (path == notes || strings.HasPrefix(path, strings.TrimSuffix(notes, "/")+"/")) {
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return ""
	}
	return dir + " has changes that are not committed (" + namedFew(paths, programFolderShown) + "); commit or stash them, then ask again"
}

// halfDone is the git operation a checkout is in the middle of — a merge, a
// rebase, a cherry-pick or a revert — in the word a person uses for it, "" when
// it is in the middle of none.
func halfDone(dir string) string {
	for _, half := range []struct{ path, what string }{
		{"MERGE_HEAD", "merge"},
		{"rebase-merge", "rebase"},
		{"rebase-apply", "rebase"},
		{"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"},
	} {
		out, err := git(dir, "rev-parse", "--git-path", half.path)
		if err != nil {
			continue
		}
		path := strings.TrimSpace(out)
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if _, err := os.Lstat(path); err == nil {
			return half.what
		}
	}
	return ""
}

// porcelainZPaths is every path `git status --porcelain -z` names, a rename
// by where it went.
func porcelainZPaths(out string) []string {
	fields := strings.Split(out, "\x00")
	var paths []string
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		paths = append(paths, entry[3:])
		if entry[0] == 'R' || entry[0] == 'C' {
			// The path it came from follows, and is not a second change.
			i++
		}
	}
	return paths
}

// programFolderBusy is the refusal for a folder another program run holds.
func programFolderBusy(dir, holder string) string {
	return dir + " is busy: " + holder + ", is working in it, and one folder takes one program run at a time; ask again when that run has ended"
}

// ProgramFolderEnd is how a program's run left its folder, as
// [ProgramFolder.Finish] found it and made it.
type ProgramFolderEnd struct {
	Folder ProgramFolder
	// Changed is every path the program's branch changed from where it
	// started.
	Changed []string
	// Kept says the program's branch holds its work.
	Kept bool
	// Dropped says the run changed nothing, so the person's own branch (or
	// commit) is checked out again and the program's branch is gone.
	Dropped bool
	// Moved says HEAD was not on the program's branch when the run ended:
	// HeadOn is the branch it was on, empty with At naming the commit when it
	// was on none.
	Moved  bool
	HeadOn string
	At     string
	// HomeMoved says the person's own branch no longer points where it did
	// when the run began — something committed on it, reset it or deleted it
	// while the program worked — and HomeAt is the commit it points at now,
	// empty when it is gone. codeaf moves it back no more than it moved it.
	HomeMoved bool
	HomeAt    string
	// Gone says the run's process went away before it could end the run
	// itself, so codeaf settled its folder without writing to git at all
	// ([ProgramFolder.settleGone]), and Uncommitted is how many files it found
	// there that are not committed.
	Gone        bool
	Uncommitted int
	// Refused is git's own line when what the program left could not be
	// committed, or the checkout could not be put back.
	Refused string
	// Notes is where the program's notes went, as a sentence.
	Notes string
}

// Finish ends a program's run in its folder, per the fourth point of the
// contract at the top of this file, and lets the folder go. result is the
// run's ending in words, the body of the commit that holds what the program
// left. It answers what it found and did.
func (f *ProgramFolder) Finish(result string) ProgramFolderEnd {
	end := f.settle(result)
	f.Ended = end.Sentence()
	f.write()
	f.release()
	return end
}

// settle is [ProgramFolder.Finish] without the record and the hold, which the
// caller owns.
func (f *ProgramFolder) settle(result string) ProgramFolderEnd {
	end := ProgramFolderEnd{Folder: *f}
	// THE NOTES GO FIRST, so the commit below can never hold them.
	end.Notes = f.keepNotes()
	if f.Branch == "" {
		return end
	}
	if strings.TrimSpace(f.place.Dir) != "" {
		defer lockGitRoot(f.place, f.key)()
	}
	if head := currentBranch(f.Dir); head != f.Branch {
		// A HEAD THE PROGRAM MOVED IS LEFT WHERE IT IS. Committing there would put
		// codeaf's commit on a branch that may be the person's own, and moving HEAD
		// back would carry whatever is in the folder somewhere nobody chose; the
		// person is told where it is instead, and decides.
		end.Moved, end.HeadOn = true, head
		if head == "" {
			end.At = shortCommit(f.Dir, "HEAD")
		}
		if tip := branchCommit(f.Dir, f.Branch); tip != "" {
			end.Changed = changedBetween(f.Dir, f.Start, tip)
			end.Kept = tip != f.Start
		}
		return end
	}
	end.HomeMoved, end.HomeAt = f.homeMoved()
	end.Refused = f.commitLeftovers(result)
	head, _ := git(f.Dir, "rev-parse", "--verify", "HEAD")
	end.Changed = changedSince(f.Dir, f.Start)
	if end.Refused == "" && strings.TrimSpace(head) == f.Start {
		if end.HomeMoved {
			// A BRANCH OF THE PERSON'S THAT MOVED IS NOT SWITCHED TO. Going back
			// would check out commits nobody here made or read, under a sentence
			// saying the run changed nothing; the empty branch stays checked out,
			// and the sentence says why.
			return end
		}
		// A RUN THAT CHANGED NOTHING LEAVES NOTHING: no branch holding nothing
		// in the person's repository, and their own branch checked out again.
		if refused := f.goBack(); refused != "" {
			end.Refused = refused
			return end
		}
		end.Dropped = true
		return end
	}
	end.Kept = true
	return end
}

// settleGone settles the folder of a run whose process went away before it
// could end the run itself — a crash, a kill, codeaf closed — and it WRITES
// NOTHING TO GIT: no add, no commit, no switch, no branch deleted. It reads
// where the checkout is, what the program's branch holds and how many files
// are not committed, moves the program's notes into the run's record folder,
// and answers the ending that says so ([ProgramFolderEnd.Gone]).
//
// ONLY AN END CODEAF SAW IS FINISHED WITH A COMMIT. Once the process that
// held the folder is gone, the folder is the person's again, and what is
// uncommitted in it may be the run's last edits or their own made on its
// branch since — codeaf cannot tell the two apart. A commit here once swept a
// person's day of edits, and a merge they were resolving, into a commit under
// codeaf's name with their hooks skipped. The read is made with git's optional
// locks off, so not even the index is refreshed.
func (f *ProgramFolder) settleGone() ProgramFolderEnd {
	end := ProgramFolderEnd{Folder: *f, Gone: true}
	end.Notes = f.keepNotes()
	if f.Branch == "" {
		return end
	}
	if head := currentBranch(f.Dir); head != f.Branch {
		end.Moved, end.HeadOn = true, head
		if head == "" {
			end.At = shortCommit(f.Dir, "HEAD")
		}
	}
	if tip := branchCommit(f.Dir, f.Branch); tip != "" {
		end.Changed = changedBetween(f.Dir, f.Start, tip)
		end.Kept = tip != f.Start
	}
	if !end.Moved {
		end.Uncommitted = uncommittedCount(f.Dir, f.Notes)
		end.HomeMoved, end.HomeAt = f.homeMoved()
	}
	return end
}

// uncommittedCount is how many files in a checkout are not committed, the
// program's notes left out, read without taking or writing any of git's locks;
// zero when git cannot say.
func uncommittedCount(dir, notes string) int {
	out, err := git(dir, "--no-optional-locks", "status", "--porcelain", "--untracked-files=all", "-z")
	if err != nil {
		return 0
	}
	count := 0
	for _, path := range porcelainZPaths(out) {
		if notes != "" && (path == notes || strings.HasPrefix(path, strings.TrimSuffix(notes, "/")+"/")) {
			continue
		}
		count++
	}
	return count
}

// homeMoved reads the person's own branch again, the one the run was cut
// from, and answers whether it no longer points at the commit the run began
// on, and where it points now ("" when it is gone). A checkout that was on no
// branch has nothing that can move: a commit is where it is.
//
// NOTHING SAYS "AS IT WAS" WITHOUT LOOKING. The program never writes the
// person's branch, but its shell can — a checkout of it, a commit there, a
// switch back — and the sentence the person relies on before they push is
// the one that must not repeat a promise nobody checked.
func (f *ProgramFolder) homeMoved() (bool, string) {
	if f.Home == "" {
		return false, ""
	}
	tip := branchCommit(f.Dir, f.Home)
	return tip != f.Start, tip
}

// commitLeftovers commits everything the program left uncommitted in its
// folder onto its branch, in one commit whose subject is the run's title and
// whose body is result, and answers git's line when it would not go.
//
// IT IS THE PROGRAM'S FOLDER, SO IT IS ALL OF IT. The checkout was clean when
// the branch was cut ([programCheckoutInTheWay]), and nothing else of codeaf's
// writes there while the run holds it ([programHoldGuard]), so everything in
// it now that is not committed is the run's. It is only ever asked of a run
// whose end this process saw: a run whose process went away is settled
// without a commit ([ProgramFolder.settleGone]).
//
// THE NOTES ARE TAKEN BACK OUT OF THE INDEX, NOT LEFT OUT OF THE ADD. A
// pathspec that excludes `.senior-dev` makes `git add` exit 1 whenever that
// folder is there and ignored — and senior-dev ignores it in every repository
// it works in — so a notes folder that was there before the run, or would not
// move, failed every finishing commit. The whole folder is staged and the
// notes' own path reset to what HEAD holds, which git does whatever its
// ignore rules say, the way [sealGroundWork] does it.
//
// A CHECKOUT IN THE MIDDLE OF A MERGE IS NOT COMMITTED. The program's shell can
// start one, and a commit now would conclude it, conflict markers and all,
// under codeaf's name; the work is left as it is and the ending says why.
func (f *ProgramFolder) commitLeftovers(result string) string {
	if half := halfDone(f.Dir); half != "" {
		return f.Dir + " is in the middle of a " + half
	}
	if out, err := git(f.Dir, "add", "-A", "--", "."); err != nil {
		return "git add: " + firstLine(out)
	}
	if f.Notes != "" {
		if out, err := git(f.Dir, "reset", "-q", "--", f.Notes); err != nil {
			return "git reset: " + firstLine(out)
		}
	}
	if _, err := git(f.Dir, "diff", "--cached", "--quiet"); err == nil {
		return ""
	}
	message := clip(firstLine(f.Title), 72)
	if strings.TrimSpace(message) == "" {
		// A run a shell started with no brief, on a command of its own, has no
		// title, and git takes no commit without a subject.
		message = f.Program + "'s work"
	}
	if result = strings.TrimSpace(result); result != "" {
		message += "\n\n" + result
	}
	args := append([]string{"-c", "commit.gpgsign=false"}, codeafGitIdentity()...)
	args = append(args, "commit", "-q", "--no-verify", "-m", signed(message, f.Sign))
	if out, err := git(f.Dir, args...); err != nil {
		return "git commit: " + firstLine(out)
	}
	return ""
}

// goBack checks out the person's own branch again (or the commit their
// checkout was on) and deletes the program's empty branch, answering git's
// line when either would not go.
//
// A SWITCH THAT FAILED AND STILL ARRIVED IS AN ARRIVAL. The checkout is read
// again after a failure ([ProgramFolder.onHome]), so a switch git reported
// badly after it had moved HEAD goes on to delete the empty branch rather
// than telling the person their folder could not be put back while it was.
func (f *ProgramFolder) goBack() string {
	back := append(switchWithoutHooks(), f.Home)
	if f.Home == "" {
		back = append(switchWithoutHooks(), "--detach", f.Start)
	}
	if out, err := git(f.Dir, back...); err != nil && !f.onHome() {
		return firstLine(out)
	}
	if out, err := git(f.Dir, "branch", "-q", "-D", f.Branch); err != nil {
		return firstLine(out)
	}
	return ""
}

// keepNotes moves the program's notes folder out of the folder it worked in
// and into the run's record folder, and answers the sentence that says where
// they went ("" when nothing moved).
//
// THE PERSON'S FOLDER GETS BACK ONLY THE WORK. A senior-dev run left 46 files
// in `.senior-dev/` — its session database and its whole conversation with its
// model among them — where `git add -A` would commit every one; and the next
// run in the same folder read the last one's checklist and pinned command as
// its own. A notes folder that was there when the run began is left alone,
// because it is not this run's alone. A move across disks falls back to a
// copy and then a removal, and a move that fails leaves the folder whole.
func (f *ProgramFolder) keepNotes() string {
	if f.Notes == "" || f.NotesWereThere || strings.TrimSpace(f.Keep) == "" {
		return ""
	}
	from := filepath.Join(f.Dir, f.Notes)
	if info, err := os.Lstat(from); err != nil || !info.IsDir() {
		return ""
	}
	if err := os.MkdirAll(f.Keep, 0o700); err != nil {
		return ""
	}
	to := filepath.Join(f.Keep, f.Program)
	for n := 1; ; n++ {
		if _, err := os.Lstat(to); os.IsNotExist(err) {
			break
		}
		to = filepath.Join(f.Keep, fmt.Sprintf("%s.%d", f.Program, n))
	}
	if err := os.Rename(from, to); err != nil {
		if err := copyPath(from, to); err != nil {
			_ = os.RemoveAll(to)
			return "its notes (" + f.Notes + "/) could not be moved out of " + f.Dir + ": " + err.Error()
		}
		_ = os.RemoveAll(from)
	}
	return "its notes (" + f.Notes + "/) are kept in " + to
}

// abandon lets a folder go that a run was readied in and then never started:
// the branch it cut, which holds nothing, deleted and the person's own checked
// out again. A nil folder is a run that readied none.
func (f *ProgramFolder) abandon() {
	if f != nil {
		f.Finish("")
	}
}

// tree is the folder as a run's tree: the folder itself, worked in where it
// is, with the program's branch and where the person's checkout was, which the
// run's row writes down ([runCopyOf]). Zero for a nil folder.
func (f *ProgramFolder) tree() taskTree {
	if f == nil {
		return taskTree{}
	}
	tree := taskTree{dir: f.Dir, merge: mergeInPlace, ground: f.Dir, mode: TaskModeInPlace, rung: GroundRungHere}
	if f.Branch != "" {
		tree.root, tree.branch, tree.home, tree.homeSha = f.Dir, f.Branch, f.Home, f.Start
	}
	return tree
}

// StopPromise is what a person who stops a program's run is told at once
// about where its work will be.
func (f *ProgramFolder) StopPromise() string {
	if f.Plain() {
		return "its work so far stays in " + f.Dir
	}
	return "its work so far stays on its branch " + f.Branch + ", checked out in " + f.Dir
}

// Sentence is how a run left its folder, in the one sentence the run's page,
// the conversation and a shell run's last lines all say: where the work is,
// how much of it, that its branch is checked out, and how to go back to the
// person's own branch and bring the work in.
func (e ProgramFolderEnd) Sentence() string {
	f := e.Folder
	var said string
	switch {
	case e.Gone && f.Branch == "" && f.Outer != "":
		said = "its work so far is in " + f.Dir + ", as it left it; the git repository around it is at " + f.Outer +
			", which holds your home folder, so codeaf cut no branch there and committed nothing"
	case e.Gone && f.Branch == "":
		said = "its work so far is in " + f.Dir + ", which has no git history, as it left it"
	case f.Branch == "" && f.Outer != "":
		said = "its work is in " + f.Dir + "; the git repository around it is at " + f.Outer +
			", which holds your home folder, so codeaf cut no branch there and committed nothing"
	case f.Branch == "":
		said = "its work is in " + f.Dir + ", which has no git history, so nothing was committed"
	case e.Moved:
		where := "the branch " + e.HeadOn
		if e.HeadOn == "" {
			where = "no branch, at " + e.At
		}
		said = f.Program + " left " + f.Dir + " on " + where + " instead of its own branch " + f.Branch +
			", so codeaf changed nothing there: nothing was committed and nothing was switched"
		if e.Kept {
			said += "; " + f.Branch + " holds " + fileCount(len(e.Changed))
		}
	case e.Gone:
		said = e.goneWords()
	case e.Dropped:
		said = "it changed nothing, so " + f.Dir + " is back on " + f.homeWords() + " and its branch " + f.Branch + " was deleted"
	case e.HomeMoved && !e.Kept && e.Refused == "":
		said = "it changed nothing, but " + e.homeMovedWords() + ", so codeaf did not switch back to it: its empty branch " +
			f.Branch + " is still checked out in " + f.Dir
	case e.Refused != "" && !e.Kept:
		said = "it changed nothing, but " + f.Dir + " could not be put back on " + f.homeWords() + " (" + e.Refused +
			"), so its empty branch " + f.Branch + " is still checked out there"
	case e.Refused != "":
		said = "its branch " + f.Branch + " is checked out in " + f.Dir + ", but what it left uncommitted could not be committed (" +
			e.Refused + "), so those changes are in the folder, uncommitted; " + e.goBackWords()
	default:
		said = "its work is on the branch " + f.Branch + " in " + f.Dir + ", " + fileCount(len(e.Changed)) +
			", and that branch is checked out there; " + e.goBackWords()
	}
	if e.Notes != "" {
		said += "; " + e.Notes
	}
	return said
}

// goneWords is where a run whose process went away left its work in a
// repository, still on its own branch ([ProgramFolder.settleGone]): the branch,
// that it is checked out as the run left it, how many files are not committed,
// and the way back — which, while something is uncommitted, starts with
// putting that somewhere, because a switch would carry it along.
func (e ProgramFolderEnd) goneWords() string {
	f := e.Folder
	said := "its work so far is on its branch " + f.Branch + " in " + f.Dir + ", which is checked out there, as it left it"
	if e.Uncommitted == 0 {
		return said + "; " + e.goBackWords()
	}
	said += ", with " + fileCount(e.Uncommitted) + " not committed"
	if e.HomeMoved {
		said += "; " + e.homeMovedWords()
	}
	return said + "; commit or stash them there before you go back to " + f.homeWords()
}

// homeWords names where the person's checkout was before the run.
func (f ProgramFolder) homeWords() string {
	if f.Home != "" {
		return "your branch " + f.Home
	}
	return "the commit " + shortSha(f.Start)
}

// goBackWords is the two commands a person holding a program's finished
// branch wants: the one that goes back to their own branch, and the one that
// brings the work in from there. THE FOLDER IS QUOTED FOR A SHELL the way
// every path this package hands one is ([shellQuoted]).
//
// AND IT SAYS "AS IT WAS" ONLY WHEN IT IS ([ProgramFolder.homeMoved]): a branch
// of the person's that moved during the run is said to have moved, from where
// to where, before anybody is told how to merge onto it.
func (e ProgramFolderEnd) goBackWords() string {
	f := e.Folder
	folder := shellQuoted(f.Dir)
	if f.Home == "" {
		return "your checkout was on no branch, at " + shortSha(f.Start) + ", and `git -C " + folder +
			" switch --detach " + shortSha(f.Start) + "` goes back to it"
	}
	back := "`git -C " + folder + " switch " + f.Home + "` goes back to it, and `git -C " + folder + " merge " +
		f.Branch + "` from there brings the work in"
	switch {
	case e.HomeMoved && e.HomeAt == "":
		return e.homeMovedWords()
	case e.HomeMoved:
		return e.homeMovedWords() + ", and codeaf did not move it: look at it before you push or merge it; " + back
	}
	return "your branch " + f.Home + " is as it was: " + back
}

// homeMovedWords says how the person's own branch moved during the run
// ([ProgramFolderEnd.HomeMoved]).
func (e ProgramFolderEnd) homeMovedWords() string {
	f := e.Folder
	if e.HomeAt == "" {
		return "your branch " + f.Home + " is gone: it was at " + shortSha(f.Start) +
			" when the run began, and codeaf did not make it again"
	}
	return "your branch " + f.Home + " moved during the run, from " + shortSha(f.Start) + " to " + shortSha(e.HomeAt)
}

// landing is a finished folder as the run's landing: the program's branch
// when it holds the work, the files, and the sentence ([RunLanding.Line]).
func (e ProgramFolderEnd) landing() RunLanding {
	landing := RunLanding{Changed: e.Changed, Home: mergeInPlace, Line: e.Sentence()}
	if e.Kept {
		landing.Branch, landing.Home = e.Folder.Branch, mergeKept
	}
	return landing
}

// shortSha is a commit as a person reads it.
func shortSha(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// shortCommit is the commit a ref names, as a person reads it.
func shortCommit(dir, ref string) string {
	out, err := git(dir, "rev-parse", "--verify", "-q", ref)
	if err != nil {
		return ""
	}
	return shortSha(strings.TrimSpace(out))
}

// changedSince is every path HEAD's tree differs from a commit in, empty when
// either cannot be read: the work a program's branch holds past its start.
func changedSince(dir, sha string) []string {
	return changedBetween(dir, sha, "HEAD")
}

// changedBetween is every path two commits' trees differ in.
func changedBetween(dir, from, to string) []string {
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		return nil
	}
	out, err := git(dir, "diff", "--name-only", from, to)
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

// claimProgramFolder takes the hold on one folder for a program's run and
// writes holder into it, so a second run is told whose it is. It answers the
// held lock, or the holder of a lock somebody else has; a nil lock with no
// holder is a filesystem that takes no locks, and the run goes ahead unheld,
// which is what every run did before the hold existed.
//
// flock DIES WITH ITS PROCESS, however it dies, so a crashed codeaf leaves no
// hold behind to be broken by hand; and it is per open file, so two
// conversations in one engine exclude each other exactly as two windows do.
func claimProgramFolder(key, holder string) (*os.File, string) {
	directory := home.Join("v3", programFolderDir)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, ""
	}
	path := filepath.Join(directory, programFolderName(key)+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, ""
	}
	if err := filelock.Lock(file, true, true); err != nil {
		_ = file.Close()
		if !isLockHeld(err) {
			return nil, ""
		}
		held, _ := os.ReadFile(path)
		if said := strings.TrimSpace(string(held)); said != "" {
			return nil, said
		}
		return nil, "another program run"
	}
	_ = file.Truncate(0)
	_, _ = file.WriteAt([]byte(holder), 0)
	return file, ""
}

// programFolderHolder is who holds a folder now, "" when nobody does: the
// hold asked for and let go at once, for a door that only wants to know.
func programFolderHolder(key string) string {
	lock, holder := claimProgramFolder(key, "")
	if lock != nil {
		_ = filelock.Unlock(lock)
		_ = lock.Close()
	}
	return holder
}

// release lets the folder go.
func (f *ProgramFolder) release() {
	if f.lock == nil {
		return
	}
	_ = filelock.Unlock(f.lock)
	_ = f.lock.Close()
	f.lock = nil
}

// programFolderName is one folder's name under [programFolderDir]: the head
// of the SHA-256 of its resolved path, the way a repository's git lock is
// named ([gitRootLockFile]).
func programFolderName(key string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(key)))
	return hex.EncodeToString(digest[:])[:gitRootLockStem]
}

// programFolderRecord is where one folder's run is written down.
func programFolderRecord(key string) string {
	return filepath.Join(home.Join("v3", programFolderDir), programFolderName(key)+".json")
}

// write keeps the record, whole, beside the hold. It is a record, so a disk
// that refuses it costs a later process its ending and never the run.
func (f *ProgramFolder) write() {
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	path := programFolderRecord(f.key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, body, 0o600); err != nil {
		return
	}
	_ = os.Rename(temporary, path)
}

// readProgramFolder is the record of the last run in one folder.
func readProgramFolder(key string) (*ProgramFolder, bool) {
	return readProgramFolderAt(programFolderRecord(key))
}

func readProgramFolderAt(path string) (*ProgramFolder, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var folder ProgramFolder
	if json.Unmarshal(body, &folder) != nil || strings.TrimSpace(folder.Dir) == "" {
		return nil, false
	}
	folder.key = canonicalPath(folder.Dir)
	return &folder, true
}

// settleOwedProgramFolder settles the folder of the run whose record folder
// is keep, when that run's process went away before it could finish it: the
// reopen of its conversation, or the next hand-off in it, comes here
// ([endOrphanedProgramRun]). It answers how the folder was left, and false
// when nothing was owed or somebody else holds the folder now.
func settleOwedProgramFolder(keep string) (ProgramFolderEnd, bool) {
	if strings.TrimSpace(keep) == "" {
		return ProgramFolderEnd{}, false
	}
	records, _ := filepath.Glob(filepath.Join(home.Join("v3", programFolderDir), "*.json"))
	for _, path := range records {
		owed, ok := readProgramFolderAt(path)
		if !ok || owed.Ended != "" || filepath.Clean(owed.Keep) != filepath.Clean(keep) {
			continue
		}
		lock, holder := claimProgramFolder(owed.key, owed.Program+", settling a run codeaf closed under")
		if holder != "" || lock == nil {
			// A HOLD SOMEBODY ELSE HAS, or one nobody can take, is a folder this
			// reopen cannot know is idle: it is left for the next codeaf that can.
			return ProgramFolderEnd{}, false
		}
		owed.lock = lock
		// NOTHING IS COMMITTED FOR A RUN WHOSE END NOBODY SAW
		// ([ProgramFolder.settleGone]): the folder has been the person's since
		// the process went away, however long ago that was.
		end := owed.settleGone()
		owed.Ended = end.Sentence()
		owed.write()
		owed.release()
		return end, true
	}
	return ProgramFolderEnd{}, false
}

// taskBranchName is the branch a task's work is cut on: `task/`, the title as
// a branch name can spell it, and a short random tail, so the same work
// proposed twice lands on two branches. ONE SPELLING FOR EVERY ROAD that cuts
// one — a task's own worktree ([cutTaskWorktree]) and a program's branch in
// the person's folder ([PrepareProgramFolder]) — so a person reading `git
// branch` meets one shape.
func taskBranchName(title string) string {
	return "task/" + slugify(title) + "-" + shortID()
}
