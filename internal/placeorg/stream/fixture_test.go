package stream

import (
	"math"
	"reflect"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// putTree builds a working tree the tests can score. The tree's own
// constructor is empty and its mutators are unexported, because only the
// runner may change a tree; a test is not a stage, so it plants nodes
// through the same memory the runner would.
func putTree(nodes []placeorg.Node, roots []string) *placeorg.Tree {
	t := placeorg.NewTree()
	m := map[string]*placeorg.Node{}
	for i := range nodes {
		n := new(placeorg.Node)
		*n = nodes[i]
		m[n.ID] = n
	}
	setTreeField(t, "nodes", m)
	setTreeField(t, "roots", append([]string(nil), roots...))
	return t
}

func setTreeField(t *placeorg.Tree, name string, val any) {
	f := reflect.ValueOf(t).Elem().FieldByName(name)
	reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Set(reflect.ValueOf(val))
}

// toward returns a unit vector whose cosine with (1, 0, …) is score.
func toward(score float32, dim int) []float32 {
	if dim < 2 {
		dim = 2
	}
	v := make([]float32, dim)
	v[0] = score
	rest := 1 - float64(score)*float64(score)
	if rest < 0 {
		rest = 0
	}
	v[1] = float32(math.Sqrt(rest))
	return v
}

func centroid(space placeorg.SpaceID, v []float32) map[placeorg.SpaceID]placeorg.Centroid {
	sum := make([]float32, len(v))
	copy(sum, v)
	return map[placeorg.SpaceID]placeorg.Centroid{space: {Sum: sum, Count: 1}}
}

func arrival(id string, v []float32) placeorg.Arrival {
	return placeorg.Arrival{Chat: placeorg.Chat{ID: id, Title: id}, Vector: v}
}
