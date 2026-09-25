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

	"github.com/Agent-Field/codeaf/internal/furrow"
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
	merge, detail, _, _ := tree.comeHome("land the work", []string{"done.txt"}, gitSignature{})
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

// ── A REPOSITORY TASK INHERITS THE WHOLE UNIVERSE (issue #172) ──────────────
//
// The tests below are the issue's acceptance clauses, plus the one refusal that
// keeps the rung honest. They share one shape, because the promise and the world
// are the two halves of one claim: a repository ground that takes the top rung
// must come home EXACTLY as it did from the rung below, and must arrive holding
// what that rung could not carry. The fourth clause — that the manual answers
// "does my task see my .env" in the asker's own words — is pinned where every
// other question about the pages is, in internal/manual/chat_test.go.

// worldGitCannotSee is [dirtyRepo] plus the two things a repository is
// configured not to see: a `.env` and an installed dependency tree. They are the
// files issue #142 named as "the world" and the ones the snapshot rung has to
// leave behind, because `git add` will not stage what `.gitignore` covers.
func worldGitCannotSee(t *testing.T) string {
	t.Helper()
	repo := dirtyRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".env\nnode_modules/\n")
	mustGit(t, repo, "add", ".gitignore")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "what this project ignores")
	writeFile(t, filepath.Join(repo, ".env"), "SECRET=1\n")
	writeFile(t, filepath.Join(repo, "node_modules", "left-pad", "index.js"), "module.exports = 1\n")
	return repo
}

// THE FIRST ACCEPTANCE CLAUSE: a repository parent holding an uncommitted edit,
// an untracked file, an ignored `.env` and an ignored dependency tree divides,
// and the child's ground has ALL FOUR.
//
// The rung below carries only the first two, and that is not an accident of this
// test: a machine commit is git's world and git's world stops at `.gitignore`.
// TestARepositoryWithoutFurrowFallsToTheSnapshotAndSaysSo below is the same
// scenario with furrow taken away, and it asserts exactly that difference.
func TestAUniverseGroundedRepositoryCarriesTheWholeWorld(t *testing.T) {
	repo := worldGitCannotSee(t)
	installFakeFurrow(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "bbbb8888bbbb8888", 10, "carry the whole world")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.rung != GroundRungUniverse {
		t.Fatalf("rung = %q, want %q", tree.rung, GroundRungUniverse)
	}
	if got := readFile(t, filepath.Join(tree.dir, "shared.txt")); !strings.Contains(got, "the parent's own") {
		t.Fatalf("the child's shared.txt is %q; the parent's uncommitted line is missing", got)
	}
	if got := readFile(t, filepath.Join(tree.dir, "invented.txt")); !strings.Contains(got, "the parent made") {
		t.Fatalf("the child's invented.txt is %q; the parent's untracked file is missing", got)
	}
	if got := readFile(t, filepath.Join(tree.dir, ".env")); !strings.Contains(got, "SECRET=1") {
		t.Fatalf("the child's .env is %q; a repository task still cannot read its parent's environment", got)
	}
	if got := readFile(t, filepath.Join(tree.dir, "node_modules", "left-pad", "index.js")); !strings.Contains(got, "module.exports") {
		t.Fatalf("the child's node_modules is %q; a repository task still cannot run the parent's tests", got)
	}
	// AND IT IS STILL A BRANCH. The promise is what the landing does, so the
	// record a person and every other part of the harness reads is unchanged.
	if tree.mode != TaskModeWorktree || !strings.HasPrefix(tree.branch, "task/") {
		t.Fatalf("mode = %q, branch = %q; want a worktree promise on a task branch", tree.mode, tree.branch)
	}
	if head := strings.TrimSpace(gitOut(t, tree.dir, "rev-parse", "--abbrev-ref", "HEAD")); head != tree.branch {
		t.Fatalf("the child stands on %q, want its own branch %q", head, tree.branch)
	}
	// The child wakes up in a CLEAN tree, exactly as it does on the rung below:
	// an inheritance it cannot tell from its own work is one it will commit.
	if status := gitOut(t, tree.dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Fatalf("the child's tree is not clean:\n%s", status)
	}
	// And the record says which world, so a report can too.
	if world := tree.world(); !strings.Contains(world, "taken whole") {
		t.Fatalf("world() = %q; it does not say the world was taken whole", world)
	}
}

