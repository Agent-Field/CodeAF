package session

import (
	"os"
	"path/filepath"
	"strings"
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
	return merge != mergeConflicted && merge != mergeAborted
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
// exists is read ONCE, by [refusalFromGit], at the seam that holds it.
type landingRefusal uint8

const (
	// refusedNothing is a landing that was not refused at all.
	refusedNothing landingRefusal = iota
	// refusedByTheWork is a refusal a second answer could get past.
	refusedByTheWork
	// refusedByTheTree is a refusal that will be the same refusal next time.
	refusedByTheTree
)

// refusalFromGit reads git's own words for the refusals that are about WHERE the
// work is rather than about the work, and it is the ONE place in this package
// that reads them.
//
// EVERYTHING IT DOES NOT RECOGNISE IS THE WORK, which is the safe side of the
// answer: an unrecognised failure keeps the landing on the road it has always
// taken — back to somebody, with the branch kept and another answer allowed —
// rather than settling a node on a guess.
func refusalFromGit(problem string) landingRefusal {
	said := strings.ToLower(problem)
	// The place, in the words the tools that refuse it actually use: a directory
	// that is no repository, a mount that will not be written, a disk with
	// nothing left on it.
	for _, mark := range []string{
		"not a git repository",
		"read-only file system",
		"permission denied",
		"no space left on device",
		"disk quota exceeded",
	} {
		if strings.Contains(said, mark) {
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
