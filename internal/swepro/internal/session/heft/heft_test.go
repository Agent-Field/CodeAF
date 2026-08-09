package heft

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Translation of src/session/heft.test.ts. Subtest names are verbatim; TS
// `expect(...).toThrow(/DAG/)` becomes "returns an error whose message matches
// /DAG/".

func tsTasks() []HeftTask {
	return []HeftTask{
		{ID: "A", Deps: []string{}},
		{ID: "B", Deps: []string{"A"}},
		{ID: "C", Deps: []string{"A"}},
		{ID: "D", Deps: []string{"B", "C"}},
		{ID: "E", Deps: []string{"D"}},
	}
}

func tsModels() []HeftModel {
	twoSlots := 2.0
	return []HeftModel{
		{ID: "fast-expensive"},
		{ID: "slow-cheap", Slots: &twoSlots},
	}
}

func tsEstimate(taskID string, modelID string) HeftEstimate {
	durations := map[string]map[string]float64{
		"A": {"fast-expensive": 2, "slow-cheap": 5},
		"B": {"fast-expensive": 3, "slow-cheap": 6},
		"C": {"fast-expensive": 3, "slow-cheap": 1},
		"D": {"fast-expensive": 2, "slow-cheap": 5},
		"E": {"fast-expensive": 2, "slow-cheap": 5},
	}
	costUsd := 1.0
	if modelID == "fast-expensive" {
		costUsd = 10
	}
	return HeftEstimate{
		DurationMs: jscompat.JSNumber(durations[taskID][modelID]),
		CostUsd:    jscompat.JSNumber(costUsd),
	}
}

func tsOptions() HeftOptions {
	return HeftOptions{Tasks: tsTasks(), Models: tsModels(), Estimate: tsEstimate}
}

