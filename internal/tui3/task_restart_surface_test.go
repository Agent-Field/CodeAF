package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestRestoredUnfinishedTaskNeverReadsDoneOnSurface(t *testing.T) {
	for _, state := range []session.TaskState{session.TaskInterrupted, session.TaskFailed} {
		t.Run(string(state), func(t *testing.T) {
			a, _, _ := taskApp(t)
			a.taskUpdate(update(11, "Write the README", state, session.TaskNotice{
				Report: "incomplete — codeaf closed while this was still running",
			}))
			node := a.tasks[11]
			if node == nil || !node.restored {
				t.Fatal("task notice did not restore the task")
			}
			checkUnfinishedSurface(t, a, node)
		})
	}
	t.Run("plan", func(t *testing.T) {
		a, _, _ := taskApp(t)
		row := session.PlanTaskRow{ID: "t-11", Title: "Write the README", Status: "running", Interrupted: true}
		node := planRailNode(row)
		a.tasks = map[uint64]*taskNode{node.id: node}
		a.taskOrder = []uint64{node.id}
		checkUnfinishedSurface(t, a, node)
		if got := workTabRow([]session.PlanTaskRow{row, {ID: "t-12", Status: "running"}}); got.ID != "t-12" {
			t.Errorf("work tab selected interrupted row: %s", got.ID)
		}
	})
}

func checkUnfinishedSurface(t *testing.T, a *app, node *taskNode) {
	t.Helper()
	header, _ := a.roomFactsWord(node, 160)
	header = plain(header)
	if !strings.Contains(header, "interrupted") && !strings.Contains(header, "incomplete") {
		t.Errorf("restored room header reads %q, want interrupted or incomplete", header)
	}
	// Ended work that did not finish is still filed under Done, first in its
	// fold (railFinalOrder); what must not happen is its own header saying done.
}

func TestQueuedOnlyTaskDoesNotMakeMainFooterWorking(t *testing.T) {
	a, _, _ := taskApp(t)
	a.state = stateIdle
	a.taskUpdate(update(11, "Write the README", session.TaskQueued, session.TaskNotice{Waiting: "machine busy"}))
	if word, _ := a.stateWord(); word == tabWorkingWord {
		t.Errorf("queued-only main footer reads %q", word)
	}
	a.taskUpdate(update(11, "Write the README", session.TaskRunning, session.TaskNotice{}))
	if word, _ := a.stateWord(); word != tabWorkingWord {
		t.Errorf("running task main footer reads %q, want working", word)
	}
}
