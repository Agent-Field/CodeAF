package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// TestTasksOrderedByWorkFactsNotConversationRecency sets up two projects whose
// conversations were last spoken to in one order and whose tasks ended in
// another, then asserts the rows within a section are in the tasks' order.
//
// The same holds for rows from the mine and away authorities.
func TestTasksOrderedByWorkFactsNotConversationRecency(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}

	// Project "fast-talk": newest conversation (at 25/11), older tasks.
	fastConv := session.SessionRow{
		ID: "fast-room", Title: "fast chat", Project: "fast-talk",
		At: at(25, 11), Open: true,
		Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{
			{ID: "1", Label: "old task", SessionID: "fast-room",
				Status: string(session.TaskDone), EndedAt: at(22, 8)},
			{ID: "2", Label: "slightly newer", SessionID: "fast-room",
				Status: string(session.TaskDone), EndedAt: at(23, 8)},
		}},
	}

	// Project "slow-talk": older conversation (at 25/9), newer tasks.
	slowConv := session.SessionRow{
		ID: "slow-room", Title: "slow chat", Project: "slow-talk",
		At: at(25, 9), Open: true,
		Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{
			{ID: "3", Label: "newest landed", SessionID: "slow-room",
				Status: string(session.TaskDone), EndedAt: at(24, 11)},
			{ID: "4", Label: "mid landed", SessionID: "slow-room",
				Status: string(session.TaskDone), EndedAt: at(24, 10)},
		}},
	}

	world := session.World{
		Projects: []session.Project{
			{Name: "fast-talk", Sessions: []session.SessionRow{fastConv}},
			{Name: "slow-talk", Sessions: []session.SessionRow{slowConv}},
		},
		Read: now,
	}
	win := session.LastDays(now, 10)

	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)

	// Collect only the "earlier" section items (both tasks are more than a day
	// old). In the desired order: newest first = ID 3 (at 25), 4 (at 24),
	// 2 (at 23), 1 (at 22).
	var earlierLabels []string
	for _, item := range reading.items {
		if item.section == tasksEarlier {
			earlierLabels = append(earlierLabels, item.entry.Label)
		}
	}
	want := []string{"newest landed", "mid landed", "slightly newer", "old task"}
	if len(earlierLabels) != len(want) {
		page := strings.Join(reading.rows(120, newPalette(tokens.NoColor, false)), "\n")
		t.Fatalf("earlier section has %d items, want %d. Page:\n%s", len(earlierLabels), len(want), page)
	}
	for i := range want {
		if earlierLabels[i] != want[i] {
			page := strings.Join(reading.rows(120, newPalette(tokens.NoColor, false)), "\n")
			t.Fatalf("earlier[%d] = %q, want %q. Page:\n%s", i, earlierLabels[i], want[i], page)
		}
	}

	// Now add mine entries and verify they land in the same ordered sections.
	mine := tasksMine{
		row: session.SessionRow{ID: "my-room", At: now},
		rows: []tasksMineRow{
			{entry: session.TaskIndexEntry{
				ID: "m1", Label: "my fresh task", SessionID: "my-room",
				Status: string(session.TaskDone), EndedAt: at(25, 11),
			}, runs: false},
		},
		away: []session.ElsewhereTask{{
			SessionID: "away-room", Session: "next door",
			Task: session.PresenceTask{
				ID: "a1", Title: "away unlanded", State: string(session.TaskRunning),
			},
		}},
	}

	readingWithMine := readTasks(world, mine, win, time.Time{}, now)
	// Running away task should be in running section. Fresh mine task should be
	// in today section.
	var sections []string
	for _, item := range readingWithMine.items {
		sections = append(sections, tasksSectionWord(item.section))
		_ = sections
	}

	// Verify away task is in running
	foundAway := false
	for _, item := range readingWithMine.items {
		if item.away && item.section != tasksRunning {
			t.Fatalf("away task filed under %q, want running", tasksSectionWord(item.section))
		}
		if item.away {
			foundAway = true
		}
	}
	if !foundAway {
		t.Fatal("away task not found in reading")
	}

	// Verify the earlier section still has tasks in work-fact order even with
	// mine/away present
	var earlier2 []string
	for _, item := range readingWithMine.items {
		if item.section == tasksEarlier {
			earlier2 = append(earlier2, item.entry.Label)
		}
	}
	for i := range want {
		if earlier2[i] != want[i] {
			t.Fatalf("with mine+away, earlier[%d] = %q, want %q", i, earlier2[i], want[i])
		}
	}
}