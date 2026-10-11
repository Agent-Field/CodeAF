package community

import (
	"math"
	"math/rand/v2"
)

// This file is one Leiden run with the RB configuration quality
// Q = sum over communities of [ e_c - gamma * K_c^2 / (4m) ], where e_c is the
// weight inside c and K_c the summed strength of its nodes. It follows Traag,
// Waltman and van Eck (2019): fast local moving, refinement, aggregation, and
// a repeat until a whole iteration changes nothing.

// refineTheta is the randomness of the refinement step. It is the paper's
// default: small enough that the best merge nearly always wins, large enough
// that different seeds reach different refinements.
const refineTheta = 0.01

// maxIterations stops a run that keeps finding improvements of rounding size.
const maxIterations = 100

// arc is one half of an undirected edge.
type arc struct {
	to int
	w  float64
}

// wgraph is a weighted undirected graph in which a node may carry a self
// loop, which is what aggregation leaves behind. strength counts a self loop
// twice, the way a degree does.
type wgraph struct {
	adj      [][]arc
	self     []float64
	strength []float64
	total    float64 // sum of strengths, 2m
}

// newWGraph builds the level-0 graph from an edge list. Edges with a
// non-positive weight carry no similarity and are dropped.
func newWGraph(n int, edges []edge) *wgraph {
	g := &wgraph{adj: make([][]arc, n), self: make([]float64, n), strength: make([]float64, n)}
	for _, e := range edges {
		if e.w <= 0 || e.i == e.j || e.i < 0 || e.j < 0 || e.i >= n || e.j >= n {
			continue
		}
		g.adj[e.i] = append(g.adj[e.i], arc{e.j, e.w})
		g.adj[e.j] = append(g.adj[e.j], arc{e.i, e.w})
	}
	g.finish()
	return g
}

type edge struct {
	i, j int
	w    float64
}

// finish derives strengths and the total once the arcs and loops are set.
func (g *wgraph) finish() {
	g.total = 0
	for v := range g.adj {
		s := 2 * g.self[v]
		for _, a := range g.adj[v] {
			s += a.w
		}
		g.strength[v] = s
		g.total += s
	}
}

// leiden runs the algorithm from the given membership (singletons when nil)
// for iterations passes, or until stable when iterations is negative, and
// returns the final membership with labels 0..k-1.
func leiden(g *wgraph, gamma float64, iterations int, seed uint64, start []int) []int {
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	n := len(g.adj)
	memb := make([]int, n)
	if start != nil {
		copy(memb, start)
	} else {
		for v := range memb {
			memb[v] = v
		}
	}
	memb = compact(memb)
	for it := 0; (iterations < 0 && it < maxIterations) || it < iterations; it++ {
		next := leidenPass(g, gamma, memb, rng)
		if samePartition(memb, next) {
			break
		}
		memb = next
	}
	return memb
}

// leidenPass is one iteration: move, refine, aggregate, repeat on the
// aggregate until no community can be split into more than one refined part.
func leidenPass(g *wgraph, gamma float64, memb []int, rng *rand.Rand) []int {
	n := len(g.adj)
	node := make([]int, n) // original node -> node of the current level
	for v := range node {
		node[v] = v
	}
	level, part := g, append([]int(nil), memb...)
	for {
		part = moveNodes(level, gamma, part, rng)
		refined, k := refine(level, gamma, part, rng)
		if k == len(level.adj) {
			break
		}
		agg, aggPart := aggregate(level, refined, k, part)
		for v := range node {
			node[v] = refined[node[v]]
		}
		level, part = agg, aggPart
	}
	out := make([]int, n)
	for v := range out {
		out[v] = part[node[v]]
	}
	return compact(out)
}

// moveNodes is the fast local move: a queue of nodes, each moved to the
// neighbouring community with the best gain, its neighbours outside the new
// community queued again. A node never moves to an empty community here;
// refinement and aggregation already split what should be split. It returns
// the new partition.
func moveNodes(g *wgraph, gamma float64, part []int, rng *rand.Rand) []int {
	n := len(g.adj)
	part = append([]int(nil), part...)
	tot := make([]float64, n) // community strengths, labels are below n
	for v, c := range part {
		tot[c] += g.strength[v]
	}
	queue := rng.Perm(n)
	queued := make([]bool, n)
	for i := range queued {
		queued[i] = true
	}
	link := make([]float64, n)
	var seen []int
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		queued[v] = false
		old, kv := part[v], g.strength[v]
		tot[old] -= kv
		seen = seen[:0]
		for _, a := range g.adj[v] {
			c := part[a.to]
			if link[c] == 0 {
				seen = append(seen, c)
			}
			link[c] += a.w
		}
		best, bestGain := old, link[old]-gamma*kv*tot[old]/g.total
		for _, c := range seen {
			if gain := link[c] - gamma*kv*tot[c]/g.total; gain > bestGain+1e-12 {
				best, bestGain = c, gain
			}
		}
		for _, c := range seen {
			link[c] = 0
		}
		part[v] = best
		tot[best] += kv
		if best == old {
			continue
		}
		for _, a := range g.adj[v] {
			if part[a.to] != best && !queued[a.to] {
				queued[a.to] = true
				queue = append(queue, a.to)
			}
		}
	}
	return compact(part)
}

