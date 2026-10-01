package session

// A PROGRAM WORKS IN THE FOLDER IT IS GIVEN, AND IN A COPY OF ITS OWN ON A
// BRANCH OF ITS OWN WHEN THAT FOLDER IS A GIT REPOSITORY.
//
// THE CONTRACT. This is the whole of what codeaf does to the folder a program
// that edits files works in (senior-dev first), for a run a conversation hands
// off and for one a person starts at a shell alike; senior-dev.md and
// delegates.md say it in a person's words.
//
//  1. WHICH FOLDER. The folder the proposal names as `ground`, or the
//     conversation's own when it names none (a typed `/senior-dev` names none),
//     or the one a shell run was started in or named with `--dir`. Inside a git
//     repository it is the repository's root unless git ignores the asked-for
//     folder, which runs as a plain folder. It is never the home folder or a folder holding it
//     ([programHomeRefusal]). A folder that is not there yet is made, empty,
//     when the folder it would be made in is there.
//  2. A GIT REPOSITORY — history, a commit, and a root below the home folder.
//     The program works in a copy of its own: a git worktree in a private
//     folder under this account's cache folder, on a branch of its own cut
//     from the commit the person's checkout stands on, removed when the
//     program exits so the branch is released and kept. THE PERSON'S CHECKOUT IS NEVER TOUCHED,
//     so it is never refused for its uncommitted changes either, and several
//     runs may work on one repository at once. programcopy.go is the whole of
//     it.
//  3. ANYTHING ELSE — no history, no commit yet, or a repository whose root is
//     the home folder or above it: the program works in the folder as it is,
//     started with its own flags for that ([delegate.Delegate.PlainFolder];
//     senior-dev's `--in-place`). codeaf passes them whenever it decided so,
//     because the program's own reading of a folder climbs to any repository
//     around it.
//  4. WHEN IT ENDS — done, not finished, stopped, crashed, or its process gone
//     — in a repository, what the program left uncommitted in its copy, except
//     paths ignored at start and known test droppings, is committed onto its
//     branch in one commit (the task's title, the result under it), and the
//     copy is removed (programcopy.go) — unless what it left could be kept
//     nowhere else, when the copy stays and the ending says where. In either kind of folder the program's
//     notes ([delegate.Delegate.Notes]) are moved into the run's record folder
//     unless they were there before the run.
//  5. ONE RUN PER PLAIN FOLDER. codeaf starts and stops the run and keeps its
//     money, its time and its screen, and nothing else. A second program run
//     on a plain folder one is working in, or on a folder inside it or around
//     it — from any conversation, any window, or a shell — is refused, naming
//     the run that holds it; the hold is a file lock, which dies with the
//     last process holding it — codeaf and the program it hands it to
//     (programhold.go). A run in a copy holds only its
//     copy, which is why runs on one repository never wait for each other.
//
// WHY THERE IS SO LITTLE HERE. Until 2026-09-24 a program ran through the
// general task machinery: a copy of the folder cut for every run, the brief's
// paths rewritten to name the copy, the program's commits squashed and its
// HEAD put back at the landing, and a ladder of placement rules, each layer
// patching the one before it. The owner asked why it was so hard to have
// senior-dev just work on the problem — "if it's in a git repo, great - if
// not, just do it" — and the answer was that codeaf had made it hard. The
// copy, the rewriting, the squash and the ladder went. Until 2026-09-28 the
// program then worked in the person's checkout itself, switched onto its
// branch, which held the whole repository for one run at a time; the owner
// asked for runs side by side, and the copy came back as a worktree the
// program's run owns start to end — with no squash, no landing and no
// rewriting, only a line at the head of the brief ([ProgramFolder.BriefNote]).
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
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
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
	// when the run ends, how the folder was left is written there
	// ([programFolderEndFile]), and it is the name a reopen finds the run's
	// folder by ([settleOwedProgramFolder]).
	Keep string
	// Instead is what a folder that is the home folder is answered with,
	// after the refusal itself ([programHomeRefusal]).
	Instead string
	// Place is the conversation's session folder, which says where the
	// repository's git lock lives ([lockGitRoot]); zero for a shell run,
	// which takes none.
	Place Place
	// Carry is the branch an earlier run of the same line left its work on,
	// which this run carries on in its copy ([programCarryOf]); nil for a
	// line's first run and for every shell run.
	Carry *programCarry
	// SignModel is the model the attribution line on the commit that finishes
	// the run names, and "" for the line that names none: codeaf signs every
	// commit it writes, and the only choice is whether the model is named
	// ([gitSignature]).
	SignModel string
}

