package session

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// nodeRootLocked follows ownership, not dependency edges: separate assignments
// in one conversation may name the same work.
func (g *TaskGraph) nodeRootLocked(id uint64) uint64 {
	for steps := 0; steps <= len(g.nodes); steps++ {
		node := g.nodes[id]
		if node == nil || node.parent == 0 {
			return id
		}
		id = node.parent
	}
	return id
}

func (g *TaskGraph) nodeGrowthRefusalLocked(parent uint64, titles []string) string {
	if bashBeltAsked() {
		return ""
	}
	root := g.nodeRootLocked(parent)
	named := g.nodeTreeTitlesLocked(root)
	batch := make(map[string]bool, len(titles))
	for _, title := range titles {
		key := plandb.NormalizedTitle(title)
		if node := named[key]; node != nil {
			return fmt.Sprintf("no: this run already has a task named %q (%d), so nothing was added. If that work is yours, do it in your own hands; if it is different work, name it for what is different.", node.spec.title, node.id)
		}
		if batch[key] || g.pendingNodeTitleLocked(root, key) {
			return fmt.Sprintf("no: this batch names the same work %q more than once, so nothing was added. Do that work in your own hands, or name each different part for what is different.", title)
		}
		batch[key] = true
	}
	return ""
}

func (g *TaskGraph) nodeTreeTitlesLocked(root uint64) map[string]*TaskNode {
	named := make(map[string]*TaskNode)
	for _, id := range g.order {
		node := g.nodes[id]
		// The older road records a cancellation as a stopped node. A
		// dollar-limit ending still reserves its unfinished assignment.
		if node == nil || node.stopped && node.ending != TaskEndingCostLimit || g.nodeRootLocked(id) != root {
			continue
		}
		named[plandb.NormalizedTitle(node.spec.title)] = node
	}
	return named
}

func (g *TaskGraph) pendingNodeTitleLocked(root uint64, key string) bool {
	for owner, pending := range g.childTitles {
		if g.nodeRootLocked(owner) != root {
			continue
		}
		for _, held := range pending {
			if plandb.NormalizedTitle(held) == key {
				return true
			}
		}
	}
	return false
}

func (g *TaskGraph) releaseChildTitleLocked(parent uint64, titles []string) {
	for _, title := range titles {
		pending := g.childTitles[parent]
		for i, held := range pending {
			if plandb.NormalizedTitle(held) == plandb.NormalizedTitle(title) {
				g.childTitles[parent] = append(pending[:i], pending[i+1:]...)
				break
			}
		}
	}
	if len(g.childTitles[parent]) == 0 {
		delete(g.childTitles, parent)
	}
}
