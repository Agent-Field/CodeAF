package tally

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestCellMeanAndVar(t *testing.T) {
	if got := (Cell{}).Mean(); got != 0 {
		t.Fatalf("empty Mean = %v, want 0", got)
	}
	if got := (Cell{}).Var(); got != 0 {
		t.Fatalf("empty Var = %v, want 0", got)
	}
	if got := (Cell{N: 1, Sum: 5, SumSq: 25}).Var(); got != 0 {
		t.Fatalf("single-observation Var = %v, want 0", got)
	}
	c := Cell{N: 3, Sum: 6, SumSq: 14} // observations 1, 2, 3
	if got := c.Mean(); got != 2 {
		t.Fatalf("Mean = %v, want 2", got)
	}
	if got := c.Var(); got != 1 {
		t.Fatalf("Var = %v, want 1", got)
	}
}

func TestCellVarNeverNegative(t *testing.T) {
	// (4 - 3*3/2) / 1 rounds negative; the clamp must hold it at zero.
	if got := (Cell{N: 2, Sum: 3, SumSq: 4}).Var(); got != 0 {
		t.Fatalf("Var = %v, want 0", got)
	}
}

func checkCell(t *testing.T, s *Sheet, metric, role, model string, dims map[string]string, want Cell) {
	t.Helper()
	got, ok := s.Cell(metric, role, model, dims)
	if !ok {
		t.Fatalf("Cell(%q, %q, %q, %v) not recorded", metric, role, model, dims)
	}
	if got != want {
		t.Fatalf("Cell(%q, %q, %q, %v) = %+v, want %+v", metric, role, model, dims, got, want)
	}
}

func checkNoCell(t *testing.T, s *Sheet, metric, role, model string, dims map[string]string) {
	t.Helper()
	if got, ok := s.Cell(metric, role, model, dims); ok {
		t.Fatalf("Cell(%q, %q, %q, %v) = %+v, want nothing recorded", metric, role, model, dims, got)
	}
}

func TestObserveAccumulates(t *testing.T) {
	s := New()
	s.Observe("tok", "coder", "vendor/model", nil, 2)
	s.Observe("tok", "coder", "vendor/model", nil, 4)
	checkCell(t, s, "tok", "coder", "vendor/model", nil, Cell{N: 2, Sum: 6, SumSq: 20})
	c, _ := s.Cell("tok", "coder", "vendor/model", nil)
	if c.Mean() != 3 || c.Var() != 2 {
		t.Fatalf("Mean = %v, Var = %v, want 3 and 2", c.Mean(), c.Var())
	}
	s.Observe("tok", "coder", "vendor/model", nil, -1.5)
	checkCell(t, s, "tok", "coder", "vendor/model", nil, Cell{N: 3, Sum: 4.5, SumSq: 22.25})
}

func TestDimsAddressing(t *testing.T) {
	s := New()
	s.Observe("m", "r", "model", nil, 1)
	s.Observe("m", "r", "model", map[string]string{}, 1)
	s.Observe("m", "r", "model", map[string]string{"quant": "fp8"}, 1)
	s.Observe("m", "r", "model", map[string]string{"quant": "fp8"}, 1) // same address again
	s.Observe("m", "r", "model", map[string]string{"a": "1", "b": "2"}, 1)
	s.Observe("m", "r", "model", map[string]string{"b": "2", "a": "1"}, 1) // order never matters
	checkCell(t, s, "m", "r", "model", nil, Cell{N: 2, Sum: 2, SumSq: 2})
	checkCell(t, s, "m", "r", "model", map[string]string{"quant": "fp8"}, Cell{N: 2, Sum: 2, SumSq: 2})
	checkCell(t, s, "m", "r", "model", map[string]string{"b": "2", "a": "1"}, Cell{N: 2, Sum: 2, SumSq: 2})
	checkNoCell(t, s, "m", "r", "model", map[string]string{"quant": "fp4"})
	checkNoCell(t, s, "m", "r", "model", map[string]string{"quant": "fp8", "extra": "x"})
	s.Observe("m", "r2", "model", nil, 1)
	s.Observe("m2", "r", "model", nil, 1)
	s.Observe("m", "r", "model2", nil, 1)
	checkCell(t, s, "m", "r2", "model", nil, Cell{N: 1, Sum: 1, SumSq: 1})
	checkCell(t, s, "m2", "r", "model", nil, Cell{N: 1, Sum: 1, SumSq: 1})
	checkCell(t, s, "m", "r", "model2", nil, Cell{N: 1, Sum: 1, SumSq: 1})
}

func TestAddressingIsUnambiguous(t *testing.T) {
	// Labels that would collide in a plain concatenation stay distinct.
	s := New()
	s.Observe("a:b", "c", "m", nil, 1)
	s.Observe("a", "b:c", "m", nil, 1)
	checkCell(t, s, "a:b", "c", "m", nil, Cell{N: 1, Sum: 1, SumSq: 1})
	checkCell(t, s, "a", "b:c", "m", nil, Cell{N: 1, Sum: 1, SumSq: 1})
}

