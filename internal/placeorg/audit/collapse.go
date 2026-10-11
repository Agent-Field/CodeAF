package audit

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// collapse folds a child of a top-level place into that place when the child
// holds at least 70% of the chats. A top-level place with a single child is
// the same test at 100%, which is how a chain of one place disappears.
//
// Only the children of a top-level place are considered. A finer place that
// holds all of its own parent stays: the reference flattens the middle level
// into the top, and leaves the finest places to be demoted or split.
func (Auditor) collapse(f *forest) []placeorg.Op {
	var ops []placeorg.Op
	for _, rid := range sortedIDs(f.liveRoots()) {
		root := f.places[rid]
		if root == nil || !root.live {
			continue
		}
		parentN := len(f.community(rid))
		if parentN == 0 {
			continue
		}
		// Copy the child list: absorb rewrites it.
		for _, cid := range sortedIDs(root.children) {
			child := f.places[cid]
			if child == nil || !child.live {
				continue
			}
			members := f.community(cid)
			if len(members)*100 < dominantPercent*parentN {
				continue
			}
			f.absorb(cid)
			ops = append(ops, proposed(placeorg.OpCollapse, rid, cid, "", members,
				fmt.Sprintf("%d of %d chats sit in this one place, so it folds into the place above it.", len(members), parentN)))
		}
	}
	return ops
}