// ProgramFolder is one program run's folder as [PrepareProgramFolder] readied
// it. It is also the record a later process settles the run's folder from
// when the process that started it went away first
// ([settleOwedProgramFolder]), which is why its fields are written down.
type ProgramFolder struct {
	// Program is the program's name, and Title is the run's.
	Program string `json:"program"`
	Title   string `json:"title"`
	// Dir is the folder the program works in: its copy for a run in a
	// repository, and otherwise the folder itself, spelled the way the door
	// asked for it.
	Dir string `json:"dir"`
	// Repo is the person's repository a run's copy was cut from, empty for a
	// folder worked in itself ([ProgramFolder.Copied]). Linked is the ignored
	// names linked into the copy from it ([programCopyLinks]), and LeftBehind
	// the paths its checkout had not committed when the copy was cut, which
	// the copy does not have ([ProgramFolder.LeftBehindWords]).
	Repo   string   `json:"repo,omitempty"`
	Linked []string `json:"linked,omitempty"`
	// Carried is the ignored names put into the copy as its own — a file
	// copied, a folder cloned — rather than linked ([carryOne]).
	Carried    []string `json:"carried,omitempty"`
	LeftBehind []string `json:"leftBehind,omitempty"`
	// Untracked names the person's untracked files copied into the copy as
	// its inputs, and Inputs is each one's fingerprint as it was copied
	// (gitidentity.Inputs): an input the run leaves as it was stays out of
	// every commit, and one it changes is its work and goes on its branch.
	Untracked []string           `json:"untracked,omitempty"`
	Inputs    gitidentity.Inputs `json:"inputs,omitempty"`
	// Snapshot is the commit that carries those uncommitted changes into the
	// copy, the first on the program's branch, whose parent is Start; empty
	// when there were none, or when they could not be carried, which
	// LeftBehindWhy then says ([ProgramFolder.snapshotLeftBehind]). A run that
	// carries on an earlier run's branch carries its snapshot too, so the work
	// it counts is the programs' and never the person's.
	Snapshot      string `json:"snapshot,omitempty"`
	LeftBehindWhy string `json:"leftBehindWhy,omitempty"`
	// Branch is the program's own branch, cut by codeaf; empty for a folder the
	// program works in without git. Home is the branch the person had checked
	// out, empty when their checkout was on no branch, and Start is the commit
	// it stood on, which the branch was cut from.
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
	// Keep is the run's record folder ([ProgramFolderOrder.Keep]) and
	// SignModel the model its attribution line names
	// ([ProgramFolderOrder.SignModel]).
	Keep      string `json:"keep,omitempty"`
	SignModel string `json:"signModel,omitempty"`
	// NoAttribution is true when the run had no answered model call, so its
	// finishing commit does not credit a model that did no work in this run.
	NoAttribution bool `json:"noAttribution,omitempty"`
	// message is the commit message the program wrote for the commit that
	// ends its run, read from its notes just before they are moved
	// ([ProgramFolder.readCommitMessage]); never written to the record.
	message string
	// Ended is the sentence the run's folder was finished with. Empty is a
	// folder still owed its ending.
	Ended string `json:"ended,omitempty"`
	// Continues says this run carries on on the branch an earlier run of the
	// same line left its work on, rather than cutting one of its own
	// ([ProgramFolderOrder.Carry]): Branch, Home and Start are that line's, so
	// what the earlier runs did is never counted as this one's nothing.
	Continues bool `json:"continues,omitempty"`
	// From is the branch this run's own was cut from when that is an earlier
	// run's rather than the person's checkout: a run of the same line after one
	// whose work passed starts on top of that work, on a new branch, so the
	// passed branch holds that run's work and nothing after it
	// ([programCarry.Fresh]). Start is then that branch's tip, so the files the
	// ending counts are this run's own.
	From string `json:"from,omitempty"`
	// ResumedAt is the commit the branch a run carries on stood at when this
	// run began ([ProgramFolder.Continues]): the earlier runs' work, and
	// whatever the branch was given between the runs — a rebase onto newer
	// history for its pull request among them. The files the ending counts are
	// measured from it, so they are this run's own ([ProgramFolder.ownBase]).
	// Empty for every other run, and in a record an older build wrote.
	ResumedAt string `json:"resumedAt,omitempty"`
	// IgnoredAtStart keeps paths git ignored before the run changed its rules,
	// together with the person's untracked inputs copied into the worktree.
	IgnoredAtStart []string `json:"ignoredAtStart,omitempty"`
	// IgnoredOuter is the enclosing repository when Dir itself is ignored by it.
	IgnoredOuter string `json:"ignoredOuter,omitempty"`

	key   string
	place Place
	lock  *os.File
}

