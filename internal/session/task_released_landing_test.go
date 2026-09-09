package session

// A LANDING THAT ARRIVES AFTER THE SETTLE GAVE THE WORKING COPY BACK (#653).
//
// The live run these tests are written from went like this: the node settled
// needing somebody's look, the settle road committed its work and turned its
// directory back into ordinary files ([taskTree.releaseKeptLocked]), and a
// minute later the work was accepted. The accept landed the ordinary way, into
// that same directory, and git answered `fatal: not a git repository` — which
// the landing read as the person's disk refusing them and reported as "taken as
// it stands, and it could not be brought home". Nothing had gone wrong with the
// work, nothing was lost, and every word the person read was false.
//
// The node had also renamed its own task branch to the name its brief asked for,
// which is legitimate and which nothing was written to survive: the name the
// tree was carved with then named nothing at all, so the check could not cut a
// fresh copy of the work either.
//
// So each test here settles a node for real in a real repository, renames the
// branch the way the run did, and then accepts — and asserts the two things the
// run got wrong: THE LANDING DOES WHAT ITS MODE PROMISES, and the branch it
// names is the branch git will answer to.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// settledNeedingALook is the state the accept in every test below arrives at: a
// node whose work is committed on its own branch, whose working copy has been
// given back, and whose branch is not the one it was carved with.
func settledNeedingALook(t *testing.T, agent *Agent, node *TaskNode, repo, renamed string) taskTree {
	t.Helper()
	// THE COPY LIVES IN THE SESSION'S OWN FOLDER, which is where a conversation
	// with a place on disk puts its trees and where the run this is written from
	// had it. A tree inside the person's repository would hide the whole defect:
	// git walks upwards, so a directory under the repository still answers "yes"
	// to `--is-inside-work-tree` after its own registration has gone.
	place := Place{Dir: t.TempDir(), Workspace: repo}
	tree, err := prepareTaskTree(place, repo, "ffff6666ffff9999", 21, "repair the duty log")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "dutylog.py"), "def log():\n    return 1\n")
	// A leaving nobody's ledger names, which the pickup must carry back untouched.
	writeFile(t, filepath.Join(tree.dir, "scratch.txt"), "notes to nobody\n")
	// THE NODE RENAMES THE BRANCH IT IS STANDING ON, exactly as the run did.
	mustGit(t, tree.dir, "branch", "-m", renamed)

	merge, changed := keptWork(tree, "repair the duty log", []string{"dutylog.py"})
	if merge != mergeAborted {
		t.Fatalf("the settle answered %q, want the mark a kept branch wears", merge)
	}
	if _, err := os.Stat(filepath.Join(tree.dir, ".git")); err == nil {
		t.Fatal("the settle left a registration behind, so this test is not standing where the run was")
	}
	if !branchIsThere(repo, renamed) {
		t.Fatalf("the settle did not leave the work on %s", renamed)
	}
	node.setTree(tree)
	node.finish(withYourCallLead(TaskFacts{Merge: merge}, "nobody could check it in 5m0s"), changed, tree.branch, merge)
	if _, _, branch, _ := node.leavings(); branch != renamed {
		t.Fatalf("first settled notice names stale branch %q, want %q", branch, renamed)
	}
	return tree
}

// AND THE ACCEPT MERGES, on a checkout tasks are allowed to write. This is the
// whole of D1: the promise an accept makes is that the work comes home, and a
// copy this runtime gave back is picked up again rather than reported as a disk
// that refused ([taskTree.reopenReleased]).
func TestAcceptingAfterTheCopyWasGivenBackLandsTheRenamedBranch(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	repo := newTestRepo(t)
	tree := settledNeedingALook(t, agent, node, repo, "fix/dutylog-pipeline")

	if err := agent.acceptTask(node, "I read it myself"); err != nil {
		t.Fatalf("acceptTask: %v", err)
	}

	report, _, branch, merge := node.leavings()
	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, want the accepted work done (report %q)", state, report)
	}
	if merge != mergeMerged {
		t.Fatalf("merge = %q, want the work merged — the copy was aforge's to pick up (report %q)", merge, report)
	}
	for _, false_ := range []string{"not a git repository", keptWhereItIsLead, "could not be brought home"} {
		if strings.Contains(report, false_) {
			t.Fatalf("the report says %q over a state the runtime made itself:\n%s", false_, report)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "dutylog.py")); err != nil {
		t.Fatalf("the work never reached the person's branch: %v", err)
	}
	// AND THE BRANCH THE RECORD NAMES IS THE ONE GIT ANSWERS TO. A person handed
	// the name the tree was carved with would be handed a ref that is not there.
	if branch != "fix/dutylog-pipeline" && branch != "" {
		t.Fatalf("branch = %q, want the name the work is actually on", branch)
	}
	if strings.Contains(report, tree.branch) && tree.branch != "fix/dutylog-pipeline" {
		t.Fatalf("the report names a branch nobody can reach:\n%s", report)
	}
}

