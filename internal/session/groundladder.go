package session

// THE GROUND LAW: A TASK INHERITS ITS PARENT'S WORLD AS IT IS.
//
// ── THE RUN THIS WAS WRITTEN FROM ──
//
// A parent task drew a division and carved five children for its parts. Every
// child was grounded the way task_run.go had always grounded one: `git worktree
// add` off the ground's HEAD. The parent's entire implementation was
// UNCOMMITTED, so not one child got a world with the change its own brief
// described. The briefs were excellent and named exact files and symbols of a
// world that was not on the disk the workers woke up on. All four checking
// lanes were doomed at birth: one looped for $7.99 rewriting a file its brief
// said had been deleted until the step cap killed it, one failed, two were
// turned back by their checkers. About $14 and an hour of wall time were spent
// proving the ground was stale.
//
// The old road even said what it was doing, in as many words: "WHAT THE BRANCH
// CARRIES IS HEAD AND NOTHING ELSE" ([cutTaskWorktree]). It was true, it was
// documented, and it was the defect.
//
// ── THE LAW ──
//
// A HANDOFF HANDS THE PARENT'S WORLD AS IT IS, NEVER A REFERENCE TO HISTORY.
// The ground of a child is the parent's state at the moment of the handoff —
// edits nobody committed, files nobody added, the data beside them — by
// construction and in every domain. A research task inherits the notes gathered
// so far, a media task the assets, a coding task the tree. "The world" is a
// directory in every one of them, and forking a directory is a solved problem.
//
// ── THE LADDER ──
//
// There is ONE ladder and it is walked highest rung first. Each rung answers
// only whether it can reach THIS ground; a rung that cannot is not a failure and
// is never reported as one, because the rung below it is the answer.
//
//  1. [universeRung] — furrow forks the whole workspace, byte-exact: the files,
//     the untracked ones, the ignored ones, the dependencies, the `.env`, the
//     dev database. It is the only rung that carries what git cannot see, and
//     on a copy-on-write filesystem it is the cheapest of the three as well.
//  2. [snapshotRung] — a machine commit of the parent's tree, untracked files
//     included, made on the task's own branch, and the child's worktree carved
//     FROM THAT COMMIT. Never from HEAD, never from a remote ref.
//  3. [copyRung] — the folder copied file by file. It is the rung that was
//     already here, and it always obeyed the law.
//
// ── THE LADDER CHOOSES THE WORLD; IT NEVER CHANGES THE PROMISE ──
//
// This is the line that decides which rung can reach which ground, and it is
// worth stating on its own because the obvious reading of "furrow first" breaks
// it. A REPOSITORY GROUND WAS PROMISED A BRANCH: its work comes home as a merge,
// the person can read the diff, a failed run leaves a branch they can check out.
// A furrow universe is a whole repository of its own, registered with nobody, so
// grounding a repository task in one would quietly turn every branch landing
// into a copy landing — a different promise, made by a rung, behind everybody's
// back. So a repository ground takes the snapshot rung, which is git's own
// answer and keeps the branch; a ground that was promised a COPY takes the
// universe, because a universe is a copy and a better one.
//
// WHAT THAT LEAVES ON THE TABLE, SAID OUT LOUD: the snapshot rung is git's
// world, and git's world stops at `.gitignore`. A `.env`, an installed
// dependency tree, a dev database are exactly the files a repository is
// configured not to see, so a repository task still does not inherit them.
// Closing that needs a landing road that can merge from a separate repository,
// which is a decision about promises and not about grounds.
//
// ── AND WHAT THE CHILD RECORDS ──
//
// A world a task worked in that nobody can name afterwards is a report nobody
// can check. Every rung writes down which rung it was and the one string that
// identifies the world it made — furrow's sealed snapshot, the machine commit's
// sha — onto the tree, onto the node, and into the node's own log.
//
// ── AND WHY AFORGE ATTACHES THE FOLDER ITSELF ──
//
// Every aforge carries furrow ([internal/furrowbin]), so the half of the answer
// that used to vary by machine no longer does; what still varied was whether
// somebody had remembered to type `furrow watch` in this project. A capability
// the binary carries and never engages is this codebase's absent-not-broken law
// running backwards, so a task about to be grounded in a folder attaches that
// folder ([furrow.Attach]) on the same consent as the write it was already
// going to make there. It attaches ONLY THE FOLDER IT IS ABOUT TO FORK: a
// workspace nothing would use furrow for is a workspace nothing here touches,
// because a side effect with no purpose is not consent, it is litter. Every way
// the attach or the fork can go wrong — no furrow, a folder it will not take, a
// machine where it takes too long — falls to the next rung down, so the worst
// case of the top rung is a bounded pause and yesterday's behaviour.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/furrow"
)

