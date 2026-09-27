package tui3

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

func TestStopFromMainBoxTargetsTheHeldPlan(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-7", Title: "held task", Status: "ready"}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, nil)
	a.slash("/stop")
	if a.stop == nil || a.stop.target.plan != row.ID || a.stop.target.id != "" {
		t.Fatalf("stop target = %+v, want the held plan identity", a.stop)
	}
}

func TestStopDoesNotChooseBetweenTwoPlanTasks(t *testing.T) {
	a, _ := planAppWith(t, []session.PlanTaskRow{
		{ID: "t-7", Title: "first", Status: "ready"},
		{ID: "t-8", Title: "second", Status: "ready"},
	}, nil)
	a.slash("/stop")
	if a.stop != nil {
		t.Fatal("stop selected work without a unique target")
	}
}

func TestStopAfterReconnectUsesThePlansCurrentEnding(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-2", Title: "held parent", Status: "running"}
	child := session.PlanTaskRow{ID: "t-3", Parent: "t-2", Title: "stopped child", Status: "cancelled", Stopped: true}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, child}, nil)
	a.tasks = make(map[uint64]*taskNode)
	a.tasks[3] = &taskNode{id: 3, state: session.TaskRunning, planTask: "t-3"}
	a.taskOrder = append(a.taskOrder, 3)
	a.railHold, a.railWhere = true, railSpot{id: 3}
	a.slash("/stop")
	if a.stop == nil || a.stop.target.plan != "t-2" {
		t.Fatalf("stop targeted stale child: %+v", a.stop)
	}
}

func TestAMachineHeldPlanNeverClaimsToBeWorking(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-2", Status: "running", Hold: "machine busy"}
	if got := planNodeState(row); got != session.TaskQueued {
		t.Fatalf("held plan state = %s", got)
	}
	if got := planNodeStatus(row); got.State != session.TaskQueued {
		t.Fatalf("held room status = %+v", got)
	}
}
