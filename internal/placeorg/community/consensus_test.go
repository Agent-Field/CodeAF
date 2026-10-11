package community

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// planted builds blocks of size sz where nodes link inside a block with
// probability pin and across blocks with pout, and returns the graph with its
// true block of every node.
func planted(blocks, sz int, pin, pout float64, seed uint64) (placeorg.Graph, []int) {
	rng := rand.New(rand.NewPCG(seed, 7))
	n := blocks * sz
	truth := make([]int, n)
	g := placeorg.Graph{N: n}
	for i := 0; i < n; i++ {
		truth[i] = i / sz
		for j := i + 1; j < n; j++ {
			p := pout
			if i/sz == j/sz {
				p = pin
			}
			if rng.Float64() < p {
				g.Edges = append(g.Edges, placeorg.Edge{I: i, J: j, W: 0.5 + 0.5*float32(rng.Float64())})
			}
		}
	}
	return g, truth
}

// ari is the adjusted Rand index of two labelings.
func ari(a, b []int) float64 {
	n := len(a)
	pair := map[[2]int]int{}
	ra, rb := map[int]int{}, map[int]int{}
	for i := range a {
		pair[[2]int{a[i], b[i]}]++
		ra[a[i]]++
		rb[b[i]]++
	}
	c2 := func(x int) float64 { return float64(x) * float64(x-1) / 2 }
	var sp, sa, sb float64
	for _, v := range pair {
		sp += c2(v)
	}
	for _, v := range ra {
		sa += c2(v)
	}
	for _, v := range rb {
		sb += c2(v)
	}
	exp := sa * sb / c2(n)
	if max := (sa + sb) / 2; max == exp {
		return 1
	}
	return (sp - exp) / ((sa+sb)/2 - exp)
}

func TestPlantedPartitionIsRecovered(t *testing.T) {
	g, truth := planted(6, 40, 0.3, 0.01, 1)
	got, err := New().Partition(context.Background(), g, 1.0, 42)
	if err != nil {
		t.Fatal(err)
	}
	if a := ari(got, truth); a < 0.95 {
		t.Fatalf("ARI against the planted blocks = %.3f, want >= 0.95", a)
	}
}

func TestResolutionMovesTheCommunityCount(t *testing.T) {
	g, _ := planted(6, 40, 0.3, 0.02, 2)
	count := func(res float64) int {
		l, _ := New().Partition(context.Background(), g, res, 42)
		_, k := relabel(l)
		return k
	}
	if lo, hi := count(0.1), count(5.0); lo >= hi {
		t.Fatalf("communities at 0.1 = %d, at 5.0 = %d; a higher resolution must not give fewer", lo, hi)
	}
}

func TestSameSeedSameLabels(t *testing.T) {
	g, _ := planted(5, 50, 0.2, 0.03, 3)
	a, _ := New().Partition(context.Background(), g, 1.0, 9)
	b, _ := New().Partition(context.Background(), g, 1.0, 9)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two runs with the same seed gave different labels")
	}
}

// TestTheEightVotesAreNotIdentical pins the reference harness's no-op: it
// passed one seed to all eight runs, so the consensus voted eight times for
// one partition. On a noisy graph, seeds seed+i must give different cuts.
func TestTheEightVotesAreNotIdentical(t *testing.T) {
	g, _ := planted(8, 30, 0.12, 0.05, 4)
	edges := make([]edge, len(g.Edges))
	for i, e := range g.Edges {
		edges[i] = edge{e.I, e.J, float64(e.W)}
	}
	runs, err := votes(context.Background(), newWGraph(g.N, edges), 2.5, 42)
	if err != nil {
		t.Fatal(err)
	}
	distinct := 0
	for i, r := range runs {
		fresh := true
		for _, q := range runs[:i] {
			if samePartition(r, q) {
				fresh = false
			}
		}
		if fresh {
			distinct++
		}
	}
	if distinct < 2 {
		t.Fatalf("%d distinct partitions among %d runs; the vote is a no-op", distinct, len(runs))
	}
}

func TestCancelledContextStops(t *testing.T) {
	g, _ := planted(2, 10, 0.5, 0.1, 5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Partition(ctx, g, 1, 1); err == nil {
		t.Fatal("a cancelled context must be an error")
	}
}

func TestEdgelessGraphIsAllSingletons(t *testing.T) {
	l, err := New().Partition(context.Background(), placeorg.Graph{N: 5}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, k := relabel(l); k != 5 {
		t.Fatalf("labels %v: want 5 singletons", l)
	}
}

type fixture struct {
	N     int              `json:"n"`
	Edges [][3]float64     `json:"edges"`
	Parts map[string][]int `json:"parts"`
}

// TestParityWithLeidenalg compares against partitions python leidenalg made on
// the reference graph (389 nodes, kNN k=8 with anchor boost). The graph is
// numbers only; the corpus it came from is not in the repository.
func TestParityWithLeidenalg(t *testing.T) {
	raw, err := os.ReadFile("testdata/fixture-graph.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	g := placeorg.Graph{N: f.N}
	for _, e := range f.Edges {
		g.Edges = append(g.Edges, placeorg.Edge{I: int(e[0]), J: int(e[1]), W: float32(e[2])})
	}
	for _, res := range placeorg.Resolutions {
		key := strconv.FormatFloat(res, 'f', 1, 64)
		got, err := New().Partition(context.Background(), g, res, 42)
		if err != nil {
			t.Fatal(err)
		}
		if a := ari(got, f.Parts[key]); a < 0.85 {
			t.Errorf("resolution %s: ARI %.3f against leidenalg, want >= 0.85", key, a)
		} else {
			_, k := relabel(got)
			_, kr := relabel(f.Parts[key])
			t.Logf("resolution %s: ARI %.3f, %d communities (leidenalg %d)", key, a, k, kr)
		}
	}
}

func TestLargeGraphIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("scale check")
	}
	const n, deg = 10000, 20
	rng := rand.New(rand.NewPCG(1, 2))
	g := placeorg.Graph{N: n}
	for i := 0; i < n; i++ {
		for d := 0; d < deg/2; d++ {
			j := i/100*100 + rng.IntN(100) // 100 blocks of 100, mostly inside
			if rng.Float64() < 0.2 {
				j = rng.IntN(n)
			}
			if i != j {
				g.Edges = append(g.Edges, placeorg.Edge{I: min(i, j), J: max(i, j), W: float32(0.4 + 0.6*rng.Float64())})
			}
		}
	}
	start := time.Now()
	for _, res := range placeorg.Resolutions {
		if _, err := New().Partition(context.Background(), g, res, 42); err != nil {
			t.Fatal(err)
		}
	}
	// Three Partition calls are 24 voting runs plus three final cuts.
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("%d edges, 24 runs took %v, want < 30s", len(g.Edges), d)
	} else {
		t.Logf("%d edges, 24 runs: %v", len(g.Edges), d)
	}
}
