package knn

import (
	"math"
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// exactOracle sorts every row using float64 cosine rather than sharing the
// production normalisation, accumulation or bounded candidate retention.
func exactOracle(rows [][]float32, k int, floor float64) placeorg.Graph {
	norms := make([]float64, len(rows))
	for i, row := range rows {
		for _, x := range row {
			norms[i] += float64(x) * float64(x)
		}
		norms[i] = math.Sqrt(norms[i])
	}
	weights := map[[2]int]float32{}
	for i, a := range rows {
		if norms[i] == 0 {
			continue
		}
		candidates := []struct {
			j   int
			sim float64
		}{}
		for j, b := range rows {
			if i == j || norms[j] == 0 {
				continue
			}
			var dot float64
			for d, x := range a {
				dot += float64(x) * float64(b[d])
			}
			sim := dot / (norms[i] * norms[j])
			if sim >= floor {
				candidates = append(candidates, struct {
					j   int
					sim float64
				}{j, sim})
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].sim == candidates[j].sim {
				return candidates[i].j < candidates[j].j
			}
			return candidates[i].sim > candidates[j].sim
		})
		for _, c := range candidates[:min(k, len(candidates))] {
			weights[[2]int{min(i, c.j), max(i, c.j)}] = float32(c.sim)
		}
	}
	graph := placeorg.Graph{N: len(rows)}
	for pair, w := range weights {
		graph.Edges = append(graph.Edges, placeorg.Edge{I: pair[0], J: pair[1], W: w})
	}
	sort.Slice(graph.Edges, func(i, j int) bool {
		a, b := graph.Edges[i], graph.Edges[j]
		if a.I == b.I {
			return a.J < b.J
		}
		return a.I < b.I
	})
	return graph
}