// Plain says the program works in its folder without git.
func (f *ProgramFolder) Plain() bool { return f == nil || f.Branch == "" }

// IgnoredFile is the run's start-time ignore list, kept outside the repository
// so the child's recorder still keeps those paths out of its trees after the
// run changes .gitignore.
func (f *ProgramFolder) IgnoredFile() string {
	if f == nil || f.Keep == "" {
		return ""
	}
	return filepath.Join(f.Keep, "ignored-at-start")
}

// InputsFile is the run's list of its copy's inputs and their fingerprints
// ([ProgramFolder.Inputs]), kept beside [ProgramFolder.IgnoredFile] for the
// child's recorder to read (gitidentity.InputsEnv); "" when there is none.
func (f *ProgramFolder) InputsFile() string {
	if f == nil || f.Keep == "" || len(f.Inputs) == 0 {
		return ""
	}
	return filepath.Join(f.Keep, "inputs-at-start")
}

// PrepareProgramFolder readies the folder a program was asked to work in, per
// the contract at the top of this file, and holds it for the run: the folder
// resolved and made when it must be, the hold taken, a run that went away in
// it settled first, and in a repository the checkout read and the program's
// branch cut. The refusal is a sentence a person can act on, and nothing of
// the person's has been changed when there is one.
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
		Notes: order.Program.Notes, Keep: order.Keep, SignModel: order.SignModel,
		key: canonicalPath(dir), place: order.Place,
	}
	if repo {
		return prepareProgramCopy(order, folder)
	}
	lock, hold, busy := claimProgramFolder(folder.key, order.Program.Name+", "+order.Holder)
	if busy {
		return nil, errors.New(programFolderBusy(dir, hold))
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
	if owed, ok := readProgramFolder(folder.key); ok && owed.Ended == "" && lock != nil {
		owed.key = folder.key
		end := owed.settleGone()
		owed.Ended = end.Sentence()
		owed.write()
		end.keepEnding()
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
	// A PLAIN FOLDER HAS NO GIT IGNORE RULES, but both launch roads hand the
	// child this path. An empty, readable list keeps the child's
	// unreadable-list safety rule intact without aborting a plain run.
	if err := folder.writeIgnoredAtStart(nil); err != nil {
		folder.release()
		return nil, err
	}
	if outer != "" && !holdsHomeFolder(outer) {
		folder.IgnoredOuter = outer
		folder.Outer = ""
	}
	folder.write()
	return folder, nil
}

// writeIgnoredAtStart makes the child's frozen safety list before either road
// can launch it. The empty file in a plain folder means no git rules existed.
func (f *ProgramFolder) writeIgnoredAtStart(body []byte) error {
	path := f.IgnoredFile()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(f.Keep, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
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
	// A FOLDER GIT IGNORES IS ITS OWN PLAIN WORKSPACE. Widening it to the
	// repository would cut an empty branch and call the run's real files no work.
	if relative, err := filepath.Rel(root, canonicalPath(asked)); err == nil {
		if _, ignored := git(root, "check-ignore", "-q", "--no-index", "--", filepath.ToSlash(relative)); ignored == nil {
			return asked, false, root, ""
		}
	}
	return root, true, "", ""
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

// ProgramFolderEnd is how a program's run left its folder, as
// [ProgramFolder.Finish] found it and made it.
type ProgramFolderEnd struct {
	Folder ProgramFolder
	// Changed is every path the program's branch changed from where this run
	// started ([ProgramFolder.ownBase]).
	Changed []string
	// Kept says the program's branch holds its work: for a run that carries on
	// an earlier run's branch, the line's work, so a run that adds nothing
	// still lands on the branch that holds it. Added says this run changed the
	// branch's tree from where it found it, which for every other run is Kept.
	Kept  bool
	Added bool
	// Upstream is the remote branch the program's branch tracks, as
	// `<remote>/<branch>` — set only for a live remote branch of its own name
	// on a plainly named remote — and UpstreamRemote and UpstreamRef its two
	// halves. Such a branch is brought in by pushing it,
	// not by merging it into the person's checkout ([ProgramFolderEnd.mergeWords]).
	Upstream       string
	UpstreamRemote string
	UpstreamRef    string
	// SnapshotHeld says a branch a run carries on still holds the change the
	// line's first run carried the person's uncommitted changes in with
	// ([ProgramFolder.Snapshot]) — that commit, or the one a rebase since wrote
	// in its place ([branchHoldsChange]). A run that cut its own branch begins
	// with that commit, and is not asked.
	SnapshotHeld bool
	// Dropped says the program's branch was deleted because it holds
	// nothing: for a run in a copy, one readied and never started
	// ([ProgramFolder.abandon]); for a run in the person's checkout itself,
	// written by a build before programs worked in copies and still read back
	// from its record, one that changed nothing and was switched back.
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
	// itself, so a later codeaf settled its folder ([ProgramFolder.settleGone]):
	// a copy finished as any ending finishes it, a folder the person works in
	// read and never written. Uncommitted is how many files are not committed
	// in a checkout left on or moved off the task branch.
	Gone        bool
	Uncommitted int
	// Committed says codeaf made the commit that finishes the run: there was
	// something left to commit, and it went. A run whose copy held nothing
	// more — or whose copy was gone — has only the commits it made itself.
	Committed bool
	// Patch is where what a program left in its copy was kept when it could
	// not be committed on its branch ([programLeftoversFile]), and CopyLeft
	// the copy itself when it is still on disk; both empty otherwise. CopyKept
	// is why a copy was left on purpose — what is in it could be neither
	// committed nor kept anywhere else — and empty for one that would not go.
	Patch    string
	CopyLeft string
	CopyKept string
	// Frozen is the copy's own refs that held something its branch does not,
	// each put on a branch of its own before the copy went
	// ([ProgramFolder.keepOwnRefs]).
	Frozen []programKeptRef
	// Saved is the branch codeaf put on commits the program made on a
	// detached HEAD in its copy, which nothing else would have kept
	// ([ProgramFolder.keepDetached]).
	Saved string
	// Refused is git's own line when what the program left could not be
	// committed, or the checkout could not be put back.
	Refused string
	// Notes is where the program's notes went, as a sentence.
	Notes string

	// said is the sentence a run's folder was ended with, read back from its
	// record folder by a process that did not end it ([keptProgramFolderEnd]);
	// [ProgramFolderEnd.Sentence] answers it as it was said.
	said string
}

// programFolderEndFile is the file in a run's own record folder that says how
// its folder was left: the branch, whether it holds the work, the files, and
// the sentence.
//
// IT IS WRITTEN WHERE THE RUN'S RECORD LIVES, NOT ONLY BESIDE THE HOLD. The
// record beside the hold is one folder's, and the next run in that folder
// writes over it; and a run whose folder was ended by one process can have its
// row settled by another — a codeaf that closed after the ending and before
// the row. Either way the conversation that reopens the run finds its ending
// here, and its row names the branch and where the work went instead of
// staying `interrupted` ([Agent.settleInterruptedProgramRow]).
const programFolderEndFile = "program-folder.json"

// programFolderEnding is what [programFolderEndFile] holds.
type programFolderEnding struct {
	Branch  string   `json:"branch,omitempty"`
	Kept    bool     `json:"kept,omitempty"`
	Changed []string `json:"changed,omitempty"`
	Said    string   `json:"said"`
}

// keepEnding writes how the run's folder was left into the run's record
// folder ([programFolderEndFile]). It is a record, so a disk that refuses it
// costs a later reopen its sentence and never the run.
func (e ProgramFolderEnd) keepEnding() {
	keep := strings.TrimSpace(e.Folder.Keep)
	if keep == "" {
		return
	}
	body, err := json.MarshalIndent(programFolderEnding{Branch: e.Folder.Branch, Kept: e.Kept, Changed: e.Changed, Said: e.Sentence()}, "", "  ")
	if err != nil || os.MkdirAll(keep, 0o700) != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(keep, programFolderEndFile), body, 0o600)
}

// keptProgramFolderEnd is how the run whose record folder is keep left its
// folder, as it wrote it there ([ProgramFolderEnd.keepEnding]); false when it
// wrote nothing.
func keptProgramFolderEnd(keep string) (ProgramFolderEnd, bool) {
	body, err := os.ReadFile(filepath.Join(keep, programFolderEndFile))
	if err != nil {
		return ProgramFolderEnd{}, false
	}
	var ending programFolderEnding
	if json.Unmarshal(body, &ending) != nil || strings.TrimSpace(ending.Said) == "" {
		return ProgramFolderEnd{}, false
	}
	return ProgramFolderEnd{Folder: ProgramFolder{Branch: ending.Branch, Keep: keep}, Kept: ending.Kept, Changed: ending.Changed, said: ending.Said}, true
}

// Finish ends a program's run in its folder, per the fourth point of the
// contract at the top of this file, and lets the folder go. result is the
// run's ending in words, the body of the commit that holds what the program
// left. It answers what it found and did.
func (f *ProgramFolder) Finish(result string) ProgramFolderEnd {
	end := f.settle(result)
	f.Ended = end.Sentence()
	end.keepEnding()
	if f.Copied() {
		f.releaseCopy()
		return end
	}
	f.write()
	f.release()
	return end
}

// settle is [ProgramFolder.Finish] without the record and the hold, which the
// caller owns: a copy finished (programcopy.go), or a plain folder's notes
// moved, which is all of git a plain folder has.
func (f *ProgramFolder) settle(result string) ProgramFolderEnd {
	if f.Copied() {
		return f.settleCopy(result, false)
	}
	end := ProgramFolderEnd{Folder: *f}
	end.Notes = f.keepNotes()
	return end
}

// settleGone settles the folder of a run whose process went away before it
// could end the run itself — a crash, a kill, codeaf closed. A copy is
// finished as though the run had ended ([ProgramFolder.settleCopy]), because
// nothing but the run's own work can be in it, and its finishing commit
// credits only the models its record says answered, read the way an ending
// codeaf saw reads them ([SetProgramAnswerAttribution]). A folder the person
// works in itself WRITES NOTHING TO GIT: no add, no commit, no switch, no
// branch deleted. It reads
// where the checkout is, what the program's branch holds and how many files
// are not committed, moves the program's notes into the run's record folder,
// and answers the ending that says so ([ProgramFolderEnd.Gone]).
//
// IN A FOLDER THE PERSON WORKS IN, ONLY AN END CODEAF SAW IS FINISHED WITH A
// COMMIT. Once the process that held the folder is gone, the folder is the
// person's again, and what is
// uncommitted in it may be the run's last edits or their own made on its
// branch since — codeaf cannot tell the two apart. A commit here once swept a
// person's day of edits, and a merge they were resolving, into a commit under
// codeaf's name with their hooks skipped. The read is made with git's optional
// locks off, so not even the index is refreshed.
func (f *ProgramFolder) settleGone() ProgramFolderEnd {
	if f.Copied() {
		_ = SetProgramAnswerAttribution(f, f.SignModel != "")
		return f.settleCopy("", true)
	}
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
	// A vanished worker can leave loose files on a checkout it moved off the
	// task branch too. The moved ending needs the same count as a seen end.
	end.Uncommitted = uncommittedCount(f.Dir, f.Notes)
	end.HomeMoved, end.HomeAt = f.homeMoved()
	return end
}

// uncommittedCount is how many files in a checkout are not committed, the
// program's notes left out, read without taking or writing any of git's locks;
// zero when git cannot say. It only counts, for a sentence: a copy's settling
// asks [uncommittedList], which never reads "cannot say" as "nothing".
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
// TRACKED WORK IS COMMITTED AT THE START, but the copied untracked inputs are
// not, and ignore rules can change during the run. An input the run left as it
// was, paths ignored at the start and known test droppings are never staged by
// this finishing commit; an input it changed is its work and is. The notes are excluded for the same reason:
// a program's private record must not enter the person's branch. It is only
// ever made in a program's copy: a folder the person works in is never
// committed by codeaf ([ProgramFolder.settleGone]).
//
// A CHECKOUT IN THE MIDDLE OF A MERGE IS NOT COMMITTED. The program's shell can
// start one, and a commit now would conclude it, conflict markers and all,
// under codeaf's name; the work is left as it is and the ending says why.
func (f *ProgramFolder) commitLeftovers(result string) string {
	if half := halfDone(f.Dir); half != "" {
		return f.Dir + " is in the middle of a " + half
	}
	// Stage named paths only, literally. A blanket add would put an initially
	// ignored secret into the index when the run rewrote .gitignore, and a
	// patterned pathspec would write an untouched input's blob before a reset.
	var toAdd []string
	for _, args := range [][]string{
		{"diff", "HEAD", "--name-only", "--no-renames", "-z", "--"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		out, err := git(f.Dir, args...)
		if err != nil {
			return "git list changes: " + firstLine(out)
		}
		for _, path := range strings.Split(strings.TrimSuffix(out, "\x00"), "\x00") {
			if path != "" && !f.excludedFromCommit(path) {
				toAdd = append(toAdd, path)
			}
		}
	}
	if len(toAdd) > 0 {
		if out, err := git(f.Dir, append([]string{"--literal-pathspecs", "add", "-A", "--"}, toAdd...)...); err != nil {
			return "git add: " + firstLine(out)
		}
	}
	staged, err := git(f.Dir, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return "git diff: " + firstLine(staged)
	}
	for _, path := range strings.Split(strings.TrimSuffix(staged, "\x00"), "\x00") {
		if path == "" || !f.excludedFromCommit(path) {
			continue
		}
		if out, err := git(f.Dir, "--literal-pathspecs", "reset", "-q", "--", path); err != nil {
			return "git reset: " + firstLine(out)
		}
	}
	if _, err := git(f.Dir, "diff", "--cached", "--quiet"); err == nil {
		// A COMMIT MAY ALREADY BE PUSHED OR SIGNED. With nothing left to stage,
		// making the model credit would require rewriting that commit.
		return ""
	}
	// THE PROGRAM'S OWN MESSAGE, WHEN IT WROTE ONE, IS THE COMMIT'S
	// ([ProgramFolder.BriefNote] asks for it): it knows what it changed and
	// why, where the task's title is the brief's first line and its ending is
	// an account of the run rather than of the change.
	message := f.message
	if message == "" {
		message = clip(firstLine(f.Title), 72)
		if strings.TrimSpace(message) == "" {
			// A run a shell started with no brief, on a command of its own, has no
			// title, and git takes no commit without a subject.
			message = f.Program + "'s work"
		}
		if result = strings.TrimSpace(result); result != "" {
			message += "\n\n" + result
		}
	}
	args := append([]string{"-c", "commit.gpgsign=false"}, codeafGitIdentity()...)
	if !f.NoAttribution {
		// signOnce, because a message the program wrote may already carry a
		// line of the signature, which would otherwise appear twice.
		message = gitSignature{named: f.SignModel != "", model: f.SignModel}.signOnce(message)
	}
	args = append(args, "commit", "-q", "--no-verify", "-m", message)
	if out, err := git(f.Dir, args...); err != nil {
		return "git commit: " + firstLine(out)
	}
	return ""
}

func (f *ProgramFolder) excludedFromCommit(path string) bool {
	if f.Notes != "" && (path == f.Notes || strings.HasPrefix(path, strings.TrimSuffix(f.Notes, "/")+"/")) {
		return true
	}
	for _, ignored := range f.IgnoredAtStart {
		if ignored != "" && (path == ignored || strings.HasPrefix(path, strings.TrimSuffix(ignored, "/")+"/")) {
			return true
		}
	}
	// AN INPUT THE RUN LEFT AS IT WAS IS NOT ITS WORK, and one it changed is
	// (gitidentity's inputs.go).
	return f.Inputs.LeftAlone(f.Dir, path) || gitidentity.GeneratedRunPath(path)
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
// a copy removed, and the branch it cut, which holds nothing, deleted — a run
// that never ran leaves no branch behind, where one that ran always does — and
// the ending that says so is the one written down. A branch a line of runs
// carries on is the line's, and stays. A nil folder is a run that readied
// none.
func (f *ProgramFolder) abandon() {
	if f == nil {
		return
	}
	if !f.Copied() {
		f.Finish("")
		return
	}
	end := f.settleCopy("", false)
	if !f.Continues && !end.Kept && end.CopyLeft == "" {
		if tip := branchCommit(f.Repo, f.Branch); tip != "" && tip == f.base() {
			if _, err := git(f.Repo, "branch", "-q", "-D", f.Branch); err == nil {
				end.Dropped = true
			}
		}
	}
	f.Ended = end.Sentence()
	end.keepEnding()
	f.releaseCopy()
}

// tree is the folder as a run's tree: the folder itself, worked in where it
// is, with the program's branch and where the person's checkout was, which the
// run's row writes down ([runCopyOf]). Zero for a nil folder.
func (f *ProgramFolder) tree() taskTree {
	if f == nil {
		return taskTree{}
	}
	tree := taskTree{dir: f.Dir, merge: mergeInPlace, ground: f.Ground(), mode: TaskModeInPlace, rung: GroundRungHere}
	if f.Branch != "" {
		// THE ROOT IS THE PERSON'S REPOSITORY, where the branch lives and
		// outlives the copy, which is gone by the time anybody reads the row.
		tree.root, tree.branch, tree.home, tree.homeSha = f.Ground(), f.Branch, f.Home, f.Start
		tree.continues, tree.from, tree.snapshot = f.Continues, f.From, f.Snapshot
		tree.untracked = slices.Clone(f.Untracked)
	}
	return tree
}

// StopPromise is what a person who stops a program's run is told at once
// about where its work will be.
func (f *ProgramFolder) StopPromise() string {
	if f.Plain() {
		return "its work so far stays in " + f.Dir
	}
	if f.Copied() {
		return "its work so far goes onto its branch " + f.Branch + " in " + f.Repo + " as it stops"
	}
	return "its work so far stays on its branch " + f.Branch + ", checked out in " + f.Dir
}

// Sentence is how a run left its folder, in the one sentence the run's page,
// the conversation and a shell run's last lines all say: where the work is,
// how much of it, that its branch is checked out, and how to go back to the
// person's own branch and bring the work in.
func (e ProgramFolderEnd) Sentence() string {
	if e.said != "" {
		return e.said
	}
	f := e.Folder
	var said string
	switch {
	case f.Copied():
		said = e.copySentence()
	case e.Gone && f.IgnoredOuter != "":
		said = "its work so far is in " + f.Dir + ", as it left it; git ignores this folder inside " + f.IgnoredOuter + ", so codeaf cut no branch and nothing was committed"
	case f.IgnoredOuter != "":
		said = "its work is in " + f.Dir + "; git ignores this folder inside " + f.IgnoredOuter + ", so codeaf cut no branch and nothing was committed"
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
			", so codeaf made no finishing commit and did not switch branches"
		if e.Uncommitted > 0 {
			said += "; the checkout has " + fileCount(e.Uncommitted) + " uncommitted"
		} else {
			said += "; no uncommitted files were left in that checkout"
		}
		if e.Kept {
			said += "; " + f.Branch + " holds " + fileCount(len(e.Changed))
		}
		if e.HomeMoved {
			said += "; " + e.homeMovedWords()
		} else if f.Home != "" {
			said += "; your branch " + f.Home + " was not given a commit by codeaf"
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

// changedBetween is every raw path two commits' trees differ in. NUL output
// keeps Git from quoting non-ASCII names or splitting a name with a newline.
func changedBetween(dir, from, to string) []string {
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		return nil
	}
	out, err := git(dir, "diff", "--name-only", "-z", from, to)
	if err != nil {
		return nil
	}
	var paths []string
	for _, path := range strings.Split(strings.TrimSuffix(out, "\x00"), "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
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
		// A HOLD SOMEBODY ELSE HAS — the run's program still stopping, holding
		// the hold its codeaf handed it ([ProgramFolder.Hold]) — or one nobody
		// can take, is a folder this reopen cannot know is idle: it is left for
		// the next codeaf that can. So is a run that ended while this asked.
		if owed = claimOwedProgramFolder(path, owed); owed == nil {
			return ProgramFolderEnd{}, false
		}
		// A COPY IS FINISHED, A PERSON'S FOLDER ONLY READ
		// ([ProgramFolder.settleGone]): nothing but the run's work can be in its
		// copy, and a folder the person works in has been theirs since the
		// process went away, however long ago that was.
		end := owed.settleGone()
		owed.Ended = end.Sentence()
		end.keepEnding()
		if owed.Copied() {
			owed.releaseCopy()
			return end, true
		}
		owed.write()
		owed.release()
		return end, true
	}
	return ProgramFolderEnd{}, false
}

// taskBranchName is the branch a task's work is cut on: `task/`, the title as
// a branch name can spell it, and a short random tail, so the same work
// proposed twice lands on two branches. ONE SPELLING FOR EVERY ROAD that cuts
// one — a task's own worktree ([cutTaskWorktree]) and a program's copy of the
// person's repository ([PrepareProgramFolder]) — so a person reading `git
// branch` meets one shape.
func taskBranchName(title string) string {
	return "task/" + slugify(title) + "-" + shortID()
}
