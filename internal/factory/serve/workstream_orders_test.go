package serve

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// AN ORGANIZATION'S STANDING ORDER IS KEPT AND RUN BY ITS SERVER (E4 F0, F6).
// santosh looks after Pantry and adds an hourly order; abir, who only edits
// it, sees the order but may not add or switch one. When it comes due, the
// server makes the run ONE task owned by the owner of incoming work (abir),
// asking "Run … now?"; abir's Go starts the workstream's agent ON THE SERVER
// in the task's own seat, as abir, and Stop ends it. Skip this one stops the
// next run's task with why, and nothing runs.
func TestAnOrganizationsOrderIsKeptAndRunByItsServer(t *testing.T) {
	o, path := taskOrg(t, "edit")
	body := api.WorkstreamOrderNew{When: "every 1h", Zone: "UTC", Does: "Check Pantry for tasks due today or late, and write a three-line note."}

	// A server that cannot run Afi keeps no orders: it says so, and refuses one.
	var list api.WorkstreamOrders
	o.do(t, "santosh", "GET", path+"/orders", -1, nil, 200, &list)
	if list.Runs || len(list.Orders) != 0 {
		t.Fatalf("a server with no runner lists %+v", list)
	}
	o.do(t, "santosh", "POST", path+"/orders", -1, body, 409, nil)

	var mu sync.Mutex
	var turns []AgentTurn
	release := make(chan struct{})
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		turns = append(turns, turn)
		mu.Unlock()
		select {
		case <-release:
		case <-ctx.Done():
		}
		return AgentTurnEnd{}, nil
	})
	defer close(release)

	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	rules := ws.Rules
	rules.DefaultOwner = factory.RulesOwner{Kind: "person", ID: "abir", Name: "abir"}
	o.do(t, "santosh", "PATCH", path, ws.Rev, api.WorkstreamPatch{Rules: &rules}, 200, nil)

	// Only the person who looks after it adds one; everyone who sees it sees it.
	o.do(t, "abir", "POST", path+"/orders", -1, body, 403, nil)
	var made api.WorkstreamOrder
	o.do(t, "santosh", "POST", path+"/orders", -1, body, 201, &made)
	if !made.On || made.Next == "" || made.When == "" || made.CreatedBy.ID != "santosh" || made.Title == "" {
		t.Fatalf("the new order = %+v", made)
	}
	o.do(t, "abir", "GET", path+"/orders", -1, nil, 200, &list)
	if !list.Runs || len(list.Orders) != 1 || list.Orders[0].ID != made.ID {
		t.Fatalf("abir sees %+v", list)
	}
	off := false
	o.do(t, "abir", "PATCH", path+"/orders/"+made.ID, made.Rev, api.WorkstreamOrderPatch{On: &off}, 403, nil)

	// It comes due: one task, asking its owner first, and nothing runs yet.
	o.h.runDueOrders(time.Now().Add(2 * time.Hour))
	tasks := o.tasks(t, "abir", path)
	if len(tasks) != 1 {
		t.Fatalf("one run made %d tasks", len(tasks))
	}
	run := tasks[0]
	if run.State != factory.TaskNeeds || run.Ask == nil || run.Ask.ID != "order:1" || run.Owner.ID != "abir" ||
		run.CameFrom.Kind != factory.TaskByOrder || run.CameFrom.OrderID != made.ID || run.DoneBy.Kind != factory.TaskByAfi ||
		run.Group == nil || run.Group.Kind != factory.TaskByOrder {
		t.Fatalf("the run's task = %+v", run)
	}
	if len(turns) != 0 {
		t.Fatal("a run that asks first started before anyone said go")
	}
	// The same slot is not taken twice.
	o.h.runDueOrders(time.Now().Add(2 * time.Hour))
	if n := len(o.tasks(t, "abir", path)); n != 1 {
		t.Fatalf("a second look made %d tasks", n)
	}

	// abir says Go: the workstream's agent runs on the server, in the task's seat, as abir.
	o.do(t, "santosh", "POST", path+"/tasks/"+run.ID+"/answer", -1, api.TaskAnswer{Option: "go"}, 403, nil)
	o.do(t, "abir", "POST", path+"/tasks/"+run.ID+"/answer", -1, api.TaskAnswer{Option: "go"}, 200, nil)
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(turns) == 1 })
	mu.Lock()
	turn := turns[0]
	mu.Unlock()
	if turn.Task == nil || turn.Task.ID != run.ID || turn.Asker != "abir" || !strings.Contains(turn.Text, "three-line note") {
		t.Fatalf("the run = %+v", turn)
	}
	if !o.h.taskTurnRunning(ws.ID, run.ID) {
		t.Fatal("the run is not going on the server")
	}
	// Stop on the task ends it.
	var cur api.WorkstreamTask
	o.do(t, "abir", "GET", path+"/tasks/"+run.ID, -1, nil, 200, &cur)
	stopped := factory.TaskStopped
	o.do(t, "abir", "PATCH", path+"/tasks/"+run.ID, cur.Rev, map[string]any{"state": stopped}, 200, nil)
	waitFor(t, func() bool { return !o.h.taskTurnRunning(ws.ID, run.ID) })

	// The next slot: Skip this one stops its task with why, and runs nothing.
	o.h.runDueOrders(time.Now().Add(4 * time.Hour))
	var next api.WorkstreamTask
	for _, x := range o.tasks(t, "abir", path) {
		if x.CameFrom.Run == 2 {
			next = x
		}
	}
	if next.Ask == nil || next.Ask.ID != "order:2" {
		t.Fatalf("the second run's task = %+v", next)
	}
	var skipped api.WorkstreamTask
	o.do(t, "abir", "POST", path+"/tasks/"+next.ID+"/answer", -1, api.TaskAnswer{Option: "skip"}, 200, &skipped)
	if skipped.State != factory.TaskStopped || skipped.StateReason == "" {
		t.Fatalf("a skipped run's task = %+v", skipped)
	}
	mu.Lock()
	n := len(turns)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("a skipped run ran: %d runs", n)
	}

	// Switched off, nothing comes due; removed, nobody sees it.
	o.do(t, "santosh", "GET", path+"/orders", -1, nil, 200, &list)
	o.do(t, "santosh", "PATCH", path+"/orders/"+made.ID, list.Orders[0].Rev, api.WorkstreamOrderPatch{On: &off}, 200, nil)
	o.h.runDueOrders(time.Now().Add(48 * time.Hour))
	if got := len(o.tasks(t, "abir", path)); got != 2 {
		t.Fatalf("an order switched off ran: %d tasks", got)
	}
	o.do(t, "santosh", "DELETE", path+"/orders/"+made.ID, -1, nil, 204, nil)
	o.do(t, "abir", "GET", path+"/orders", -1, nil, 200, &list)
	if len(list.Orders) != 0 {
		t.Fatalf("a removed order is still listed: %+v", list)
	}
}

