package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"testing"
)

// Clicking an ancestor must start its read now, without needing another key.
func TestClickingAHeldTaskBreadcrumbStartsItsTranscriptRead(t *testing.T) {
	a, _ := startingRoomLab(t)
	a.width, a.height = 120, 40
	reads := 0
	a.farRoomRecord = func(id uint64, max int) (session.TaskRecord, error) {
		reads++
		if id != 9 {
			t.Fatalf("read task %d, wanted the clicked ancestor", id)
		}
		return session.TaskRecord{}, nil
	}
	child := *a.tasks[9]
	child.id, child.parent, child.title = 10, "9", "Child task"
	a.tasks[10] = &child
	a.openFarRoom(&child, child.title)
	a.takeRoomPump() // Only the next, clicked navigation is exercised here.
	a.roomHeadRows(a.width)
	var target crumbHit
	for _, hit := range a.crumbs {
		if hit.crumb.kind == crumbAncestor {
			target = hit
			break
		}
	}
	if target.crumb.node == nil {
		t.Fatal("no ancestor target")
	}
	_, cmd := a.Update(tea.MouseClickMsg{X: target.span.from, Y: a.roomHeadRow(), Button: tea.MouseLeft})
	if a.room.id != 9 || cmd == nil {
		t.Fatal("click opened the ancestor without starting its read")
	}
	runCmd(cmd)
	if reads != 1 {
		t.Fatalf("click started %d reads", reads)
	}
}
