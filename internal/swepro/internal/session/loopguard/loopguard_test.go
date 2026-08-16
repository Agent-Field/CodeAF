// Port of src/session/loop-guard.test.ts. Subtest names are the verbatim TS
// describe/test strings so coverage can be diffed 1:1 against the original,
// plus a few Go-only cases for behaviour JSON fixtures cannot express (NaN
// snapshots, aliasing, V8 string quoting).

package loopguard

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

// action mirrors the TS test helper
// `(tool, argsKey, costUsd) => ({ tool, argsKey, costUsd })`.
func action(tool string, argsKey string, costUsd ...float64) LoopAction {
	act := LoopAction{Tool: tool, ArgsKey: argsKey}
	if len(costUsd) > 0 {
		value := costUsd[0]
		act.CostUsd = &value
	}
	return act
}

// expectVerdict mirrors `expect(...).toEqual({ status: "ok" })`, which in TS
// also asserts the absence of a reason key.
func expectVerdict(t *testing.T, got LoopVerdict, status LoopStatus) {
	t.Helper()
	if got.Status != status || got.Reason != nil {
		t.Fatalf("expected {status: %q}, got %s", status, describeVerdict(got))
	}
}

func expectStatus(t *testing.T, got LoopVerdict, status LoopStatus) {
	t.Helper()
	if got.Status != status {
		t.Fatalf("expected status %q, got %s", status, describeVerdict(got))
	}
}

func expectReasonContains(t *testing.T, got LoopVerdict, substring string) {
	t.Helper()
	if got.Reason == nil || !strings.Contains(*got.Reason, substring) {
		t.Fatalf("expected reason containing %q, got %s", substring, describeVerdict(got))
	}
}

func describeVerdict(verdict LoopVerdict) string {
	if verdict.Reason == nil {
		return fmt.Sprintf("{status: %q, reason: undefined}", verdict.Status)
	}
	return fmt.Sprintf("{status: %q, reason: %q}", verdict.Status, *verdict.Reason)
}

func TestCreateLoopGuardRepetitionDetection(t *testing.T) {
	t.Run("stops exactly at the consecutive-repeat cap", func(t *testing.T) {
		guard := CreateLoopGuard(LoopGuardOptions{})
		expectVerdict(t, guard.Observe(action("search", "same")), LoopStatusOK)
		expectVerdict(t, guard.Observe(action("search", "same")), LoopStatusOK)
		verdict := guard.Observe(action("search", "same"))
		expectStatus(t, verdict, LoopStatusStop)
		expectReasonContains(t, verdict, "repeated 3 times")
	})

	t.Run("detects a period-two cycle after two occurrences", func(t *testing.T) {
		guard := CreateLoopGuard(LoopGuardOptions{})
		guard.Observe(action("read", "a"))
		guard.Observe(action("write", "b"))
		guard.Observe(action("read", "a"))
		verdict := guard.Observe(action("write", "b"))
		expectStatus(t, verdict, LoopStatusStop)
		expectReasonContains(t, verdict, "period 2")
	})

	t.Run("detects a period-three cycle after two occurrences", func(t *testing.T) {
		guard := CreateLoopGuard(LoopGuardOptions{})
		for _, item := range [][2]string{{"a", "1"}, {"b", "2"}, {"c", "3"}, {"a", "1"}, {"b", "2"}} {
			expectStatus(t, guard.Observe(action(item[0], item[1])), LoopStatusOK)
		}
		verdict := guard.Observe(action("c", "3"))
		expectStatus(t, verdict, LoopStatusStop)
		expectReasonContains(t, verdict, "period 3")
	})

	t.Run("does not flag progressing work with varied arguments", func(t *testing.T) {
		guard := CreateLoopGuard(LoopGuardOptions{})
		for i := 0; i < 20; i++ {
			verdict := guard.Observe(action("search", fmt.Sprintf("query-%d", i)))
			expectStatus(t, verdict, LoopStatusOK)
		}
	})
}

func TestCreateLoopGuardBudgets(t *testing.T) {
	t.Run("warns before stopping on a cost budget", func(t *testing.T) {
		guard := CreateLoopGuard(LoopGuardOptions{MaxCostUsd: floatptr(10)})
		expectStatus(t, guard.Observe(action("a", "1", 4)), LoopStatusOK)
		expectStatus(t, guard.Observe(action("a", "2", 4)), LoopStatusWarn)
		expectStatus(t, guard.Observe(action("a", "3", 2)), LoopStatusStop)
	})

	t.Run("warns before stopping on an action budget", func(t *testing.T) {
		guard := CreateLoopGuard(LoopGuardOptions{MaxActions: floatptr(5)})
		expectStatus(t, guard.Observe(action("a", "1")), LoopStatusOK)
		expectStatus(t, guard.Observe(action("a", "2")), LoopStatusOK)
		expectStatus(t, guard.Observe(action("a", "3")), LoopStatusOK)
		expectStatus(t, guard.Observe(action("a", "4")), LoopStatusWarn)
		expectStatus(t, guard.Observe(action("a", "5")), LoopStatusStop)
	})
}

