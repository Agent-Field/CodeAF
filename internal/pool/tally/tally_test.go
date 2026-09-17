package tally

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"sync"
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

func TestMarshalEmpty(t *testing.T) {
	j, err := json.Marshal(New())
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"schema":1,"cells":[],"wins":[]}`; string(j) != want {
		t.Fatalf("empty sheet = %s, want %s", j, want)
	}
}

func TestMarshalDeterministic(t *testing.T) {
	s1 := New()
	s1.Observe("b", "r", "m", nil, 1)
	s1.Observe("a", "r", "m", nil, 1)
	s1.Observe("m", "r", "m1", map[string]string{"b": "2"}, 1)
	s1.Observe("m", "r", "m1", map[string]string{"a": "1"}, 2)
	s1.Win("r", "y", "x")
	s1.Win("r", "x", "y")

	s2 := New()
	s2.Observe("a", "r", "m", nil, 1)
	s2.Observe("b", "r", "m", nil, 1)
	s2.Observe("m", "r", "m1", map[string]string{"a": "1"}, 2)
	s2.Observe("m", "r", "m1", map[string]string{"b": "2"}, 1)
	s2.Win("r", "x", "y")
	s2.Win("r", "y", "x")

	j1, err := json.Marshal(s1)
	if err != nil {
		t.Fatal(err)
	}
	j2, err := json.Marshal(s2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(j1, j2) {
		t.Fatalf("marshal depends on insertion order:\n%s\n%s", j1, j2)
	}
	if i, k := strings.Index(string(j1), `"metric":"a"`), strings.Index(string(j1), `"metric":"b"`); i > k {
		t.Fatalf("cells not sorted: %s", j1)
	}
	if i, k := strings.Index(string(j1), `"winner":"x"`), strings.Index(string(j1), `"winner":"y"`); i > k {
		t.Fatalf("wins not sorted: %s", j1)
	}
}

func TestRoundTrip(t *testing.T) {
	s := New()
	s.Observe("tok", "coder", "vendor/m", map[string]string{"quant": "fp8"}, 1.5)
	s.Observe("tok", "coder", "vendor/m", nil, 2)
	s.Win("coder", "a", "b")
	j, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	back := New()
	if err := json.Unmarshal(j, back); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, back, s) {
		t.Fatalf("round trip changed the sheet: %s", j)
	}
	checkCell(t, back, "tok", "coder", "vendor/m", map[string]string{"quant": "fp8"}, Cell{N: 1, Sum: 1.5, SumSq: 2.25})
	checkCell(t, back, "tok", "coder", "vendor/m", nil, Cell{N: 1, Sum: 2, SumSq: 4})
	if x, y := back.Wins("coder", "a", "b"); x != 1 || y != 0 {
		t.Fatalf("Wins = %d, %d, want 1, 0", x, y)
	}
}

func TestUnmarshalReplacesContents(t *testing.T) {
	src := New()
	src.Observe("m", "r", "model", nil, 1)
	j, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	dirty := New()
	dirty.Observe("old", "r", "model", nil, 9)
	dirty.Win("r", "x", "y")
	if err := json.Unmarshal(j, dirty); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, dirty, src) {
		t.Fatal("UnmarshalJSON did not replace existing contents")
	}
	if _, ok := dirty.Cell("old", "r", "model", nil); ok {
		t.Fatal("old contents survived UnmarshalJSON")
	}
}

func TestUnmarshalIgnoresUnknownFields(t *testing.T) {
	doc := `{"schema":1,"note":"hello","cells":[` +
		`{"metric":"m","role":"r","model":"vendor/m","dims":{"quant":"fp8"},"n":2,"sum":3,"sumsq":5,"taken_by":"x"},` +
		`{"metric":"m2","role":"r","model":"vendor/m","n":1,"sum":2,"sumsq":4}` +
		`],"wins":[{"role":"r","winner":"a","loser":"b","wins":3,"when":12}],"extra":[1]}`
	s := New()
	if err := json.Unmarshal([]byte(doc), s); err != nil {
		t.Fatal(err)
	}
	checkCell(t, s, "m", "r", "vendor/m", map[string]string{"quant": "fp8"}, Cell{N: 2, Sum: 3, SumSq: 5})
	checkCell(t, s, "m2", "r", "vendor/m", nil, Cell{N: 1, Sum: 2, SumSq: 4})
	if x, y := s.Wins("r", "a", "b"); x != 3 || y != 0 {
		t.Fatalf("Wins = %d, %d, want 3, 0", x, y)
	}
}

func TestUnmarshalRefusesNewerSchema(t *testing.T) {
	if err := json.Unmarshal([]byte(`{"schema":2}`), New()); err == nil {
		t.Fatal("schema 2 accepted")
	}
	if err := json.Unmarshal([]byte(`{"schema":99,"cells":[]}`), New()); err == nil {
		t.Fatal("schema 99 accepted")
	}
	if err := json.Unmarshal([]byte(`{"schema":1,"cells":[],"wins":[]}`), New()); err != nil {
		t.Fatalf("schema 1 refused: %v", err)
	}
	if err := json.Unmarshal([]byte(`{}`), New()); err != nil {
		t.Fatalf("document without a schema refused: %v", err)
	}
}

func TestUnmarshalInvalidJSON(t *testing.T) {
	for _, bad := range []string{``, `{`, `{"schema":1,"cells":"no"}`, `{"schema":"1"}`} {
		if err := json.Unmarshal([]byte(bad), New()); err == nil {
			t.Fatalf("invalid document %q accepted", bad)
		}
	}
}

func TestConcurrentUse(t *testing.T) {
	const goroutines = 8
	const each = 100
	s := New()

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				s.Observe("m", "r", "model", nil, 1)
				s.Observe("m", "r", "model", map[string]string{"g": strconv.Itoa(g)}, 2)
				s.Win("r", "a", "b")
			}
		}(g)
	}
	// A writer merging while the others observe, and a reader walking the
	// sheet the whole time.
	part := New()
	part.Observe("m", "r", "model", map[string]string{"part": "1"}, 3)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			s.Merge(part)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			s.Wins("r", "a", "b")
			s.Cell("m", "r", "model", nil)
			if j, err := json.Marshal(s); err != nil {
				t.Error(err)
				return
			} else if len(j) == 0 {
				t.Error("empty marshal")
				return
			}
		}
	}()
	wg.Wait()

	checkCell(t, s, "m", "r", "model", nil, Cell{N: goroutines * each, Sum: goroutines * each, SumSq: goroutines * each})
	for g := 0; g < goroutines; g++ {
		checkCell(t, s, "m", "r", "model", map[string]string{"g": strconv.Itoa(g)},
			Cell{N: each, Sum: 2 * each, SumSq: 4 * each})
	}
	checkCell(t, s, "m", "r", "model", map[string]string{"part": "1"}, Cell{N: 50, Sum: 150, SumSq: 450})
	if x, y := s.Wins("r", "a", "b"); x != goroutines*each || y != 0 {
		t.Fatalf("Wins = %d, %d, want %d, 0", x, y, goroutines*each)
	}
}

// Each hands a metric's cells over one call apiece, in a settled order, and
// nothing under another metric and nothing the call does can move the sheet.
func TestEachHandsAMetricsCellsOverInASettledOrder(t *testing.T) {
	s := New()
	// Inserted deliberately out of order: the order Each answers must not
	// carry the order the cells were observed in.
	s.Observe("m", "worker", "b/late", nil, 2)
	s.Observe("m", "high", "a/early", map[string]string{"quant": "fp8"}, 9)
	s.Observe("m", "worker", "a/first", nil, 1)
	s.Observe("other", "worker", "a/first", nil, 5)
	s.Observe("m", "worker", "a/first", map[string]string{"quant": "fp8"}, 3)

	var seen []string
	s.Each("m", func(role, model string, dims map[string]string, c Cell) {
		word := role + " " + model
		if dims == nil {
			word += " nil"
		} else {
			word += " " + dims["quant"]
		}
		seen = append(seen, word)
		switch {
		case role == "high":
			if c.N != 1 || c.Sum != 9 {
				t.Errorf("the high cell is %+v, want one observation of 9", c)
			}
		case model == "a/first" && dims == nil:
			if c.N != 1 || c.Sum != 1 {
				t.Errorf("the first cell is %+v, want one observation of 1", c)
			}
		case model == "b/late":
			if c.N != 1 || c.Sum != 2 {
				t.Errorf("the late cell is %+v, want one observation of 2", c)
			}
		}
	})
	want := []string{"high a/early fp8", "worker a/first nil", "worker a/first fp8", "worker b/late nil"}
	if len(seen) != len(want) {
		t.Fatalf("Each handed over %d cells (%v), want %d", len(seen), seen, len(want))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("the %d cell handed over is %q, want %q — role, then model, then the dim labels", i, seen[i], want[i])
		}
	}

	// Nil dims and an empty map are the same address, so a second observation
	// under an empty map lands in the cell Each already handed over.
	s.Observe("m", "worker", "a/first", map[string]string{}, 4)
	checkCell(t, s, "m", "worker", "a/first", nil, Cell{N: 2, Sum: 5, SumSq: 17})
}
