package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestTaskRoomConflictAgreesWithMainCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	for _, state := range []session.TaskState{session.TaskDone, session.TaskUnverified} {
		a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
			ID: 7, State: state, Title: "write collision", Merge: "conflicted", GroundHeld: true,
			Conflicts: []string{"COLLISION.txt"},
		}})
		node := a.tasks[7]
		if state == session.TaskDone {
			node.planRow = &session.PlanTaskRow{ID: "7", Status: string(session.TaskUnverified)}
		}
		if got := a.roomStateWord(node); got != "your call" || a.taskStatus(node).Word != got {
			t.Fatalf("state %s: room=%q main=%q", state, got, a.taskStatus(node).Word)
		}
	}
}
