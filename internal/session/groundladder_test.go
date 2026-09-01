package session

// THE GROUND LAW, PROVED (issue #142): a task inherits its parent's world as it
// is, on every rung of the ladder.
//
// Every test here is written from the same counterfactual, because it is the one
// that cost real money: a parent holds work nobody has committed, hands out a
// child, and the child wakes up in a world that does not contain it. Before
// groundladder.go that was the guaranteed outcome of every division — the branch
// carried HEAD and said so — and four workers spent an hour proving it.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/furrow"
)

// dirtyRepo is the shape every test below starts from: a repository with one
// commit, one line changed that nobody has committed, and one file nobody has
// even added. The untracked file is the half a `git stash` or a `diff HEAD`
// would miss, and it is exactly the half the dogfood run's parent was holding.
func dirtyRepo(t *testing.T) string {
	t.Helper()
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "shared.txt"), "the original line\nand the parent's own\n")
	writeFile(t, filepath.Join(repo, "invented.txt"), "a file the parent made\n")
	return repo
}

// THE ACCEPTANCE, ON THE SNAPSHOT RUNG: the parent writes an uncommitted file,
// divides, and the child's ground contains it.
func TestASnapshotGroundCarriesTheParentsUncommittedWork(t *testing.T) {
	repo := dirtyRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "aaaa1111aaaa1111", 3, "carry the world")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if got := readFile(t, filepath.Join(tree.dir, "shared.txt")); !strings.Contains(got, "the parent's own") {
		t.Fatalf("the child's shared.txt is %q; the parent's uncommitted line is missing", got)
	}
	if got := readFile(t, filepath.Join(tree.dir, "invented.txt")); !strings.Contains(got, "the parent made") {
		t.Fatalf("the child's invented.txt is %q; the parent's untracked file is missing", got)
	}
	// And the promise about a repository is untouched: a registered worktree on
	// a task branch, which is what makes the work a merge later.
	if tree.mode != TaskModeWorktree || !strings.HasPrefix(tree.branch, "task/") {
		t.Fatalf("mode = %q, branch = %q; want a worktree on a task branch", tree.mode, tree.branch)
	}
	if list := gitOut(t, repo, "worktree", "list"); !strings.Contains(list, tree.dir) {
		t.Fatalf("git does not know the worktree:\n%s", list)
	}
	// The child wakes up in a CLEAN tree: its inheritance is a commit, not a pile
	// of somebody else's edits it cannot tell from its own work.
	if status := gitOut(t, tree.dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Fatalf("the child's tree is not clean:\n%s", status)
	}
}

// THE ACCEPTANCE'S THIRD CLAUSE: the child's ground records which rung grounded
// it, so a report can say what world it worked in.
func TestAGroundedTaskRecordsWhichRungMadeItsWorld(t *testing.T) {
	repo := dirtyRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "bbbb2222bbbb2222", 4, "say which world")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.rung != GroundRungSnapshot {
		t.Fatalf("rung = %q, want %q", tree.rung, GroundRungSnapshot)
	}
	if strings.TrimSpace(tree.seal) == "" {
		t.Fatal("the world has no name; a report cannot say what the work was done in")
	}
	if tree.base == "" {
		t.Fatal("a parent with uncommitted work was sealed into no commit")
	}
	world := tree.world()
	if !strings.Contains(world, repo) || !strings.Contains(world, tree.seal[:12]) {
		t.Fatalf("world() = %q; want the ground and the seal in it", world)
	}
	if !strings.Contains(world, "uncommitted work included") {
		t.Fatalf("world() = %q; it does not say the parent's uncommitted work came with it", world)
	}
}