func TestHeftSchedule(t *testing.T) {
	t.Run("uses upward rank and beats uniform slow-cheap assignment on a diamond", func(t *testing.T) {
		// Mean durations and upward ranks:
		// E=3.5, D=3.5+E=7, B=4.5+D=11.5, C=2+D=9, A=3.5+max(B,C)=15.
		// Thus the task order is A, B, C, D, E. HEFT chooses:
		// A F:0-2, B F:2-5, C S lane 1:2-3, D F:5-7, E F:7-9.
		// Uniform S uses its two lanes for A:0-5, B/C:5-11 and 5-6,
		// then D:11-16 and E:16-21. HEFT saves 12ms.
		result, err := HeftSchedule(tsOptions())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []HeftAssignment{
			{TaskID: "A", ModelID: "fast-expensive", StartMs: 0, FinishMs: 2, CostUsd: 10},
			{TaskID: "B", ModelID: "fast-expensive", StartMs: 2, FinishMs: 5, CostUsd: 10},
			{TaskID: "C", ModelID: "slow-cheap", StartMs: 2, FinishMs: 3, CostUsd: 1},
			{TaskID: "D", ModelID: "fast-expensive", StartMs: 5, FinishMs: 7, CostUsd: 10},
			{TaskID: "E", ModelID: "fast-expensive", StartMs: 7, FinishMs: 9, CostUsd: 10},
		}
		if !reflect.DeepEqual(result.Assignments, want) {
			t.Errorf("assignments\n got: %+v\nwant: %+v", result.Assignments, want)
		}
		if result.MakespanMs != 9 {
			t.Errorf("makespanMs = %v, want 9", result.MakespanMs)
		}
		if result.TotalCostUsd != 41 {
			t.Errorf("totalCostUsd = %v, want 41", result.TotalCostUsd)
		}

		comparison, err := CompareToUniform(CompareToUniformOptions{
			HeftOptions:    tsOptions(),
			UniformModelID: "slow-cheap",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantComparison := UniformComparison{HeftMakespanMs: 9, UniformMakespanMs: 21, SavedMs: 12}
		if comparison != wantComparison {
			t.Errorf("compareToUniform\n got: %+v\nwant: %+v", comparison, wantComparison)
		}
	})

	t.Run("rejects a dependency cycle", func(t *testing.T) {
		_, err := HeftSchedule(HeftOptions{
			Tasks: []HeftTask{
				{ID: "a", Deps: []string{"b"}},
				{ID: "b", Deps: []string{"a"}},
			},
			Models: []HeftModel{{ID: "m"}},
			Estimate: func(string, string) HeftEstimate {
				return HeftEstimate{DurationMs: 1, CostUsd: 0}
			},
		})
		if err == nil {
			t.Fatal("expected an error")
		}
		if !regexp.MustCompile("DAG").MatchString(err.Error()) {
			t.Errorf("error %q does not match /DAG/", err.Error())
		}
		cycleErr, ok := err.(*HeftCycleError)
		if !ok {
			t.Fatalf("expected *HeftCycleError, got %T", err)
		}
		if !reflect.DeepEqual(cycleErr.TaskIDs, []string{"a", "b"}) {
			t.Errorf("taskIds = %v, want [a b]", cycleErr.TaskIDs)
		}
	})

	t.Run("is deterministic for identical inputs", func(t *testing.T) {
		first, err := HeftSchedule(tsOptions())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		second, err := HeftSchedule(tsOptions())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(second, first) {
			t.Errorf("second run differs\n first: %+v\nsecond: %+v", first, second)
		}
	})
}

// TestLocaleCompare pins the hand-rolled collation against values read off
// bun 1.2.23's String.prototype.localeCompare. Byte order (strings.Compare)
// gets the sign wrong on every pair marked "byte order disagrees".
func TestLocaleCompare(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
		note        string
	}{
		{"_", "-", -1, "byte order disagrees: connector punctuation sorts before the dash"},
		{"-", ".", -1, ""},
		{".", "/", -1, ""},
		{"/", "_", 1, "byte order disagrees"},
		{"_", "0", -1, "byte order disagrees"},
		{"-", "0", -1, ""},
		{"0", "a", -1, ""},
		{"a", "A", -1, "byte order disagrees: lowercase first at the tertiary level"},
		{"A", "b", -1, "byte order disagrees"},
		{"aB", "Ab", -1, "byte order disagrees: tertiary level compares left to right"},
		{"ABc", "aBC", 1, ""},
		{"aAa", "Aaa", -1, ""},
		{"a-b", "ab", -1, "punctuation is non-ignorable in the root collation"},
		{"a-b", "aab", -1, ""},
		{"a_b", "a-b", -1, "byte order disagrees"},
		{"a.b", "a/b", -1, ""},
		{"a/b", "a_b", 1, "byte order disagrees"},
		{"a-", "a", 1, ""},
		{"10", "9", -1, "no numeric collation"},
		{"2", "10", 1, ""},
		{"m9", "m10", 1, ""},
		{"m1", "mA", -1, ""},
		{"gpt-4", "gpt4", -1, ""},
		{"gpt-4", "gpt-40", -1, ""},
		{"claude-opus-4-5", "claude-opus-4.5", -1, ""},
		{"a\u0000b", "ab", 0, "U+0000 is completely ignorable"},
		{"a\u00adb", "ab", 0, "soft hyphen is completely ignorable"},
		{"a\u200bb", "ab", 0, "zero-width space is completely ignorable"},
		{"a b", "ab", -1, "the space itself is NOT ignorable"},
		{"a\u00a0b", "a b", 1, "NBSP shares the space primary, differs at tertiary"},
		{"a\u3000b", "a\u00a0b", -1, ""},
		{"a\tb", "a b", -1, "tab sorts before the space"},
		{"", "a", -1, ""},
		{"", "", 0, ""},
		{"abc", "ab", 1, ""},
	}
	for _, tc := range cases {
		if got := localeCompare(tc.left, tc.right); got != tc.want {
			t.Errorf("localeCompare(%q, %q) = %d, want %d %s", tc.left, tc.right, got, tc.want, tc.note)
		}
		if got := localeCompare(tc.right, tc.left); got != -tc.want {
			t.Errorf("localeCompare(%q, %q) = %d, want %d (antisymmetry)", tc.right, tc.left, got, -tc.want)
		}
	}
}

// TestHeftSlotsCap verifies the CODEAF_GO_FIX_HEFT_SLOTS_CAP guard.
func TestHeftSlotsCap(t *testing.T) {
	// Flag on: astronomically large integral slot count is rejected.
	t.Run("rejects huge slot count with flag on", func(t *testing.T) {
		t.Setenv("CODEAF_GO_FIX_HEFT_SLOTS_CAP", "1")
		huge := 1e21
		_, err := HeftSchedule(HeftOptions{
			Tasks:  []HeftTask{{ID: "a"}},
			Models: []HeftModel{{ID: "m", Slots: &huge}},
			Estimate: func(string, string) HeftEstimate {
				return HeftEstimate{DurationMs: 1, CostUsd: 0}
			},
		})
		if err == nil {
			t.Fatal("expected an error")
		}
		inputErr, ok := err.(*HeftInputError)
		if !ok {
			t.Fatalf("expected *HeftInputError, got %T", err)
		}
		if inputErr.Error() != "model slots out of range" {
			t.Errorf("error message = %q, want %q", inputErr.Error(), "model slots out of range")
		}
	})

	// Flag on: max allowed slots (4096) works.
	t.Run("allows 4096 slots with flag on", func(t *testing.T) {
		t.Setenv("CODEAF_GO_FIX_HEFT_SLOTS_CAP", "1")
		slots4096 := 4096.0
		result, err := HeftSchedule(HeftOptions{
			Tasks:  []HeftTask{{ID: "a"}},
			Models: []HeftModel{{ID: "m", Slots: &slots4096}},
			Estimate: func(string, string) HeftEstimate {
				return HeftEstimate{DurationMs: 1, CostUsd: 0}
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Assignments) != 1 {
			t.Errorf("expected 1 assignment, got %d", len(result.Assignments))
		}
	})

	// Flag off: normal scenario still passes (uses tsOptions which has 2 slots).
	t.Run("normal scenario passes with flag off", func(t *testing.T) {
		result, err := HeftSchedule(tsOptions())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.MakespanMs != 9 {
			t.Errorf("makespanMs = %v, want 9", result.MakespanMs)
		}
	})
}