// THE SECOND ACCEPTANCE CLAUSE: the child's commits land on the parent's task
// branch through the existing road, and nothing about the landing reads
// differently from a snapshot-grounded one.
func TestAUniverseGroundedRepositoryLandsOnItsTaskBranch(t *testing.T) {
	repo := worldGitCannotSee(t)
	installFakeFurrow(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "cccc9999cccc9999", 11, "land the work")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.rung != GroundRungUniverse {
		t.Fatalf("rung = %q, want %q", tree.rung, GroundRungUniverse)
	}
	writeFile(t, filepath.Join(tree.dir, "done.txt"), "what the node made\n")
	merge, detail, _, _ := tree.comeHome("land the work", []string{"done.txt"}, gitSignature{})
	if merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want it to come home", merge, detail)
	}
	if got := readFile(t, filepath.Join(repo, "done.txt")); !strings.Contains(got, "what the node made") {
		t.Fatalf("the person's tree has %q; the node's work did not land", got)
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
	// FURROW'S OWN BOOKKEEPING IS NOT IN THEIR WAY. Attaching writes a
	// `.furrow/` into the folder, and an untracked file a merge would write over
	// is a merge git refuses outright ([hideFurrowMarker]).
	if strings.Contains(status, ".furrow") {
		t.Fatalf("furrow's marker is sitting in the person's git status:\n%s", status)
	}
	// And the branch is gone the way a merged branch always goes.
	if out, err := git(repo, "rev-parse", "--verify", "--quiet", tree.branch); err == nil {
		t.Fatalf("the merged branch is still there: %s", out)
	}
}

// THE SECOND CLAUSE'S OTHER HALF: work that is NOT merged is still offered as a
// branch in the person's own repository, because that is what the sentence about
// it says. A branch that only ever existed inside a fork would be a recovery
// artifact nobody could check out.
func TestAKeptUniverseBranchIsInThePersonsOwnRepository(t *testing.T) {
	repo := worldGitCannotSee(t)
	installFakeFurrow(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "dddd0000dddd0000", 12, "keep the work")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "half.txt"), "as far as it got\n")
	merge, changed := keptWork(tree, "keep the work", []string{"half.txt"}, gitSignature{})
	if merge != mergeAborted {
		t.Fatalf("merge = %q, want the branch kept", merge)
	}
	if len(changed) == 0 {
		t.Fatal("the kept work names no files")
	}
	if _, err := git(repo, "rev-parse", "--verify", "--quiet", tree.branch); err != nil {
		t.Fatalf("%s cannot be checked out in the person's repository, and the report offers it to them", tree.branch)
	}
	held := gitOut(t, repo, "show", "--stat", "--oneline", tree.branch)
	if !strings.Contains(held, "half.txt") {
		t.Fatalf("the kept branch does not hold the work:\n%s", held)
	}
}