// GroundRung names which rung of the ladder made one task's world. It is
// written onto the node so that a report can say what world the work was done
// in, which is the question nobody could answer about the run this file was
// written from.
type GroundRung string

const (
	// GroundRungUniverse is a furrow fork of the whole workspace.
	GroundRungUniverse GroundRung = "universe"
	// GroundRungSnapshot is a machine commit of the parent's tree, with the
	// child's worktree carved from it.
	GroundRungSnapshot GroundRung = "snapshot"
	// GroundRungCopy is the folder copied file by file.
	GroundRungCopy GroundRung = "copy"
	// GroundRungHere is the honest nothing: the task works in the ground
	// itself, so it inherits the world by standing in it.
	GroundRungHere GroundRung = "here"
)

// groundOrder is everything a rung needs to make one child's world: the ground
// to inherit, the directory to make it in, and the names the landing will use.
type groundOrder struct {
	// place is the session folder the work belongs to, carried for the
	// repository lock exactly as [taskTree] carries it.
	place Place
	// ground is the repository or folder the work is ABOUT, canonical.
	ground string
	// root is ground's repository root when it has one, and empty otherwise.
	// It is resolved once by the caller rather than by each rung, so that two
	// rungs can never disagree about which repository this is.
	root string
	// dir is the directory the child will work in, and mode is the permission
	// its parent directory is made with.
	dir  string
	mode os.FileMode
	// branch is the branch a rung cuts when it cuts one, and title is what the
	// machine commit says it is.
	branch string
	title  string
	// promise is what the ground was already resolved to be — a branch cut off
	// a repository ([TaskModeWorktree]) or a copy of a folder
	// ([TaskModeMirror]) — and it is how a rung knows whether it applies. The
	// ladder changes WHICH WORLD a task starts in; it does not get to change
	// what was promised about how the work comes home.
	promise TaskMode
}

// groundRung is one way of handing a child the world its parent stands in.
//
// It is an interface with a registry behind it rather than a switch, because
// the ladder is the thing this file is about: a fourth way of forking a
// directory — a filesystem that snapshots, a remote that clones — is a rung
// added to [groundLadder] and nothing else touched.
type groundRung interface {
	// rung is the name this rung writes onto the world it makes.
	rung() GroundRung
	// carve makes the child's world, or answers false when this rung cannot
	// reach this ground. FALSE IS NOT A FAILURE and is never reported as one:
	// the rung below is the answer, and only a caller that runs out of rungs
	// has anything to say to anybody.
	carve(ctx context.Context, order groundOrder) (taskTree, bool)
}

// groundLadder is THE ONE LADDER, highest rung first. Order is meaning here:
// see this file's header for what each rung costs and why furrow is above git.
var groundLadder = []groundRung{universeRung{}, snapshotRung{}, copyRung{}}