func TestObserveIgnoresNonFinite(t *testing.T) {
	s := New()
	s.Observe("m", "r", "model", nil, math.NaN())
	s.Observe("m", "r", "model", nil, math.Inf(1))
	s.Observe("m", "r", "model", nil, math.Inf(-1))
	checkNoCell(t, s, "m", "r", "model", nil)
	s.Observe("m", "r", "model", nil, 1) // the sheet still records after refusals
	checkCell(t, s, "m", "r", "model", nil, Cell{N: 1, Sum: 1, SumSq: 1})
}

func TestObserveRefusesBadNames(t *testing.T) {
	long := strings.Repeat("x", maxNameLen+1)
	cases := []struct {
		name              string
		metric, role, mod string
		dims              map[string]string
	}{
		{"metric with newline", "to\nk", "r", "m", nil},
		{"long metric", long, "r", "m", nil},
		{"metric with path separator", "to/k", "r", "m", nil},
		{"role with path separator", "m", "ro/le", "m", nil},
		{"long role", "m", long, "m", nil},
		{"long model", "m", "r", long, nil},
		{"model with backslash", "m", "r", `ven\dor`, nil},
		{"model with two separators", "m", "r", "ven/dor/name", nil},
		{"dim key with separator", "m", "r", "m", map[string]string{"qu/ant": "fp8"}},
		{"long dim key", "m", "r", "m", map[string]string{long: "fp8"}},
		{"long dim value", "m", "r", "m", map[string]string{"quant": long}},
		{"dim value with newline", "m", "r", "m", map[string]string{"quant": "fp\n8"}},
	}
	for _, tc := range cases {
		s := New()
		s.Observe(tc.metric, tc.role, tc.mod, tc.dims, 1)
		s.mu.Lock()
		got := len(s.cells)
		s.mu.Unlock()
		if got != 0 {
			t.Fatalf("%s: recorded a cell", tc.name)
		}
	}
	// At the bound a label is fine, and the single separator a model id
	// carries between vendor and name is fine.
	s := New()
	s.Observe(strings.Repeat("x", maxNameLen), "r", "vendor/name", nil, 1)
	s.Observe("plain", "r", "bare", nil, 1)
	checkCell(t, s, strings.Repeat("x", maxNameLen), "r", "vendor/name", nil, Cell{N: 1, Sum: 1, SumSq: 1})
	checkCell(t, s, "plain", "r", "bare", nil, Cell{N: 1, Sum: 1, SumSq: 1})
}

func TestWinAndWins(t *testing.T) {
	s := New()
	s.Win("coder", "a", "b")
	s.Win("coder", "a", "b")
	s.Win("coder", "b", "a")
	aOverB, bOverA := s.Wins("coder", "a", "b")
	if aOverB != 2 || bOverA != 1 {
		t.Fatalf("Wins = %d, %d, want 2, 1", aOverB, bOverA)
	}
	aOverB, bOverA = s.Wins("coder", "b", "a") // the query is symmetric
	if aOverB != 1 || bOverA != 2 {
		t.Fatalf("Wins = %d, %d, want 1, 2", aOverB, bOverA)
	}
	if x, y := s.Wins("reviewer", "a", "b"); x != 0 || y != 0 {
		t.Fatalf("wins leak across roles: %d, %d", x, y)
	}
	if x, y := s.Wins("coder", "a", "c"); x != 0 || y != 0 {
		t.Fatalf("unrecorded pair: %d, %d", x, y)
	}
	// A model id may carry its separator here too.
	s.Win("coder", "vendor/a", "vendor/b")
	if x, y := s.Wins("coder", "vendor/a", "vendor/b"); x != 1 || y != 0 {
		t.Fatalf("Wins = %d, %d, want 1, 0", x, y)
	}
}

func TestWinRefusesBadCalls(t *testing.T) {
	s := New()
	s.Win("", "a", "b")
	s.Win("r", "", "b")
	s.Win("r", "a", "")
	s.Win("r", "a", "a") // winner == loser
	s.Win("r", "a\n", "b")
	s.Win("r", "a", "b\n")
	s.Win("r", "a/b/c", "b") // more than the one separator a model id carries
	s.Win("r", "a", `b\c`)   // a backslash is a path separator
	s.Win("ro/le", "a", "b") // a role carries none
	s.Win("r", "a", strings.Repeat("b", maxNameLen+1))
	if x, y := s.Wins("r", "a", "b"); x != 0 || y != 0 {
		t.Fatalf("refused Win recorded: %d, %d", x, y)
	}
}

func sameJSON(t *testing.T, a, b *Sheet) bool {
	t.Helper()
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.Equal(ja, jb)
}

