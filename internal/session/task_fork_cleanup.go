package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// retireCheckpointForks preserves the checkpoint whenever retirement fails.
// Closed conversations retry landed copies; only litter reaping may remove
// unfinished copies. Children go first so their grounds are still available.
func retireCheckpointForks(ctx context.Context, dir string, landedOnly bool, note func(string)) bool {
	nodes, ok := checkpointForks(dir, note)
	if !ok {
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
		if !sessionOwnsForkDirectory(dir, tree.dir) {
			note(fmt.Sprintf("sweep: cannot retire fork %s outside its session trees", tree.universe))
			return false
		}
		if landedOnly && forkHasLaterWork(ctx, tree) {
			note(fmt.Sprintf("sweep: kept fork %s of %s because its copy has work after landing", tree.universe, tree.ground))
			continue
		}
		if err := tree.dropUniverse(); err != nil {
			note(fmt.Sprintf("sweep: could not retire fork %s of %s: %v", tree.universe, tree.ground, err))
			return false
		}
		note(fmt.Sprintf("sweep: retired fork %s of %s", tree.universe, tree.ground))
	}
	return !contextDone(ctx)
}

// checkpointForks reads the session's checkpoint and answers its forks in the
// order they may be retired, children first. A session that never wrote a
// checkpoint has nothing to retire; one whose checkpoint cannot be read or
// ordered answers false, because a sweep that cannot name a fork cannot prove
// the removal safe.
func checkpointForks(dir string, note func(string)) ([]taskRecord, bool) {
	document, ok := loadTaskCheckpoint((Place{Dir: dir}).Tasks())
	if !ok {
		if _, err := os.Stat((Place{Dir: dir}).Tasks()); !os.IsNotExist(err) {
			note("sweep: cannot retire forks without a readable task checkpoint")
			return nil, false
		}
		return nil, true
	}
	// Tasks standing in place can share a ground and are not fork dependencies.
	var forks []taskRecord
	for _, record := range document.Nodes {
		if _, ok := universeInRecord(record); ok {
			forks = append(forks, record)
		}
	}
	nodes, err := forkRetirementOrder(forks)
	if err != nil {
		note("sweep: " + err.Error())
		return nil, false
	}
	return nodes, true
}

// forkHasLaterWork protects a retained copy that somebody may have used after
// cleanup failed. A status read includes untracked files, and a commit must
// already be reachable from the ground before this sweep may remove its copy.
// Any failed reading keeps the copy, since a retry can ask again later.
func forkHasLaterWork(ctx context.Context, tree taskTree) bool {
	if _, err := os.Stat(tree.dir); os.IsNotExist(err) {
		return false
	}
	args := []string{"--no-optional-locks", "status", "--porcelain=v1", "--untracked-files=all", "--", "."}
	for _, dropping := range taskDroppingNames() {
		args = append(args, ":(exclude)"+dropping)
	}
	status, err := gitContext(ctx, tree.dir, nil, args...)
	if err != nil || strings.TrimSpace(status) != "" {
		return true
	}
	tip, err := gitContext(ctx, tree.dir, nil, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(tip) == "" {
		return true
	}
	_, err = gitContext(ctx, tree.ground, nil, "merge-base", "--is-ancestor", strings.TrimSpace(tip), "HEAD")
	return err != nil
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

// A checkpoint may name only a copy below this session's tree directory.
// Resolve symlinks before checking containment, just as task creation does.
func sessionOwnsForkDirectory(session, path string) bool {
	trees := canonicalPath((Place{Dir: session}).Trees())
	copy := canonicalPath(path)
	return path != "" && filepath.IsAbs(path) && copy != trees && withinDir(trees, copy)
}
