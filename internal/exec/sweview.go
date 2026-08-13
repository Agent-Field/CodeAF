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

	"github.com/Agent-Field/aforge-v2/internal/home"
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

	// substrate is the commit this NODE's whole change set is measured from,
	// across every pass the node ever gets. base above is the commit THIS pass's
	// worktree grew from, which is a different fact and moves whenever a sibling
	// lands: a node's second pass grows from a root that already contains the
	// node's own first pass, so a change set measured from it reports nothing.
	//
	// It lives in a git ref rather than on this struct because a pass is a
	// process. See [sweView.anchor].
	substrate string
	// landed records that this run's work reached the shared workspace, which is
	// what decides WHERE the change set can be read from — the root once it has
	// landed, the leaf's own branch while it has not.
	landed bool

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
		private: space.PrivateScratch(),
		locks:   sweRootLocksFor(root), trace: trace,
	}

	initialized, err := ensureGitRepository(ctx, root, obsDir+"/", traceDir+"/")
	if err != nil {
		return nil, false, err
	}
	// Before either arm, because both of them are a pass of the same node and
	// the node's substrate must be the same commit for both. A leaf that runs in
	// place on its first attempt and in a view on its second is one node with
	// one change set.
	view.anchor(ctx)

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

// anchor settles the commit this node's change set is measured from, once, for
// the whole life of the node.
//
// The ref is the mechanism and the mechanism is the point. Nothing is carried
// in memory between passes: a pass is a process, and a process that crashed
// mid-run, was cancelled at the wall clock, or is a repair round started an hour
// later has no memory of the one before it. git does. The first pass writes the
// ref at the shared workspace's HEAD; every later pass reads it and writes
// nothing, so `base..HEAD` is the node's whole change set on pass one, pass two
// and pass six alike, including the passes that landed and the passes that did
// not.
//
// It is read-not-rewritten deliberately and that is the whole invariant. A pass
// that moved the ref to its own starting point would erase the previous passes'
// work from the account the moment a repair round ran — which is exactly the
// per-pass narration bug this replaces, reimplemented in git.
//
// Failure is silent and leaves substrate empty, which every reader treats as
// "not measured" rather than "measured, and nothing changed": a workspace with
// no commits at all reaches here on the path where ensureGitRepository could
// not make one, and a node whose base cannot be named still has to run.
func (v *sweView) anchor(ctx context.Context) {
	ref := sweBaseRef(v.leaf)
	if existing := gitLines(ctx, v.root, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); len(existing) > 0 {
		v.substrate = existing[0]
		return
	}
	heads := gitLines(ctx, v.root, "rev-parse", "HEAD")
	if len(heads) == 0 {
		return
	}
	if err := gitQuiet(ctx, v.root, "update-ref", ref, heads[0]); err != nil {
		// The commit is still the right answer for THIS pass; what is lost is
		// only its durability, and a change set measured from a base that a
		// later pass will recompute is better than none at all.
		v.trace.note("workspace: this node's base commit could not be recorded (" + err.Error() +
			") — its change set is measured from the workspace as it stands")
	}
	v.substrate = heads[0]
}

// measure names where THIS PASS's work can be read and the commit it grew from.
//
// Two arrangements, and they are not a special case of each other. Work that
// reached the shared workspace is read there, against the state of that
// workspace immediately underneath this leaf's own landing — never against the
// node's durable base, because a sibling that landed while this leaf was working
// sits between the two, and a diff taken across it would credit this leaf with
// the sibling's files. Work still sitting on an undelivered branch is read on
// that branch, from the commit the branch grew from, for exactly the same
// reason.
//
// The node's durable base is not this and does not compete with it: this is one
// pass, and what makes the ACCOUNT per-node is that every pass records its own
// range and the account is their union. See [SWE.substrate].
// live is the third answer and it is a safety fact rather than a convenience.
// It says this directory's WORKING TREE is ours to read: uncommitted edits and
// untracked files in it are this leaf's or nobody's. It is false for exactly one
// arrangement — an isolated leaf that has already landed — because the shared
// tree stops being ours the moment the lease is given back, and a sibling
// half-way through its own `merge --squash` has that leaf's files staged in it
// with the commit not yet written. Reading the porcelain there attributes the
// sibling's landing to this leaf, which is measured and is exactly what the
// per-pass range exists to prevent. Nothing of ours is lost by not looking: a
// branch that landed was squashed and committed in full.
func (v *sweView) measure(before repoState) (dir, base string, live bool) {
	if v != nil && v.isolated && !v.landed {
		return v.dir, v.base, true
	}
	if v != nil {
		return v.root, before.head, !v.isolated
	}
	return "", before.head, false
}

