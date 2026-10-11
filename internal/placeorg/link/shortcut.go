package link

import (
	"context"
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// place is one finest place and the direction it is scored by.
type place struct {
	id   string
	unit []float64
}

// scored is one place's cosine with one chat.
type scored struct {
	id  string
	sim float64
}

// shortcutOps scores every chat against the finest places and returns one
// OpLink per shortcut. The histogram of counts, including the chats that
// get none, is what the reference run records.
func shortcutOps(ctx context.Context, corpus *placeorg.Corpus, vectors [][]float32, nodes []placeorg.Node, state *placeorg.State) ([]placeorg.Op, error) {
	space := spaceOf(state, nodes)
	places := finest(nodes, space)
	floor, delta := limits(state, space)
	byID := vectorsByChat(corpus, vectors)
	var ops []placeorg.Op
	seen := map[string]bool{}
	for i, chat := range corpus.Chats {
		if i&31 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if seen[chat.ID] || corpus.Kinds[chat.ID] == placeorg.KindProbe {
			continue
		}
		seen[chat.ID] = true
		for _, hit := range shortcuts(byID[chat.ID], places, floor, delta) {
			ops = append(ops, placeorg.Op{
				Kind:   placeorg.OpLink,
				Stage:  placeorg.StageLink,
				Place:  hit.id,
				Chat:   chat.ID,
				Scores: []float32{float32(hit.sim)},
				Why:    whyAlsoIn,
			})
		}
	}
	return ops, nil
}

// finest keeps the places at the greatest depth that have a centroid in
// space. Those are the leaf communities when any fine node survived: a flat
// root is shallower and is not scored, which is the set the reference
// histogram was computed on. Split children, when a run has them, are deeper
// still and are the places a chat can also be in.
func finest(nodes []placeorg.Node, space placeorg.SpaceID) []place {
	depth := 0
	for _, n := range nodes {
		if n.Depth > depth {
			depth = n.Depth
		}
	}
	var out []place
	for _, n := range nodes {
		if n.Depth != depth {
			continue
		}
		u := unit(n.Centroids[space])
		if u == nil {
			continue
		}
		out = append(out, place{id: n.ID, unit: u})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// shortcuts returns the other places within delta of the best and at or
// above floor, at most MaxLinks, closest first. The single best is the
// chat's fit and is not a shortcut; ties for that best drop one place, the
// lowest id, so the count matches the reference (qualifying minus one,
// capped).
func shortcuts(v []float32, places []place, floor, delta float64) []scored {
	var all []scored
	for _, p := range places {
		sim, ok := cosine(p.unit, v)
		if !ok {
			continue
		}
		all = append(all, scored{id: p.id, sim: sim})
	}
	if len(all) == 0 {
		return nil
	}
	best := all[0].sim
	for _, s := range all[1:] {
		if s.sim > best {
			best = s.sim
		}
	}
	var qual []scored
	for _, s := range all {
		if s.sim >= best-delta && s.sim >= floor {
			qual = append(qual, s)
		}
	}
	sort.Slice(qual, func(i, j int) bool {
		if qual[i].sim != qual[j].sim {
			return qual[i].sim > qual[j].sim
		}
		return qual[i].id < qual[j].id
	})
	if len(qual) <= 1 {
		return nil
	}
	rest := qual[1:]
	if len(rest) > placeorg.MaxLinks {
		rest = rest[:placeorg.MaxLinks]
	}
	return rest
}

func cosine(unit []float64, v []float32) (float64, bool) {
	if len(v) == 0 || len(unit) != len(v) {
		return 0, false
	}
	var s float64
	for i, x := range unit {
		s += x * float64(v[i])
	}
	return s, true
}

// spaceOf picks the space the centroids are read in. The tree's own space
// wins when those nodes have it; otherwise the only space present; otherwise
// the lowest id, so two runs with the same tree score the same places.
func spaceOf(state *placeorg.State, nodes []placeorg.Node) placeorg.SpaceID {
	have := map[placeorg.SpaceID]bool{}
	for _, n := range nodes {
		for sp, c := range n.Centroids {
			if len(c.Sum) > 0 && c.Count > 0 {
				have[sp] = true
			}
		}
	}
	if state != nil && have[state.Space] {
		return state.Space
	}
	if len(have) == 1 {
		for sp := range have {
			return sp
		}
	}
	ids := make([]string, 0, len(have))
	for sp := range have {
		ids = append(ids, string(sp))
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return placeorg.SpaceID(ids[0])
}

// limits returns the floor and the delta for space. A missing calibration
// uses the reference figures. A stored delta of zero means only a tie with
// the best qualifies, and a negative delta is clamped to that rather than
// widening the band.
func limits(state *placeorg.State, space placeorg.SpaceID) (floor, delta float64) {
	floor = float64(defaultFloor)
	delta = float64(defaultDelta)
	if state == nil || state.Thresholds == nil {
		return floor, delta
	}
	th, ok := state.Thresholds[space]
	if !ok {
		return floor, delta
	}
	if th.Floor > 0 {
		floor = float64(th.Floor)
	}
	delta = float64(th.Delta)
	if delta < 0 {
		delta = 0
	}
	return floor, delta
}

func vectorsByChat(corpus *placeorg.Corpus, vectors [][]float32) map[string][]float32 {
	out := make(map[string][]float32, len(corpus.Chats))
	for i, chat := range corpus.Chats {
		if i >= len(vectors) {
			break
		}
		if _, ok := out[chat.ID]; ok {
			continue
		}
		out[chat.ID] = vectors[i]
	}
	return out
}