// AND ON A CHECKOUT TASKS DO NOT WRITE, THE KEEP STILL KEEPS. Picking the copy
// up again changes nothing about where work is allowed to land: the branch is
// named, the person's checkout is untouched, and the sentence is the ordinary
// kept one rather than a refusal (task_branch_protection.go).
func TestAcceptingAfterTheCopyWasGivenBackKeepsAProtectedCheckout(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	repo := newTestRepo(t)
	mustGit(t, repo, "checkout", "-b", "main")
	tree := settledNeedingALook(t, agent, node, repo, "fix/dutylog-pipeline")

	if err := agent.acceptTask(node, "I read it myself"); err != nil {
		t.Fatalf("acceptTask: %v", err)
	}

	report, _, branch, merge := node.leavings()
	if merge != mergeKept {
		t.Fatalf("merge = %q, want the branch kept on a checkout tasks do not write (report %q)", merge, report)
	}
	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, want kept work counted as done", state)
	}
	if strings.Contains(report, "not a git repository") {
		t.Fatalf("the report blames the disk for the runtime's own state:\n%s", report)
	}
	if !strings.Contains(report, "fix/dutylog-pipeline") {
		t.Fatalf("the report never names the branch the work is on:\n%s", report)
	}
	if branch != "fix/dutylog-pipeline" {
		t.Fatalf("branch = %q, want the name the work is actually on", branch)
	}
	if _, err := os.Stat(filepath.Join(repo, "dutylog.py")); err == nil {
		t.Fatal("a protected checkout was written to")
	}
	// AND THE FILES ARE ALL STILL THERE. A pickup that overwrote or dropped a
	// leaving would be a repair that cost somebody work nobody had a copy of.
	if _, err := os.Stat(filepath.Join(tree.dir, "scratch.txt")); err != nil {
		t.Fatalf("the leaving nobody's ledger named is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree.dir, "dutylog.py")); err != nil {
		t.Fatalf("the work is gone from the copy that holds it: %v", err)
	}
}

// AND THE CHECK CAN STILL CUT A FRESH COPY OF WORK ON A RENAMED BRANCH. This is
// the half of the run that failed BEFORE the settle: `fatal: invalid reference`
// dropped both checking calls into the node's own directory, which is the tree
// the check exists not to trust ([restoreFromBranch]).
func TestAFreshCheckoutFollowsTheBranchTheNodeRenamed(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "ffff6666ffffaaaa", 22, "repair the duty log")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "dutylog.py"), "def log():\n    return 1\n")
	mustGit(t, tree.dir, "add", "-A")
	mustGit(t, tree.dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "the work")
	mustGit(t, tree.dir, "branch", "-m", "fix/dutylog-pipeline")

	ground, problem := restoreFromBranch(tree, []string{"dutylog.py"})
	if problem != "" {
		t.Fatalf("a fresh copy of the work could not be made: %s", problem)
	}
	defer ground.drop()
	if !ground.restored {
		t.Fatal("the check fell back to the tree the work was done in")
	}
	if _, err := os.Stat(filepath.Join(ground.dir, "dutylog.py")); err != nil {
		t.Fatalf("the fresh copy does not hold the work: %v", err)
	}
}

// AND A DIRECTORY THAT WAS NEVER A REPOSITORY IS STILL EXACTLY THAT. The repair
// above must not turn every missing `.git` into something aforge claims it can
// put back: a workspace with no repository under it is a fact about the person's
// disk, it is answered where it happens, and it stays answered that way
// (task_run.go's [stageTaskWork], task_land_unsaved.go's [landingRefusal]).
func TestAWorkspaceThatWasNeverARepositoryIsNotAReleasedCopy(t *testing.T) {
	plain := t.TempDir()
	writeFile(t, filepath.Join(plain, "notes.md"), "just files\n")
	if _, released := rememberedRelease(plain); released {
		t.Fatal("a folder nobody released reads as a copy the runtime gave back")
	}
	problem, why := stageTaskWork(plain, []string{"notes.md"})
	if problem == "" || why != refusedByTheTree {
		t.Fatalf("staging in a folder that is no repository answered %q/%v, want the place refusing", problem, why)
	}
}

