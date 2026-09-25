package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE HEADER HOLDS STILL AND THE TASK ACTION FOLLOWS THE LIST. The column's
// first row is its header, with its key at the right, whatever the column
// holds, however far it is scrolled and however short the frame; the `+ /task`
// action is the row after the last task.
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
				if len(rows) != 20 || rows[0].side == nil || rows[0].side.key != sideHeadKey ||
					!strings.HasSuffix(strings.TrimRight(plain(rows[0].text), " "), sideHideKey) {
					t.Fatalf("the header moved after %d tasks: %+v", count, rows)
				}
				entries, last, action := a.railEntries(), 0, -1
				var ids []uint64
				for i, row := range rows {
					if row.entry >= 0 {
						if e := entries[row.entry]; !e.head {
							ids = append(ids, e.node.id)
						}
						last = i
					}
					if row.door == marginTaskType {
						action = i
					}
				}
				if len(ids) != count || action != last+1 {
					t.Fatalf("tasks or action are misplaced: ids=%v last=%d action=%d", ids, last, action)
				}
				// NEWEST FIRST, which is what a person arriving at the column
				// wants to read first.
				for i, id := range ids {
					if id != uint64(count-i) {
						t.Fatalf("tasks are not newest first: %v", ids)
					}
				}
			}
			for id := uint64(4); id <= 30; id++ {
				a.taskUpdate(update(id, fmt.Sprintf("Task number %d", id), session.TaskRunning, session.TaskNotice{}))
			}
			a.railTop = 20
			for _, height := range []int{1, 4, 8, 20} {
				rows, _ := a.railView(height)
				if len(rows) != height || rows[0].side == nil || rows[0].side.key != sideHeadKey || rows[0].fade != 0 {
					t.Fatalf("scrolling or resizing displaced the header at height %d: %+v", height, rows)
				}
			}
		})
	}
}
