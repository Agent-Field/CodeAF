package exec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/swepro/enginestate"
)

// A view is where one coding leaf's engine actually runs, and how what it
// produced gets back to the workspace everyone else can see.
//
// It exists because of two things that were measured on live runs, and they are
// the same thing seen from either end.
//
// From the job's end: a workspace is shared, and the coding engine verifies by
// running the repository's own suite — `go test ./...`, the whole project, not
// the files this leaf touched. Four leaves editing one checkout therefore fail
// each other. A sibling half-way through an edit puts the tree in a state that
// does not compile, an innocent leaf runs the suite against it, and the engine
// reads its own change as broken and starts fixing something it never wrote.
// Measured: twice the spend, and the repair loop became the critical path.
// Serialising the leaves would fix it and would also throw away the only reason
// to have four of them.
//
// From the person's end: the engine keeps its state in the directory it runs
// in — `.codeaf/`, `.plandb.db`, scratch notes — and it commits as it merges
// its own judged worktrees. Pointed at somebody's own repository for a two-line
// fix, it left 438 files of machinery beside their work and a run of `wip(edit)`
// commits in their history. None of that is wrong for a directory the harness
// owns; all of it is wrong for a directory a person owns.
//
// One mechanism answers both. The leaf gets its own git worktree off the shared
// repository, on its own branch. It edits there, commits there, verifies there —
// against a tree no sibling can move under it — and when it succeeds, its branch
// is squashed back into the shared root as ONE commit with a real message. The
// engine's litter lives and dies inside the view. The `wip(edit)` commits never
// leave the branch.
//
// Where isolation buys nothing it is not paid for. The FIRST coding leaf to
// claim a harness-owned workspace works in it directly, exactly as before: a
// job with one coding leaf has no sibling to be isolated from and no person's
// files to keep clean, and a worktree checkout is not free on a large
// repository. Every later leaf that arrives while that one is still running
// takes a view. A person's own directory is never worked in directly at all.
//
// The safety of the mixed case is the whole reason occupancy is a lease rather
// than a count: the leaf working in the root holds it for its entire run, and a
// sibling's landing waits on that lease, so nothing a view contains reaches the
// shared tree while somebody is reading it.

// sweRootLocks is what one workspace root is governed by, and its two halves
// are two different shapes of exclusion on purpose.
//
// bootstrap is an ordinary critical section: short, scoped, always released by
// the defer under its own Lock. It guards the git operations that write
// repository metadata — the initial `init`, adding a worktree, pruning stale
// ones.
//
// occupancy is not a critical section at all. It is a LEASE on the shared
// working tree, taken in one function and given back in another, held across a
// whole engine run that may last an hour. A mutex cannot say that: its release
// belongs under its acquisition, which is exactly the law internal/guard holds
// this tree to and exactly the law a lease has to break. A one-slot channel says
// it honestly — a send is "I have it", a receive is "somebody else may" — and it
// is safer besides, because the release rides a defer in the caller that unwinds
// a panic instead of a defer that was never there to run.
type sweRootLocks struct {
	bootstrap sync.Mutex
	occupancy chan struct{}
}

// claim takes the working tree if it is free, and answers immediately either
// way: a leaf that cannot have the shared directory does not wait for it, it
// takes a view of its own instead.
func (l *sweRootLocks) claim() bool {
	select {
	case l.occupancy <- struct{}{}:
		return true
	default:
		return false
	}
}

// await takes the working tree, waiting for whoever has it. It is what a view
// landing its work calls: nobody may be running in the tree while it is written.
func (l *sweRootLocks) await() { l.occupancy <- struct{}{} }

// vacate gives it back.
func (l *sweRootLocks) vacate() { <-l.occupancy }

// sweRoots keys the locks by workspace, so two jobs in flight never wait on
// each other and only leaves sharing one directory do. Keyed by the cleaned
// absolute path — the same directory reached by two spellings is still one
// repository and one .git to race on.
var sweRoots sync.Map // string → *sweRootLocks

