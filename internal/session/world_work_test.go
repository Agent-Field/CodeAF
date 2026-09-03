package session

import (
	"testing"
	"time"
)

// TestWorkOrdersByTaskFactsNotConversationRecency verifies two laws:
//
//  1. World.Work() returns every task this machine has run ordered by the
//     work's own facts — needs a person, then running, then by when it landed
//     newest first — with (SessionID, ID) as the tie-break. Project and
//     conversation recency are not part of the order.
//
//  2. Touching a conversation's LastUserAt does not change the order, because
//     the sort is on task facts alone.
func TestWorkOrdersByTaskFactsNotConversationRecency(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}

	fastConv := SessionRow{
		ID: "fast-room", At: at(25, 11),
		Tasks: TaskRollup{Rows: []TaskIndexEntry{
			{ID: "1", SessionID: "fast-room", Status: string(TaskDone), EndedAt: at(22, 8)},
			{ID: "2", SessionID: "fast-room", Status: string(TaskUnverified), EndedAt: at(23, 8)},
		}},
	}
	slowConv := SessionRow{
		ID: "slow-room", At: at(25, 9),
		Tasks: TaskRollup{Rows: []TaskIndexEntry{
			{ID: "3", SessionID: "slow-room", Status: string(TaskDone), EndedAt: at(25, 10)},
			{ID: "4", SessionID: "slow-room", Status: string(TaskRunning), EndedAt: time.Time{}},
		}},
	}

	world := World{
		Projects: []Project{
			{Name: "fast-talk", Sessions: []SessionRow{fastConv}},
			{Name: "slow-talk", Sessions: []SessionRow{slowConv}},
		},
		Read: now,
	}

	work := world.Work()

	// Expected: needs-person (2), running (4), then landed by EndedAt newest
	// first: 3 (at 25/10) then 1 (at 22/8).
	want := []string{"2", "4", "3", "1"}
	got := make([]string, len(work))
	for i, e := range work {
		got[i] = e.Entry.ID
	}
	if len(got) != len(want) {
		t.Fatalf("Work() returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Work()[%d] = %s, want %s. Full order: %v", i, got[i], want[i], got)
		}
	}

	// Flip slowConv's LastUserAt to be even newer than fastConv. The order
	// must not change — it is about the work, not about the conversation.
	world.Projects[1].Sessions[0].At = at(25, 12)
	work2 := world.Work()
	got2 := make([]string, len(work2))
	for i, e := range work2 {
		got2[i] = e.Entry.ID
	}
	for i := range want {
		if got2[i] != want[i] {
			t.Fatalf("after touching conversation, Work()[%d] = %s, want %s. The order changed when only a conversation timestamp moved.", i, got2[i], want[i])
		}
	}
}

// TestWorkTiebreakIsSessionIDThenID verifies that entries with the same status
// and the same EndedAt are ordered by (SessionID, ID), which is the pair the
// rest of this package states identifies one row.
func TestWorkTiebreakIsSessionIDThenID(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	at := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)

	row := SessionRow{
		ID: "room-b",
		Tasks: TaskRollup{Rows: []TaskIndexEntry{
			{ID: "2", SessionID: "room-b", Status: string(TaskDone), EndedAt: at},
			{ID: "1", SessionID: "room-b", Status: string(TaskDone), EndedAt: at},
			{ID: "1", SessionID: "room-a", Status: string(TaskDone), EndedAt: at},
		}},
	}
	world := World{
		Projects: []Project{{Name: "p", Sessions: []SessionRow{row}}},
		Read: now,
	}
	work := world.Work()
	// SessionID "room-a" < "room-b", and within room-b ID "1" < "2".
	want := []string{"room-a/1", "room-b/1", "room-b/2"}
	got := make([]string, len(work))
	for i, e := range work {
		got[i] = e.Entry.SessionID + "/" + e.Entry.ID
	}
	if len(got) != len(want) {
		t.Fatalf("Work() returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tiebreak: Work()[%d] = %s, want %s. Full: %v", i, got[i], want[i], got)
		}
	}
}
