package knn

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"reflect"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func TestCosineSelectionPrecedesAnchorBoost(t *testing.T) {
	vectors := [][]float32{{5, 0}, {4, 3}, {3, 4}, {-5, 0}, {0, 0}}
	ids := []string{"a", "b", "c", "d", "zero"}
	anchors := map[string]placeorg.AnchorSet{"a": {"rare": 4}, "c": {"rare": 4}}
	got, err := New(1, 0.35).KNN(context.Background(), vectors, anchors, ids)
	if err != nil {
		t.Fatal(err)
	}
	want := placeorg.Graph{N: 5, Edges: []placeorg.Edge{{I: 0, J: 1, W: 0.8}, {I: 1, J: 2, W: 0.96}}}
	assertGraph(t, got, want)
	if vectors[0][0] != 5 {
		t.Fatal("input vectors were normalised in place")
	}
}

func TestAnchorWeightsAreBoundedIDFOverlap(t *testing.T) {
	cases := []struct {
		name string
		a, b placeorg.AnchorSet
		want float32
	}{
		{"none", nil, nil, 0},
		{"disjoint", placeorg.AnchorSet{"a": 2}, placeorg.AnchorSet{"b": 2}, 0},
		{"identical", placeorg.AnchorSet{"a": 3, "b": 9}, placeorg.AnchorSet{"a": 3, "b": 9}, 0.5},
		{"rare shared", placeorg.AnchorSet{"rare": 9, "a": 1}, placeorg.AnchorSet{"rare": 9, "b": 1}, 0.5 * 9 / 11},
		{"common shared", placeorg.AnchorSet{"common": 1, "a": 9}, placeorg.AnchorSet{"common": 1, "b": 9}, 0.5 / 19},
		{"unequal weights", placeorg.AnchorSet{"a": 2}, placeorg.AnchorSet{"a": 4}, 0.25},
		{"invalid weights", placeorg.AnchorSet{"a": float32(math.NaN()), "b": -2}, placeorg.AnchorSet{"a": float32(math.Inf(1))}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := anchorBoost(tc.a, tc.b)
			if math.Abs(float64(got-tc.want)) > 1e-6 {
				t.Fatalf("boost = %g, want %g", got, tc.want)
			}
			if got != anchorBoost(tc.b, tc.a) {
				t.Fatal("overlap is not symmetric")
			}
		})
	}
	graph, err := New(1, 0.35).KNN(context.Background(), [][]float32{{1}, {1}}, map[string]placeorg.AnchorSet{"a": {"rare": 5}, "b": {"rare": 5}}, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	assertGraph(t, graph, placeorg.Graph{N: 2, Edges: []placeorg.Edge{{I: 0, J: 1, W: 1.5}}})
}

func TestDefaultKFloorAndDeterministicTies(t *testing.T) {
	vectors := make([][]float32, 13)
	ids := make([]string, len(vectors))
	for i := range vectors {
		vectors[i] = []float32{1, 0}
		ids[i] = strconv.Itoa(i)
	}
	vectors[12] = []float32{0, 1}
	graph, err := (&Builder{}).KNN(context.Background(), vectors, nil, ids)
	if err != nil {
		t.Fatal(err)
	}
	// Equal rows choose the lowest ten other indices; their union excludes only
	// the pair of the last two equal rows. The orthogonal row stays isolated.
	if len(graph.Edges) != 65 {
		t.Fatalf("default graph has %d edges, want 65", len(graph.Edges))
	}
	for _, e := range graph.Edges {
		if e.J == 12 || (e.I == 10 && e.J == 11) {
			t.Fatalf("unexpected edge %+v", e)
		}
	}
	again, err := (&Builder{}).KNN(context.Background(), vectors, nil, ids)
	if err != nil || !reflect.DeepEqual(graph, again) {
		t.Fatal("repeated graph changed", err)
	}
}