func sweRootLocksFor(directory string) *sweRootLocks {
	key := filepath.Clean(directory)
	if absolute, err := filepath.Abs(directory); err == nil {
		key = filepath.Clean(absolute)
	}
	locks, _ := sweRoots.LoadOrStore(key, &sweRootLocks{occupancy: make(chan struct{}, 1)})
	return locks.(*sweRootLocks)
}

// sweView is one leaf's answer to "where do I work, and how does it get home".
//
// It is a value handed down the executor's own call chain rather than a field on
// the worker, because one worker object serves every leaf a headless registry
// dispatches: state about this run may not live on it.
type sweView struct {
	// root is the shared workspace. dir is where the engine runs, and the two
	// are the same string whenever this leaf works in place.
	root string
	dir  string
	// branch and base are the view's own: the branch the engine commits onto,
	// and the root commit it grew from. Both empty in place.
	branch string
	base   string
	// leaf is the identity the view is named for, so a restarted leaf finds its
	// own checkout — with its resume checkpoint still in it — rather than a
	// fresh one.
	leaf string
	// state is this leaf's engine state that does not belong in any tree: the
	// plan database. It sits beside the view rather than inside it, so the one
	// piece of the engine's bookkeeping aforge can address by configuration is
	// not in the deliverable at all. Keyed like the view, so a restart finds
	// the same database and the engine's resume means something.
	state string
	// tracked is the one sentence owed to somebody whose repository already has
	// the engine's bookkeeping committed in it from a run before these
	// defences. Empty is the ordinary case.
	tracked string

	isolated bool
	// occupying records that this run holds the shared working tree, so release
	// gives back exactly what was taken and never somebody else's.
	occupying bool
	// private is true when the root belongs to a person rather than to the
	// harness. It is what decides that the engine may never run in it, and what
	// turns on the sweep of the fallback path.
	private bool

	locks *sweRootLocks
	trace *tracer
}

// sweOpen makes the workspace something the engine can run in and decides where
// this leaf's engine will run.
//
// The repository precondition is settled first and for everybody: a view is a
// worktree of the shared repository, so the shared repository has to exist
// before any view can. The decision after it is one claim on the shared working
// tree, and the order of its two clauses is the policy in two lines — a person's
// directory is never the run directory, and a harness directory is until
// somebody else is already in it.
func sweOpen(ctx context.Context, space *Workspace, leaf string, trace *tracer) (*sweView, bool, error) {
	root := space.Root()
	view := &sweView{
		root: root, dir: root, leaf: leaf,
		state:   sweStateDir(root, leaf),
		private: space.PrivateScratch(),
		locks:   sweRootLocksFor(root), trace: trace,
	}
	_ = os.MkdirAll(view.state, 0o755)

	// The engine's own state is excluded here, with the harness's, and for the
	// same reason: this is the one window between the repository existing and
	// anything being staged. It is not left to the engine, which writes its own
	// exclusions from inside its run directory and — until D7 — wrote them into
	// a file git never reads whenever that directory was a linked worktree,
	// which is every isolated leaf. Written at the ROOT it lands in the common
	// git directory, which is the file every view of this repository shares:
	// one write, effective in all of them, before the first of them exists.
	initialized, err := ensureGitRepository(ctx, root,
		append(enginestate.ExcludePatterns(), obsDir+"/", traceDir+"/")...)
	if err != nil {
		return nil, false, err
	}
	view.tracked = sweTrackedState(ctx, root)
	if view.tracked != "" {
		trace.note("workspace: " + view.tracked)
	}
	sweReap(ctx, root, leaf)

	if !view.private && view.locks.claim() {
		view.occupying = true
		return view, initialized, nil
	}
	if err := view.checkout(ctx); err != nil {
		// A build of git too old for worktrees, a filesystem that refused the
		// checkout: a leaf must still run. It runs in the shared directory,
		// behind the lock rather than beside whoever holds it, and says so — a
		// silent loss of isolation is worse than a slow leaf.
		view.trace.note("workspace: " + err.Error() +
			" — running in the shared directory instead, one leaf at a time")
		view.locks.await()
		view.occupying = true
	}
	return view, initialized, nil
}

