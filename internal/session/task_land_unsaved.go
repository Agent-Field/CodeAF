package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// task_land_unsaved.go is one law for both landing roads: A LANDING THAT COULD
// NOT SAVE THE WORK IS NOT A LANDING.
//
// Both roads used to answer the same mark for the failing attempt as for the
// ordinary one. A branch landing threw away what [commitTaskWork] told it, merged
// a branch holding nothing, and released the working copy — so the one file the
// worker wrote was gone from the person's branch AND from the disk, under a card
// that said done (#255). A folder landing answered [mergeInPlace] after a copy
// that stopped halfway, leaving the person's folder with the first half of a
// deliverable and a row that read finished (#256).
//
// What is here is what the two share: the reading of an outcome, the sentence a
// person is given, and the one question [stageTaskWork] has to answer before a
// landing may go on. The roads themselves stay where they are.

// cameHome reads a landing's outcome as the one question every road asks of it:
// did the work get where the person can see it?
//
// IT IS A QUESTION RATHER THAN A COMPARISON, because the roads used to test
// `merge == mergeConflicted` by hand and a second reason to keep a branch —
// work that could not be committed at all — walked straight past all five of
// them. A mark nobody may land on is added here once, and every road refuses it.
func cameHome(merge string) bool {
	switch merge {
	case mergeConflicted, mergeAborted:
		return false
	case mergeKept:
		// FINISHED WORK ON A PROTECTED CHECKOUT IS COMPLETE. It is visible on
		// the named branch and unblocks dependents even though no checkout moved.
		return true
	default:
		return true
	}
}

// landingRefusal is WHY a landing could not be saved, in the two kinds that have
// to be answered differently.
//
// ── THE DIFFERENCE IS WHETHER ASKING AGAIN CAN CHANGE ANYTHING ──────────────
//
// A landing that failed goes back to somebody to decide, and their answer runs
// the same commands into the same place. Where what refused was THE WORK — a
// commit a hook would not take, a change git would not sign — a second answer is
// worth having: the person can fix the thing that was refused and accept it
// again. Where what refused was THE PLACE — a directory that is not a repository,
// a read-only mount, a full disk, a folder with a file where a directory has to
// go — the second answer gets the same refusal, and the third, which is exactly
// the loop a measured run spent its parent's remaining minutes on: accept,
// refuse, offer, accept, refuse, offer (#513).
//
// SO THE PLACE REFUSING SETTLES THE NODE WHERE IT STANDS and the work refusing
// keeps today's road. Nothing is lost either way: no merge happens, no working
// copy is released, and the report names the directory the only copy is in.
//
// AND IT IS DECIDED WHERE THE REFUSAL HAPPENS, never afterwards from the
// sentence. [stageTaskWork] asks git whether the place is a repository at all and
// answers that arm outright; [taskTree.landMirror] knows a folder that would not
// take the lay is the folder refusing; and the one arm where only git's prose
// exists puts the question to the tree itself ([askTheTree]) rather than reading
// it back out of git's prose.
type landingRefusal uint8

const (
	// refusedNothing is a landing that was not refused at all.
	refusedNothing landingRefusal = iota
	// refusedByTheWork is a refusal a second answer could get past.
	refusedByTheWork
	// refusedByTheTree is a refusal that will be the same refusal next time.
	refusedByTheTree
	// refusedByYourFiles is the one refusal ONLY THE PERSON CAN GET PAST: their
	// own uncommitted copies of the very files the task wrote are sitting in the
	// folder, so the merge would have to write over work nobody has looked at.
	// The landing may not do that by itself — a landing that moved somebody's
	// unfinished edits without being asked is the carry-and-leave groundcarry.go
	// forbids — and asking again changes nothing. What changes it is the person
	// saying `resolve it`, which is what spends the carry (task_merge_round.go's
	// [Agent.ResolveConflict]).
	refusedByYourFiles
)

// askTheTree asks THE REPOSITORY ITSELF whether it can still be written to, and
// it is how a landing that could not be saved is told apart from a place that
// will refuse it again.
//
// ── IT USED TO READ GIT'S PROSE, AND PROSE IS NOT EVIDENCE ──────────────────
//
// The five phrases that name a place git will not write — no repository, a
// read-only mount, a permission, a full disk, a quota — turn up in sentences
// that are about the WORK just as readily. A hook that prints "permission
// denied" and refuses the commit, a path with `read-only file system` in its
// name, a message quoting an error somebody else's tool produced: each of those
// is answerable, and each of them was being settled for good on the strength of
// a substring (#513). A landing settled wrongly this way cannot be accepted
// again, which is the one mistake on this road that a person cannot undo.
//
// ── SO THE QUESTION IS A WRITE, WHICH IS THE THING THAT WAS REFUSED ─────────
//
// A scratch file is created inside the repository's own git directory and
// removed again. A tree that takes it is a tree a second answer could get past,
// whatever git said about the first; a tree that refuses it with the errno of a
// read-only mount, a permission, a disk with nothing left on it or a quota is
// the PLACE refusing, and it will refuse the same way next time.
//
// AND ANYTHING ELSE IS THE WORK. A probe that fails for a reason nobody
// recognises keeps the landing on the road it has always taken — back to
// somebody, with the branch kept and another answer allowed — rather than
// settling a node on a guess. The one tree refusal that is NOT asked here is a
// directory that is no repository at all: [stageTaskWork] already asks git that
// as a question and answers it where it happens.
func askTheTree(dir string) landingRefusal {
	// THE GIT DIRECTORY IS ASKED FOR BY NAME rather than assumed to be `dir/.git`,
	// because a node works in a worktree, where `.git` is a FILE naming the real
	// directory somewhere under the parent repository. A tree that will not say
	// where it keeps itself is probed where it stands, which is the safe side of
	// the answer: an unrecognised failure is the work.
	place := dir
	if out, err := git(dir, "rev-parse", "--absolute-git-dir"); err == nil {
		if named := strings.TrimSpace(out); named != "" {
			place = named
		}
	}
	scratch, err := os.CreateTemp(place, ".aforge-write-")
	if err != nil {
		return refusalFromWrite(err)
	}
	name := scratch.Name()
	_ = scratch.Close()
	_ = os.Remove(name)
	return refusedByTheWork
}

