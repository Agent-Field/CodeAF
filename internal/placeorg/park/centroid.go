package park

import (
	"math"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// communityDirections is each place's unit direction before anyone is sent
// to Now. The direction is the normalised sum of the chats whose path
// includes the place, which is the community, not the chats that will end
// up parked there. The decision has to be made against that, and only then
// are the chats that fell below the floor taken back out.
func communityDirections(t *placeorg.Tree, chats []placeorg.Chat, vectors [][]float32) map[string][]float64 {
	sums := map[string][]float64{}
	for i, chat := range chats {
		if len(vectors[i]) == 0 {
			continue
		}
		addAlongPath(t, chat.ID, vectors[i], sums)
	}
	out := make(map[string][]float64, len(sums))
	for id, sum := range sums {
		if dir := direction(sum); dir != nil {
			out[id] = dir
		}
	}
	return out
}

// recount rewrites this space's positions from the chats that stayed. A
// chat parked at a broader place is still on the path of the smaller place
// it came from, so it still counts in that smaller place's sum: parked
// chats are part of the place, not visitors. Each chat counts once. The
// sum is the total, and the count is how many vectors went into it, so a
// later correction can weigh a chat as more than one without rebuilding.
func recount(t *placeorg.Tree, chats []placeorg.Chat, vectors [][]float32, kept []bool, space placeorg.SpaceID) error {
	eachNode(t, func(id string) { placeorg.ClearCentroid(t, id, space) })
	sums := map[string][]float64{}
	counts := map[string]float64{}
	for i, chat := range chats {
		if !kept[i] || len(vectors[i]) == 0 {
			continue
		}
		addAlongPath(t, chat.ID, vectors[i], sums)
		for _, step := range pathOf(t, chat.ID) {
			counts[step.id]++
		}
	}
	for id, sum := range sums {
		stored := make([]float32, len(sum))
		for d, v := range sum {
			stored[d] = float32(v)
		}
		if err := placeorg.SetCentroid(t, id, space, placeorg.Centroid{Sum: stored, Count: counts[id]}); err != nil {
			return err
		}
	}
	return nil
}

func addAlongPath(t *placeorg.Tree, chat string, vector []float32, sums map[string][]float64) {
	for _, step := range pathOf(t, chat) {
		sum := sums[step.id]
		if sum == nil {
			sum = make([]float64, len(vector))
			sums[step.id] = sum
		}
		for d, v := range vector {
			if d < len(sum) {
				sum[d] += float64(v)
			}
		}
	}
}

func direction(sum []float64) []float64 {
	var norm float64
	for _, v := range sum {
		norm += v * v
	}
	if norm == 0 {
		return nil
	}
	scale := 1 / math.Sqrt(norm)
	out := make([]float64, len(sum))
	for i, v := range sum {
		out[i] = v * scale
	}
	return out
}

func eachNode(t *placeorg.Tree, fn func(string)) {
	var walk func(string)
	walk = func(id string) {
		fn(id)
		node, ok := t.Node(id)
		if !ok {
			return
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	for _, root := range t.Roots() {
		walk(root)
	}
}

func sqrt(v float64) float64 { return math.Sqrt(v) }