func TestCreateLoopGuardSnapshotAndRestore(t *testing.T) {
	t.Run("round-trips repetition and budget state", func(t *testing.T) {
		original := CreateLoopGuard(LoopGuardOptions{MaxCostUsd: floatptr(10)})
		original.Observe(action("read", "same", 4))
		expectStatus(t, original.Observe(action("read", "same", 4)), LoopStatusWarn)

		restored := CreateLoopGuard(LoopGuardOptions{MaxCostUsd: floatptr(10)})
		originalSnapshot := original.Snapshot()
		restored.Restore(&originalSnapshot)
		expectStatus(t, restored.Observe(action("read", "same", 2)), LoopStatusStop)
		expectStatus(t, original.Observe(action("read", "same", 2)), LoopStatusStop)
		if !reflect.DeepEqual(restored.Snapshot(), original.Snapshot()) {
			t.Fatalf("snapshots differ:\nrestored: %+v\noriginal: %+v", restored.Snapshot(), original.Snapshot())
		}
	})

	t.Run("ignores malformed snapshots", func(t *testing.T) {
		guard := CreateLoopGuard(LoopGuardOptions{})
		guard.Restore(nil)
		guard.Restore(&LoopGuardSnapshot{
			Version:           2,
			Actions:           []string{},
			ActionCount:       0,
			CumulativeCostUsd: 0,
			WarnedCost:        false,
			WarnedActions:     false,
			Stopped:           false,
		})
		expectStatus(t, guard.Observe(action("a", "1")), LoopStatusOK)
	})
}

// ── Go-only coverage ────────────────────────────────────────────────────

// TestEncodeActionMatchesV8Quoting pins the hand-rolled JSON quoting against
// output captured from bun/V8. Expectations are code-point sequences, not Go
// string literals, so no source-encoding question can hide a mismatch. They
// come from `JSON.stringify([tool, argsKey])` run under bun 1.2.23:
// V8 emits U+2028, U+2029, U+007F, U+00A0 and U+FEFF LITERALLY and escapes
// only C0 controls (lowercase hex) — Go's encoding/json escapes U+2028/U+2029
// unconditionally, which is why encodeAction cannot use jscompat.Stringify.
func TestEncodeActionMatchesV8Quoting(t *testing.T) {
	inputs := [][2]string{
		{"a" + string(rune(0x2028)) + "b" + string(rune(0x2029)) + "c" + string(rune(0x7f)) + "d" + string(rune(0x01)), "x"},
		{"a\"b", "c\\d"},
		{"tab\tnew\nret\r", "ff\f\b"},
		{"", ""},
		{"\U0001f600\u4e2d", "e" + string(rune(0x0301))},
		{string(rune(0x1f)), string(rune(0x00))},
		{string(rune(0xa0)) + string(rune(0xfeff)), "x"},
	}
	want := [][]int{
		{91, 34, 97, 8232, 98, 8233, 99, 127, 100, 92, 117, 48, 48, 48, 49, 34, 44, 34, 120, 34, 93},
		{91, 34, 97, 92, 34, 98, 34, 44, 34, 99, 92, 92, 100, 34, 93},
		{91, 34, 116, 97, 98, 92, 116, 110, 101, 119, 92, 110, 114, 101, 116, 92, 114, 34, 44, 34, 102, 102, 92, 102, 92, 98, 34, 93},
		{91, 34, 34, 44, 34, 34, 93},
		{91, 34, 128512, 20013, 34, 44, 34, 101, 769, 34, 93},
		{91, 34, 92, 117, 48, 48, 49, 102, 34, 44, 34, 92, 117, 48, 48, 48, 48, 34, 93},
		{91, 34, 160, 65279, 34, 44, 34, 120, 34, 93},
	}
	for i, input := range inputs {
		codes := []int{}
		for _, r := range encodeAction(LoopAction{Tool: input[0], ArgsKey: input[1]}) {
			codes = append(codes, int(r))
		}
		if !reflect.DeepEqual(codes, want[i]) {
			t.Errorf("encodeAction case %d: got code points %v, want %v", i, codes, want[i])
		}
	}
}