// where is the sentence the trace opens with. It is written here rather than at
// the call site because the three arrangements differ in what a reader needs to
// know, and only this type knows which one happened.
func (v *sweView) where(initialized bool) string {
	origin := "an existing git repository"
	if initialized {
		origin = "no committed git repository here — initialised one and committed a baseline"
	}
	if !v.isolated {
		return "workspace: " + origin + ", run in place"
	}
	return "workspace: " + origin + "; this leaf works in its own view at " + v.dir +
		" on branch " + v.branch + ", and lands one commit back when it succeeds"
}

// checkout gives this leaf its own worktree.
//
// A view that is already there is kept rather than rebuilt. That is the whole
// of what makes the engine's resume worth having across a restart: the
// checkpoint the engine writes lives in the directory it ran in, and a leaf
// handed a fresh empty checkout on its second attempt would start over.
func (v *sweView) checkout(ctx context.Context) error {
	v.locks.bootstrap.Lock()
	defer v.locks.bootstrap.Unlock()

	heads := gitLines(ctx, v.root, "rev-parse", "HEAD")
	if len(heads) == 0 {
		return fmt.Errorf("the workspace has no commit for this leaf's own view to grow from")
	}
	dir, branch := sweViewDir(v.root, v.leaf), sweViewBranch(v.leaf)

	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil &&
		gitQuiet(ctx, dir, "rev-parse", "--verify", "HEAD") == nil {
		v.dir, v.branch, v.base, v.isolated = dir, branch, heads[0], true
		return nil
	}
	// Whatever is there is not a working view: a half-made checkout, or the
	// registration a process that died mid-run left behind. Git will refuse to
	// reuse either name until both are gone, and the plan database beside it
	// goes with them: a database describing a checkout that no longer exists is
	// not a checkpoint to resume from, it is a resume into nothing.
	_ = gitQuiet(ctx, v.root, "worktree", "remove", "--force", dir)
	_ = os.RemoveAll(dir)
	_ = os.RemoveAll(v.state)
	_ = os.MkdirAll(v.state, 0o755)
	_ = gitQuiet(ctx, v.root, "worktree", "prune")
	_ = gitQuiet(ctx, v.root, "branch", "-D", branch)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("this leaf's own view could not be made: %w", err)
	}
	if err := gitQuiet(ctx, v.root, "worktree", "add", "--force", "-b", branch, dir, heads[0]); err != nil {
		return fmt.Errorf("this leaf could not be given its own view of the repository: %w", err)
	}
	v.dir, v.branch, v.base, v.isolated = dir, branch, heads[0], true
	return nil
}

// release gives the shared root back. It is idempotent because it is deferred
// on a path with several exits, and unlocking a mutex twice is a panic rather
// than a mistake somebody notices later.
func (v *sweView) release() {
	if v.occupying {
		v.occupying = false
		v.locks.vacate()
	}
}

// sweLanding is what a merge back leaves the caller to do about it.
//
// before replaces the repository state the run started from whenever the change
// arrived as one commit on the shared root: the artifact list is a before/after
// diff, and a leaf whose "before" was read an hour and three siblings ago would
// claim every one of their files as its own. Empty when nothing landed, which
// the caller reads as "keep what you had".
//
// refusal is the one sentence a person is owed when the work is real, whole,
// and cannot be applied. It is deliberately not an error: the leaf did its job,
// the branch is on disk with the change on it, and a conflict is a fact about
// the tree it is landing into.
type sweLanding struct {
	before  repoState
	refusal string
}

