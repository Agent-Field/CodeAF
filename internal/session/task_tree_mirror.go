package session

// THE FAMILY TREE OF A FOLDER FAMILY IS A REPOSITORY.
//
// ── WHAT THIS REPAIRS ──
//
// A task family is a claim on one body of material, worked in one workspace of
// record, whose product is reintegrated into the material. For a repository
// ground that abstraction has always been real: a part is a worktree cut off
// the family tree and its branch merges back into it. For a PLAIN FOLDER —
// research notes, a document sweep, a data directory, a report with a section
// per region, which is most general work — it was not real at all. The parts of
// a mirrored parent fell through to the parent's own directory and worked on top
// of each other, concurrently, with no isolation and no merge, while both
// prompts promised each of them "a copy of the repository taken from yours".
//
// So the mirror — the private copy of the folder this session already makes at
// family start (task_run.go's [mirrorGround], groundladder.go's [copyRung]) —
// is opened as a repository of its own the moment it is made. It is harness-
// owned space; nothing of this reaches the person's folder, which is still laid
// by name at the family's landing ([taskTree.landMirror]).
//
// ── WHY THAT IS THE WHOLE CHANGE ──
//
// ONE ROAD, AND NO SECOND MERGE PATH. Once the mirror is a repository, a part
// of a mirrored parent takes the roads that already exist, end to end, with
// nothing written for it: the part's workspace IS the mirror, so the ground
// ladder resolves it to the repository it is standing in (taskstands.go's
// [Agent.groundLadder], which holds a part to where its parent stands), the
// mode falls out as [TaskModeWorktree], [prepareTaskTreeOn] cuts the worktree
// at [taskOwnFolder] off the mirror's HEAD, and [taskTree.comeHome] commits the
// part's ledger and merges that branch back into the mirror exactly as it merges
// a repository part into the person's checkout. The audit reaches it too: a part
// has a branch, so it is judged from a clean checkout of that branch
// (task_audit.go's [restoreFromBranch]), and the parent is still judged against
// the untouched folder with the integrated mirror's work laid over it.
//
// AND THE WIP COMMIT COMES FREE WHEN IT IS ORDERED. The snapshot rung seals a
// parent's uncommitted world into a machine commit before it carves a child off
// it and the landing replays it back out (groundladder.go's [sealGroundWork],
// [taskTree.replayOwnWork]) — so the moment the mirror is a repository, a part
// cut off it starts in the parent's world rather than in the family's baseline.
//
// ── WHERE THIS DELIBERATELY DOES NOT GO ──
//
// IN PLACE AND FOLDER FAMILIES ARE OUT OF SCOPE, and that is a ruling rather
// than an omission. Those two modes are the honest un-isolations: the person
// said "here", or there was nowhere else to stand, so the family tree IS the
// person's own directory. There is no private copy to open, and initialising a
// repository in somebody's folder to gain one would be the harness putting its
// own machinery where they live. Parts of such a parent go on sharing the
// directory, which is what "here" means.

import "strings"

// openFamilyTree makes a mirrored family tree into the repository its parts cut
// their worktrees off, and hands the tree back either way.
//
// IT NEVER FAILS THE TASK. A ground with no git, a read-only disk, a folder
// somebody moved — none of them are a reason to refuse work that ran perfectly
// well before this file existed. What they are is a reason to SAY SO: the tree
// comes back carrying one sentence ([taskTree.note]) that the job log prints and
// the parent's own brief carries, because a parent that is about to hand out
// parts is the one who has to know they will be working beside it rather than
// each in a copy of their own. Degrading in silence is how the defect this file
// repairs went unnoticed for as long as it did.
func openFamilyTree(tree taskTree) taskTree {
	dir := strings.TrimSpace(tree.dir)
	if tree.mode != TaskModeMirror || dir == "" {
		return tree
	}
	if familyTreeIsOpen(dir) {
		// A resumed node stands in the mirror its first run already opened, and
		// opening it twice would lay a second baseline over the work in it.
		return tree
	}
	if problem := openFamilyRepository(dir); problem != "" {
		tree.note = sharedFamilyTreeNote(problem)
	}
	return tree
}

