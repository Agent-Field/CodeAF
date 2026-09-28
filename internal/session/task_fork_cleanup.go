package session

import (
	"context"
	"fmt"
	"path/filepath"
)

// retireCheckpointForks preserves the checkpoint whenever retirement fails.
// Closed conversations retry landed copies; only litter reaping may remove
// unfinished copies. Children go first so their grounds are still available.
func retireCheckpointForks(ctx context.Context, dir string, landedOnly bool, note func(string)) bool {
	document, ok := loadTaskCheckpoint((Place{Dir: dir}).Tasks())
	if !ok {
		return true
	}
	nodes, err := forkRetirementOrder(document.Nodes)
	if err != nil {
		note("sweep: " + err.Error())
		return false
	}
	asked := map[string]bool{}
	for _, record := range nodes {
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

// Fork directories are siblings even when their work is nested. Follow the
// recorded grounds rather than path length so a child's parent remains open.
func forkRetirementOrder(nodes []taskRecord) ([]taskRecord, error) {
	var ordered []taskRecord
	state := make([]uint8, len(nodes))
	var visit func(int) error
	visit = func(i int) error {
		if state[i] == 2 {
			return nil
		}
		if state[i] == 1 {
			return fmt.Errorf("cyclic fork grounds in task checkpoint")
		}
		state[i] = 1
		if nodes[i].Worktree != "" {
			for j := range nodes {
				if j != i && nodes[j].Ground != "" && withinDir(canonicalPath(nodes[i].Worktree), canonicalPath(nodes[j].Ground)) {
					if err := visit(j); err != nil {
						return err
					}
				}
			}
		}
		state[i] = 2
		ordered = append(ordered, nodes[i])
		return nil
	}
	for i := range nodes {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}