// carveGround walks the ladder and hands back the first world that was made.
//
// A ground no rung would take is an error and not a silent lesser world: the
// caller ([prepareTaskTreeOn]) has its own honest un-isolations for a ground
// with nothing to fork, and choosing one of them here would be this file
// quietly overruling a promise somebody else made.
func carveGround(ctx context.Context, order groundOrder) (taskTree, error) {
	for _, rung := range groundLadder {
		tree, ok := rung.carve(ctx, order)
		if !ok {
			continue
		}
		tree.rung = rung.rung()
		return tree, nil
	}
	return taskTree{}, errors.New("no copy of " + order.ground + " could be made for this task")
}

// ── the top rung: a universe ────────────────────────────────────────────────

// universeRung forks the whole workspace with furrow and gives the child the
// fork to work in.
//
// WHAT IT CARRIES THAT NOTHING BELOW IT DOES is everything git was told to
// ignore: the `.env`, the installed dependencies, the dev database, a build
// somebody spent ten minutes on. That is the difference between a child that
// can run the parent's tests and one that spends its first four steps
// discovering it cannot. It is also the difference between a copy that costs a
// second and [mirrorGround], which walks a folder file by file and refuses one
// holding more than [auditRestoreEntries] of them.
//
// It reaches a ground that was promised a COPY, and this file's header says at
// length why it does not reach one that was promised a branch.
type universeRung struct{}

func (universeRung) rung() GroundRung { return GroundRungUniverse }

func (universeRung) carve(ctx context.Context, order groundOrder) (taskTree, bool) {
	if order.promise != TaskModeMirror || strings.TrimSpace(order.ground) == "" || strings.TrimSpace(order.dir) == "" {
		return taskTree{}, false
	}
	workspace := furrow.Attach(ctx, order.ground)
	if workspace == nil {
		return taskTree{}, false
	}
	// furrow will not fork into a directory that is already occupied, and the
	// occupant here can only be this session's own wreckage — the same argument
	// [cutWorktreeAt] makes about the path it reclaims, for the same reason: the
	// session id is in the path and one live process holds one session id.
	_ = os.RemoveAll(order.dir)
	if err := os.MkdirAll(filepath.Dir(order.dir), order.mode); err != nil {
		return taskTree{}, false
	}
	fork, err := workspace.Fork(ctx, filepath.Base(order.dir)+"-"+shortID(), order.dir)
	if err != nil || strings.TrimSpace(fork.Path) == "" {
		_ = os.RemoveAll(order.dir)
		return taskTree{}, false
	}
	// The landing is the copy's own, unchanged and by design: the files the node
	// wrote, laid back over the folder by name ([taskTree.landMirror]). A
	// universe of a folder IS a copy of that folder, so it comes home the way
	// every copy has always come home.
	return taskTree{
		dir:      fork.Path,
		merge:    mergeInPlace,
		place:    order.place,
		ground:   order.ground,
		mode:     TaskModeMirror,
		seal:     fork.Head,
		universe: fork.Name,
	}, true
}

// ── the middle rung: a snapshot ─────────────────────────────────────────────

// snapshotRung commits the parent's tree as it stands and carves the child's
// worktree from that commit.
//
// IT IS THE RUNG EVERY REPOSITORY GROUND TAKES, and it is the whole of the
// repair to the run this file was written from: what the branch carries is no
// longer "HEAD and nothing else" but the parent's world, uncommitted edits and
// untracked files included, with the branch and the merge that come after it
// exactly as they were.
type snapshotRung struct{}

func (snapshotRung) rung() GroundRung { return GroundRungSnapshot }

func (snapshotRung) carve(ctx context.Context, order groundOrder) (taskTree, bool) {
	if order.promise != TaskModeWorktree || strings.TrimSpace(order.root) == "" || !hasCommit(order.root) {
		return taskTree{}, false
	}
	// THE COMMIT IS MADE FIRST AND THE WORKTREE CARVED FROM IT. Doing it the
	// other way — cut at HEAD, then bring the parent's work across — is the
	// shape that leaves a window where the child is standing in the wrong world,
	// and it is also two answers to "what is this branch based on".
	base := sealGroundWork(order.root, order.title)
	from := base
	if from == "" {
		// The parent has nothing uncommitted, so HEAD already IS its world and
		// there is no commit to make. This is the ordinary case and it costs
		// nothing; the rung is still the snapshot rung, because the world is
		// still the parent's world as it stands.
		from = "HEAD"
	}
	tree, err := cutWorktreeFrom(order.place, order.root, order.dir, order.branch, order.mode, from)
	if err != nil {
		return taskTree{}, false
	}
	tree.ground, tree.base = order.ground, base
	if base != "" {
		tree.seal = base
	} else if head, err := git(order.root, "rev-parse", "HEAD"); err == nil {
		tree.seal = strings.TrimSpace(head)
	}
	return tree, true
}