// familyTreeIsOpen reports whether this directory is ALREADY the repository its
// parts branch from — its own, with a commit to cut from, and not merely a
// directory that happens to sit inside somebody else's checkout. The last
// clause is the one that matters: under the legacy layout a mirror lives beneath
// the conversation's workspace, so a bare [repositoryRoot] answers with the
// person's repository and the mirror would never be opened at all.
func familyTreeIsOpen(dir string) bool {
	root, ok := repositoryRoot(dir)
	return ok && canonicalPath(root) == canonicalPath(dir) && hasCommit(root)
}

// openFamilyRepository is the git of it: a repository over the copy that is
// already there, and one baseline commit holding it as it stands. It answers
// the empty string, or the one line a person reads about why there is no family
// tree here.
//
// THE BASELINE IS NOT A BASELINE FOR THE CHECK. What a mirrored task is judged
// against is the person's own folder, untouched, with the ledger laid over it
// (task_audit.go's [restoreFromFolder]); this commit exists for exactly two
// jobs — to give a part something to cut a worktree from, and to give that
// part's branch something to merge into.
//
// IT IS BOUNDED BY THE COPY THAT PRECEDED IT. [mirrorGround] refuses a folder
// holding more than [auditRestoreEntries] files before a byte of it is copied,
// so everything this stages is already inside that cap and no second bound is
// needed here.
func openFamilyRepository(dir string) string {
	if out, err := git(dir, "init", "--quiet"); err != nil {
		return familyTreeProblem(out, err)
	}
	// EVERYTHING IN THE COPY GOES IN, ignore rules included (`--force`). A
	// `.gitignore` is the person's answer about what belongs in THEIR history;
	// this history is the family's medium, and a file left out of it is a file
	// every part wakes up without. The two corners kept out are the ones that
	// belong to machinery rather than to anybody's world — a task's private
	// metadata and furrow's bookkeeping — exactly as [sealGroundWork] keeps them
	// out of the world it hands a child.
	if out, err := git(dir, "add", "--all", "--force", "--",
		".", ":(exclude)"+aforgeDroppings, ":(exclude)"+furrowMarkerDir); err != nil {
		return familyTreeProblem(out, err)
	}
	// AN EMPTY FOLDER STILL GETS A FAMILY TREE (`--allow-empty`): a family that
	// starts from nothing and writes everything is the ordinary shape of a
	// research or a drafting task, and a mirror with no commit is a mirror no
	// part can branch from.
	//
	// THE IDENTITY AND THE SIGNATURE ARE THE HARNESS'S OWN. A commit written
	// with no `user.name` is refused outright on every hermetic HOME, and a
	// commit somebody's global settings would sign is a passphrase prompt on a
	// terminal nobody is watching — neither is a thing to lose a family tree
	// over, and neither commit is the person's to answer for.
	if out, err := git(dir,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"-c", "commit.gpgsign=false",
		"commit", "--quiet", "--no-verify", "--allow-empty",
		"-m", familyTreeCommitMessage); err != nil {
		return familyTreeProblem(out, err)
	}
	return ""
}

// familyTreeProblem is the one line to quote about a git command that would not
// run. It PREFERS GIT'S OWN WORDS and falls back to the error, because the two
// failures here fail in different places: git refusing (a read-only disk, a
// repository somebody broke) says it on its output, while git never starting at
// all (no git on the machine, a directory that has gone) says it only in the
// error and would otherwise leave the sentence saying nothing.
func familyTreeProblem(out string, err error) string {
	if line := firstLine(strings.TrimSpace(out)); line != "" {
		return line
	}
	return err.Error()
}

// familyTreeCommitMessage is what the baseline says it is, in a person's words,
// for whoever reads this history while a family is running.
const familyTreeCommitMessage = "the material this work started from"

// sharedFamilyTreeNote is the sentence a parent reads when its family has no
// tree of its own, and it is written to be ACTIONABLE rather than apologetic:
// the one thing the parent can do about it is keep the parts off each other's
// files, so that is what it says. The reason is quoted at the end because the
// same sentence is the job log's line, and a log that says only "it degraded"
// is a log nobody can act on either.
func sharedFamilyTreeNote(problem string) string {
	return "Anything you hand out will work in this same folder beside you, rather than each part in a copy of its own: " +
		strings.TrimSpace(problem) +
		". So hand out only parts that write different files, and expect their work to appear beside yours as they go."
}