// land brings an isolated leaf's work home, and is a no-op for a leaf that was
// already working in the shared directory.
//
// Mechanical, in both directions. On success the branch is squashed into the
// root as one commit — never a fast-forward of the engine's own `wip(edit)`
// history, which is bookkeeping rather than a change anybody asked to read. On
// conflict it aborts and reports; resolving somebody else's merge is not a thing
// a coding leaf gets to invent, and a half-applied change is worse than a
// refused one.
func (v *sweView) land(ctx context.Context, delivered bool, message string) sweLanding {
	if !v.isolated {
		if delivered && v.private {
			// The fallback path: git could not give this leaf a view, so the
			// engine ran in somebody's own directory after all. Its state is
			// git-excluded, which keeps it out of the diff and does nothing at
			// all about it being there. Swept only once the work is delivered —
			// the checkpoint inside it is what a failed leaf resumes from.
			v.sweep()
		}
		return sweLanding{}
	}
	if !delivered {
		v.trace.note("workspace: this leaf did not deliver — its work stays on branch " +
			v.branch + " in " + v.dir)
		return sweLanding{}
	}
	// The engine commits as it merges its own judged worktrees, so a finished
	// run usually leaves a clean tree. Usually is not always, and an edit that
	// was never committed is not on the branch to be squashed.
	v.commitRemainder(ctx, message)
	if head := gitLines(ctx, v.dir, "rev-parse", "HEAD"); len(head) == 0 || head[0] == v.base {
		v.trace.note("workspace: the view is identical to the workspace — nothing to bring back")
		v.discard(ctx)
		return sweLanding{}
	}

	// Nobody may be working in the shared tree while it is written, and two
	// views landing at once must not interleave. Both are the same lock: an
	// in-place sibling holds it for its whole run, which is exactly the wait
	// this needs.
	v.locks.await()
	defer v.locks.vacate()
	v.locks.bootstrap.Lock()
	defer v.locks.bootstrap.Unlock()

	before := readRepoState(ctx, v.root)
	if err := gitQuiet(ctx, v.root, "merge", "--squash", v.branch); err != nil {
		return sweLanding{refusal: v.refuse(ctx)}
	}
	// The last gate, and the only one that cannot be skipped by a mistake made
	// somewhere else. Exclusions govern UNTRACKED files: once the engine's
	// state has been committed onto the branch — by its own eager checkpoint or
	// by our commitRemainder's `add -A`, either of which can win a race with an
	// exclusion that was written late or into the wrong file — `merge --squash`
	// takes the whole tree and no exclude file anywhere is consulted. So the
	// index is read before the commit is written, and the engine's own paths
	// are taken out of it. This is what stops the damage being self-
	// perpetuating: state that lands once is tracked forever after, and every
	// later view checks it back out with no exclusion able to help.
	v.unstage(ctx, before.top)
	if gitQuiet(ctx, v.root, "diff", "--cached", "--quiet") == nil {
		// The branch has commits and the tree already has their effect —
		// somebody landed the same change first, or everything on it was the
		// engine's own bookkeeping and the line above took it out. Nothing to
		// commit, nothing wrong, and a `git commit` here would fail on an
		// empty index.
		_ = gitQuiet(ctx, v.root, "reset")
		v.trace.note("workspace: the change was already in the workspace — nothing to land")
		v.remove(ctx)
		return sweLanding{}
	}
	if err := sweCommit(ctx, v.root, message); err != nil {
		_ = gitQuiet(ctx, v.root, "reset")
		return sweLanding{refusal: "this leaf's change could not be committed to the shared workspace (" +
			err.Error() + "); it is on branch " + v.branch + " in " + v.dir}
	}
	v.trace.note("workspace: landed as one commit on the shared workspace from branch " + v.branch)
	v.remove(ctx)
	return sweLanding{before: before}
}

// refuse cleans up after a merge that would not apply and says what happened.
//
// The reset is conditional on the index actually being conflicted, and that
// condition is not defensive politeness: `git merge --squash` refuses outright
// when local changes would be overwritten, touching nothing, and a `reset
// --merge` fired at that moment would throw away work the merge never went near.
func (v *sweView) refuse(ctx context.Context) string {
	if len(gitLines(ctx, v.root, "ls-files", "--unmerged")) > 0 {
		_ = gitQuiet(ctx, v.root, "reset", "--merge")
	}
	note := "this leaf's change conflicts with what is already in the shared workspace, " +
		"so nothing was applied — it is complete on branch " + v.branch + " in " + v.dir +
		", and merging it is a decision for a person rather than for the run"
	v.trace.note("workspace: " + note)
	return note
}

