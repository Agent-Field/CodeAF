package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// S4 and S7 preserve the original append bytes for an ordinary run.
func TestALocalStepLineHasNoOrderFields(t *testing.T) {
	dir := t.TempDir()
	// A sibling path that cannot be read as a file must never be scanned by
	// an ordinary append. The original append only touches its own record.
	if err := os.MkdirAll(filepath.Join(plandb.TaskDir(dir, "sibling"), trajectoryName), 0700); err != nil {
		t.Fatal(err)
	}
	if err := appendTrajectory(dir, "1", Step{Kind: "step", Step: 1, Command: "pwd"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(plandb.TaskDir(dir, "1"), trajectoryName))
	if err != nil {
		t.Fatal(err)
	}
	const original = "{\"kind\":\"step\",\"step\":1,\"command\":\"pwd\"}\n"
	if string(raw) != original {
		t.Fatalf("local trajectory bytes changed: %s", raw)
	}
}

// S7 orders only the opted-in child, across tasks, and restores local bytes.
func TestOnlyTheTaskDoorChildRecordsStepOrder(t *testing.T) {
	dir := t.TempDir()
	restore := RecordTaskTrajectoryOrder()
	for _, id := range []string{"2", "1"} {
		if err := appendTrajectory(dir, id, Step{Kind: "step", Step: 1, Command: "pwd"}); err != nil {
			restore()
			t.Fatal(err)
		}
	}
	restore()
	for i, id := range []string{"2", "1"} {
		steps, err := Trajectory(dir, id)
		if err != nil || len(steps) != 1 || steps[0].RecordedOrder != uint64(i+1) || steps[0].RecordedAt.IsZero() {
			t.Fatalf("task %s order=%+v, %v", id, steps, err)
		}
	}
	TestALocalStepLineHasNoOrderFields(t)
}
