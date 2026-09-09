package session

// THE GROUND A TASK IS CARVED FROM IS THE GROUND IT MERGES INTO.
//
// ── THE RUN THIS WAS WRITTEN FROM ──
//
// A chat turn edited the person's live checkout for seven minutes, and then the
// checkpoint ceiling moved what was left of it onto a task. The ground ladder
// did what it is written to do: it sealed the parent's tree AS IT STOOD into
// the task's base commit (groundladder.go's [sealGroundWork]) and left the
// person's checkout alone. So the same hunks then existed in two places — on the
// task's branch and, uncommitted, in the person's tree.
//
// At the landing the seal-stripping replay would not go, git refused the merge
// before it started ("Your local changes to the following files would be
// overwritten by merge"), and because a merge refused pre-start leaves no
// conflicted index, the one sentence the person read named NO FILES at all and
// ended in a bare colon. The node landed unverified over a conflict nobody could
// see.
//
// ── THE LAW ──
//
// CARRY OR REFUSE, NEVER CARRY-AND-LEAVE. If the seal carried uncommitted ground
// work, the landing has to be able to merge into that ground. So the merge sets
// the person's own work aside, merges, and puts it back — and when it cannot put
// it back over the merge, it puts the tree back EXACTLY as it was, refuses, and
// names the files. What it may never do is what it used to do: fail with a
// sentence that names nothing.
//
// ── WHY ONLY THE TRACKED HALF IS SET ASIDE ──
//
// `git stash push` with no `--include-untracked` moves the modifications git is
// already watching and leaves everything else on the disk untouched. That is the
// deliberate half:
//
//   - It is the half that blocks a merge for the reason this file exists — a
//     file changed on both sides — and the half a stash can put back with a
//     three-way merge it understands.
//   - An untracked file that would be overwritten is a DIFFERENT refusal from
//     git, and a stash of it cannot be put back over itself: the restore fails
//     with "already exists, no checkout" and the entry is left behind. Measured,
//     with the whole sequence in a scratch repository, before this was written.
//     So an untracked clash is refused and named rather than carried.
//
// ── AND THE UNDO IS THE POINT ──
//
// Everything here is reversible by construction: the pre-merge commit is read
// before anything moves, and every road that does not end in a landing ends with
// `reset --hard` back to it and the set-aside work put back on top. A restore
// that conflicts leaves markers, so it is never left standing — the tree goes
// back to the commit the stash was taken at, where the same restore applies
// cleanly, and the person is told the branch was kept.

import (
	"strings"
)

// mergeIntoGround merges the node's branch into the ground and answers whether
// the work went in, the one sentence a person is owed about how, and — where it
// did not go in — the names of the files it was about.
//
// A merge that goes straight in says nothing: the outcome is already on the card
// and in the completion note, and a third telling would be the same fact three
// times. It speaks only when the person's own uncommitted work had to be moved
// out of the way, and when the branch was kept.
//
// THE NAMES TRAVEL AS A LIST AND NOT ONLY AS PROSE. The sentence is what a person
// reads on the landing; the list is what a row says out loud ("conflicts with your
// branch: parser.go") and what a resolver round is aimed at, and reading it back
// out of the sentence afterwards would be this program parsing its own writing.
func (t taskTree) mergeIntoGround() (bool, string, []string) {
	out, err := mergeTaskBranch(t.root, t.branch)
	if err == nil {
		return true, "", nil
	}
	// THE ONE SHAPE WHERE THE INDEX KNOWS NOTHING. A merge git refused before it
	// started never touched the index, so [conflictedPaths] is legitimately
	// empty and git's own message is the only place the file list exists.
	blocked, untracked := overwrittenPaths(out)
	if len(blocked) == 0 || untracked {
		// Either an ordinary conflict — the index has the names — or an untracked
		// clash, which is the one this may not carry.
		said, clashing := t.refuseMerge(blocked, out)
		return false, said, clashing
	}
	return t.carryGroundWork(blocked)
}

