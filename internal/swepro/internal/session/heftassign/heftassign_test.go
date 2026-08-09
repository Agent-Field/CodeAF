package heftassign

import (
	"fmt"
	"math"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Translation of src/session/heft-assign.test.ts. Subtest names are verbatim
// from the TS `describe`/`test` pairs; assertion semantics are unchanged.

const (
	testHighModelID = "deepseek/deepseek-v4-pro"
	testLowModelID  = "deepseek/deepseek-v4-flash"
)

// base mirrors the TS `base` object literal.
func base() HeftAssignInput {
	return HeftAssignInput{
		HighModelID:    testHighModelID,
		LowModelID:     testLowModelID,
		ParallelWindow: 4,
		CriticalIDs:    []string{},
		LowCanTake:     func(string) bool { return true },
	}
}

func tasks(ids ...string) []RemainingTask {
	out := make([]RemainingTask, 0, len(ids))
	for _, id := range ids {
		out = append(out, RemainingTask{ID: id})
	}
	return out
}

func mustGet[V any](t *testing.T, m *jscompat.OrderedMap[string, V], key string) V {
	t.Helper()
	value, ok := m.Get(key)
	if !ok {
		t.Fatalf("expected key %q to be present", key)
	}
	return value
}

func TestComputeHeftAssignments(t *testing.T) {
	t.Run("chain: start times increase along dependencies", func(t *testing.T) {
		input := base()
		input.Remaining = tasks("a", "b", "c")
		input.Edges = []Edge{{FromTask: "a", ToTask: "b"}, {FromTask: "b", ToTask: "c"}}
		input.DurationMsFor = func(string) float64 { return 10000 }
		result := ComputeHeftAssignments(input)
		if result == nil {
			t.Fatalf("expected a result, got nil")
		}
		a := float64(mustGet(t, result.StartMs, "a"))
		b := float64(mustGet(t, result.StartMs, "b"))
		c := float64(mustGet(t, result.StartMs, "c"))
		if !(a < b) {
			t.Errorf("expected startMs[a] (%v) < startMs[b] (%v)", a, b)
		}
		if !(b < c) {
			t.Errorf("expected startMs[b] (%v) < startMs[c] (%v)", b, c)
		}
		if float64(result.MakespanMs) != 30000 {
			t.Errorf("makespanMs = %v, want 30000", float64(result.MakespanMs))
		}
	})

	t.Run("critical-path tasks are forced HIGH regardless of lane tie-breaks", func(t *testing.T) {
		input := base()
		input.Remaining = tasks("crit", "slack1", "slack2")
		input.Edges = []Edge{}
		input.DurationMsFor = func(id string) float64 {
			if id == "crit" {
				return 60000
			}
			return 5000
		}
		input.CriticalIDs = []string{"crit"}
		result := ComputeHeftAssignments(input)
		if result == nil {
			t.Fatalf("expected a result, got nil")
		}
		if got := mustGet(t, result.Tier, "crit"); got != TierHigh {
			t.Errorf("tier[crit] = %q, want %q", got, TierHigh)
		}
	})

	t.Run("capability guard: LOW-unreliable task is never assigned LOW", func(t *testing.T) {
		input := base()
		// enough parallel work that HEFT must use the low lane for someone
		input.Remaining = tasks("t1", "t2", "t3", "t4", "t5", "t6")
		input.Edges = []Edge{}
		input.DurationMsFor = func(string) float64 { return 10000 }
		input.LowCanTake = func(id string) bool { return id != "t3" }
		result := ComputeHeftAssignments(input)
		if result == nil {
			t.Fatalf("expected a result, got nil")
		}
		if got := mustGet(t, result.Tier, "t3"); got != TierHigh {
			t.Errorf("tier[t3] = %q, want %q", got, TierHigh)
		}
	})

	t.Run("wide independent set uses both lanes and beats serial makespan", func(t *testing.T) {
		remaining := make([]RemainingTask, 0, 8)
		for i := 0; i < 8; i++ {
			remaining = append(remaining, RemainingTask{ID: fmt.Sprintf("w%d", i)})
		}
		input := base()
		input.Remaining = remaining
		input.Edges = []Edge{}
		input.DurationMsFor = func(string) float64 { return 10000 }
		result := ComputeHeftAssignments(input)
		if result == nil {
			t.Fatalf("expected a result, got nil")
		}
		lanes := map[Tier]bool{}
		for _, lane := range result.Tier.Values() {
			lanes[lane] = true
		}
		if !lanes[TierHigh] {
			t.Errorf("expected at least one high lane")
		}
		if !lanes[TierLow] {
			t.Errorf("expected at least one low lane")
		}
		if !(float64(result.MakespanMs) < 8*10000) {
			t.Errorf("makespanMs = %v, want < %v", float64(result.MakespanMs), 8*10000)
		}
	})

	t.Run("cycle falls back to null instead of throwing", func(t *testing.T) {
		input := base()
		input.Remaining = tasks("a", "b")
		input.Edges = []Edge{{FromTask: "a", ToTask: "b"}, {FromTask: "b", ToTask: "a"}}
		input.DurationMsFor = func(string) float64 { return 1000 }
		if result := ComputeHeftAssignments(input); result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})

	t.Run("degenerate inputs return null: empty set, identical lane ids", func(t *testing.T) {
		empty := base()
		empty.Remaining = []RemainingTask{}
		empty.Edges = []Edge{}
		empty.DurationMsFor = func(string) float64 { return 1 }
		if result := ComputeHeftAssignments(empty); result != nil {
			t.Errorf("expected nil for an empty remaining set, got %+v", result)
		}

		sameLanes := base()
		sameLanes.LowModelID = sameLanes.HighModelID
		sameLanes.Remaining = tasks("a")
		sameLanes.Edges = []Edge{}
		sameLanes.DurationMsFor = func(string) float64 { return 1 }
		if result := ComputeHeftAssignments(sameLanes); result != nil {
			t.Errorf("expected nil for identical lane ids, got %+v", result)
		}
	})

	t.Run("edges outside the remaining set are ignored", func(t *testing.T) {
		input := base()
		input.Remaining = tasks("a")
		input.Edges = []Edge{{FromTask: "done-task", ToTask: "a"}}
		input.DurationMsFor = func(string) float64 { return 1000 }
		result := ComputeHeftAssignments(input)
		if result == nil {
			t.Fatalf("expected a result, got nil")
		}
		if got := float64(mustGet(t, result.StartMs, "a")); got != 0 {
			t.Errorf("startMs[a] = %v, want 0", got)
		}
	})
}

func TestRecencyWeightedMean(t *testing.T) {
	t.Run("weights recent values above old ones, exact for uniform input", func(t *testing.T) {
		if got := RecencyWeightedMean([]float64{10, 10, 10}); got != 10 {
			t.Errorf("RecencyWeightedMean([10,10,10]) = %v, want 10", got)
		}
		// old slow runs, recent fast runs -> estimate below the plain mean
		shifted := RecencyWeightedMean([]float64{60000, 60000, 10000, 10000}, 2)
		if !(shifted < 35000) {
			t.Errorf("shifted = %v, want < 35000", shifted)
		}
		if !(shifted > 10000) {
			t.Errorf("shifted = %v, want > 10000", shifted)
		}
		if !math.IsNaN(RecencyWeightedMean(nil)) {
			t.Errorf("expected NaN for an empty value list")
		}
	})
}

func TestOrderByHeftStart(t *testing.T) {
	t.Run("orders by planned start, stable for ties and unknowns last", func(t *testing.T) {
		startMs := jscompat.NewOrderedMap[string, jscompat.JSNumber]()
		startMs.Set("late", 20000)
		startMs.Set("early", 0)
		startMs.Set("mid", 10000)
		startMs.Set("tie-a", 5000)
		startMs.Set("tie-b", 5000)
		candidates := []orderCandidate{
			{IDValue: "tie-a"}, {IDValue: "late"}, {IDValue: "unknown"},
			{IDValue: "tie-b"}, {IDValue: "mid"}, {IDValue: "early"},
		}
		want := []string{"early", "tie-a", "tie-b", "mid", "late", "unknown"}
		got := []string{}
		for _, candidate := range OrderByHeftStart(candidates, startMs) {
			got = append(got, candidate.ID())
		}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	})
}

// ── Go-only coverage of the accessor variant ──────────────────────────────

func TestOrderByHeftStartByMatchesTheMirroredForm(t *testing.T) {
	type plainTask struct {
		ID  string
		Tag int
	}
	startMs := jscompat.NewOrderedMap[string, jscompat.JSNumber]()
	startMs.Set("b", 1)
	startMs.Set("a", 2)
	candidates := []plainTask{{ID: "a", Tag: 1}, {ID: "gone", Tag: 2}, {ID: "b", Tag: 3}}
	ordered := OrderByHeftStartBy(candidates, startMs, func(task plainTask) string { return task.ID })
	want := []string{"b", "a", "gone"}
	for i, task := range ordered {
		if task.ID != want[i] {
			t.Fatalf("position %d = %q, want %q", i, task.ID, want[i])
		}
	}
	// The input slice must not be reordered in place (TS builds a new array).
	if candidates[0].ID != "a" || candidates[1].ID != "gone" || candidates[2].ID != "b" {
		t.Fatalf("input slice was mutated: %+v", candidates)
	}
}