// unstage takes the engine's own bookkeeping out of the index between the
// squash and the landing commit, and says what it took.
//
// It is the load-bearing half of a defence with three layers, and the only one
// that holds when the other two have already failed. Exclusions (sweOpen)
// govern untracked files and are a hint; the engine's own exclusion writer had
// been writing that hint into a file git never reads for the whole of the
// layout aforge runs leaves in. Once state is committed onto the branch — by
// the engine's eager checkpoint or by commitRemainder's `add -A` — the squash
// takes the tree wholesale and no exclusion is consulted at all. So the index
// itself is read, here, where the commit is about to be written.
//
// Nothing is silent. A drop is named in the trace, and a drop that could not be
// made is named louder: the work still lands, because refusing somebody's
// finished change over bookkeeping would be the worse trade, but nobody is left
// to discover it in a diff six months later.
func (v *sweView) unstage(ctx context.Context, top string) {
	if top == "" {
		top = v.root
	}
	dropped := sweStateIn(gitLines(ctx, v.root, "diff", "--cached", "--name-only"))
	if len(dropped) == 0 {
		return
	}
	// The pathspec is the five NAMES rather than the paths just found, and that
	// is deliberate on two counts: it is a fixed five arguments whether the
	// engine left three files or the measured four hundred and ninety, and it
	// takes out anything under those names that the read above and this write
	// disagreed about. `git reset` with a pathspec matching nothing is a
	// no-op that succeeds, so the fixed list costs nothing when the engine
	// behaved.
	//
	// `:(top)` because a workspace may be a subdirectory of the repository it
	// is in, and git answers in paths from the repository's top while it would
	// read a bare pathspec from this directory.
	_ = gitQuiet(ctx, v.root, append([]string{"reset", "-q", "--"}, sweTopSpec(enginestate.Names())...)...)
	if left := sweStateIn(gitLines(ctx, v.root, "diff", "--cached", "--name-only")); len(left) > 0 {
		v.trace.note("workspace: the engine's own bookkeeping could not be taken out of the landing commit (" +
			sweNames(left) + ") — it is in the commit and it is not part of this change")
		return
	}
	v.trace.note("workspace: the engine's own bookkeeping was left out of the landing commit (" +
		sweNames(dropped) + ")")

	// The squash wrote those files into the shared working tree as well as into
	// the index, and taking them out of the index leaves them lying there. What
	// happens to each one is decided by whether the repository already tracks
	// it, and the rule has no exceptions: a tracked path is restored to what
	// HEAD says — this change did not touch it, so it must read as it did —
	// and an untracked path is the engine's litter in somebody's directory and
	// is removed. Nothing is ever deleted from an index.
	// Asked of the five names rather than of each path, for the reason above:
	// this is bounded work on a set that is not. `--full-name` because git
	// answers `ls-files` relative to the directory it was run in unless it is
	// told otherwise, and every other path here is from the repository's top.
	tracked := map[string]bool{}
	for _, name := range enginestate.Names() {
		found := gitLines(ctx, v.root, "ls-files", "--full-name", "--", ":(top)"+name)
		for _, path := range found {
			tracked[strings.TrimSpace(path)] = true
		}
		if len(found) > 0 {
			// One name at a time, and only the names that matched: `checkout`
			// with a pathspec matching nothing is a fatal error rather than the
			// no-op `reset` gives, and one bad spec would take the whole call
			// down with it.
			_ = gitQuiet(ctx, v.root, "checkout", "-q", "--", ":(top)"+name)
		}
	}
	for _, path := range dropped {
		if tracked[path] {
			continue
		}
		sweDiscard(top, path)
	}
}

// sweStateIn is the engine's own paths among a list of repository paths, in the
// order git gave them.
func sweStateIn(paths []string) []string {
	found := make([]string, 0, len(paths))
	for _, path := range paths {
		if enginestate.Holds(path) {
			found = append(found, path)
		}
	}
	return found
}

// sweTopSpec makes pathspecs that mean the same thing from any directory of the
// repository.
func sweTopSpec(paths []string) []string {
	specs := make([]string, 0, len(paths))
	for _, path := range paths {
		specs = append(specs, ":(top)"+path)
	}
	return specs
}

// sweNames is a path list as one short phrase. Three names and a count, because
// the engine's state is hundreds of files and a trace line is read by a person.
func sweNames(paths []string) string {
	shown := paths
	if len(shown) > 3 {
		shown = shown[:3]
	}
	phrase := strings.Join(shown, ", ")
	if extra := len(paths) - len(shown); extra > 0 {
		phrase += fmt.Sprintf(" and %d more", extra)
	}
	return phrase
}

