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
//     dev database, and a repository's own `.git` with them. It is the only
//     rung that carries what git cannot see, and on a copy-on-write filesystem
//     it is the cheapest of the three as well.
//  2. [snapshotRung] — a machine commit of the parent's tree, untracked files
//     included, made on the task's own branch, and the child's worktree carved
//     FROM THAT COMMIT. Never from HEAD, never from a remote ref.
//  3. [copyRung] — the folder copied file by file. It is the rung that was
//     already here, and it always obeyed the law.
//
// ── THE LADDER CHOOSES THE WORLD; THE LANDING KEEPS THE PROMISE ──
//
// This is the line that decides what a rung may do, and it is worth stating on
// its own because the obvious reading of "furrow first" breaks it. A REPOSITORY
// GROUND WAS PROMISED A BRANCH: its work comes home as a merge, the person can
// read the diff, a failed run leaves a branch they can check out. A furrow
// universe is a whole repository of its own, registered with nobody, so for one
// release the ladder kept that promise by refusing a repository the top rung
// altogether. The cost was written down right here: a repository task could not
// inherit a `.env`, an installed dependency tree or a dev database — which are
// exactly the files a repository is configured not to see, and exactly the ones
// the ground law was written to hand over.
//
// THE PROMISE IS KEPT BY THE LANDING AND NOT BY THE REFUSAL. A universe of a
// repository is a byte-exact copy of that repository, `.git` and all, so the
// task's branch is cut INSIDE the fork and the node's commits are made there.
// At the landing one `git fetch <the fork> <the branch>` puts those commits in
// the ground's own repository ([taskTree.carryBranchHomeLocked]) and the merge
// that follows is the merge that was always there. The person is handed the
// same branch, the same merge, the same diff and the same rail; what is new is
// a child that can read the `.env` and run the tests without installing
// anything first.
//
// So a rung may choose ANY world that is the parent's world as it stands. What
// a rung may still never do is change how the work comes home.
//
// WHAT THE RUNGS BELOW STILL CANNOT CARRY, SAID OUT LOUD: the snapshot rung is
// git's world, and git's world stops at `.gitignore`. A machine whose furrow
// will not run, a folder furrow will not attach, a ground whose `.git` belongs
// to somebody else (a linked worktree — see [universeReaches]) all fall to it,
// and on it a task inherits every tracked and untracked file and none of the
// ignored ones. Which of the two happened is never a guess: every rung writes
// down which rung it was.
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

// ── THE WORDS A PERSON READS ────────────────────────────────────────────────
//
// groundWords is THE TABLE: the plain words for the copy of the ground one task
// worked in, one line per rung, and the only place any surface may get them.
//
// IT EXISTS BECAUSE A RUNG IS A RECORD AND NOT A SCREEN WORD, which is the law
// [TaskMode] states about itself one file over. `universe` and `snapshot` are
// how this package writes down what it did; a surface printing either of them
// would be handing somebody the machinery's own vocabulary, which this codebase
// bans in anything a person reads.
//
// THE DEFECT IT CLOSES was a surface doing exactly that from the other end. The
// settled card labelled every task's directory `worktree` — the machinery's word
// for the rung below, printed with no idea which rung had run — so once the
// universe rung reached a repository (#183) a person reading the card was told
// the mechanism that was NOT in use: the work had happened in a fork.
//
// ONE TABLE AND MORE THAN ONE READER. The settled card in the project's record
// and the landing note a task writes when it comes home both name where the work
// was left, and they are read minutes apart about the same node (internal/tui3's
// taskrecord.go and taskdone.go). Two surfaces wording one fact separately is two
// wordings that drift, and a person told "worktree" by one and "its own copy" by
// the other has been given two places when there was only ever one.
//
// THEY NAME NO MACHINERY AND THEY NAME NO PATH. What a person needs from this
// line is whether the work happened somewhere of its own or in the folder they
// are standing in, and — where it was a repository — that a branch of theirs is
// what came back. The seal, the fork id and the machine commit are the record's
// business ([taskTree.world] writes the long form into the node's own log).
var groundWords = map[GroundRung]string{
	// A fork is a copy of the whole folder, `.git` and all, so the words are the
	// copy's words below and not a second spelling of one fact. A person cannot
	// act on the difference between a furrow fork and a file-by-file copy, and a
	// line that made them learn it would be the machinery leaking again.
	GroundRungUniverse: "its own copy of the folder",
	// The snapshot rung's directory IS a branch of the person's repository,
	// checked out somewhere else — which is the one thing about it they can act
	// on, because the branch is what comes home.
	GroundRungSnapshot: "a branch of your repository",
	GroundRungCopy:     "its own copy of the folder",
	// Standing in the ground is not a copy at all, and saying it was one would be
	// the exact wrong thing to tell somebody whose own files the work is editing.
	GroundRungHere: "your own folder",
}

