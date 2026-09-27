package tui3

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestPlanRailKeepsCheckRowsUnderTheirRun(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "run", Title: "run", Status: "done"},
		{ID: "check", Parent: "run", Title: "check: store.py", Status: "done"},
	}
	forest := (tasksReading{items: c253PlanItems(rows, "chat"), now: taskFixtureNow}).planRailForest(rows)
	if len(forest) != 1 || len(forest[0].kids) != 1 || forest[0].kids[0].row.Title != "check: store.py" {
		t.Fatalf("check row was not kept under its run: %+v", forest)
	}
}

func TestPlanCheckIsNotHiddenByAWorkerWithTheSameTitle(t *testing.T) {
	now := taskFixtureNow
	chat := session.SessionRow{ID: "chat", Title: "chat", Open: true}
	chat.Tasks.Rows = []session.TaskIndexEntry{{ID: "worker", Title: "test cart.py", SessionID: chat.ID, Status: string(session.TaskDone)}}
	mine := tasksMine{
		row:  chat,
		rows: []tasksMineRow{{entry: chat.Tasks.Rows[0]}},
		plan: []session.PlanTaskRow{{ID: "t-check", Title: "test cart.py", Seat: "check", Status: "done"}},
	}
	reading := readTasks(session.World{Projects: []session.Project{{Name: "project", Sessions: []session.SessionRow{chat}}}}, mine,
		session.LastDays(now, 10), tasksSort{}, time.Time{}, now)
	for _, item := range reading.items {
		if item.plan != nil && item.plan.Seat == "check" {
			return
		}
	}
	t.Fatal("the check row was hidden by the worker row with the same title")
}
