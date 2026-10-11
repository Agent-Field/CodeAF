package audit

import (
	"context"
	"fmt"
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// split subdivides a finest place while two or more destination children,
// each at least a quorum, fall out of its neighbour list. It stops at
// MaxDepth and does not start on a place shallower than splitMinDepth, so a
// flat top-level place is left whole the way the reference leaves it.
//
// Neighbours are a short list per chat (splitK above splitFloor). The pairs
// are computed and discarded; there is no chat-by-chat matrix.
func (a Auditor) split(ctx context.Context, f *forest, ix *corpusIndex, judge placeorg.Judge) ([]placeorg.Op, error) {
	var ids []string
	for id, p := range f.places {
		if p.live && p.depth >= splitMinDepth && len(p.children) == 0 {
			ids = append(ids, id)
		}
	}
	var ops []placeorg.Op
	for _, id := range sortedIDs(ids) {
		more, err := a.splitOne(ctx, f, id, ix, judge)
		if err != nil {
			return nil, err
		}
		ops = append(ops, more...)
	}
	return ops, nil
}

func (a Auditor) splitOne(ctx context.Context, f *forest, id string, ix *corpusIndex, judge placeorg.Judge) ([]placeorg.Op, error) {
	p := f.places[id]
	if p == nil || !p.live || len(p.children) > 0 || p.depth < splitMinDepth || p.depth >= placeorg.MaxDepth {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	members := f.community(id)
	// The reference does not try a place smaller than two quorums plus two.
	if len(members) < 2*placeorg.Quorum+2 {
		return nil, nil
	}
	groups := knnGroups(members, ix.vector)
	var dest [][]string
	for _, g := range groups {
		if len(g) < placeorg.Quorum {
			continue
		}
		ok, err := a.destination(ctx, g, members, ix, judge)
		if err != nil {
			return nil, err
		}
		if ok {
			dest = append(dest, g)
		}
	}
	if len(dest) < 2 {
		return nil, nil
	}
	moving := map[string]bool{}
	var ops []placeorg.Op
	for i, g := range dest {
		cid := fmt.Sprintf("%s+%d", id, i+1)
		for _, m := range g {
			moving[m] = true
		}
		f.places[cid] = &place{
			id:      cid,
			parent:  id,
			members: append([]string(nil), g...),
			depth:   p.depth + 1,
			live:    true,
		}
		p.children = append(p.children, cid)
		ops = append(ops, proposed(placeorg.OpSplit, id, cid, "", g,
			fmt.Sprintf("%d chats in this place form a group large enough to be its own place.", len(g))))
		more, err := a.splitOne(ctx, f, cid, ix, judge)
		if err != nil {
			return nil, err
		}
		ops = append(ops, more...)
	}
	var stay []string
	for _, m := range p.members {
		if !moving[m] {
			stay = append(stay, m)
		}
	}
	p.members = stay
	return ops, nil
}

type neighbor struct {
	j   int
	sim float32
}

// knnGroups returns the connected components of the k-nearest graph over
// members. Each row keeps at most splitK neighbours at or above splitFloor
// and then drops the row. Nothing quadratic is retained.
func knnGroups(members []string, vector map[string][]float32) [][]string {
	n := len(members)
	if n == 0 {
		return nil
	}
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	k := splitK
	if k > n-1 {
		k = n - 1
	}
	for i := 0; i < n; i++ {
		var best []neighbor
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			s := cosine(vector[members[i]], vector[members[j]])
			if s < splitFloor {
				continue
			}
			best = keepTop(best, neighbor{j: j, sim: s}, k)
		}
		for _, nb := range best {
			union(i, nb.j)
		}
	}
	buckets := map[int][]string{}
	var roots []int
	seen := map[int]bool{}
	for i := 0; i < n; i++ {
		r := find(i)
		if !seen[r] {
			seen[r] = true
			roots = append(roots, r)
		}
		buckets[r] = append(buckets[r], members[i])
	}
	sort.Ints(roots)
	out := make([][]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, buckets[r])
	}
	return out
}

// keepTop retains the k strongest neighbours. An equal similarity does not
// displace one already kept, so the earlier chat wins the tie.
func keepTop(best []neighbor, add neighbor, k int) []neighbor {
	if k <= 0 {
		return best
	}
	if len(best) < k {
		best = append(best, add)
		sort.Slice(best, func(i, j int) bool { return best[i].sim > best[j].sim })
		return best
	}
	if add.sim <= best[len(best)-1].sim {
		return best
	}
	best[len(best)-1] = add
	sort.Slice(best, func(i, j int) bool { return best[i].sim > best[j].sim })
	return best
}