// A PARENT'S OWN CHECKOUT IS NOT TOUCHED BY HANDING OUT A CHILD. The machine
// commit is written through an index of its own, so the person's index, their
// HEAD and their working tree are exactly as they left them.
func TestSealingTheGroundLeavesTheParentsCheckoutAlone(t *testing.T) {
	repo := dirtyRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	before := gitOut(t, repo, "status", "--porcelain")

	if _, err := prepareTaskTree(place, repo, "cccc3333cccc3333", 5, "leave it alone"); err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if now := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); now != head {
		t.Fatalf("the parent's HEAD moved from %s to %s", head, now)
	}
	if now := gitOut(t, repo, "status", "--porcelain"); now != before {
		t.Fatalf("the parent's working tree changed:\nbefore:\n%s\nafter:\n%s", before, now)
	}
}

// A CLEAN PARENT PAYS FOR NOTHING. There is no world to seal when HEAD already
// is the parent's world, so no commit is written and the branch is the branch it
// always was.
func TestACleanGroundIsCarvedStraightFromHead(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "dddd4444dddd4444", 6, "nothing to carry")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.base != "" {
		t.Fatalf("base = %q; a clean parent should be sealed into no commit", tree.base)
	}
	if want := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); tree.seal != want {
		t.Fatalf("seal = %q, want the ground's HEAD %q", tree.seal, want)
	}
	if head := strings.TrimSpace(gitOut(t, tree.dir, "rev-parse", "HEAD")); head != tree.seal {
		t.Fatalf("the child stands at %s, want %s", head, tree.seal)
	}
}

// WHAT COMES HOME IS THE NODE'S OWN WORK AND NOT ITS INHERITANCE. The parent's
// uncommitted world is scaffolding the child stood on; merging it back would
// hand somebody a merge of their own unfinished edits, and git refuses that
// merge outright — which is how every snapshot-grounded landing failed before
// [taskTree.replayOwnWork] existed.
func TestAGroundedTaskLandsWithoutMergingItsInheritance(t *testing.T) {
	repo := dirtyRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "eeee5555eeee5555", 7, "land the work")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "done.txt"), "what the node made\n")
	merge, detail := tree.comeHome("land the work", []string{"done.txt"})
	if merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want it to come home", merge, detail)
	}
	if _, err := os.Stat(filepath.Join(repo, "done.txt")); err != nil {
		t.Fatalf("the node's work did not land in the person's tree: %v", err)
	}
	// The parent's own work is still THEIRS: uncommitted, in their tree, and not
	// swept into history by somebody else's landing.
	status := gitOut(t, repo, "status", "--porcelain")
	if !strings.Contains(status, "shared.txt") || !strings.Contains(status, "invented.txt") {
		t.Fatalf("the parent's uncommitted work was taken away from them:\n%s", status)
	}
	if log := gitOut(t, repo, "log", "--oneline"); strings.Contains(log, "the world this task started from") {
		t.Fatalf("the machine commit came home with the work:\n%s", log)
	}
}

// THE ACCEPTANCE'S FOURTH CLAUSE: a plain folder inherits the parent's folder
// state the same way, on the rung that has always done it.
func TestACopiedGroundCarriesTheFolderAsItStands(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "notes.md"), "what the parent gathered\n")
	place := Place{Dir: t.TempDir(), Workspace: folder}

	tree, err := prepareTaskTreeOn(context.Background(), place, folder, "ffff6666ffff6666", 8, "read the notes",
		taskStand{dir: folder, mode: TaskModeMirror})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn: %v", err)
	}
	if tree.rung != GroundRungCopy {
		t.Fatalf("rung = %q, want %q", tree.rung, GroundRungCopy)
	}
	if got := readFile(t, filepath.Join(tree.dir, "notes.md")); !strings.Contains(got, "what the parent gathered") {
		t.Fatalf("the child's notes.md is %q; the parent's folder did not come with it", got)
	}
	if tree.world() == "" {
		t.Fatal("a copied world says nothing about itself")
	}
}