// TestRestoreRejectsNonFiniteNumbers covers the Number.isFinite guards with
// values JSON cannot carry, so the fixture corpus cannot reach them.
func TestRestoreRejectsNonFiniteNumbers(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		guard := CreateLoopGuard(LoopGuardOptions{})
		guard.Observe(action("a", "1", 2.5))
		guard.Restore(&LoopGuardSnapshot{Version: 1, ActionCount: value, CumulativeCostUsd: value})
		snapshot := guard.Snapshot()
		if snapshot.ActionCount != 1 || snapshot.CumulativeCostUsd != 2.5 {
			t.Errorf("restore(%v) overwrote state: %+v", value, snapshot)
		}
	}
}

// TestSnapshotDoesNotAliasGuardState mirrors `actions.slice()` in snapshot()
// and the `.filter().slice()` pair in restore(): neither side may share a
// backing array with the guard.
func TestSnapshotDoesNotAliasGuardState(t *testing.T) {
	guard := CreateLoopGuard(LoopGuardOptions{})
	guard.Observe(action("a", "1"))
	snapshot := guard.Snapshot()
	snapshot.Actions[0] = "tampered"
	if guard.Snapshot().Actions[0] == "tampered" {
		t.Error("snapshot() aliases the guard's action history")
	}

	other := CreateLoopGuard(LoopGuardOptions{})
	restoreFrom := LoopGuardSnapshot{Version: 1, Actions: []string{`["a","1"]`}}
	other.Restore(&restoreFrom)
	other.Observe(action("b", "2"))
	if len(restoreFrom.Actions) != 1 || restoreFrom.Actions[0] != `["a","1"]` {
		t.Errorf("restore() aliases the caller's snapshot: %+v", restoreFrom.Actions)
	}
}

// TestFormatNumberEatsExponentZeros locks in the trailing-zero strip bug that
// the TS formatNumber has for |value| >= 1e21, where toFixed falls back to the
// exponent form. Cross-checked against bun.
func TestFormatNumberEatsExponentZeros(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{10, "10"},
		{0, "0"},
		{0.5, "0.5"},
		{12.345678, "12.3457"},
		{0.00001, "0"},
		{1e21, "1e+21"},
		{1e30, "1e+3"},
		{1e100, "1e+1"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "Infinity"},
		{-10.5, "-10.5"},
	}
	for _, testCase := range cases {
		if got := formatNumber(testCase.value); got != testCase.want {
			t.Errorf("formatNumber(%v) = %q, want %q", testCase.value, got, testCase.want)
		}
	}
}

// TestFormatPercentUsesJSRounding pins Math.round semantics (floor(x + 0.5),
// i.e. halves go toward +Infinity) rather than Go's half-away-from-zero.
func TestFormatPercentUsesJSRounding(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{0.8, "80%"},
		{0.845, "85%"},
		{2.0 / 3.0, "67%"},
		{-0.005, "0%"},
		{-0.015, "-1%"},
	}
	for _, testCase := range cases {
		if got := formatPercent(testCase.value); got != testCase.want {
			t.Errorf("formatPercent(%v) = %q, want %q", testCase.value, got, testCase.want)
		}
	}
}

func floatptr(value float64) *float64 { return &value }

// TestCycleDetectionWithFlagHugeMaxPeriod verifies that with the fix flag
// enabled, a guard configured with maxCyclePeriod = 1e21 and a small action
// history completes quickly (the scan is bounded by the history length, not
// by the huge float knob).
func TestCycleDetectionWithFlagHugeMaxPeriod(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_LOOPGUARD_CYCLE_CAP", "1")
	huge := 1e21
	guard := CreateLoopGuard(LoopGuardOptions{MaxCyclePeriod: &huge})
	// Feed a period-2 cycle; should be detected quickly.
	guard.Observe(action("read", "a"))
	guard.Observe(action("write", "b"))
	guard.Observe(action("read", "a"))
	verdict := guard.Observe(action("write", "b"))
	expectStatus(t, verdict, LoopStatusStop)
	expectReasonContains(t, verdict, "period 2")
}

// TestCycleDetectionWithoutFlagPreservesFloatLoop verifies that with the
// fix flag unset the existing float-loop behaviour is unchanged — a normal
// period-2 cycle detection case still works identically.
func TestCycleDetectionWithoutFlagPreservesFloatLoop(t *testing.T) {
	guard := CreateLoopGuard(LoopGuardOptions{})
	guard.Observe(action("read", "a"))
	guard.Observe(action("write", "b"))
	guard.Observe(action("read", "a"))
	verdict := guard.Observe(action("write", "b"))
	expectStatus(t, verdict, LoopStatusStop)
	expectReasonContains(t, verdict, "period 2")
}
