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

// unsavedTail is the phrase BOTH unsaved sentences carry, and it is one constant
// because [taskNote] reads it back: a branch that was kept with the work
// committed on it and a landing that saved nothing anywhere wear the same
// [mergeAborted] mark, and the note may not offer the person a branch to merge
// when there is nothing on it.
const unsavedTail = " and could not be saved"

// unsavedSentence is what a person reads when a node's work could not be put on
// its own branch — a full disk, a read-only mount, a permission somebody
// changed. It NAMES THE DIRECTORY, because that directory now holds the only
// copy of the work there is: nothing merged, nothing was released, and the
// worktree is still standing exactly where the node left it.
func unsavedSentence(dir, problem string) string {
	return "its work is in " + dir + unsavedTail + " to its branch: " + firstLine(problem)
}

// unlaidSentence is the same sentence for a folder ground, where the work is
// laid back by name rather than merged ([taskTree.landMirror]). The copy it
// names is untouched by the refusal, so everything the family made is still in
// it.
func unlaidSentence(dir, ground, problem string) string {
	return "its work is in " + dir + unsavedTail + " into " + ground + ": " + problem
}

// unsavedLanding says whether a settled node is one whose work was saved
// nowhere, read off the report that already carries the sentence.
//
// IT READS THE REPORT RATHER THAN A SECOND FIELD, the way the incomplete ending
// is told from the other failures on the same mark ([taskNote] reads
// [incompleteLead] one arm above). What a person is told about where their work
// is has to come from one sentence, and a flag beside it would be the second
// copy that drifts.
func unsavedLanding(report string) bool { return strings.Contains(report, unsavedTail) }

// literalPathspec is how a filename gets to mean itself to git: a node that
// wrote `report[1].md` named a file, and to git's pathspec parser that is a glob.
const literalPathspec = ":(literal)"

// unstagedWork answers the one question a refused `git add` leaves open: is
// there work here that the index did not take and the person would not find?
//
// A BATCH GIT REFUSES IS NOT YET A FAILURE. One path .gitignore covers fails the
// whole add, which is the reason the retry exists at all ([stageTaskWork]), and
// a retry that gets the rest of the ledger in has lost nothing — the landing goes
// on exactly as it always did. What is left when every single path is refused as
// well is a repository that cannot be written, and that is work about to
// disappear.
//
// A PATH THAT IS NOT THERE HAS NOTHING TO LOSE. A node that wrote a file and
// then removed it still names it on its ledger, and a landing that stopped the
// world over a file nobody can point at would be refusing over nothing.
func unstagedWork(dir string, paths []string) bool {
	staged, onDisk := false, false
	for _, path := range paths {
		if _, err := git(dir, "add", "--all", "--", path); err == nil {
			staged = true
		}
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(path, literalPathspec)))); err == nil {
			onDisk = true
		}
	}
	return !staged && onDisk
}