// THE TOP RUNG, WITH A FURROW THAT REALLY COPIES. The fake stands in for the
// program — this package cannot require a furrow on the machine running its
// tests, and the furrow package's own tests make the same trade for the same
// reason — but everything on this side of the seam is the real thing: the
// attach, the fork, the destination, the world that comes back and the record
// of which rung made it.
func TestAUniverseGroundsACopiedTaskAndSaysSo(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "notes.md"), "what the parent gathered\n")
	writeFile(t, filepath.Join(folder, ".env"), "SECRET=1\n")
	installFakeFurrow(t)
	place := Place{Dir: t.TempDir(), Workspace: folder}

	tree, err := prepareTaskTreeOn(context.Background(), place, folder, "aaaa7777aaaa7777", 9, "read the notes",
		taskStand{dir: folder, mode: TaskModeMirror})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn: %v", err)
	}
	if tree.rung != GroundRungUniverse {
		t.Fatalf("rung = %q, want %q", tree.rung, GroundRungUniverse)
	}
	if tree.seal != "aaaabbbbcccc0009" {
		t.Fatalf("seal = %q, want furrow's own sealed snapshot", tree.seal)
	}
	if got := readFile(t, filepath.Join(tree.dir, "notes.md")); !strings.Contains(got, "what the parent gathered") {
		t.Fatalf("the child's notes.md is %q; the parent's folder did not come with it", got)
	}
	// THE HALF NO OTHER RUNG CARRIES. A `.env` is invisible to git by design, so
	// a world that has one is a world only the universe rung could have made.
	if got := readFile(t, filepath.Join(tree.dir, ".env")); !strings.Contains(got, "SECRET=1") {
		t.Fatalf("the child's .env is %q; the rung that carries what git cannot see did not", got)
	}
	// A copy is still a copy: it lands by laying the files back, which is the
	// promise the ladder is forbidden to change.
	if tree.mode != TaskModeMirror || tree.merge != mergeInPlace {
		t.Fatalf("mode = %q, merge = %q; a universe of a folder must land like a copy", tree.mode, tree.merge)
	}
}

// A REPOSITORY IS NOT GROUNDED IN A UNIVERSE EVEN WHEN FURROW IS RIGHT THERE,
// and this is the law the ladder is written to: the ladder chooses the world, it
// never changes the promise. A repository ground was promised a branch, and a
// universe is a repository of its own that merges into nobody.
func TestAUniverseNeverTakesTheBranchAwayFromARepository(t *testing.T) {
	repo := dirtyRepo(t)
	installFakeFurrow(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "bbbb8888bbbb8888", 10, "keep the branch")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.rung != GroundRungSnapshot {
		t.Fatalf("rung = %q, want %q", tree.rung, GroundRungSnapshot)
	}
	if list := gitOut(t, repo, "worktree", "list"); !strings.Contains(list, tree.dir) {
		t.Fatalf("git does not know the worktree:\n%s", list)
	}
}

// installFakeFurrow puts a furrow-shaped script where internal/furrow looks
// first, so that one test can drive the top rung on any machine.
//
// IT REALLY COPIES THE FOLDER. A fake that only printed furrow's JSON would let
// this file assert that a fork was asked for and never that a world came back,
// which is the only claim worth making about the rung.
func installFakeFurrow(t *testing.T) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "furrow")
	writeFile(t, script, `#!/bin/sh
if [ "$1" = "--version" ]; then echo "furrow 0.1.0"; exit 0; fi
repo="$2"
shift 3
case "$1" in
status)
  echo '{"workspace":"'"$repo"'","head":"aaaabbbbcccc0001","watcher_running":true}'
  ;;
fork)
  name="$2"
  destination=""
  while [ $# -gt 0 ]; do
    if [ "$1" = "--destination" ]; then destination="$2"; fi
    shift
  done
  mkdir -p "$destination"
  cp -a "$repo"/. "$destination"/
  echo '{"plan":{},"result":{"name":"'"$name"'","destination":"'"$destination"'","base_snapshot":"aaaabbbbcccc0001","head_snapshot":"aaaabbbbcccc0009"}}'
  ;;
*)
  echo "the fake furrow was asked for $1" >&2
  exit 2
  ;;
esac
`)
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod the fake furrow: %v", err)
	}
	t.Setenv(furrow.BinaryEnvVar, script)
	furrow.Forget()
	t.Cleanup(furrow.Forget)
}