// A RUN "START WITHOUT ASKING" COVERS STARTS ALONE: an order whose run may
// spend at most $1 in a workstream that lets agents start up to $2 runs on the
// server as soon as it comes due, with no question asked.
func TestAnOrdersRunStartsAloneWithinStartWithoutAsking(t *testing.T) {
	o, path := taskOrg(t, "edit")
	started := make(chan AgentTurn, 1)
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		started <- turn
		<-ctx.Done()
		return AgentTurnEnd{}, nil
	})
	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	rules := ws.Rules
	rules.StartWithoutAskingUSD = 2
	o.do(t, "santosh", "PATCH", path, ws.Rev, api.WorkstreamPatch{Rules: &rules}, 200, nil)
	o.do(t, "santosh", "POST", path+"/orders", -1, api.WorkstreamOrderNew{When: "every 0 9 * * 1", Does: "Draft This week in Pantry.", BudgetPerRunUSD: 1}, 201, nil)

	o.h.runDueOrders(time.Now().Add(8 * 24 * time.Hour))
	select {
	case turn := <-started:
		if turn.Task == nil || turn.Asker != "santosh" {
			t.Fatalf("the run = %+v", turn)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a run Start without asking covers did not start")
	}
	tasks := o.tasks(t, "santosh", path)
	if len(tasks) != 1 || tasks[0].State != factory.TaskRunning || tasks[0].Ask != nil {
		t.Fatalf("the run's task = %+v", tasks)
	}
	o.h.stopTaskTurn(ws.ID, tasks[0].ID)
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("waited too long")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
