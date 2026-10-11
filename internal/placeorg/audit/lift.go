package audit

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// lift separates a root whose places are not one subject. The places share
// no derived anchor, and their centroids are less similar than sepMin, so
// the root is not their parent: each place becomes a root. The chats filed
// on the root stay there.
//
// The shadow is updated, so demote and split see the places where they now
// stand. When making them roots would put more than MaxFanout places at the
// top, the parent stays in the shadow and the op is still proposed. The
// running count includes places this pass already separated, so a later
// root is not separated once the top is full.
func (Auditor) lift(f *forest, ix *corpusIndex) []placeorg.Op {
	roots := sortedIDs(f.liveRoots())
	rootsNow := len(roots)
	var ops []placeorg.Op
	for _, rid := range roots {
		root := f.places[rid]
		if root == nil || !root.live {
			continue
		}
		mids := liveChildren(f, root)
		if len(mids) < 2 {
			continue
		}
		groups := make([][]string, len(mids))
		cents := make([][]float32, len(mids))
		var moved []string
		ready := true
		for i, id := range mids {
			members := f.community(id)
			groups[i] = members
			moved = append(moved, members...)
			var vecs [][]float32
			for _, m := range members {
				if v := ix.vector[m]; len(v) > 0 {
					vecs = append(vecs, v)
				}
			}
			cents[i] = centroid(vecs)
			if len(cents[i]) == 0 {
				// A place with no position is not evidence that it sits far
				// from the others. Unknown stays put.
				ready = false
				break
			}
		}
		if !ready || midsShareAnchor(groups, ix.anchor) {
			continue
		}
		// Strictly under sepMin. A pair that lands on the figure stays,
		// the same way a group that lands on the interpose figure is kept.
		if meanPairwise(cents) >= sepMin {
			continue
		}
		op := proposed(placeorg.OpLift, "", rid, "", moved,
			"These places are not about one thing, so each one stands on its own.")
		op.Children = append([]string(nil), mids...)
		if rootsNow+len(mids) > placeorg.MaxFanout {
			op.Why = fmt.Sprintf("These places are not about one thing. Putting each one on its own would leave more than %d places side by side, so they stay inside this one for now.", placeorg.MaxFanout)
			ops = append(ops, op)
			continue
		}
		f.promote(rid, mids)
		rootsNow += len(mids)
		ops = append(ops, op)
	}
	return ops
}

func liveChildren(f *forest, p *place) []string {
	var out []string
	for _, id := range p.children {
		child := f.places[id]
		if child != nil && child.live {
			out = append(out, id)
		}
	}
	return sortedIDs(out)
}

// midsShareAnchor reports whether any derived anchor shows up in two or
// more of the places. One place carrying an anchor the others do not is
// not a shared subject.
func midsShareAnchor(groups [][]string, anchors map[string]placeorg.AnchorSet) bool {
	seen := map[string]int{}
	for _, members := range groups {
		keys := map[string]bool{}
		for _, id := range members {
			for key := range anchors[id] {
				if key == "" {
					continue
				}
				keys[key] = true
			}
		}
		for key := range keys {
			seen[key]++
		}
	}
	for _, n := range seen {
		if n >= 2 {
			return true
		}
	}
	return false
}
