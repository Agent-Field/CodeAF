package session

import "testing"

// A ROLLUP'S NEWEST MOMENT IS A LANDING MOMENT. Live rows contribute to the
// running and incomplete counts exactly as the conversation's presence says,
// but none can supply a clock for work that has not ended.
func TestARollupOfOnlyRunningRowsHasNoNewestMoment(t *testing.T) {
	rows := []TaskIndexEntry{
		{ID: "1", Title: "read the tariff table", Status: string(TaskRunning)},
		{ID: "2", Title: "read the invoice writer", Status: string(TaskRunning)},
	}
	held := SessionRow{
		Live:     true,
		Presence: SessionPresence{RunningTasks: []PresenceTask{{ID: "1"}}},
	}
	got := rollUp(rows, held)
	if !got.Newest.IsZero() {
		t.Fatalf("running rows gave the rollup a newest landing at %s", got.Newest)
	}
	if got.Running != 1 || got.Incomplete != 1 {
		t.Fatalf("the rollup counts running/incomplete as %d/%d, want 1/1", got.Running, got.Incomplete)
	}
}