// THE THIRD ACCEPTANCE CLAUSE: with furrow absent the same scenario passes on
// the snapshot rung MINUS the ignored files, and the record says which rung it
// was. This is the whole of the difference between the two rungs, in one test.
func TestARepositoryWithoutFurrowFallsToTheSnapshotAndSaysSo(t *testing.T) {
	// Nothing is installed: the package's own TestMain pins furrow at a path
	// that is not there, which is the whole of "a machine without furrow"
	// (hermetic_test.go).
	repo := worldGitCannotSee(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "eeee1111eeee1111", 13, "the world git can see")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.rung != GroundRungSnapshot {
		t.Fatalf("rung = %q, want %q", tree.rung, GroundRungSnapshot)
	}
	if got := readFile(t, filepath.Join(tree.dir, "shared.txt")); !strings.Contains(got, "the parent's own") {
		t.Fatalf("the child's shared.txt is %q; the parent's uncommitted line is missing", got)
	}
	if got := readFile(t, filepath.Join(tree.dir, "invented.txt")); !strings.Contains(got, "the parent made") {
		t.Fatalf("the child's invented.txt is %q; the parent's untracked file is missing", got)
	}
	// AND WHAT THIS RUNG CANNOT CARRY IS ABSENT RATHER THAN HALF THERE.
	if _, err := os.Stat(filepath.Join(tree.dir, ".env")); err == nil {
		t.Fatal("the snapshot rung carried an ignored file; git cannot see one, so this is a test lying about which rung ran")
	}
	if _, err := os.Stat(filepath.Join(tree.dir, "node_modules")); err == nil {
		t.Fatal("the snapshot rung carried an ignored directory; git cannot see one")
	}
	// The record is what a report reads, so it has to say so in words.
	if world := tree.world(); !strings.Contains(world, "a branch off") || !strings.Contains(world, "uncommitted work included") {
		t.Fatalf("world() = %q; it does not say which world this was", world)
	}
	if list := gitOut(t, repo, "worktree", "list"); !strings.Contains(list, tree.dir) {
		t.Fatalf("git does not know the worktree:\n%s", list)
	}
}

// A GROUND WHOSE `.git` BELONGS TO SOMEBODY ELSE IS NOT THIS RUNG'S, and it is
// the one refusal worth a test of its own: a linked worktree's `.git` is a line
// naming an administrative directory inside ANOTHER repository, so a byte-exact
// copy of it would write its commits, its branch and its HEAD into that
// repository and move a checkout somebody is standing in.
func TestAUniverseIsNeverForkedFromALinkedWorktree(t *testing.T) {
	repo := dirtyRepo(t)
	installFakeFurrow(t)
	linked := filepath.Join(t.TempDir(), "linked")
	mustGit(t, repo, "worktree", "add", "-b", "beside", linked)
	place := Place{Dir: t.TempDir(), Workspace: linked}

	tree, err := prepareTaskTree(place, linked, "ffff2222ffff2222", 14, "do not move their checkout")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.rung != GroundRungSnapshot {
		t.Fatalf("rung = %q, want the rung below %q", tree.rung, GroundRungSnapshot)
	}
	if head := strings.TrimSpace(gitOut(t, linked, "rev-parse", "--abbrev-ref", "HEAD")); head != "beside" {
		t.Fatalf("the person's own checkout is standing on %q now, and it was on beside", head)
	}
}

// realFurrowEnvVar names a furrow binary to run ONE test against the real
// program. It is opt-in and never found by looking, because this package's own
// TestMain states the law it would otherwise break: a suite whose answers depend
// on what the person running it happens to have installed is a suite that cannot
// be believed. So the fakes above carry every claim, and this carries the one
// thing a fake cannot — that furrow itself forks a repository the way the rung
// assumes it does.
//
//	CODEAF_FURROW_REAL=$(which furrow) go test ./internal/session/ -run RealFurrow
const realFurrowEnvVar = "CODEAF_FURROW_REAL"

// THE WHOLE ROAD, AGAINST THE PROGRAM ITSELF: fork a real repository holding
// everything git can and cannot see, work in it, and come home to a merge.
func TestARealFurrowGroundsARepositoryTaskAndItComesHome(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv(realFurrowEnvVar))
	if binary == "" {
		t.Skip("set " + realFurrowEnvVar + " to a furrow binary to run this against the real program")
	}
	repo := worldGitCannotSee(t)
	t.Setenv(furrow.BinaryEnvVar, binary)
	furrow.Forget()
	t.Cleanup(furrow.Forget)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "aaaa3333aaaa3333", 15, "the real thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.rung != GroundRungUniverse {
		t.Fatalf("rung = %q, want %q — the real furrow would not fork this ground", tree.rung, GroundRungUniverse)
	}
	if got := readFile(t, filepath.Join(tree.dir, ".env")); !strings.Contains(got, "SECRET=1") {
		t.Fatalf("the child's .env is %q", got)
	}
	if got := readFile(t, filepath.Join(tree.dir, "node_modules", "left-pad", "index.js")); !strings.Contains(got, "module.exports") {
		t.Fatalf("the child's node_modules is %q", got)
	}
	if status := gitOut(t, tree.dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Fatalf("the child's tree is not clean:\n%s", status)
	}
	writeFile(t, filepath.Join(tree.dir, "done.txt"), "what the node made\n")
	if merge, detail, _, _ := tree.comeHome("the real thing", []string{"done.txt"}, gitSignature{}); merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want it to come home", merge, detail)
	}
	if got := readFile(t, filepath.Join(repo, "done.txt")); !strings.Contains(got, "what the node made") {
		t.Fatalf("the person's tree has %q; the node's work did not land", got)
	}
	status := gitOut(t, repo, "status", "--porcelain")
	if !strings.Contains(status, "shared.txt") || !strings.Contains(status, "invented.txt") {
		t.Fatalf("the parent's uncommitted work was taken away from them:\n%s", status)
	}
	if strings.Contains(status, ".furrow") {
		t.Fatalf("furrow's marker is sitting in the person's git status:\n%s", status)
	}
}

