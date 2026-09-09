package tui3

import (
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// BenchmarkTasksConversationHistory measures a developer's accumulated history,
// with several levels of delegated work in each conversation. The fixture is
// built outside the measurement so allocation results belong to the reading.
func BenchmarkTasksConversationHistory(b *testing.B) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	project := session.Project{Name: "developer workspace"}
	for chat := 0; chat < 40; chat++ {
		row := session.SessionRow{ID: "chat-" + strconv.Itoa(chat), Title: "Repair streaming parser and integrate regression coverage", At: now}
		for task := 1; task <= 128; task++ {
			parent := ""
			if task > 1 {
				parent = strconv.Itoa((task-2)/4 + 1)
			}
			row.Tasks.Rows = append(row.Tasks.Rows, session.TaskIndexEntry{
				SessionID: row.ID, ID: strconv.Itoa(task), Parent: parent,
				Title:  "Repair parser recovery and test malformed input " + strconv.Itoa(task),
				Status: string(session.TaskDone), EndedAt: now.Add(-time.Duration(task) * time.Minute),
			})
		}
		project.Sessions = append(project.Sessions, row)
	}
	world := session.World{Projects: []session.Project{project}, Read: now}
	win := session.LastDays(now, 14)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
		_ = reading.lay(120)
	}
}
