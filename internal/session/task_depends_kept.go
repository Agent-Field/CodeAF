package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// keptDependencyBranches is the finished branch work this root node depends
// on, in the same order its edges were admitted. Parts never inherit this way:
// their frozen family world is the one world every sibling was promised.
func (n *TaskNode) keptDependencyBranches() []string {
	if n == nil || n.graph == nil {
		return nil
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.parent != 0 || n.Mode != TaskModeWorktree {
		return nil
	}
	var branches []string
	for _, id := range n.dependsOn {
		dependency := n.graph.nodes[id]
		if dependency == nil || dependency.merge != mergeKept || strings.TrimSpace(dependency.branch) == "" {
			continue
		}
		branches = append(branches, dependency.branch)
	}
	return branches
}

// prepareTaskTreeForNode adds the one inheritance a graph, rather than a stand,
// knows about: completed dependency branches that were kept out of the person's
// protected checkout.
func prepareTaskTreeForNode(ctx context.Context, place Place, workspace, session string, node *TaskNode) (taskTree, error) {
	stand := node.stand()
	branches := node.keptDependencyBranches()
	if len(branches) == 0 {
		return prepareTaskTreeOn(ctx, place, workspace, session, node.id, node.title(), stand)
	}
	root, ok := repositoryRoot(stand.dir)
	if !ok {
		return taskTree{}, errors.New("the branch of the work this depends on is not in this repository")
	}
	start, err := git(root, "rev-parse", branches[0])
	if err != nil || strings.TrimSpace(start) == "" {
		return taskTree{}, errors.New("the branch of the work this depends on is not in this repository")
	}
	// A DEPENDENT STARTS AT ITS DEPENDENCY'S BRANCH. This reuses the frozen
	// start-point road, so the person's uncommitted checkout is deliberately not
	// sealed into the dependent's world a second time.
	stand.frozen = strings.TrimSpace(start)
	tree, err := prepareTaskTreeOn(ctx, place, workspace, session, node.id, node.title(), stand)
	if err != nil {
		return taskTree{}, err
	}
	for _, branch := range branches[1:] {
		if _, err := git(tree.dir,
			"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
			"merge", "--no-edit", branch); err == nil {
			continue
		}
		_, _ = git(tree.dir, "merge", "--abort")
		tree.discardBeforeStart()
		return taskTree{}, errors.New("the branches of the work this depends on do not merge cleanly (" +
			strings.Join(branches, ", ") + ") — merge them yourself first")
	}
	return tree, nil
}

// discardBeforeStart removes a working copy whose dependency branches could
// not be combined. No worker has run and no work belongs to this branch, so
// keeping either the registration or the branch would leave an artifact with
// nothing a person can take from it.
func (t taskTree) discardBeforeStart() {
	defer lockGitRoot(t.place, t.root)()
	if t.ownRepository() {
		_ = os.RemoveAll(t.dir)
		_ = t.dropUniverse()
	} else {
		_, _ = git(t.root, "worktree", "remove", "--force", t.dir)
		_, _ = git(t.root, "worktree", "prune")
	}
	_, _ = git(t.root, "branch", "-D", t.branch)
	_ = os.Remove(filepath.Dir(t.dir))
}
