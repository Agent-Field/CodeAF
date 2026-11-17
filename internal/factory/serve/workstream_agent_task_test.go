package serve

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A TASK AFI WORKS RUNS IN ITS OWN SEAT, NEVER IN A PERSON'S PANE. The run
// works as the person who started it, says what it did on the task, and
// nothing of it reaches anyone's private conversation; a second start while
// it runs is refused, a stop ends it, and a run that ends without saying done
// or failed leaves the task failed rather than running with nobody on it.
func TestATaskRunsInItsOwnSeatAndReportsOnTheTask(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	ctx := context.Background()
	mia := &agentDoor{h: o.h, id: webhook.ID, asker: "mia"}
	newTask := func(title string) factory.WorkstreamTask {
		t.Helper()
		line, err := mia.Work(ctx, "ws_task_create", json.RawMessage(`{"title":"`+title+`","request":"Do it."}`))
		if err != nil {
			t.Fatal(err)
		}
		var out struct{ Task struct{ ID string } }
		_ = json.Unmarshal([]byte(line), &out)
		if _, err := mia.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+out.Task.ID+`","state":"running"}`)); err != nil {
			t.Fatal(err)
		}
		tk, err := mia.task(ctx, out.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	ws, _, _ := o.st.Workstream(webhook.ID)

	if err := o.h.runTaskTurn(ws, factory.WorkstreamTask{ID: "tk_x"}, "mia", "x"); !errors.Is(err, errTaskNoRunner) {
		t.Fatalf("with no runner a start = %v", err)
	}

	release := make(chan struct{})
	var seen []AgentTurn
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		seen = append(seen, turn)
		d := turn.Door.(workDoor)
		switch turn.Task.Title {
		case "Finish":
			if _, err := d.Work(ctx, "ws_task_comment", json.RawMessage(`{"task":"`+turn.Task.ID+`","text":"Wrote the table."}`)); err != nil {
				return AgentTurnEnd{}, err
			}
			_, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done"}`))
			return AgentTurnEnd{USD: 0.01}, err
		case "Wait":
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return AgentTurnEnd{}, nil
	})

	finish := newTask("Finish")
	if err := o.h.runTaskTurn(ws, finish, "mia", "Write the table."); err != nil {
		t.Fatal(err)
	}
	o.settle()
	if got, _ := mia.task(ctx, finish.ID); got.State != factory.TaskDone {
		t.Fatalf("a run that said done left %+v", got)
	}
	if s := seen[0]; s.Asker != "mia" || s.Text != "Write the table." || s.Task.ID != finish.ID || !strings.Contains(s.Dir, "/tasks/"+finish.ID+"/") {
		t.Fatalf("the runner was handed %+v", s)
	}

	// Nothing of the run is in mia's private conversation, nor anyone's.
	if _, err := os.Stat(o.h.agentDir(agentSeat{webhook.ID, "mia"})); err == nil {
		t.Fatal("the task run made mia a conversation")
	}
	var pane api.WorkstreamAgent
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &pane)
	if len(pane.Entries) != 0 || pane.State != api.AgentIdle {
		t.Fatalf("the task run wrote into mia's pane: %+v", pane)
	}

	silent := newTask("Silent")
	if err := o.h.runTaskTurn(ws, silent, "mia", "Do it."); err != nil {
		t.Fatal(err)
	}
	o.settle()
	if got, _ := mia.task(ctx, silent.ID); got.State != factory.TaskFailed || got.StateReason == "" {
		t.Fatalf("a run that ended without a word left %+v", got)
	}

	wait := newTask("Wait")
	if err := o.h.runTaskTurn(ws, wait, "mia", "Do it."); err != nil {
		t.Fatal(err)
	}
	if err := o.h.runTaskTurn(ws, wait, "abir", "Do it."); !errors.Is(err, errTaskBusy) {
		t.Fatalf("a second start while it runs = %v", err)
	}
	if !o.h.taskTurnRunning(webhook.ID, wait.ID) || !o.h.stopTaskTurn(webhook.ID, wait.ID) {
		t.Fatal("the run could not be stopped")
	}
	o.settle()
	if got, _ := mia.task(ctx, wait.ID); got.State != factory.TaskRunning {
		t.Fatalf("a stopped run changed the task itself: %+v (the task's Stop says so)", got)
	}
	if o.h.taskTurnRunning(webhook.ID, wait.ID) || o.h.stopTaskTurn(webhook.ID, wait.ID) {
		t.Fatal("a stopped run is still listed")
	}
	close(release)
}