// sweDiscard removes one untracked engine file and any directory it emptied.
//
// The parents are removed with a bare Remove rather than a recursive one, and
// that is the whole safety of it: an empty directory goes, a directory holding
// anything at all — a file this run never wrote, a sibling's — refuses to and
// the walk stops there. It never climbs past the repository's own top.
func sweDiscard(top, path string) {
	full := filepath.Join(top, filepath.FromSlash(path))
	if err := os.Remove(full); err != nil {
		return
	}
	for parent := filepath.Dir(full); parent != top && parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if os.Remove(parent) != nil {
			return
		}
	}
}

// commitRemainder puts whatever the engine left uncommitted onto the branch, so
// the squash below has all of the work rather than most of it.
//
// It stages everything, deliberately: the engine's judgement about what is part
// of its change is made by what it wrote, not by what a pattern here would
// guess. What keeps its machinery out of the landing commit is not this
// function and is not the engine's exclusions either — an exclusion is a hint
// about untracked files and dies the moment one of them is committed. It is
// unstage above, which reads the index of the shared repository immediately
// before the commit that will hold it forever.
func (v *sweView) commitRemainder(ctx context.Context, message string) {
	if len(gitLines(ctx, v.dir, "status", "--porcelain")) == 0 {
		return
	}
	if gitQuiet(ctx, v.dir, "add", "-A") != nil {
		return
	}
	_ = sweCommit(ctx, v.dir, message)
}

// discard removes the view once its work is home. A failed leaf keeps its view:
// the engine's checkpoint is in it, and that checkpoint is the whole of what a
// restarted coding leaf resumes from.
//
// It writes the same repository metadata a sibling's checkout writes, so it is
// held to the same lock. The caller may already hold it — sync.Mutex is not
// reentrant, so this takes it only when it is free and the merge path, which
// holds it throughout, calls the inner half directly.
func (v *sweView) discard(ctx context.Context) {
	v.locks.bootstrap.Lock()
	defer v.locks.bootstrap.Unlock()
	v.remove(ctx)
}

// remove takes this leaf's view, its plan database and its branch away.
//
// The last line is what stops the view root growing forever. Every view of one
// repository lives under a directory named for that repository's digest, and
// nothing has ever owned that directory: a machine that had run a thousand
// coding leaves against fifty repositories kept fifty empty directories with no
// mechanism anywhere that would ever remove one. A BARE Remove is what makes it
// safe to try on every removal — it succeeds only when the directory is empty,
// so a sibling view still working under it makes this a no-op rather than a
// race, and there is nothing to check first and nothing to lock.
func (v *sweView) remove(ctx context.Context) {
	_ = gitQuiet(ctx, v.root, "worktree", "remove", "--force", v.dir)
	_ = os.RemoveAll(v.dir)
	_ = os.RemoveAll(v.state)
	_ = gitQuiet(ctx, v.root, "worktree", "prune")
	if v.branch != "" {
		_ = gitQuiet(ctx, v.root, "branch", "-D", v.branch)
	}
	if v.isolated {
		_ = os.Remove(filepath.Dir(v.dir))
	}
}

// sweep removes the engine's own state from a directory that is not ours.
//
// The list is the engine's own declaration (internal/swepro/enginestate),
// imported rather than copied: those are exactly the paths the engine tells git
// to ignore, which is exactly the set it considers its own bookkeeping, and a
// second copy of it here is how the boundary drifted the first time. Nothing
// tracked is ever touched.
func (v *sweView) sweep() {
	for _, name := range enginestate.Names() {
		_ = os.RemoveAll(filepath.Join(v.dir, name))
	}
	_ = os.RemoveAll(v.state)
	v.trace.note("workspace: the engine's own state was removed from the workspace")
}