// refuseMerge is the sentence for a merge that was refused, and the move that
// makes it safe to read: the person's checkout comes back out of the merge
// FIRST, because A CONFLICT MARKER IS NEVER WRITTEN ONTO THE PERSON'S BRANCH
// ([abandonMerge]), and the names are read before that because abandoning is
// what removes the evidence.
//
// It names the files whichever way git said no: from the index when there was
// one, and out of git's own message when the merge was refused before it ever
// touched the index.
func (t taskTree) refuseMerge(blocked []string, out string) (string, []string) {
	clashing := conflictedPaths(t.root)
	abandonMerge(t.root)
	if len(blocked) > 0 {
		return dirtyGroundSentence(t.branch, blocked), blocked
	}
	return conflictSentence(t.branch, clashing, out), conflictNames(clashing, out)
}

// carryGroundWork is the carry half of the law: the person's own uncommitted
// work is set aside, the branch merges into the ground it was carved from, and
// their work goes back on top of it.
//
// EVERY ROAD OUT OF HERE THAT IS NOT A LANDING PUTS THE TREE BACK. The commit
// the ground stood at is read first, and a merge that conflicted or a restore
// that would have written markers both end at `reset --hard` onto it with the
// set-aside work applied cleanly on top — which is the same state the person was
// in before the landing started, to the byte.
func (t taskTree) carryGroundWork(blocked []string) (bool, string, []string) {
	stood, err := git(t.root, "rev-parse", "HEAD")
	if err != nil {
		said, clashing := t.refuseMerge(blocked, stood)
		return false, said, clashing
	}
	stood = strings.TrimSpace(stood)
	// THE STASH IS PROVED TO HAVE HAPPENED, and this is not belt-and-braces. `git
	// stash push` with nothing to save prints "No local changes to save" AND EXITS
	// ZERO — so a road that trusted the exit code would go on to `stash pop` an
	// entry that belongs to the person, from some other day, over a tree it just
	// reset. The ref before and after is the only honest test.
	held := stashTop(t.root)
	if out, err := git(t.root, "stash", "push", "-m", groundStashMessage(t.branch)); err != nil {
		said, clashing := t.refuseMerge(blocked, out)
		return false, said, clashing
	}
	if stashTop(t.root) == held {
		said, clashing := t.refuseMerge(blocked, "")
		return false, said, clashing
	}
	if out, err := mergeTaskBranch(t.root, t.branch); err != nil {
		// The names are read while the conflicted index still holds them, exactly
		// as the ordinary road reads them ([conflictSentence]).
		clashing := conflictedPaths(t.root)
		abandonMerge(t.root)
		t.putGroundWorkBack(stood)
		return false, conflictSentence(t.branch, clashing, out), conflictNames(clashing, out)
	}
	if _, err := git(t.root, "stash", "pop"); err == nil {
		return true, carriedGroundSentence(blocked), nil
	}
	// A RESTORE THAT CONFLICTS IS NEVER LEFT STANDING. `git stash pop` leaves
	// `<<<<<<<` in files the person has open and keeps the entry, which is the
	// carry-and-leave this file forbids, one road further along. So the merge is
	// undone, their work goes back over the commit it was taken at — where it
	// applies with no merge at all — and the branch is kept for them to look at.
	t.putGroundWorkBack(stood)
	return false, dirtyGroundSentence(t.branch, blocked), blocked
}

// putGroundWorkBack returns the ground to the commit it stood at and puts the
// set-aside work on top of it.
//
// The reset comes first and it is what makes the restore safe: the stash was
// taken at this commit, so applied onto it there is nothing to merge and nothing
// that can conflict. A conflicted restore may already have written markers and
// left the entry behind — the reset clears the first and the clean pop drops the
// second.
func (t taskTree) putGroundWorkBack(stood string) {
	if strings.TrimSpace(stood) != "" {
		_, _ = git(t.root, "reset", "--hard", stood)
	}
	_, _ = git(t.root, "stash", "pop")
}

