//go:build !windows

package loopguard

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func action(tool string, argsKey string, costUsd ...float64) LoopAction {
	act := LoopAction{Tool: tool, ArgsKey: argsKey}
	if len(costUsd) > 0 {
		value := costUsd[0]
		act.CostUsd = &value
	}
	return act
}

// expectVerdict asserts the status and the absence of a reason.
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
		return fmt.Sprintf("{status: %q, reason: nil}", verdict.Status)
	}
	return fmt.Sprintf("{status: %q, reason: %q}", verdict.Status, *verdict.Reason)
}

func floatptr(value float64) *float64 { return &value }

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

	t.Run("a huge max cycle period is bounded by the history", func(t *testing.T) {
		huge := 1e21
		guard := CreateLoopGuard(LoopGuardOptions{MaxCyclePeriod: &huge})
		guard.Observe(action("read", "a"))
		guard.Observe(action("write", "b"))
		guard.Observe(action("read", "a"))
		verdict := guard.Observe(action("write", "b"))
		expectStatus(t, verdict, LoopStatusStop)
		expectReasonContains(t, verdict, "period 2")
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
		verdict := guard.Observe(action("a", "5"))
		expectStatus(t, verdict, LoopStatusStop)
		expectReasonContains(t, verdict, "action budget reached (5/5)")
	})

	t.Run("cost-only observations charge the budget", func(t *testing.T) {
		guard := CreateLoopGuard(LoopGuardOptions{MaxCostUsd: floatptr(1)})
		expectStatus(t, guard.ObserveCost(floatptr(0.5)), LoopStatusOK)
		expectStatus(t, guard.ObserveCost(floatptr(0.3)), LoopStatusWarn)
		verdict := guard.ObserveCost(floatptr(0.25))
		expectStatus(t, verdict, LoopStatusStop)
		expectReasonContains(t, verdict, "cost budget reached (1.05/1 USD)")
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
		guard.Restore(&LoopGuardSnapshot{Version: 2, Actions: []string{}})
		expectStatus(t, guard.Observe(action("a", "1")), LoopStatusOK)
	})

	t.Run("rejects non-finite or negative numbers", func(t *testing.T) {
		for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
			guard := CreateLoopGuard(LoopGuardOptions{})
			guard.Observe(action("a", "1", 2.5))
			guard.Restore(&LoopGuardSnapshot{Version: 1, ActionCount: -1, CumulativeCostUsd: value})
			snapshot := guard.Snapshot()
			if snapshot.ActionCount != 1 || snapshot.CumulativeCostUsd != 2.5 {
				t.Errorf("restore(%v) overwrote state: %+v", value, snapshot)
			}
		}
	})

	t.Run("does not alias guard state", func(t *testing.T) {
		guard := CreateLoopGuard(LoopGuardOptions{})
		guard.Observe(action("a", "1"))
		snapshot := guard.Snapshot()
		snapshot.Actions[0] = "tampered"
		if guard.Snapshot().Actions[0] == "tampered" {
			t.Error("Snapshot aliases the guard's action history")
		}

		other := CreateLoopGuard(LoopGuardOptions{})
		restoreFrom := LoopGuardSnapshot{Version: 1, Actions: []string{`["a","1"]`}}
		other.Restore(&restoreFrom)
		other.Observe(action("b", "2"))
		if len(restoreFrom.Actions) != 1 || restoreFrom.Actions[0] != `["a","1"]` {
			t.Errorf("Restore aliases the caller's snapshot: %+v", restoreFrom.Actions)
		}
	})
}

func TestEncodeActionIsAJSONArray(t *testing.T) {
	got := encodeAction(LoopAction{Tool: "bash", ArgsKey: "ls <dir> & echo"})
	want := `["bash","ls <dir> & echo"]`
	if got != want {
		t.Fatalf("encodeAction = %q, want %q", got, want)
	}
	if encodeAction(LoopAction{Tool: "a", ArgsKey: "b"}) == encodeAction(LoopAction{Tool: "ab", ArgsKey: ""}) {
		t.Fatal("distinct actions must not collide")
	}
}

func TestFormatNumber(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{10, "10"},
		{0, "0"},
		{0.5, "0.5"},
		{12.345678, "12.3457"},
		{0.00001, "0"},
		{-10.5, "-10.5"},
	}
	for _, testCase := range cases {
		if got := formatNumber(testCase.value); got != testCase.want {
			t.Errorf("formatNumber(%v) = %q, want %q", testCase.value, got, testCase.want)
		}
	}
}

func TestFormatPercent(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{0.8, "80%"},
		{0.845, "85%"},
		{2.0 / 3.0, "67%"},
		{0, "0%"},
	}
	for _, testCase := range cases {
		if got := formatPercent(testCase.value); got != testCase.want {
			t.Errorf("formatPercent(%v) = %q, want %q", testCase.value, got, testCase.want)
		}
	}
}
