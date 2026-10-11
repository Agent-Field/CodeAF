// Package community partitions the kNN graph by seeded consensus Leiden. It
// implements placeorg.Communities: the same graph is cut ConsensusRuns times
// with different seeds, a pair of chats that share a community in enough of
// the cuts stays joined, and one last Leiden over those pairs is the answer.
package community

import (
	"context"
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// finalResolution is the resolution of the last cut. The votes already carry
// the scale, so this cut should read the agreed pairs back, not split them
// again: at 1.0 it split a coarse community the voters agreed on and landed in
// a worse optimum (quality 0.756 against 0.777 at resolution 0.4 on the
// reference graph), and at 0.5 and below it matches the voters' best.
const finalResolution = 0.5

// voteIterations is how many Leiden passes each voting run gets; the final
// cut runs until stable.
const voteIterations = 4

// Consensus implements placeorg.Communities.
type Consensus struct{}

// New returns the consensus partitioner. It holds no state.
func New() *Consensus { return &Consensus{} }

var _ placeorg.Communities = (*Consensus)(nil)

// Partition cuts g at one resolution and returns a label per node, numbered
// from 0 in order of first appearance. Runs use seeds seed+i, so the voters
// differ; the same seed always gives the same labels.
func (*Consensus) Partition(ctx context.Context, g placeorg.Graph, resolution float64, seed uint64) ([]int, error) {
	edges := make([]edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		edges = append(edges, edge{e.I, e.J, float64(e.W)})
	}
	w := newWGraph(g.N, edges)
	runs, err := votes(ctx, w, resolution, seed)
	if err != nil {
		return nil, err
	}
	// The vote is counted only over the graph's own edges: a pair that is not
	// a neighbour pair has no similarity to defend, and counting every pair
	// inside a community would cost n² on a large library.
	var kept []edge
	for _, e := range w.edgeList() {
		agree := 0
		for _, r := range runs {
			if r[e.i] == r[e.j] {
				agree++
			}
		}
		if share := float64(agree) / float64(len(runs)); share >= placeorg.ConsensusAgree {
			kept = append(kept, edge{e.i, e.j, share})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return leiden(newWGraph(g.N, kept), finalResolution, -1, seed, nil), nil
}

// votes cuts the graph ConsensusRuns times, run i seeded with seed+i. Giving
// every run the same seed makes the vote a no-op (eight identical partitions
// and a co-association of zeros and ones), which the reference harness did
// without noticing; the test pins that the runs differ.
func votes(ctx context.Context, w *wgraph, resolution float64, seed uint64) ([][]int, error) {
	runs := make([][]int, placeorg.ConsensusRuns)
	for i := range runs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		runs[i] = leiden(w, resolution, voteIterations, seed+uint64(i), nil)
	}
	return runs, nil
}

// edgeList returns each undirected edge once, in a fixed order.
func (g *wgraph) edgeList() []edge {
	var out []edge
	for v, as := range g.adj {
		for _, a := range as {
			if v < a.to {
				out = append(out, edge{v, a.to, a.w})
			}
		}
	}
	return out
}

func sortArcs(a []arc) { sort.Slice(a, func(i, j int) bool { return a[i].to < a[j].to }) }