// stashTop is the commit `refs/stash` points at, or the empty string when the
// repository holds no stash at all. It is the before-and-after reading that says
// whether a `git stash push` actually put anything away ([taskTree.carryGroundWork]
// says what happens to somebody's older stash if nothing does).
func stashTop(root string) string {
	out, err := git(root, "rev-parse", "--verify", "--quiet", "refs/stash")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// mergeTaskBranch is the one spelling of the merge, and it carries the identity
// for the reason [taskTree.comeHome] states: git refuses to write a merge commit
// for a checkout with no user.name, which is every hermetic HOME, and without
// these two flags a clean merge came back as a conflict that never existed.
func mergeTaskBranch(root, branch string) (string, error) {
	return git(root, append(aforgeGitIdentity(), "merge", "--no-edit", branch)...)
}

// groundStashMessage is what the person reads in `git stash list` if anything
// ever leaves one behind. It says whose it is and why it moved, because a stash
// entry nobody can account for is the most alarming thing a landing can leave in
// somebody's repository.
func groundStashMessage(branch string) string {
	return "aforge: your own work, set aside to land " + branch
}

// overwrittenPaths reads the file list out of the merge git REFUSED BEFORE IT
// STARTED, and says whether the clash was with untracked files.
//
// This is the one failure shape where [conflictedPaths] is rightly empty — there
// is no conflicted index because there was no merge — and git puts the whole
// answer in the body of its message rather than on the first line:
//
//	error: Your local changes to the following files would be overwritten by merge:
//		a.go
//	Please commit your changes or stash them before you merge.
//
// The list is the indented lines under the heading, and it ends at the first
// line that is not indented. Both headings — the local-changes one and the
// untracked one — are read, because the sentence a person needs names the files
// either way; what differs is whether the landing may set them aside.
func overwrittenPaths(out string) ([]string, bool) {
	var paths []string
	untracked, reading := false, false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.HasSuffix(strings.TrimSpace(trimmed), overwrittenByMerge) {
			reading = true
			untracked = untracked || strings.Contains(trimmed, "untracked working tree files")
			continue
		}
		if !reading {
			continue
		}
		// The names are indented under the heading, one per line. Anything flush
		// with the margin is git's next sentence and the end of the list.
		if trimmed == "" || !strings.HasPrefix(trimmed, "\t") && !strings.HasPrefix(trimmed, " ") {
			reading = false
			continue
		}
		if name := strings.TrimSpace(trimmed); name != "" {
			paths = append(paths, name)
		}
	}
	return paths, untracked
}

// overwrittenByMerge is git's own words for the refusal, and it is spelled once
// because [overwrittenPaths] matches on it and [conflictSentence] falls back
// through it.
const overwrittenByMerge = "would be overwritten by merge:"

// carriedGroundSentence is what a person reads when their own uncommitted work
// was in the way and the landing moved it rather than refusing. It names the
// files, because "something of yours was moved and put back" with no names is
// the sentence that sends somebody to `git stash list` in a panic.
func carriedGroundSentence(paths []string) string {
	return "your own uncommitted work in " + namedFew(paths, conflictNamesShown) +
		" was set aside while its branch merged, and put back afterwards"
}

// dirtyGroundSentence is the refusal half of the law: the branch is kept, the
// person's tree is exactly as they left it, and the files that made the two
// impossible to have at once are named.
//
// IT NEVER ENDS IN A COLON, which is not a style note — the sentence it replaces
// did, because it quoted git's first line and git's file list was on the lines
// after it, and a person read a landing that failed for reasons nobody named.
func dirtyGroundSentence(branch string, paths []string) string {
	return "its branch " + branch + " did not merge cleanly and was kept: your own uncommitted work in " +
		namedFew(paths, conflictNamesShown) + " is in the same files, so your tree was left exactly as it was"
}

// strandedGroundSentence is what a person reads when the branch came home still
// carrying THEIR OWN uncommitted work underneath the node's.
//
// The ground ladder seals the parent's tree into a machine commit so the child
// stands in the parent's world, and the landing lifts that commit back out again
// ([taskTree.replayOwnWork]). When the lift will not go, the branch holds the
// person's own unfinished edits as well as the node's work — and that used to be
// silent: the rebase was abandoned, nothing was written down, and the only
// evidence was a merge that then refused for reasons the report could not
// explain.
func strandedGroundSentence(branch string) string {
	return "its branch " + branch + " still carries your own uncommitted work: the copy of it the task " +
		"started from could not be taken back out"
}
