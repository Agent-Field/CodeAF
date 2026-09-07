package tui3

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// Returning from a record opened on Home must reveal that exact child in the
// list, even when both its conversation and its worker ancestors were folded.
func TestTaskRecordReturnRevealsItsAncestors(t *testing.T) {
	a, _, _ := roomApp(t)
	now := time.Now()
	row := session.SessionRow{ID: "record-owner", Title: "Repair the parser"}
	for _, spec := range []struct{ id, parent string }{{"1", ""}, {"2", "1"}, {"3", "2"}} {
		row.Tasks.Rows = append(row.Tasks.Rows, session.TaskIndexEntry{
			SessionID: row.ID, ID: spec.id, Parent: spec.parent,
			Title: "Parser work " + spec.id, Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour),
		})
	}
	world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}
	a.taskSheet = tasksPlace{
		reading: readTasks(world, tasksMine{}, session.LastDays(now, 14), time.Time{}, now),
		awayAt:  a.elsewhere().Read, mineAt: a.railStamp,
	}
	for _, want := range row.Tasks.Rows[1:] {
		a.taskSheet.opened = nil
		a.taskSheetPointAt(want)
		got, ok := a.taskSheetCurrent()
		if !ok || !taskSameRecord(got.entry, want) {
			t.Fatalf("return from child record selected %+v, want %+v", got.entry, want)
		}
	}
}