// GroundWord is the plain words for where one task's work happened: its rung's
// line in [groundWords], or "" when nothing here can honestly say.
//
// THE PROMISE IS THE FALLBACK AND NOT A SECOND TABLE. A record row written
// before the ladder existed carries no rung and still carries how the task stood
// on its ground, and before the ladder a repository got a worktree and a folder
// got a copy — so the promise answers for exactly the rows the rung cannot, in
// the words the rung would have used.
//
// A REFERENCE GETS NOTHING, DELIBERATELY. That promise hands the node an EMPTY
// folder of its own and leaves the ground read-only ([prepareTaskTreeOn]), so
// both lines of the table would be false about it: it is nobody's copy and it is
// not the person's folder. A surface handed "" says where the work was without
// claiming what made it, which is the emptiness law doing its job.
func GroundWord(rung GroundRung, promise TaskMode) string {
	if word, ok := groundWords[rung]; ok {
		return word
	}
	switch promise {
	case TaskModeWorktree:
		return groundWords[GroundRungSnapshot]
	case TaskModeMirror:
		return groundWords[GroundRungCopy]
	case TaskModeInPlace, TaskModeFolder:
		return groundWords[GroundRungHere]
	}
	return ""
}

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
// IT REACHES BOTH PROMISES. A ground promised a copy is handed the fork and
// lands the way every copy lands. A ground promised a BRANCH is handed the fork
// too — a universe of a repository is that repository, `.git` included — and the
// branch is cut inside it ([universeBranch]); what the landing then owes is one
// fetch, which is this file's header at length.
type universeRung struct{}

func (universeRung) rung() GroundRung { return GroundRungUniverse }

