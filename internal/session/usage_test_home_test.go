package session

import (
	"testing"
)

// useTaskUsageHome owns a disposable home shared by a task and its child agents.
func useTaskUsageHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ledger := UsageLedgerPath()
	// Agents registered later close before this shared home writer is joined.
	t.Cleanup(func() {
		if !StopUsageWriter(ledger) {
			t.Error("task usage writer did not stop before home cleanup")
		}
	})
}

func TestTaskUsageHomeCleanupJoinsWriter(t *testing.T) {
	var writer *usageWriter
	var path string
	if !t.Run("task home", func(t *testing.T) {
		useTaskUsageHome(t)
		path = UsageLedgerPath()
		RecordUsage(path, UsageLine{ID: "task-home-cleanup", Input: 1, USD: .01})
		if !FlushUsage() {
			t.Fatal("task usage did not reach its ledger")
		}
		writer = usageWritersAt(path)[0]
	}) {
		return
	}
	t.Cleanup(func() { StopUsageWriter(path) })
	select {
	case <-writer.stopped:
	default:
		t.Fatal("task home cleanup left its usage writer running")
	}
}
