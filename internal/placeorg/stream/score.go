package stream

import (
	"math"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// place is one node scored against the arrival. score is the cosine of the
// unit centroid and the arrival, which is how close the chat is to the
// chats already there. has is false when the node has no centroid in this
// space; such a node is not a candidate.
type place struct {
	id     string
	name   string
	parent string
	root   string
	depth  int
	score  float32
	has    bool
}

// scorePlaces walks the forest and scores every node that has a centroid in
// space. Roots are the top-level nodes that could be scored. kids lists the
// scored children of a node, which is the only descent Chow is allowed.
func scorePlaces(t *placeorg.Tree, space placeorg.SpaceID, v []float32) (roots []place, kids map[string][]place, all []place) {
	kids = map[string][]place{}
	if t == nil || len(v) == 0 {
		return nil, kids, nil
	}
	var walk func(id, rootID string, depth int)
	walk = func(id, rootID string, depth int) {
		n, ok := t.Node(id)
		if !ok {
			return
		}
		if rootID == "" {
			rootID = id
		}
		p := place{id: id, name: n.Name, parent: n.Parent, root: rootID, depth: n.Depth}
		if p.depth == 0 {
			p.depth = depth
		}
		if c, ok := n.Centroids[space]; ok && c.Count > 0 {
			if u := unit(c.Sum); len(u) == len(v) {
				p.score = dot(u, v)
				p.has = true
			}
		}
		if p.has {
			all = append(all, p)
			if depth == 1 {
				roots = append(roots, p)
			} else {
				kids[p.parent] = append(kids[p.parent], p)
			}
		}
		for _, child := range n.Children {
			walk(child, rootID, depth+1)
		}
	}
	for _, id := range t.Roots() {
		walk(id, "", 1)
	}
	return roots, kids, all
}

// unit returns the direction of sum. The centroid stores a sum and a count
// so a correction can weigh more than one chat; the cosine only needs the
// direction, and dividing by the count would not change it.
func unit(sum []float32) []float32 {
	var norm float64
	for _, x := range sum {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return nil
	}
	inv := float32(1 / math.Sqrt(norm))
	out := make([]float32, len(sum))
	for i, x := range sum {
		out[i] = x * inv
	}
	return out
}

// dot is the cosine of two unit vectors. The sum is float64 so a long vector
// does not lose the margin to rounding; the comparison itself stays float32,
// which is the width the centroids are stored at.
func dot(a, b []float32) float32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var s float64
	for i := 0; i < n; i++ {
		s += float64(a[i]) * float64(b[i])
	}
	return float32(s)
}

// prefer returns the closer place, breaking a tie by id so the same tree
// always files the same way.
func prefer(a, b place) place {
	if !a.has {
		return b
	}
	if !b.has {
		return a
	}
	if a.score != b.score {
		if a.score > b.score {
			return a
		}
		return b
	}
	if a.id <= b.id {
		return a
	}
	return b
}

// broader returns the place higher in the tree. Same depth keeps the closer
// one: "broader" has nothing to say when neither contains the other.
func broader(a, b place) place {
	if a.depth != b.depth {
		if a.depth < b.depth {
			return a
		}
		return b
	}
	return prefer(a, b)
}
