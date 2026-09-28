package session

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
)

// retireCheckpointForks preserves the checkpoint whenever retirement fails.
// Closed conversations retry landed copies; only litter reaping may remove
// unfinished copies. Children go first so their grounds are still available.
func retireCheckpointForks(ctx context.Context, dir string, landedOnly bool, note func(string)) bool {
	document, ok := loadTaskCheckpoint((Place{Dir: dir}).Tasks())
	if !ok {
		return true
	}
	sort.SliceStable(document.Nodes, func(i, j int) bool {
		return len(document.Nodes[i].Worktree) > len(document.Nodes[j].Worktree)
	})
	asked := map[string]bool{}
	for _, record := range document.Nodes {
		if contextDone(ctx) {
			return false
		}
		if landedOnly && record.Merge != mergeMerged {
			continue
		}
		tree, isUniverse := universeInRecord(record)
		if !isUniverse {
			continue
		}
		key := tree.ground + "\x00" + tree.universe
		if asked[key] {
			continue
		}
		asked[key] = true
		trees := canonicalPath((Place{Dir: dir}).Trees())
		if tree.dir == "" || !filepath.IsAbs(tree.dir) || !withinDir(trees, canonicalPath(tree.dir)) || canonicalPath(tree.dir) == trees {
			note(fmt.Sprintf("sweep: cannot retire fork %s outside its session trees", tree.universe))
			return false
		}
		if err := tree.dropUniverse(); err != nil {
			note(fmt.Sprintf("sweep: could not retire fork %s of %s: %v", tree.universe, tree.ground, err))
			return false
		}
		note(fmt.Sprintf("sweep: retired fork %s of %s", tree.universe, tree.ground))
	}
	return !contextDone(ctx)
}