// sweCommit writes one commit with the harness's own identity on the command
// line rather than in anybody's config. `--no-verify` because a repository's
// commit hooks are written for a person at a keyboard, and gpgsign off because
// a harness has no key and a prompt no one can answer is a hang.
func sweCommit(ctx context.Context, directory, message string) error {
	return gitQuiet(ctx, directory,
		"-c", "user.name=aforge",
		"-c", "user.email=agentfield-bot@users.noreply.github.com",
		"-c", "commit.gpgsign=false",
		"commit", "--no-verify", "-m", message)
}

// sweLandingMessage is the commit a person reads six months later.
//
// The subject is what the leaf was asked to do, because that is the sentence
// that was written by somebody who knew the point of it. The body is what the
// engine said it did. The trailer is provenance and obeys the operator's own
// attribution setting — off means the absence of a line, never a line saying a
// machine did not help.
func sweLandingMessage(task Task, said string, attribution bool) string {
	subject := sweFirstLine(task.Title)
	if subject == "" {
		subject = sweFirstLine(task.Brief)
	}
	if subject == "" {
		subject = "a coding change"
	}
	message := subject
	if body := sweFirstLine(said); body != "" && body != subject {
		message += "\n\n" + body
	}
	if attribution {
		message += "\n\n" + AttributionTrailer
	}
	return message
}

func sweFirstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// sweViewDir is where views live: under aforge's own state root, never under
// the repository they are a view of.
//
// Inside would be wrong twice — the engine's verification runs the whole
// project and would descend into a checkout of the project, and the root's own
// `git status` would report a second working tree as untracked noise in
// somebody's repository. The root is named by a digest of its path rather than
// by its basename so two repositories called `api` cannot share a directory,
// and the path is short because a worktree's own path is written into git's
// metadata and macOS still has opinions about how long a socket path may be.
func sweViewDir(root, leaf string) string {
	return filepath.Join(sweViewRoot(root), sweSlug(leaf))
}

// sweViewRoot is the one directory that holds every view of one repository. It
// is named separately from the views inside it because it is now something that
// is owned: remove empties it and the reaper below prunes it.
func sweViewRoot(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return home.Join("views", hex.EncodeToString(sum[:6]))
}

// sweStateDir is where a leaf's engine state goes when it may not be in any
// tree: today the plan database, which is the one piece of the engine's
// bookkeeping addressable from outside (PLANDB_DB, honoured at internal/swepro/
// internal/plandb/persist.go). It sits beside the view rather than inside it,
// keyed the same way, so a resumed leaf opens the same database it wrote.
//
// `.codeaf/` is not here and cannot be: the engine joins that name onto its run
// directory in some forty places of its own, and a variable it does not read is
// not a boundary. What closes that boundary instead is unstage's pathspec at
// the landing commit. A `codeafDir()` seam in the engine — one helper, forty
// call sites — is the work that would let the rest follow the database out.
func sweStateDir(root, leaf string) string {
	return filepath.Join(sweViewRoot(root), sweSlug(leaf)+".state")
}

// plandb is the engine's plan database for this leaf.
func (v *sweView) plandb() string { return filepath.Join(v.state, "plandb.db") }

// sweViewRetention is how long a view that never delivered is kept.
//
// A leaf that failed keeps its view on purpose: the engine's checkpoint is in
// it and a restarted leaf resumes from it. Nothing was ever keeping the other
// side of that promise, so a machine accumulated a checkout and a branch per
// leaf that ever failed, forever, in the view root and in the person's own
// `aforge/leaf/*` ref namespace.
//
// A week is chosen against the one clock that bounds a live run: a coding
// leaf's wall-clock ceiling is hours, so nothing this old can still be running
// and be reaped out from under itself. It is also long enough that a job paused
// on Friday is still resumable on Monday, which is the case the retention is
// for.
const sweViewRetention = 7 * 24 * time.Hour