// Recovery must not delete a previous recovery directory or move the retained
// files piecemeal. A leftover name can contain the only copy of an earlier edit.
func TestReleasedPickupPreservesPriorRecoveryAndLooseFiles(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	repo := newTestRepo(t)
	tree := settledNeedingALook(t, agent, node, repo, "fix/preserve-recovery")
	prior := tree.dir + ".rehoming"
	if err := os.MkdirAll(prior, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(prior, "only-copy.txt"), "earlier recovery")
	writeFile(t, filepath.Join(tree.dir, "loose.txt"), "new uncommitted notes")
	reopened, back, problem := tree.reopenReleased()
	if !back || problem != "" {
		t.Fatalf("pickup failed: %s", problem)
	}
	for path, want := range map[string]string{
		filepath.Join(prior, "only-copy.txt"):    "earlier recovery",
		filepath.Join(reopened.dir, "loose.txt"): "new uncommitted notes",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("lost %s: %q %v", path, got, err)
		}
	}
	reopened.releaseKept()
}

func TestReleasedPickupWithMissingBranchStaysAnswerableAndKeepsFiles(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	repo := newTestRepo(t)
	tree := settledNeedingALook(t, agent, node, repo, "fix/deleted-branch")
	mustGit(t, repo, "branch", "-D", "fix/deleted-branch")
	if err := agent.acceptTask(node, "I read it"); err != nil {
		t.Fatal(err)
	}
	report, _, _, merge := node.leavings()
	if node.stateNow() != TaskUnverified || merge != mergeAborted || strings.Contains(report, "holds the work") {
		t.Fatalf("missing branch reported delivered: %s %s %s", node.stateNow(), merge, report)
	}
	if _, err := os.Stat(filepath.Join(tree.dir, "dutylog.py")); err != nil {
		t.Fatal(err)
	}
}

func TestReleasedPickupRecognizesAnOlderLeavingsRecord(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	repo := newTestRepo(t)
	tree := settledNeedingALook(t, agent, node, repo, "fix/legacy-release")
	tree.branch = "fix/legacy-release"
	forgetReleased(tree.dir)
	reopened, back, problem := tree.reopenReleased()
	if !back || problem != "" {
		t.Fatalf("older release not recovered: %s", problem)
	}
	if _, err := os.Stat(filepath.Join(reopened.dir, "dutylog.py")); err != nil {
		t.Fatal(err)
	}
	reopened.releaseKept()
}

// A retained folder may sit below another checkout. Git walks upward when its
// own registration is absent; recovery must never reset the containing index.
func TestReleasedPickupInsideCheckoutDoesNotBorrowItsParentsRegistration(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	repo := newTestRepo(t)
	tree := settledNeedingALook(t, agent, node, repo, "fix/nested-pickup")
	nested := filepath.Join(repo, "retained-task")
	if err := os.Rename(tree.dir, nested); err != nil {
		t.Fatal(err)
	}
	tree.dir = nested
	writeFile(t, filepath.Join(repo, "parent-only.txt"), "person's staged work")
	mustGit(t, repo, "add", "parent-only.txt")
	before, err := git(repo, "diff", "--cached", "--binary")
	if err != nil {
		t.Fatal(err)
	}
	reopened, back, problem := tree.reopenReleased()
	if !back || problem != "" {
		t.Fatalf("pickup failed: %s", problem)
	}
	defer reopened.releaseKept()
	if after, err := git(repo, "diff", "--cached", "--binary"); err != nil || after != before {
		t.Fatalf("pickup changed the containing checkout's index:\nbefore %s\nafter %s", before, after)
	}
	if root, ok := repositoryRoot(reopened.dir); !ok || root != canonicalPath(reopened.dir) {
		t.Fatalf("pickup borrowed containing repository %q instead of restoring its own registration", root)
	}
	if currentBranch(reopened.dir) != "fix/nested-pickup" {
		t.Fatalf("pickup selected the containing checkout's branch")
	}
}