func TestMergeCommutative(t *testing.T) {
	a := New()
	a.Observe("m", "r", "model", map[string]string{"quant": "fp8"}, 3)
	a.Win("r", "x", "y")
	b := New()
	b.Observe("m", "r", "model", nil, 4)
	b.Observe("m", "r", "model", nil, 5)
	b.Win("r", "y", "x")

	ab := New()
	ab.Merge(a)
	ab.Merge(b)
	ba := New()
	ba.Merge(b)
	ba.Merge(a)
	if !sameJSON(t, ab, ba) {
		t.Fatal("merge is not commutative")
	}
}

func TestMergeAssociative(t *testing.T) {
	a := New()
	a.Observe("m", "r", "model", nil, 1)
	a.Win("r", "x", "y")
	b := New()
	b.Observe("m", "r", "model", map[string]string{"q": "1"}, 2)
	c := New()
	c.Observe("m", "r", "model", nil, 3)
	c.Observe("m", "r", "model", nil, 4)
	c.Win("r", "y", "x")

	ab := New()
	ab.Merge(a)
	ab.Merge(b)
	abC := New()
	abC.Merge(ab)
	abC.Merge(c)

	bc := New()
	bc.Merge(b)
	bc.Merge(c)
	aBC := New()
	aBC.Merge(a)
	aBC.Merge(bc)

	if !sameJSON(t, abC, aBC) {
		t.Fatal("merge is not associative")
	}
}

func TestMergeSelfDoubles(t *testing.T) {
	s := New()
	s.Observe("m", "r", "model", nil, 2)
	s.Observe("m", "r", "model", map[string]string{"q": "1"}, 5)
	s.Win("r", "a", "b")
	s.Win("r", "b", "a")
	s.Merge(s)
	checkCell(t, s, "m", "r", "model", nil, Cell{N: 2, Sum: 4, SumSq: 8})
	checkCell(t, s, "m", "r", "model", map[string]string{"q": "1"}, Cell{N: 2, Sum: 10, SumSq: 50})
	if x, y := s.Wins("r", "a", "b"); x != 2 || y != 2 {
		t.Fatalf("Wins = %d, %d, want 2, 2", x, y)
	}
}

func TestMergeNilIsNoOp(t *testing.T) {
	s := New()
	s.Observe("m", "r", "model", nil, 1)
	before, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	s.Merge(nil)
	if j, _ := json.Marshal(s); !bytes.Equal(j, before) {
		t.Fatal("Merge(nil) changed the sheet")
	}
	var nilSheet *Sheet
	nilSheet.Merge(s)   // must not panic
	nilSheet.Merge(nil) // must not panic either
}

func TestMergeLeavesOtherAlone(t *testing.T) {
	other := New()
	other.Observe("m", "r", "model", nil, 7)
	other.Observe("m", "r", "model", map[string]string{"q": "1"}, 1)
	other.Win("r", "p", "q")
	before, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Merge(other)
	if j, _ := json.Marshal(other); !bytes.Equal(j, before) {
		t.Fatal("Merge modified the merged-in sheet")
	}
	checkCell(t, s, "m", "r", "model", nil, Cell{N: 1, Sum: 7, SumSq: 49})
	checkCell(t, s, "m", "r", "model", map[string]string{"q": "1"}, Cell{N: 1, Sum: 1, SumSq: 1})
	if x, y := s.Wins("r", "p", "q"); x != 1 || y != 0 {
		t.Fatalf("Wins = %d, %d, want 1, 0", x, y)
	}
}

func TestMergedSheetsEqualSingleSheet(t *testing.T) {
	one := New()
	one.Observe("tok", "coder", "vendor/m", nil, 1)
	one.Observe("tok", "coder", "vendor/m", nil, 2)
	one.Observe("tok", "coder", "vendor/m", map[string]string{"quant": "fp8"}, 7)
	one.Observe("latency", "reviewer", "vendor/m", nil, 0.5)
	one.Win("coder", "a", "b")
	one.Win("coder", "b", "a")
	one.Win("reviewer", "a", "b")

	// The same observations and wins, split over three sheets and recorded
	// in a different order.
	parts := []*Sheet{New(), New(), New()}
	parts[2].Observe("latency", "reviewer", "vendor/m", nil, 0.5)
	parts[0].Observe("tok", "coder", "vendor/m", nil, 2)
	parts[1].Observe("tok", "coder", "vendor/m", map[string]string{"quant": "fp8"}, 7)
	parts[0].Win("coder", "b", "a")
	parts[1].Win("reviewer", "a", "b")
	parts[0].Observe("tok", "coder", "vendor/m", nil, 1)
	parts[1].Win("coder", "a", "b")

	merged := New()
	merged.Merge(parts[2])
	merged.Merge(parts[0])
	merged.Merge(parts[1])
	if !sameJSON(t, merged, one) {
		t.Fatal("merged sheets disagree with one sheet that saw everything")
	}
}