// nodeBase is where this node's history starts — the commit its durable ref was
// pinned at the first time it ever opened a view. It is what the account reports
// as the range's near end, and it is nil-safe because a view that could not be
// opened at all still reaches the code that lands one.
func (v *sweView) nodeBase() string {
	if v == nil {
		return ""
	}
	return v.substrate
}

// sweBaseRef names one node's base commit: where the node's history starts,
// written once and never moved. It is under refs/aforge/ rather than refs/heads/
// so it is invisible to `git branch`, never checked out, and deletable by a
// person in one command — and it is a ref rather than a file because git is the
// thing that already survives a crash here.
func sweBaseRef(leaf string) string { return "refs/aforge/leaf-base/" + sweSlug(leaf) }

// swePassRefs is the namespace one node's passes are recorded under, one ref per
// pass. See [swePassRef].
func swePassRefs(leaf string) string { return "refs/aforge/leaf-pass/" + sweSlug(leaf) }

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
	// reuse either name until both are gone.
	_ = gitQuiet(ctx, v.root, "worktree", "remove", "--force", dir)
	_ = os.RemoveAll(dir)
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
	// head is the shared workspace's commit immediately AFTER this leaf's
	// landing, read while the lease is still held.
	//
	// It is read here rather than by the caller for the one reason the lease
	// exists. The caller runs after this returns, by which time the lease is
	// given back and a sibling may already have landed on top — so a HEAD read
	// there names a commit that includes somebody else's work, and this leaf's
	// range would be `before..that`, crediting it with the sibling's files. The
	// pair (before, head) is one observation and has to be taken as one.
	head string
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
		// A leaf that worked in the shared directory has already landed
		// everything it will ever land: it committed there.
		v.landed = true
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
		// Nothing was withheld, so the shared root is where this node's change
		// set is read from. On a node whose earlier pass DID land, that reading
		// is the earlier pass's work, which is the honest answer: it is still
		// there and it is still this node's.
		v.landed = true
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
	if gitQuiet(ctx, v.root, "diff", "--cached", "--quiet") == nil {
		// The branch has commits and the tree already has their effect —
		// somebody landed the same change first. Nothing to commit, nothing
		// wrong, and a `git commit` here would fail on an empty index.
		_ = gitQuiet(ctx, v.root, "reset")
		v.trace.note("workspace: the change was already in the workspace — nothing to land")
		v.landed = true
		v.remove(ctx)
		return sweLanding{}
	}
	if err := sweCommit(ctx, v.root, message); err != nil {
		_ = gitQuiet(ctx, v.root, "reset")
		return sweLanding{refusal: "this leaf's change could not be committed to the shared workspace (" +
			err.Error() + "); it is on branch " + v.branch + " in " + v.dir}
	}
	v.trace.note("workspace: landed as one commit on the shared workspace from branch " + v.branch)
	v.landed = true
	landed := sweLanding{before: before}
	if head := gitLines(ctx, v.root, "rev-parse", "HEAD"); len(head) > 0 {
		landed.head = head[0]
	}
	v.remove(ctx)
	return landed
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

// commitRemainder puts whatever the engine left uncommitted onto the branch, so
// the squash below has all of the work rather than most of it. The engine's own
// exclusions keep its machinery out of this: `.codeaf/` and the plan database
// are in the repository's exclude file before the first stage runs.
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

func (v *sweView) remove(ctx context.Context) {
	_ = gitQuiet(ctx, v.root, "worktree", "remove", "--force", v.dir)
	_ = os.RemoveAll(v.dir)
	_ = gitQuiet(ctx, v.root, "worktree", "prune")
	if v.branch != "" {
		_ = gitQuiet(ctx, v.root, "branch", "-D", v.branch)
	}
}

// sweep removes the engine's own state from a directory that is not ours.
//
// It names the engine's exclusion list rather than guessing (internal/swepro/
// internal/util/gitexclude.go): those are exactly the paths the engine tells
// git to ignore, which is exactly the set it considers its own bookkeeping.
// Nothing tracked is ever touched.
func (v *sweView) sweep() {
	for _, name := range sweEngineState {
		_ = os.RemoveAll(filepath.Join(v.dir, name))
	}
	v.trace.note("workspace: the engine's own state was removed from the workspace")
}

// sweEngineState is the engine's git-exclusion list, read from outside. It is
// duplicated rather than imported for the reason the whole vendored engine is
// reached through one door: this package may not import the engine's internals.
var sweEngineState = []string{
	".codeaf", ".plandb", ".plandb.db", ".plandb.db-shm", ".plandb.db-wal",
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
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return home.Join("views", hex.EncodeToString(sum[:6]), sweSlug(leaf))
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