// refine splits every community into well-connected parts. Nodes start alone;
// each still-alone node that is well connected to its community joins one of
// the well-connected parts with a non-negative gain, chosen at random with
// probability growing with the gain. It returns the refined labels and count.
func refine(g *wgraph, gamma float64, part []int, rng *rand.Rand) ([]int, int) {
	n := len(g.adj)
	ref := make([]int, n)
	tot := make([]float64, n) // strength of each refined part
	ext := make([]float64, n) // weight from a refined part to the rest of its community
	size := make([]int, n)
	ctot := map[int]float64{}
	for v := 0; v < n; v++ {
		ref[v] = v
		tot[v] = g.strength[v]
		size[v] = 1
		ctot[part[v]] += g.strength[v]
		for _, a := range g.adj[v] {
			if part[a.to] == part[v] {
				ext[v] += a.w
			}
		}
	}
	link := make([]float64, n)
	var cand []int
	var weight []float64
	for _, v := range rng.Perm(n) {
		if size[ref[v]] != 1 {
			continue
		}
		kv, c := g.strength[v], part[v]
		if ext[v] < gamma*kv*(ctot[c]-kv)/g.total {
			continue
		}
		cand, weight = cand[:0], weight[:0]
		var seen []int
		for _, a := range g.adj[v] {
			if part[a.to] == c {
				r := ref[a.to]
				if link[r] == 0 {
					seen = append(seen, r)
				}
				link[r] += a.w
			}
		}
		var sum float64
		for _, r := range seen {
			wellConnected := ext[r] >= gamma*tot[r]*(ctot[c]-tot[r])/g.total
			gain := link[r] - gamma*kv*tot[r]/g.total
			if r != ref[v] && wellConnected && gain >= 0 {
				cand = append(cand, r)
				w := math.Exp(gain / refineTheta)
				if math.IsInf(w, 1) {
					w = math.MaxFloat64 / 1e6
				}
				weight = append(weight, w)
				sum += w
			}
		}
		if len(cand) > 0 {
			pick := cand[len(cand)-1]
			x := rng.Float64() * sum
			for i, w := range weight {
				if x -= w; x < 0 {
					pick = cand[i]
					break
				}
			}
			ext[pick] += ext[v] - 2*link[pick]
			tot[pick] += kv
			size[pick]++
			size[ref[v]]--
			ref[v] = pick
		}
		for _, r := range seen {
			link[r] = 0
		}
	}
	return relabel(ref)
}

// relabel renumbers labels to 0..k-1 in order of first appearance.
func relabel(labels []int) ([]int, int) {
	m := map[int]int{}
	out := make([]int, len(labels))
	for i, l := range labels {
		id, ok := m[l]
		if !ok {
			id = len(m)
			m[l] = id
		}
		out[i] = id
	}
	return out, len(m)
}

func compact(labels []int) []int { out, _ := relabel(labels); return out }

// aggregate collapses each refined part into a node. Edges inside a part
// become its self loop; the aggregate starts in the unrefined partition, so
// the next move step begins from what was already found.
func aggregate(g *wgraph, ref []int, k int, part []int) (*wgraph, []int) {
	agg := &wgraph{adj: make([][]arc, k), self: make([]float64, k), strength: make([]float64, k)}
	aggPart := make([]int, k)
	acc := make([]map[int]float64, k)
	for v := range g.adj {
		r := ref[v]
		aggPart[r] = part[v]
		agg.self[r] += g.self[v]
		for _, a := range g.adj[v] {
			s := ref[a.to]
			if s == r {
				agg.self[r] += a.w / 2 // each inner edge is seen from both ends
				continue
			}
			if acc[r] == nil {
				acc[r] = map[int]float64{}
			}
			acc[r][s] += a.w
		}
	}
	for r, m := range acc {
		for s, w := range m {
			agg.adj[r] = append(agg.adj[r], arc{s, w})
		}
	}
	for r := range agg.adj {
		sortArcs(agg.adj[r])
	}
	agg.finish()
	return agg, compact(aggPart)
}

func samePartition(a, b []int) bool {
	m := map[int]int{}
	for i := range a {
		if x, ok := m[a[i]]; ok && x != b[i] {
			return false
		}
		m[a[i]] = b[i]
	}
	return len(m) == countLabels(b)
}

func countLabels(a []int) int {
	_, k := relabel(a)
	return k
}