// installFakeFurrow puts a furrow-shaped script where internal/furrow looks
// first, so that one test can drive the top rung on any machine.
//
// IT REALLY COPIES THE FOLDER. A fake that only printed furrow's JSON would let
// this file assert that a fork was asked for and never that a world came back,
// which is the only claim worth making about the rung.
//
// AND IT REALLY ATTACHES. The real program answers `status` only for a folder it
// has been pointed at, and writes a `.furrow/` directory into that folder when
// it is — which is the whole reason [hideFurrowMarker] exists, so a fake that
// skipped it would leave the one thing nobody could test.
//
// AND IT REALLY KEEPS THE FORK RECORDS, one file per universe under the marker
// directory, which `forks` lists and `fork-rm` removes. A fake that printed a
// drop without forgetting anything would let issue #195's test assert that a
// drop was ASKED FOR and never that the record went, and the record going is the
// entire claim.
func installFakeFurrow(t *testing.T) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "furrow")
	writeFile(t, script, `#!/bin/sh
if [ "$1" = "--version" ]; then echo "furrow 0.1.0"; exit 0; fi
repo="$2"
shift 3
case "$1" in
status)
  if [ ! -d "$repo/.furrow" ]; then echo "this workspace is not watched" >&2; exit 1; fi
  echo '{"workspace":"'"$repo"'","head":"aaaabbbbcccc0001","watcher_running":true}'
  ;;
watch)
  mkdir -p "$repo/.furrow"
  echo "workspace" > "$repo/.furrow/workspace-id"
  echo '{"snapshot":"aaaabbbbcccc0001","workspace":"'"$repo"'"}'
  ;;
fork-rm)
  rm -f "$repo/.furrow/forks/$2"
  echo '{"dropped":"'"$2"'"}'
  ;;
forks)
  printf '['
  separator=""
  for record in "$repo"/.furrow/forks/*; do
    [ -f "$record" ] || continue
    printf '%s' "$separator"
    cat "$record"
    separator=","
  done
  printf ']\n'
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
  mkdir -p "$repo/.furrow/forks"
  echo '{"name":"'"$name"'","destination":"'"$destination"'","base_snapshot":"aaaabbbbcccc0001","head_snapshot":"aaaabbbbcccc0009"}' > "$repo/.furrow/forks/$name"
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

// forkNames is what furrow says it is keeping a record of, in the ground's own
// workspace. It reads through the package every caller reads through, so a test
// asserting that a record went is asserting it the way the harness would see it.
func forkNames(t *testing.T, ground string) []string {
	t.Helper()
	workspace := furrow.Open(context.Background(), ground)
	if workspace == nil {
		t.Fatalf("furrow is not here for %s, so nothing can be asked about its forks", ground)
	}
	forks, err := workspace.Forks(context.Background())
	if err != nil {
		t.Fatalf("furrow forks: %v", err)
	}
	names := make([]string, 0, len(forks))
	for _, fork := range forks {
		names = append(names, fork.Name)
	}
	return names
}