// sealGroundWork commits a working tree AS IT STANDS onto whatever branch the
// directory is on, and answers the commit it wrote — or the empty string when
// there was nothing uncommitted to write.
//
// ── HOW IT LEAVES THE PARENT'S CHECKOUT ALONE ──
//
// A parent whose index or working tree moved because a child was handed out
// would be the machinery editing somebody's work behind their back. So the
// staging happens in AN INDEX OF ITS OWN (`GIT_INDEX_FILE`), the tree is
// written from that index, and the commit is written with `commit-tree`, which
// touches no ref at all. The parent's index, HEAD and working tree are exactly
// as they were; what is new is one commit object and, when the caller carves
// from it, a branch pointing at it.
//
// ── WHAT IT CARRIES, AND WHAT IT HONESTLY CANNOT ──
//
// `git add -A` is everything git can see, WHICH INCLUDES THE UNTRACKED FILES
// THE PARENT CREATED — the half of the defect that a `git stash` or a
// `diff HEAD` would have missed. It does not include what `.gitignore` covers,
// and it cannot: those files are not in git's world at all. Carrying them is
// exactly what the rung above this one is for.
func sealGroundWork(dir, title string) string {
	index := filepath.Join(dir, ".git", "aforge-ground-index")
	// A worktree's .git is a file, so the private index goes beside the real one
	// wherever git actually keeps it.
	if common, err := git(dir, "rev-parse", "--absolute-git-dir"); err == nil {
		if trimmed := strings.TrimSpace(common); trimmed != "" {
			index = filepath.Join(trimmed, "aforge-ground-index")
		}
	}
	_ = os.Remove(index)
	defer func() { _ = os.Remove(index) }()

	withIndex := func(args ...string) (string, error) {
		return gitWith(dir, []string{"GIT_INDEX_FILE=" + index}, args...)
	}
	if _, err := withIndex("read-tree", "HEAD"); err != nil {
		return ""
	}
	// The exclusion is the harness's own corner and nothing else: a task's
	// private metadata is not part of anybody's world (task_run.go's
	// [aforgeDroppings]).
	if _, err := withIndex("add", "-A", "--", ".", ":(exclude)"+aforgeDroppings); err != nil {
		return ""
	}
	tree, err := withIndex("write-tree")
	if err != nil {
		return ""
	}
	tree = strings.TrimSpace(tree)
	if tree == "" {
		return ""
	}
	head, err := git(dir, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return ""
	}
	if tree == strings.TrimSpace(head) {
		// Nothing uncommitted. Saying so with an empty answer keeps a clean
		// parent from paying for a commit nobody would ever read, and keeps the
		// landing from replaying work off a commit that changed nothing.
		return ""
	}
	commit, err := git(dir,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"commit-tree", tree, "-p", "HEAD", "-m", groundCommitMessage(title))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(commit)
}

// groundCommitMessage is what the machine commit says it is. It says it in a
// person's words, because somebody reading `git log` after a task has landed is
// entitled to know why a commit they did not make is sitting in their history.
func groundCommitMessage(title string) string {
	title = clip(firstLine(strings.TrimSpace(title)), 60)
	if title == "" {
		return "the world this task started from"
	}
	return "the world this task started from: " + title
}

// ── the bottom rung: a copy ─────────────────────────────────────────────────

