// Package knn builds the organiser's exact sparse cosine graph without a dense
// similarity matrix, so the working set stays bounded on a large library.
package knn

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// The production thresholds keep sparse evidence without amplifying anchors
// beyond half a cosine point; blocks bound the number of worker goroutines.
const (
	DefaultK               = 10
	DefaultFloor   float32 = 0.35
	BlockRows              = 512
	MaxAnchorBoost float32 = 0.5
)

// Builder keeps the strongest cosine neighbours before adding anchor evidence.
// Zero fields select the production defaults; New permits an explicit zero floor.
type Builder struct {
	K          int
	Floor      float32
	configured bool
}

var _ placeorg.Neighbours = (*Builder)(nil)

// New selects thresholds explicitly, including the harness's K of eight.
func New(k int, floor float32) *Builder {
	return &Builder{K: k, Floor: floor, configured: true}
}

// KNN returns the union of directed nearest neighbours as sorted undirected
// edges. Anchor evidence changes weights, never which neighbours make the cut.
func (b *Builder) KNN(ctx context.Context, vectors [][]float32, anchors map[string]placeorg.AnchorSet, ids []string) (placeorg.Graph, error) {
	k, floor := b.K, b.Floor
	if !b.configured {
		if k == 0 {
			k = DefaultK
		}
		if floor == 0 {
			floor = DefaultFloor
		}
	}
	if k < 1 || !finite(floor) || floor < -1 || floor > 1 {
		return placeorg.Graph{}, fmt.Errorf("knn: invalid K or cosine floor")
	}
	if len(ids) != len(vectors) {
		return placeorg.Graph{}, fmt.Errorf("knn: ids and vectors have different lengths")
	}
	rows, valid, err := unitRows(ctx, vectors)
	if err != nil {
		return placeorg.Graph{}, err
	}
	n := len(rows)
	graph := placeorg.Graph{N: n}
	if n < 2 {
		return graph, ctx.Err()
	}
	k = min(k, n-1)
	nearest := make([]candidate, n*k)
	counts := make([]int, n)
	var workers sync.WaitGroup
	for start := 0; start < n; start += BlockRows {
		workers.Add(1)
		go func(start int) {
			defer workers.Done()
			searchBlock(ctx, start, rows, valid, nearest, counts, k, floor)
		}(start)
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return placeorg.Graph{}, err
	}
	graph.Edges = symmetricEdges(nearest, counts, k, anchors, ids)
	if err := ctx.Err(); err != nil {
		return placeorg.Graph{}, err
	}
	return graph, nil
}

// searchBlock writes only its own rows, so workers share vectors without locks
// and cancellation can discard the entire result after they have joined.
func searchBlock(ctx context.Context, start int, rows [][]float32, valid []bool, nearest []candidate, counts []int, k int, floor float32) {
	for i := start; i < min(start+BlockRows, len(rows)); i++ {
		if ctx.Err() != nil {
			return
		}
		if !valid[i] {
			continue
		}
		top := nearest[i*k : (i+1)*k]
		for j, row := range rows {
			if i == j || !valid[j] {
				continue
			}
			sim := cosine(rows[i], row)
			if sim >= floor {
				counts[i] = retain(top, counts[i], candidate{j, sim})
			}
		}
	}
}

// symmetricEdges sorts the sparse selections rather than allocating a dense
// matrix or a hash table for every possible pair.
func symmetricEdges(nearest []candidate, counts []int, k int, anchors map[string]placeorg.AnchorSet, ids []string) []placeorg.Edge {
	edges := make([]placeorg.Edge, 0, len(nearest))
	for i, count := range counts {
		for _, c := range nearest[i*k : i*k+count] {
			edges = append(edges, placeorg.Edge{I: min(i, c.index), J: max(i, c.index), W: c.sim})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].I == edges[j].I {
			return edges[i].J < edges[j].J
		}
		return edges[i].I < edges[j].I
	})
	var unique []placeorg.Edge
	for _, edge := range edges {
		last := len(unique) - 1
		if last >= 0 && unique[last].I == edge.I && unique[last].J == edge.J {
			continue
		}
		edge.W += anchorBoost(anchors[ids[edge.I]], anchors[ids[edge.J]])
		unique = append(unique, edge)
	}
	return unique
}

type candidate struct {
	index int
	sim   float32
}

// retain breaks equal similarities by row index so worker scheduling cannot
// change the graph, unlike the harness's unspecified argpartition tie order.
func retain(top []candidate, count int, c candidate) int {
	pos := count
	for pos > 0 {
		prev := top[pos-1]
		if prev.sim > c.sim || (prev.sim == c.sim && prev.index < c.index) {
			break
		}
		pos--
	}
	if pos == len(top) {
		return count
	}
	end := min(count, len(top)-1)
	copy(top[pos+1:end+1], top[pos:end])
	top[pos] = c
	return min(count+1, len(top))
}

// cosine uses independent accumulators to avoid one long dependency chain in
// the inner loop. Both arguments have already been normalised once.
func cosine(a, b []float32) float32 {
	var s0, s1, s2, s3 float32
	d := len(a)
	i := 0
	for ; i+3 < d; i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}
	sum := (s0 + s1) + (s2 + s3)
	for ; i < d; i++ {
		sum += a[i] * b[i]
	}
	return min(float32(1), max(float32(-1), sum))
}

// unitRows owns its normalised copy because a stage must not mutate vectors
// supplied by another stage. Zero vectors carry no similarity evidence.
func unitRows(ctx context.Context, vectors [][]float32) ([][]float32, []bool, error) {
	n := len(vectors)
	rows := make([][]float32, n)
	valid := make([]bool, n)
	if n == 0 {
		return rows, valid, ctx.Err()
	}
	dims := len(vectors[0])
	if dims == 0 {
		return nil, nil, fmt.Errorf("knn: empty vector")
	}
	data := make([]float32, n*dims)
	for i, v := range vectors {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if len(v) != dims {
			return nil, nil, fmt.Errorf("knn: row %d has inconsistent dimensions", i)
		}
		var norm float64
		for _, x := range v {
			if !finite(x) {
				return nil, nil, fmt.Errorf("knn: row %d has a nonfinite coordinate", i)
			}
			norm += float64(x) * float64(x)
		}
		rows[i] = data[i*dims : (i+1)*dims]
		if norm == 0 {
			continue
		}
		valid[i] = true
		scale := 1 / math.Sqrt(norm)
		for j, x := range v {
			rows[i][j] = float32(float64(x) * scale)
		}
	}
	return rows, valid, nil
}

func finite(x float32) bool { return !math.IsNaN(float64(x)) && !math.IsInf(float64(x), 0) }

// anchorBoost is weighted Jaccard: shared IDF mass divided by total IDF mass.
// It rewards rare shared anchors without letting many anchors dominate cosine.
// Invalid weights carry no evidence; sorting makes float sums reproducible.
func anchorBoost(a, b placeorg.AnchorSet) float32 {
	keys := make([]string, 0, len(a)+len(b))
	for key := range a {
		keys = append(keys, key)
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var shared, total float64
	for _, key := range keys {
		x, y := a[key], b[key]
		if !finite(x) || x < 0 {
			x = 0
		}
		if !finite(y) || y < 0 {
			y = 0
		}
		shared += float64(min(x, y))
		total += float64(max(x, y))
	}
	if total == 0 {
		return 0
	}
	return MaxAnchorBoost * float32(shared/total)
}