func (universeRung) carve(ctx context.Context, order groundOrder) (taskTree, bool) {
	if !universeReaches(order) {
		return taskTree{}, false
	}
	workspace := furrow.Attach(ctx, order.ground)
	if workspace == nil {
		return taskTree{}, false
	}
	hideFurrowMarker(order.ground)
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
	if order.promise == TaskModeWorktree {
		return universeBranch(ctx, workspace, order, fork)
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

// universeReaches answers whether this rung may make THIS ground's world, and
// it is asked before furrow is touched so that a ground it cannot take costs
// nothing at all.
//
// THE ONE REFUSAL WORTH READING TWICE IS A GROUND WHOSE `.git` IS A FILE. That
// is a linked worktree — somebody's own `git worktree add`, which is how much
// of this repository is worked on — and its `.git` is one line naming an
// administrative directory inside ANOTHER repository. A byte-exact copy of that
// folder copies the line, so the fork's commits, its branch and its HEAD would
// all be written into the original's `.git` and would move the original's
// checkout under the person. So a ground like that is not this rung's, and the
// snapshot below it is git's own answer and entirely correct.
//
// A DESTINATION INSIDE THE GROUND is the other one: furrow would be copying a
// folder into itself. It cannot happen under a session folder, which is where
// every task's world is made, and it can under the legacy layout that hangs the
// trees off the repository — so it is asked rather than assumed.
func universeReaches(order groundOrder) bool {
	ground, dir := strings.TrimSpace(order.ground), strings.TrimSpace(order.dir)
	if ground == "" || dir == "" || withinDir(ground, dir) {
		return false
	}
	switch order.promise {
	case TaskModeMirror:
		return true
	case TaskModeWorktree:
		root := strings.TrimSpace(order.root)
		return root != "" && hasCommit(root) && holdsItsOwnGit(root)
	}
	return false
}

// universeBranch turns a forked repository into a child's working copy: the task
// branch is cut INSIDE the fork, and the parent's world is sealed onto it there.
//
// THE CHILD WAKES UP IN A CLEAN TREE, exactly as it does on the snapshot rung
// and for the same reason: an inheritance it cannot tell from its own work is an
// inheritance it will commit, and what comes home has to be what the node's own
// hands wrote. The seal is an ordinary commit here rather than [sealGroundWork]'s
// index-of-its-own gymnastics, because there is nobody to leave alone — the fork
// is the harness's, made a moment ago, and the person's checkout is untouched by
// construction.
func universeBranch(ctx context.Context, workspace *furrow.Workspace, order groundOrder, fork furrow.Fork) (taskTree, bool) {
	drop := func() {
		// The fork is being abandoned before anything was written in it, so a
		// record furrow will not let go of is the same one line in a listing a
		// landing's is: there is nothing here to report it to.
		_ = workspace.DropFork(ctx, fork.Name)
		_ = os.RemoveAll(order.dir)
	}
	// AND THE FORK IS ASKED WHOSE `.git` IT IS BEFORE ANYTHING IS WRITTEN IN IT.
	// [universeReaches] already refused the ground this can happen with; this is
	// the same question asked of what came back, because the whole hazard is a
	// commit landing in somebody else's repository and it is cheaper to be sure
	// twice than to be sorry once.
	gitDir, err := git(fork.Path, "rev-parse", "--absolute-git-dir")
	if err != nil || !withinDir(canonicalPath(fork.Path), canonicalPath(strings.TrimSpace(gitDir))) {
		drop()
		return taskTree{}, false
	}
	base, ok := sealForkWorld(fork.Path, order.branch, order.title)
	if !ok {
		drop()
		return taskTree{}, false
	}
	return taskTree{
		dir:      fork.Path,
		root:     order.root,
		branch:   order.branch,
		place:    order.place,
		ground:   order.ground,
		mode:     TaskModeWorktree,
		seal:     fork.Head,
		universe: fork.Name,
		base:     base,
	}, true
}

// sealForkWorld cuts the task's branch inside a forked repository and commits
// the world it was forked from onto it, answering the commit it wrote — or the
// empty string when the parent had nothing uncommitted, which is the ordinary
// case and costs nothing.
//
// WHAT IT COMMITS IS WHAT GIT CAN SEE, and that is not a limitation here the way
// it is on the rung below: the `.env` and the installed dependencies are in the
// fork's working copy whether or not any commit mentions them, which is the
// whole point of the rung. They stay ignored, so they are never staged, never
// merged, and never reported as something the node left behind.
func sealForkWorld(dir, branch, title string) (string, bool) {
	if _, err := git(dir, "checkout", "-b", branch); err != nil {
		return "", false
	}
	// The harness's own corner is not part of anybody's world (task_run.go's
	// [aforgeDroppings]), exactly as [sealGroundWork] excludes it.
	if _, err := git(dir, "add", "-A", "--", ".", ":(exclude)"+aforgeDroppings); err != nil {
		return "", false
	}
	if _, err := git(dir, "diff", "--cached", "--quiet"); err == nil {
		// Nothing staged, so the parent had nothing uncommitted and HEAD already
		// is its world. The rung is still the universe rung: what makes it one is
		// the files git never sees, and they came across regardless.
		return "", true
	}
	if _, err := git(dir,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"commit", "-m", groundCommitMessage(title)); err != nil {
		return "", false
	}
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(head), true
}

// hideFurrowMarker keeps furrow's own bookkeeping out of the person's `git
// status`.
//
// Attaching a folder writes a `.furrow/` directory into it, and in a repository
// that directory is an untracked path the person never made and did not ask for
// — litter by this file's own standard, and worse than litter at a landing: an
// untracked file that a merge would have to write over is a merge git refuses
// outright. So the pattern goes in `.git/info/exclude`, which is the one place
// git keeps a rule that is THIS CHECKOUT'S ALONE — never committed, never
// pushed, never seen by anybody else working on the project.
//
// It is best-effort and answers nothing: a ground with no `.git` of its own has
// nowhere to put it and needs it nowhere, and a repository that will not take
// the line is a `git status` with one extra directory in it.
func hideFurrowMarker(ground string) {
	if !holdsItsOwnGit(ground) {
		return
	}
	exclude := filepath.Join(ground, ".git", "info", "exclude")
	if held, err := os.ReadFile(exclude); err == nil {
		for _, line := range strings.Split(string(held), "\n") {
			if strings.TrimSpace(line) == furrowMarkerPattern {
				return
			}
		}
	} else if !os.IsNotExist(err) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_, _ = file.WriteString("\n" + furrowMarkerPattern + "\n")
}

// furrowMarkerDir is the directory furrow keeps its workspace ids in, and
// furrowMarkerPattern is that name in git's own spelling for "this directory,
// anywhere". The pattern is derived rather than written twice.
const (
	furrowMarkerDir     = ".furrow"
	furrowMarkerPattern = furrowMarkerDir + "/"
)

// holdsItsOwnGit reports that a directory is a repository whose administrative
// directory is INSIDE IT. A linked worktree has a `.git` file naming somebody
// else's, which is the distinction [universeReaches] turns away on.
func holdsItsOwnGit(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
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
	// The exclusions are the two corners that belong to machinery rather than to
	// anybody's world: a task's private metadata (task_run.go's
	// [aforgeDroppings]), and furrow's own bookkeeping, which a folder gains the
	// moment anything attaches it ([hideFurrowMarker]). Neither is in the
	// worktree this commit is about to be carved into, so committing either
	// would put a file in the child's world that its parent's world does not
	// have.
	if _, err := withIndex("add", "-A", "--", ".", ":(exclude)"+aforgeDroppings, ":(exclude)"+furrowMarkerDir); err != nil {
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

// ownRepository reports that the node's branch lives in A REPOSITORY OF ITS OWN
// — the universe it was forked into — rather than in a worktree the ground
// repository has registered.
//
// IT IS THE ONE QUESTION THE TWO LANDING ROADS DIFFER ON, and it is asked here
// rather than at each of them so that nothing outside this file has to know how
// a rung works. Everything else about the landing — the commit, the replay, the
// merge, the branch that is deleted afterwards, the sentence a person reads — is
// the same road for both, which is the promise the ladder is forbidden to
// change.
func (t taskTree) ownRepository() bool {
	return t.rung == GroundRungUniverse && strings.TrimSpace(t.dir) != ""
}

// branchHolder is the repository the node's BRANCH is in: the fork for a node
// grounded in a universe, and the ground itself for every other node. It is
// what a caller asks when it wants to check the branch out somewhere
// (task_audit.go's [restoreFromBranch]) rather than to merge it.
func (t taskTree) branchHolder() string {
	if t.ownRepository() {
		return t.dir
	}
	return t.root
}

// carryBranchHome is [taskTree.carryBranchHomeLocked] for a caller that does not
// already hold the ground repository's lock, which is every landing that keeps a
// branch rather than merging it ([keptWork]).
func (t taskTree) carryBranchHome() {
	if !t.ownRepository() || strings.TrimSpace(t.root) == "" || strings.TrimSpace(t.branch) == "" {
		return
	}
	defer lockGitRoot(t.place, t.root)()
	_, _ = t.carryBranchHomeLocked()
}

// carryBranchHomeLocked puts the node's commits where the GROUND can reach them,
// and it is the whole of the landing road the universe rung needed.
//
// A branch cut in a worktree has been in the ground's own repository since the
// moment it was cut, so for every other rung this is nothing at all. A branch
// cut inside a universe is in a repository of its own — a byte-exact copy of the
// ground, which is why the two share every object either of them had at the fork
// and the fetch carries only what the node itself wrote. Afterwards the ground
// holds the same branch, by the same name, that it would have held on any other
// rung: the merge below it, the `git checkout` a person does when a run failed,
// and the sentence naming the branch are all true either way.
//
// IT DOES NOT FORCE THE UPDATE. The only way the ground can already hold this
// name at a different commit is that somebody kept the branch, checked it out
// and worked on it, and overwriting that would be the machinery throwing away a
// person's commits to save itself a sentence.
func (t taskTree) carryBranchHomeLocked() (string, error) {
	if !t.ownRepository() || strings.TrimSpace(t.root) == "" || strings.TrimSpace(t.branch) == "" {
		return "", nil
	}
	return git(t.root, "fetch", "--no-tags", t.dir, t.branch+":"+t.branch)
}

// releaseLanded gives back the working copy of a node whose work is IN, and it
// is the mirror image of [taskTree.releaseKeptLocked], which gives back the
// working copy of one whose work is not.
//
// A worktree is unregistered before its directory goes, or the ground repository
// is left pointing at a path that a later sweep may remove underneath it. A
// universe was never registered with anybody, so the directory is simply the
// session's own and goes with the work it carried; what furrow is told is that
// the record may be forgotten ([taskTree.dropUniverse]).
func (t taskTree) releaseLanded() {
	if t.ownRepository() {
		_ = os.RemoveAll(t.dir)
	} else if _, err := git(t.root, "worktree", "remove", t.dir); err != nil {
		_, _ = git(t.root, "worktree", "remove", "--force", t.dir)
	}
	// The branch is gone only once its work is in: the removal above is what
	// keeps `git branch -d` from refusing on a branch that is still checked out.
	_, _ = git(t.root, "branch", "-d", t.branch)
	// And the session's own directory once its last working copy has gone home.
	// The remove is deliberately not recursive: it succeeds on an empty directory
	// and fails on one that still holds a node, which is precisely the question
	// being asked. Without it every conversation that ever ran a task would leave
	// an empty directory behind forever.
	_ = os.Remove(filepath.Dir(t.dir))
	// The work is in, so the universe that carried it is furrow's to forget. A
	// refusal is nothing this caller can act on and nothing it reports: what a
	// landing leaves behind when furrow will not drop the record is one line in a
	// listing, which is the reading [furrow.Workspace.DropFork] leaves to whoever
	// asked. The sweep's reading is the other one.
	_ = t.dropUniverse()
}

// dropUniverse tells furrow to forget a fork. The files are the session's to
// remove and are left alone; what is dropped is the record, so that `furrow
// forks` in somebody's project does not accumulate one line per task this
// machine has ever run.
//
// IT IS THE ONE DOOR ONTO FURROW'S FORK RECORDS and it has two callers with two
// different stakes in the answer: [taskTree.releaseLanded], for a fork whose
// work has come home, and the sweep that reaps a session killed mid-run
// (sweep.go's [dropSweptForks]). So it reports what happened rather than
// deciding what a miss is worth — a fork nothing will ever name again is a
// different kind of leftover from one whose work is safely in.
//
// A node that was never grounded in a universe has no record to drop and this
// says so with a nil, which is why every caller may ask unconditionally.
func (t taskTree) dropUniverse() error {
	if t.rung != GroundRungUniverse || strings.TrimSpace(t.universe) == "" || strings.TrimSpace(t.ground) == "" {
		return nil
	}
	workspace := furrow.Open(context.Background(), t.ground)
	if workspace == nil {
		// Furrow is not on this machine any more, or the ground has been deleted,
		// moved or detached since the fork was made. The record, wherever it is,
		// is out of reach from here.
		return errors.New("furrow is not here to forget the fork " + t.universe + " of " + t.ground)
	}
	return workspace.DropFork(context.Background(), t.universe)
}

// universeInRecord is the tree a caller holding ONLY WHAT WAS WRITTEN DOWN can
// forget a fork with, and false for a node that never had one.
//
// THE SWEEP IS WHY IT EXISTS. A landing holds the whole tree it carved; a
// session killed mid-run leaves nothing but its checkpoint, and the reaper that
// removes that session's folder (sweep.go's [reapSession]) is the last thing on
// this machine that will ever know the fork's name. Rebuilding the three fields
// [taskTree.dropUniverse] reads — rather than calling furrow from the sweep —
// is what keeps one door onto those records: whatever a landing does to forget a
// fork, a sweep does exactly the same thing, and a fourth rung added to the
// ladder changes both at once or neither.
func universeInRecord(record taskRecord) (taskTree, bool) {
	tree := taskTree{
		rung:     record.Rung,
		ground:   strings.TrimSpace(record.Ground),
		universe: strings.TrimSpace(record.Universe),
	}
	if tree.rung != GroundRungUniverse || tree.ground == "" || tree.universe == "" {
		return taskTree{}, false
	}
	return tree, true
}

// world is the one line that says what a task worked in, for the node's log and
// for a report. THE EMPTINESS LAW: a task standing in the folder it was already
// about has no copy to describe and this says nothing at all.
//
// IT IS THE LONG FORM AND [GroundWord] IS THE SHORT ONE, and they are two
// sentences about one fact rather than two facts: this names the ground and
// seals the world with an id so that two reports about two worlds can be told
// apart, because it is read in a log by somebody reconstructing a run. A row on
// a card has no room for either and its reader wants neither.
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
