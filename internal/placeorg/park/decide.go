package park

import "github.com/Agent-Field/codeaf/internal/placeorg"

// absent is the score of a place that has no position. It sits below any
// real cosine, which is at least -1, so a missing place can never win a
// comparison and can never clear the floor on its own.
const absent = -2.0

// pathStep is one place on the chat's path from the top of the tree down
// to the place that is currently its home. deepest marks the home, which
// is the last step.
type pathStep struct {
	id      string
	depth   int
	deepest bool
}

// pathOf walks from the chat's home up to the root and returns the path
// from the root down. A chat in Now has an empty path.
func pathOf(t *placeorg.Tree, chat string) []pathStep {
	id, ok := t.Home(chat)
	if !ok {
		return nil
	}
	var up []pathStep
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		node, ok := t.Node(id)
		if !ok {
			break
		}
		up = append(up, pathStep{id: id, depth: node.Depth})
		id = node.Parent
	}
	if len(up) == 0 {
		return nil
	}
	down := make([]pathStep, len(up))
	for i := range up {
		down[i] = up[len(up)-1-i]
	}
	down[len(down)-1].deepest = true
	return down
}

// belowFloor reports whether every place on the path is under the floor.
// The comparison is strict: a score equal to the floor still counts as a
// home, matching the reference rule.
func belowFloor(scores []float64, floor float64) bool {
	best := absent
	for _, s := range scores {
		if s > best {
			best = s
		}
	}
	return best < floor
}

// landingIndex is the hierarchical rule. The deepest place wins only when
// it beats every place above it by the margin. If it does not, the next
// one up is tried the same way, and otherwise the chat stays at the top.
// Beating only the place immediately above is not enough: a small place
// that loses to the top place is not a clearer home.
func landingIndex(scores []float64, margin float64) int {
	for i := len(scores) - 1; i >= 1; i-- {
		if beatsShallower(scores, i, margin) {
			return i
		}
	}
	return 0
}

func beatsShallower(scores []float64, i int, margin float64) bool {
	for j := 0; j < i; j++ {
		if scores[j] <= absent {
			continue
		}
		if scores[i] <= scores[j]+margin {
			return false
		}
	}
	return true
}

// similarity is the cosine of the chat's vector and a place's direction.
// A place with no direction, and a vector that cannot be compared, score
// as absent.
func similarity(vector []float32, direction []float64) float64 {
	if len(direction) == 0 || len(vector) != len(direction) {
		return absent
	}
	var dot, norm float64
	for i, v := range vector {
		dv := float64(v)
		dot += dv * direction[i]
		norm += dv * dv
	}
	if norm == 0 {
		return absent
	}
	return dot / sqrt(norm)
}
