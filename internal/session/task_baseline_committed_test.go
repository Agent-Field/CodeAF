package session

import (
	"context"
	"path/filepath"
	"testing"
)

// A worker may commit its change, so its branch tip cannot identify the world
// that existed before the work began.
func TestAWorkersCommittedFailureIsNotPreExisting(t *testing.T) {
	repo := newGoModuleRepo(t)
	writeFile(t, filepath.Join(repo, "check.sh"), "exit 0\n")
	mustGit(t, repo, "add", "check.sh")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "green base")
	tree, err := cutWorktreeAt(Place{}, repo, filepath.Join(t.TempDir(), "worker"), "task/test-committed-red", 0700)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = git(repo, "worktree", "remove", "--force", tree.dir) })
	writeFile(t, filepath.Join(tree.dir, "check.sh"), "exit 1\n")
	mustGit(t, tree.dir, "add", "check.sh")
	mustGit(t, tree.dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "new failure")
	before := (&Agent{}).baseChecksFor(context.Background(), tree, []string{"sh check.sh"})
	if !before.read || len(before.unread) != 0 || len(before.red) != 0 {
		t.Fatalf("worker's own committed failure treated as baseline: %+v", before)
	}
}

// Check provenance survives the same durable record used to resume a task.
func TestTheCheckBaseSurvivesATaskRecord(t *testing.T) {
	graph := &TaskGraph{}
	node := &TaskNode{graph: graph, CheckBase: "before-worker-commit"}
	record := node.recordLocked()
	restored := restoreNode(graph, record)
	if got := restored.ladderRecord(taskTree{}).checkBase; got != node.CheckBase {
		t.Fatalf("restored check base = %q, want %q", got, node.CheckBase)
	}
}
