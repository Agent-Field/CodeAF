package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestSidebarKeepsHideAboveNewTasksAndTaskActionBelow(t *testing.T) {
	for _, width := range []int{100, 120, 180} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, _, _ := taskApp(t)
			a.width = width
			for count := 0; count <= 3; count++ {
				if count > 0 {
					a.taskUpdate(update(uint64(count), fmt.Sprintf("Task number %d", count), session.TaskRunning, session.TaskNotice{}))
				}
				rows, _ := a.railView(20)
				if len(rows) != 20 || !rows[0].stow || !strings.Contains(plain(rows[0].text), railStowHint) {
					t.Fatalf("hide moved after %d tasks: %+v", count, rows)
				}
				entries, last, action := a.railEntries(), 0, -1
				var ids []uint64
				for i, row := range rows {
					if row.entry >= 0 && row.head {
						ids = append(ids, entries[row.entry].node.id)
						last = i
					}
					if row.door == marginTaskType {
						action = i
					}
				}
				if len(ids) != count || action != last+1 {
					t.Fatalf("tasks or action are misplaced: ids=%v last=%d action=%d", ids, last, action)
				}
				for i, id := range ids {
					if id != uint64(i+1) {
						t.Fatalf("tasks are not in creation order: %v", ids)
					}
				}
			}
			a.taskUpdate(update(1, "Task number 1", session.TaskDone, session.TaskNotice{}))
			for i, entry := range a.railEntries() {
				if entry.node.id != uint64(i+1) {
					t.Fatal("completion reordered the task list")
				}
			}
			for id := uint64(4); id <= 30; id++ {
				a.taskUpdate(update(id, fmt.Sprintf("Task number %d", id), session.TaskRunning, session.TaskNotice{}))
			}
			a.railTop = 20
			for _, height := range []int{1, 4, 8, 20} {
				rows, _ := a.railView(height)
				if len(rows) != height || !rows[0].stow || rows[0].fade != 0 {
					t.Fatalf("scrolling or resizing displaced the header at height %d: %+v", height, rows)
				}
			}
		})
	}
}
