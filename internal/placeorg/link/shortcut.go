package link

import (
	"context"
	"math"
	"slices"
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
	// bar is what sim had to reach at the anchor that earned the place.
	bar float64
}

// shortcutOps scores every chat against the finest places and returns one
// OpLink per extra home, plus the histogram of how many extra homes each chat
// earned, including the chats that earned none. end is the end-anchor vector
// per chat, or nil when the run has no end text; a chat then earns homes at
// the start anchor alone.
func shortcutOps(ctx context.Context, corpus *placeorg.Corpus, vectors, end [][]float32, t *placeorg.Tree, state *placeorg.State) ([]placeorg.Op, map[int]int, error) {
	nodes := forest(t)
	space := spaceOf(state, nodes)
	places := finest(nodes, space)
	floor, delta := limits(state, space)
	byID := vectorsByChat(corpus, vectors)
	endByID := vectorsByChat(corpus, end)
	var ops []placeorg.Op
	hist := map[int]int{}
	seen := map[string]bool{}
	for i, chat := range corpus.Chats {
		if i&31 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		if seen[chat.ID] || corpus.Kinds[chat.ID] == placeorg.KindProbe {
			continue
		}
		seen[chat.ID] = true
		home, _ := t.Home(chat.ID)
		extra := homes(t, home, floor, delta, byID[chat.ID], endByID[chat.ID], places)
		hist[len(extra)]++
		for _, hit := range extra {
			ops = append(ops, placeorg.Op{
				Kind:   placeorg.OpLink,
				Stage:  placeorg.StageLink,
				Place:  hit.id,
				Chat:   chat.ID,
				Scores: []float32{float32(hit.sim), float32(hit.bar)},
				Why:    whyAlsoIn,
			})
		}
	}
	return ops, hist, nil
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

// scored is defined above; bar is the score the place had to reach at the
// anchor that earned it: the higher of the floor and the best minus delta.

// earned returns the places a vector clearly fits: at or above floor and
// within delta of its own best match. Each carries the bar it cleared.
func earned(v []float32, places []place, floor, delta float64) []scored {
	var all []scored
	for _, p := range places {
		if sim, ok := cosine(p.unit, v); ok {
			all = append(all, scored{id: p.id, sim: sim})
		}
	}
	if len(all) == 0 {
		return nil
	}
	best := all[0].sim
	for _, s := range all[1:] {
		best = math.Max(best, s.sim)
	}
	bar := math.Max(floor, best-delta)
	var out []scored
	for _, s := range all {
		if s.sim >= bar {
			s.bar = bar
			out = append(out, s)
		}
	}
	return out
}

// homes returns the extra homes a chat earns beyond the one it lives in.
// A place qualifies at the start anchor or the end anchor, with no cap, so a
// chat that drifted earns the place it ended up in. Three guards keep the
// count honest: homes are counted by distinct branch (leaves under one parent
// that all clear the bar are one home, the best of them); a chat is never
// listed in a place it already lives in nor in an ancestor of it; and no
// extra home sits in an ancestor of another. When the chat's own home is not
// among those it earned, the best earned place is its fit and is not extra,
// which is the reference rule (earned minus one).
func homes(t *placeorg.Tree, home string, floor, delta float64, start, end []float32, places []place) []scored {
	best := map[string]scored{}
	for _, v := range [][]float32{start, end} {
		for _, s := range earned(v, places, floor, delta) {
			if old, ok := best[s.id]; !ok || s.sim > old.sim {
				best[s.id] = s
			}
		}
	}
	qual := make([]scored, 0, len(best))
	for _, s := range best {
		qual = append(qual, s)
	}
	sort.Slice(qual, func(i, j int) bool {
		if qual[i].sim != qual[j].sim {
			return qual[i].sim > qual[j].sim
		}
		return qual[i].id < qual[j].id
	})
	if len(qual) == 0 {
		return nil
	}
	fit := home
	if i := slices.IndexFunc(qual, func(s scored) bool { return s.id == home }); i >= 0 {
		qual = slices.Delete(qual, i, i+1)
	} else {
		fit, qual = qual[0].id, qual[1:]
		// With no recorded home the fit stands in for it, so its siblings
		// are the same branch.
		if home == "" {
			home = fit
		}
	}
	var out []scored
	branches := map[string]bool{}
	if n, ok := t.Node(home); ok && n.Parent != "" {
		// The branch the chat already lives in is already a home.
		branches[n.Parent] = true
	}
	for _, s := range qual {
		if home != "" && ancestorOf(t, s.id, home) {
			continue
		}
		if n, _ := t.Node(s.id); n.Parent != "" {
			if branches[n.Parent] {
				continue
			}
			branches[n.Parent] = true
		}
		out = append(out, s)
	}
	return out
}

// ancestorOf reports whether id is a proper ancestor of place.
func ancestorOf(t *placeorg.Tree, id, place string) bool {
	n, ok := t.Node(place)
	for steps := 0; ok && n.Parent != "" && steps < 64; steps++ {
		if n.Parent == id {
			return true
		}
		n, ok = t.Node(n.Parent)
	}
	return false
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
