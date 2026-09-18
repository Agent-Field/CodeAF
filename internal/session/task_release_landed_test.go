package session

import (
	"os"
	"strings"
	"testing"
)

// A TREE WITH NO GROUND IS LEFT ALONE BY ITS RELEASE.
//
// [taskTree.releaseKept] refuses a tree with an empty root or directory, for
// the reason gitWith does: a command with no directory runs wherever the
// process happens to be, and a removal and a branch deletion pointed there
// take apart the checkout the process is standing in. [taskTree.releaseLanded]
// is the mirror image of releaseKept and was missing both the refusal and the
// root lock; these tests hold the refusal in place.

// TestReleaseLandedLeavesAnEmptyTreeAlone proves the empty case never runs git
// at all: the tree is released while the process stands inside a real
// repository, and neither that repository's worktrees nor its branches move.
func TestReleaseLandedLeavesAnEmptyTreeAlone(t *testing.T) {
	repo := newTestRepo(t)
	for _, tree := range []taskTree{
		{},           // nothing was ever grounded
		{dir: repo},  // a directory with no root
		{root: repo}, // a root with no directory
		{root: "  ", dir: "  ", branch: "task/x"}, // whitespace is emptiness too
	} {
		before := gitOut(t, repo, "worktree", "list", "--porcelain")
		branches := gitOut(t, repo, "branch", "--list", "--format=%(refname:short)")

		t.Chdir(repo)
		tree.releaseLanded()

		if after := gitOut(t, repo, "worktree", "list", "--porcelain"); after != before {
			t.Errorf("releasing %+v changed the registered worktrees:\n%s", tree, after)
		}
		if after := gitOut(t, repo, "branch", "--list", "--format=%(refname:short)"); after != branches {
			t.Errorf("releasing %+v changed the branches:\n%s", tree, after)
		}
		if _, err := os.Stat(repo); err != nil {
			t.Fatalf("releasing %+v disturbed the repository: %v", tree, err)
		}
	}
}

// TestReleaseLandedRemovesItsWorktreeAndBranch is the ordinary case beside it:
// a released landing still unregisters its worktree and deletes its branch,
// so the guard above cannot be read as the release having gone quiet.
func TestReleaseLandedRemovesItsWorktreeAndBranch(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	tree, err := prepareTaskTree(place, repo, "abcd1234abcd9876", 3, "paint the fence")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if !strings.Contains(gitOut(t, repo, "worktree", "list"), tree.dir) {
		t.Fatalf("git does not know the worktree %s", tree.dir)
	}

	tree.releaseLanded()

	if list := gitOut(t, repo, "worktree", "list"); strings.Contains(list, tree.dir) {
		t.Errorf("the worktree is still registered:\n%s", list)
	}
	if branchIsThere(repo, tree.branch) {
		t.Errorf("the branch %s is still there", tree.branch)
	}
}