// sweReap is the view root's garbage collection, and it runs at the one moment
// somebody is already here with the lock in hand: a leaf opening a view of this
// repository. There is no reaper process and no timer, so nothing needs to
// survive a crash — the next open is the reaper, and a crash merely means the
// litter waits for it.
//
// keep is the leaf being opened. Its own view is never touched however old it
// looks, because that view is about to be resumed into.
func sweReap(ctx context.Context, root, keep string) {
	locks := sweRootLocksFor(root)
	locks.bootstrap.Lock()
	defer locks.bootstrap.Unlock()

	views := sweViewRoot(root)
	mine := sweSlug(keep)
	for _, entry := range sweReadDir(views) {
		if entry.Name() == mine || entry.Name() == mine+".state" {
			continue
		}
		if sweFreshness(filepath.Join(views, entry.Name())) < sweViewRetention {
			continue
		}
		stale := filepath.Join(views, entry.Name())
		_ = gitQuiet(ctx, root, "worktree", "remove", "--force", stale)
		_ = os.RemoveAll(stale)
	}
	_ = gitQuiet(ctx, root, "worktree", "prune")

	// A branch outlives its view by exactly one prune: `worktree remove` frees
	// the branch, and what is left is a ref in somebody's repository named for a
	// leaf that no longer has a checkout anywhere. A branch that still has a
	// worktree is a leaf that may yet be resumed and is left alone — git refuses
	// to delete it anyway, which is the same law said twice rather than a check
	// this has to get right.
	live := map[string]bool{}
	for _, line := range gitLines(ctx, root, "worktree", "list", "--porcelain") {
		if branch, found := strings.CutPrefix(line, "branch refs/heads/"); found {
			live[strings.TrimSpace(branch)] = true
		}
	}
	for _, branch := range gitLines(ctx, root,
		"for-each-ref", "--format=%(refname:short)", "refs/heads/aforge/leaf") {
		branch = strings.TrimSpace(branch)
		if branch == "" || branch == sweViewBranch(keep) || live[branch] {
			continue
		}
		_ = gitQuiet(ctx, root, "branch", "-D", branch)
	}
	// And the digest directory itself, when this repository has no views left.
	// Bare, for remove's reason: empty goes, occupied stays.
	_ = os.Remove(views)
}

func sweReadDir(directory string) []os.DirEntry {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	return entries
}

// sweFreshness is how long ago anything happened in a view, from outside it.
//
// A directory's own timestamp moves when an entry is added or removed in it and
// not when a file deeper inside is written, so the engine — which works in
// `.codeaf/` and in its own worktrees — can leave a view whose top-level
// timestamp is as old as the checkout while the run is still going. Reading the
// newest of the directory and everything immediately in it costs one listing
// and closes that gap.
func sweFreshness(directory string) time.Duration {
	info, err := os.Stat(directory)
	if err != nil {
		return 0
	}
	newest := info.ModTime()
	for _, entry := range sweReadDir(directory) {
		if at, err := entry.Info(); err == nil && at.ModTime().After(newest) {
			newest = at.ModTime()
		}
	}
	return time.Since(newest)
}

// sweTrackedState is the sentence owed to somebody whose repository already has
// the engine's bookkeeping committed in it.
//
// It is damage from before these defences existed, and it is self-perpetuating
// by nature: tracked content is checked out into every view, staged by every
// `add -A`, and carried by every squash, with no exclusion anywhere able to
// touch it. aforge will not fix it — removing files from a person's index is a
// decision that belongs to the person, and a harness that quietly deleted
// tracked paths would be a worse actor than the one that added them. So it is
// said, once, in the trace and in what the leaf reports, and the change itself
// is kept clean by unstage.
func sweTrackedState(ctx context.Context, root string) string {
	found := gitLines(ctx, root, append(
		[]string{"ls-files", "--full-name", "--"}, sweTopSpec(enginestate.Names())...)...)
	if len(found) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"the coding engine's own bookkeeping is tracked in this repository from an earlier run "+
			"(%d files, %s) — it is not part of this change, and removing it is a decision for a person",
		len(found), sweNames(found))
}

// sweViewBranch names the leaf's branch. The prefix is a namespace a person can
// delete in one command, and it says out loud whose branch it is.
func sweViewBranch(leaf string) string { return "aforge/leaf/" + sweSlug(leaf) }

// sweSlug makes a leaf key safe to be both a directory and a git ref. A key is
// already a node id in practice; this is what keeps that a fact rather than an
// assumption a stranger's id could break.
func sweSlug(leaf string) string {
	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return '-'
	}, strings.TrimSpace(leaf))
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "leaf"
	}
	return slug
}