func TestFloorIncludesEqualityAndAllowsExplicitZero(t *testing.T) {
	graph, err := New(1, 0).KNN(context.Background(), [][]float32{{1, 0}, {0, 1}}, nil, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	assertGraph(t, graph, placeorg.Graph{N: 2, Edges: []placeorg.Edge{{I: 0, J: 1, W: 0}}})
}

func TestInvalidInputsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		builder *Builder
		vectors [][]float32
		ids     []string
	}{
		{"ids", New(1, 0.35), [][]float32{{1}}, nil},
		{"ragged", New(1, 0.35), [][]float32{{1}, {1, 2}}, []string{"a", "b"}},
		{"empty row", New(1, 0.35), [][]float32{{}}, []string{"a"}},
		{"nan", New(1, 0.35), [][]float32{{float32(math.NaN())}}, []string{"a"}},
		{"infinity", New(1, 0.35), [][]float32{{float32(math.Inf(1))}}, []string{"a"}},
		{"k", New(0, 0.35), nil, nil},
		{"floor", New(1, 2), nil, nil},
		{"nan floor", New(1, float32(math.NaN())), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.builder.KNN(context.Background(), tc.vectors, nil, tc.ids); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Builder{}).KNN(ctx, [][]float32{{1}}, nil, []string{"a"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call: %v", err)
	}
	for _, v := range [][][]float32{nil, {{0, 0}}, {{1, 0}}} {
		ids := make([]string, len(v))
		got, err := (&Builder{}).KNN(context.Background(), v, nil, ids)
		if err != nil || got.N != len(v) || len(got.Edges) != 0 {
			t.Fatalf("small corpus: %+v, %v", got, err)
		}
	}
}

func TestBlockedGraphMatchesIndependentExactOracle(t *testing.T) {
	vectors, ids := fixture(1031, 17)
	got, err := New(8, 0.35).KNN(context.Background(), vectors, nil, ids)
	if err != nil {
		t.Fatal(err)
	}
	want := exactOracle(vectors, 8, 0.35)
	assertGraph(t, got, want)
}

// TestTenThousandBudget includes all allocations made by the stage, including
// the returned graph, and leaves the caller's vectors alive during the reading.
func TestTenThousandBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("10k performance proof is excluded by -short")
	}
	vectors, ids := fixture(10000, 256)
	anchors := make(map[string]placeorg.AnchorSet, len(ids))
	for i, id := range ids {
		anchors[id] = placeorg.AnchorSet{
			"group/" + strconv.Itoa(i%10): float32(math.Log(10)),
			"row/" + id:                   float32(math.Log(float64(len(ids)))),
		}
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	graph, err := (&Builder{}).KNN(context.Background(), vectors, anchors, ids)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(vectors)
	runtime.KeepAlive(anchors)
	runtime.KeepAlive(graph)
	if err != nil {
		t.Fatal(err)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("10k x 256: %s, allocated %.2f MB, %d edges, GOMAXPROCS=%d", elapsed, float64(allocated)/1e6, len(graph.Edges), runtime.GOMAXPROCS(0))
	if len(graph.Edges) < 50000 {
		t.Fatal("fixture did not exercise retained edges")
	}
	if elapsed >= 5*time.Second {
		t.Errorf("kNN took %s; budget is <5s", elapsed)
	}
	if allocated >= 50_000_000 {
		t.Errorf("kNN allocated %d bytes; budget is <50 MB", allocated)
	}
}

func fixture(n, dims int) ([][]float32, []string) {
	rng := rand.New(rand.NewSource(42))
	data := make([]float32, n*dims)
	rows := make([][]float32, n)
	ids := make([]string, n)
	for i := range rows {
		rows[i] = data[i*dims : (i+1)*dims]
		ids[i] = strconv.Itoa(i)
		for d := range rows[i] {
			rows[i][d] = float32(rng.NormFloat64() * 0.2)
		}
		rows[i][i%min(10, dims)] += 3
	}
	return rows, ids
}

func assertGraph(t *testing.T, got, want placeorg.Graph) {
	t.Helper()
	if got.N != want.N || len(got.Edges) != len(want.Edges) {
		t.Fatalf("graph N/edges = %d/%d, want %d/%d", got.N, len(got.Edges), want.N, len(want.Edges))
	}
	for i, e := range got.Edges {
		w := want.Edges[i]
		if e.I != w.I || e.J != w.J || math.Abs(float64(e.W-w.W)) > 2e-6 {
			t.Fatalf("edge %d = %+v, want %+v", i, e, w)
		}
	}
}

// cancelDuringSearch cancels on the first worker's row checks, after all
// normalisation checks, so the test needs no sleep or scheduler assumption.
type cancelDuringSearch struct {
	context.Context
	cancel context.CancelFunc
	calls  atomic.Int64
	after  int64
}

func (c *cancelDuringSearch) Err() error {
	if c.calls.Add(1) >= c.after {
		c.cancel()
	}
	return c.Context.Err()
}

func TestCancellationDuringBlockedSearchReturnsNoGraph(t *testing.T) {
	rows, ids := fixture(1031, 17)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &cancelDuringSearch{Context: ctx, cancel: cancel, after: int64(len(rows) + 3)}
	graph, err := New(8, 0.35).KNN(controlled, rows, nil, ids)
	if !errors.Is(err, context.Canceled) || graph.N != 0 || len(graph.Edges) != 0 {
		t.Fatalf("cancelled search returned %+v, %v", graph, err)
	}
}