// refusalFromWrite reads the ERRNO of a refused write, which is the one account
// of a refusal that nobody wrote in prose.
//
// The four kinds are the place refusing in the only ways a place can: the mount
// is read-only, the permission is not there, the disk is full, the quota is
// spent. Everything else — a name that is already taken, a directory that moved
// under us, anything the operating system spells some other way — is left as the
// work, so an unfamiliar failure keeps a landing answerable.
func refusalFromWrite(err error) landingRefusal {
	for _, refusing := range []error{
		syscall.EROFS,
		syscall.EACCES,
		syscall.EPERM,
		syscall.ENOSPC,
		syscall.EDQUOT,
	} {
		if errors.Is(err, refusing) {
			return refusedByTheTree
		}
	}
	return refusedByTheWork
}

// unsavedTail is the phrase BOTH unsaved sentences carry, and it is one constant
// because [taskNote] reads it back: a branch that was kept with the work
// committed on it and a landing that saved nothing anywhere wear the same
// [mergeAborted] mark, and the note may not offer the person a branch to merge
// when there is nothing on it.
const unsavedTail = " and could not be saved"

// unsavedLead opens both of those sentences, and reading the two together is what
// keeps the note honest in the other direction: a worker quoting an error about
// something that could not be saved must not silence the branch a kept landing is
// offering, so the sentence is recognised by its shape rather than by one phrase
// that could turn up in anybody's prose.
const unsavedLead = "its work is in "

// unsavedSentence is what a person reads when a node's work could not be put on
// its own branch — a full disk, a read-only mount, a permission somebody
// changed. It NAMES THE DIRECTORY, because that directory now holds the only
// copy of the work there is: nothing merged, nothing was released, and the
// worktree is still standing exactly where the node left it.
func unsavedSentence(dir, problem string) string {
	return unsavedLead + dir + unsavedTail + " to its branch: " + firstLine(problem)
}

// unlaidSentence is the same sentence for a folder ground, where the work is
// laid back by name rather than merged ([taskTree.landMirror]). The copy it
// names is untouched by the refusal, so everything the family made is still in
// it.
func unlaidSentence(dir, ground, problem string) string {
	return unsavedLead + dir + unsavedTail + " into " + ground + ": " + problem
}

// unsavedLanding says whether a settled node is one whose work was saved
// nowhere, read off the report that already carries the sentence.
//
// IT READS THE REPORT RATHER THAN A SECOND FIELD, the way the incomplete ending
// is told from the other failures on the same mark ([taskNote] reads
// [incompleteLead] one arm above). What a person is told about where their work
// is has to come from one sentence, and a flag beside it would be the second
// copy that drifts.
func unsavedLanding(report string) bool {
	return strings.Contains(report, unsavedLead) && strings.Contains(report, unsavedTail)
}

// literalPathspec is how a filename gets to mean itself to git: a node that
// wrote `report[1].md` named a file, and to git's pathspec parser that is a glob.
const literalPathspec = ":(literal)"

// unstagedWork retries a batch git refused one path at a time and answers the one
// question that is left: is there work here that the index did not take and the
// person would not find on their branch afterwards?
//
// A BATCH GIT REFUSES IS NOT YET A FAILURE. One path .gitignore covers fails the
// whole add, which is the reason the retry exists at all ([stageTaskWork]), and a
// retry that gets the rest of the ledger in has lost nothing — the landing goes on
// exactly as it always did. So the answer is per path rather than for the batch:
// ONE FILE LEFT OUT IS ENOUGH, because a landing merges and then removes the only
// other copy, and a file that was quietly dropped on the way is a file nobody has.
//
// TWO PATHS ARE NOT WORK AT RISK. One that is GONE and was never tracked — a node
// that wrote a file and then removed it still names it on its ledger — has nothing
// to lose; one that is gone and IS tracked is a deletion the branch has to carry,
// so it counts. And a path the repository IGNORES was never going to be on the
// branch under `add -A` either, so refusing a landing over it would be this file's
// law inverted. Anything else that cannot be looked at at all is work at risk by
// default: a file behind a directory nobody can read is still a file.
//
// The two extra questions are asked of git ONLY on this road, which is the one a
// landing never takes: an ordinary batch goes in whole and nothing here runs.
func unstagedWork(dir string, paths []string) bool {
	unstaged := false
	for _, path := range paths {
		if _, err := git(dir, "add", "--all", "--", path); err == nil {
			continue
		}
		if unstaged {
			// The landing is refusing already. What is left is to let every other path
			// have its retry, so nothing is lost for want of being asked, and to stop
			// spending git on a question that is answered.
			continue
		}
		relative := strings.TrimPrefix(path, literalPathspec)
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(relative))); os.IsNotExist(err) {
			if _, known := git(dir, "ls-files", "--error-unmatch", "--", relative); known != nil {
				// Never tracked and no longer there: nothing to lose either way.
				continue
			}
			// Tracked and gone is a deletion, and a deletion that did not reach the
			// index is half a change waiting to land on somebody's branch.
			unstaged = true
			continue
		}
		if _, err := git(dir, "check-ignore", "-q", "--", relative); err == nil {
			continue
		}
		unstaged = true
	}
	return unstaged
}
