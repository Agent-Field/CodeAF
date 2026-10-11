package audit

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// interpose offers a new parent over top-level places when there are more
// of them than MaxFanout. Grouping is average-linkage on centroid cosine,
// the reference's agglomerative cut. Two guards refuse a group: it would
// hold more than 40% of the chats still in a place, or the places in it
// are too alike to be worth a parent (mean similarity above 0.80).
//
// The shadow tree is not changed. The reference appended a line and left
// the labels alone, so demote and split still see the places where they
// were. The difference is that the line is now a proposed op the runner
// can apply, instead of a sentence that changes nothing.
func (Auditor) interpose(f *forest, ix *corpusIndex) []placeorg.Op {
	roots := sortedIDs(f.liveRoots())
	if len(roots) <= placeorg.MaxFanout {
		return nil
	}
	cents := map[string][]float32{}
	sizes := map[string]int{}
	for _, id := range roots {
		members := f.community(id)
		sizes[id] = len(members)
		var vecs [][]float32
		for _, m := range members {
			if v := ix.vector[m]; len(v) > 0 {
				vecs = append(vecs, v)
			}
		}
		cents[id] = centroid(vecs)
	}
	groups := clusterPlaces(roots, cents)
	live := len(f.liveChats())
	if live == 0 {
		return nil
	}
	var ops []placeorg.Op
	n := 0
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		total := 0
		var members []string
		var cs [][]float32
		for _, id := range g {
			total += sizes[id]
			members = append(members, f.community(id)...)
			if c := cents[id]; len(c) > 0 {
				cs = append(cs, c)
			}
		}
		// Strictly above 40%, and strictly above 0.80. The reference rejects
		// with `>` and keeps a group that lands on the figure.
		if total*100 > interposeSharePercent*live {
			continue
		}
		if meanPairwise(cs) > interposeSepMax {
			continue
		}
		n++
		id := fmt.Sprintf("group-%d", n)
		ops = append(ops, proposed(placeorg.OpInterpose, "", id, "", members,
			fmt.Sprintf("These %d places sit together under a wide list, so a place above them is offered.", len(g))))
		ops[len(ops)-1].Children = append([]string(nil), g...)
	}
	return ops
}

// clusterPlaces groups ids by average linkage while the cosine distance is
// within interposeDistance. n is the number of top-level places, a handful,
// so the pairs are computed directly.
func clusterPlaces(ids []string, cents map[string][]float32) [][]string {
	groups := make([][]string, len(ids))
	for i, id := range ids {
		groups[i] = []string{id}
	}
	for {
		bestI, bestJ := -1, -1
		best := float32(2)
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				d := averageDistance(groups[i], groups[j], cents)
				if d < best {
					best = d
					bestI, bestJ = i, j
				}
			}
		}
		if bestI < 0 || best > interposeDistance {
			break
		}
		groups[bestI] = append(groups[bestI], groups[bestJ]...)
		groups = append(groups[:bestJ], groups[bestJ+1:]...)
	}
	for i := range groups {
		groups[i] = sortedIDs(groups[i])
	}
	return groups
}

func averageDistance(a, b []string, cents map[string][]float32) float32 {
	var sum float64
	n := 0
	for _, i := range a {
		for _, j := range b {
			if len(cents[i]) == 0 || len(cents[j]) == 0 {
				return 2
			}
			sum += 1 - float64(cosine(cents[i], cents[j]))
			n++
		}
	}
	if n == 0 {
		return 2
	}
	return float32(sum / float64(n))
}

func meanPairwise(cents [][]float32) float32 {
	var sum float64
	n := 0
	for i := 0; i < len(cents); i++ {
		for j := i + 1; j < len(cents); j++ {
			sum += float64(cosine(cents[i], cents[j]))
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float32(sum / float64(n))
}