// copyRung copies the folder, which is what a ground with no history behind it
// has always been given ([mirrorGround]). It obeyed the law before this file
// existed — a copy is the folder as it stands, by construction — and it is here
// so that the ladder is the whole answer rather than most of it.
type copyRung struct{}

func (copyRung) rung() GroundRung { return GroundRungCopy }

func (copyRung) carve(ctx context.Context, order groundOrder) (taskTree, bool) {
	if order.promise != TaskModeMirror {
		return taskTree{}, false
	}
	if err := os.MkdirAll(order.dir, order.mode); err != nil {
		return taskTree{}, false
	}
	if problem := mirrorGround(order.ground, order.dir); problem != "" {
		return taskTree{}, false
	}
	return taskTree{
		dir:    order.dir,
		merge:  mergeInPlace,
		ground: order.ground,
		mode:   TaskModeMirror,
	}, true
}

// ── what the landing owes the ladder ────────────────────────────────────────

// replayOwnWork takes the machine commit back out of the branch's history, so
// that what comes home is THE NODE'S OWN WORK AND NOT ITS INHERITANCE.
//
// The parent's uncommitted world is scaffolding: the child stood on it, and it
// belongs to the parent, who still has it in their own checkout. Merging it
// back would hand somebody a merge of their own unfinished edits — and git
// refuses that merge outright, because the paths it would write are the very
// paths the person has open ("your local changes would be overwritten").
// Measured: every landing of a snapshot-grounded child failed that way before
// this existed.
//
// So the node's commits are replayed onto the commit the machine commit was
// made from, which is the ground's HEAD at the moment the child was carved and
// an object both this directory and the ground always hold. A replay that will
// not go is abandoned and NOT reported here: the branch still holds the work,
// the merge that follows will refuse it, and the one sentence a person reads
// about a branch that could not come home is [conflictSentence]'s.
func (t taskTree) replayOwnWork() {
	if strings.TrimSpace(t.base) == "" || strings.TrimSpace(t.dir) == "" {
		return
	}
	if _, err := git(t.dir,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"rebase", "--onto", t.base+"^", t.base); err != nil {
		_, _ = git(t.dir, "rebase", "--abort")
	}
}

// dropUniverse tells furrow to forget a fork whose work has come home. The
// files are the session's to remove and are left alone; what is dropped is the
// record, so that `furrow forks` in somebody's project does not accumulate one
// line per task this machine has ever run.
func (t taskTree) dropUniverse() {
	if t.rung != GroundRungUniverse || strings.TrimSpace(t.universe) == "" || strings.TrimSpace(t.ground) == "" {
		return
	}
	workspace := furrow.Open(context.Background(), t.ground)
	if workspace == nil {
		return
	}
	workspace.DropFork(context.Background(), t.universe)
}

// world is the one line that says what a task worked in, for the node's log and
// for a report. THE EMPTINESS LAW: a task standing in the folder it was already
// about has no copy to describe and this says nothing at all.
func (t taskTree) world() string {
	switch t.rung {
	case GroundRungUniverse:
		return "a fork of " + t.ground + " as it stood, taken whole" + sealSuffix(t.seal)
	case GroundRungHere:
		return ""
	case GroundRungSnapshot:
		if t.base == "" {
			return "a branch off " + t.ground + ", which had nothing uncommitted" + sealSuffix(t.seal)
		}
		return "a branch off " + t.ground + " as it stood, uncommitted work included" + sealSuffix(t.seal)
	case GroundRungCopy:
		return "a copy of " + t.ground + " as it stood"
	}
	return ""
}

// sealSuffix names the world so that two reports about two worlds can be told
// apart. Twelve characters is git's own habit and furrow's ids are the same
// shape.
func sealSuffix(seal string) string {
	if seal = strings.TrimSpace(seal); seal == "" {
		return ""
	}
	if len(seal) > 12 {
		seal = seal[:12]
	}
	return " · " + seal
}
