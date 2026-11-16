package serve

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A START ON THE ORGANIZATION'S SERVER WAKES AFI, AND ONLY A TASK AFI TOOK
// SAYS RUNNING. Two people: santosh owns the workstream, abir can edit it.
// abir's Start runs the task in its own seat with abir's rights, Afi's result
// lands in the task's thread and the task ends done; Start all does the same
// for a group; santosh's Stop ends a run; and with no runner a Start is
// refused and the task stays up next (live use, L1 F5 / L4 B6).
func TestAStartOnTheServerWakesAfiAndOnlyThenSaysRunning(t *testing.T) {
	o, path := taskOrg(t, "edit")
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}

	alone := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft the notes", Request: "Two paragraphs.", DoneBy: afi})
	o.do(t, "abir", "PATCH", path+"/tasks/"+alone.ID, alone.Rev, map[string]any{"state": "running"}, 409, nil)
	if got := o.one(t, path, alone.ID); got.State != factory.TaskUpNext {
		t.Fatalf("a start nothing could take left the task %q", got.State)
	}

	var mu sync.Mutex
	asked := map[string]AgentTurn{}
	release := make(chan struct{})
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		asked[turn.Task.Title] = turn
		mu.Unlock()
		d := turn.Door.(workDoor)
		if turn.Task.Title == "Hold" {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return AgentTurnEnd{}, nil
		}
		if _, err := d.Work(ctx, "ws_task_comment", json.RawMessage(`{"task":"`+turn.Task.ID+`","text":"Done: `+turn.Task.Title+`"}`)); err != nil {
			return AgentTurnEnd{}, err
		}
		_, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done"}`))
		return AgentTurnEnd{}, err
	})

	// Start, by the person who is not the owner.
	var running api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+alone.ID, alone.Rev, map[string]any{"state": "running"}, 200, &running)
	if running.State != factory.TaskRunning {
		t.Fatalf("started %+v", running)
	}
	o.settle()
	if got := o.one(t, path, alone.ID); got.State != factory.TaskDone || got.DoneBy.Kind != factory.TaskByAfi {
		t.Fatalf("Afi's run left %+v", got)
	}
	if turn := asked["Draft the notes"]; turn.Asker != "abir" || turn.Text != "Draft the notes\n\nTwo paragraphs." {
		t.Fatalf("Afi was asked %+v", turn)
	}
	if !o.threadSays(t, path, alone.ID, "Done: Draft the notes") {
		t.Fatal("Afi's result is not in the task's thread")
	}
	for _, who := range []string{"santosh", "abir"} {
		var pane api.WorkstreamAgent
		o.do(t, who, "GET", path+"/agent", -1, nil, 200, &pane)
		if len(pane.Entries) != 0 {
			t.Fatalf("the run wrote into %s's pane: %+v", who, pane.Entries)
		}
	}

	// Start all: both Afi tasks run, the chat's stays for its chat.
	g := &factory.TaskGroup{Name: "Launch", Kind: "batch"}
	one := o.task(t, "santosh", path, api.TaskCreate{Title: "Write the post", DoneBy: afi, Group: g})
	o.task(t, "santosh", path, api.TaskCreate{Title: "Pick the image", DoneBy: afi, Group: g})
	chat := o.task(t, "santosh", path, api.TaskCreate{Title: "Ship the build", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByChat, ChatID: "c1", Name: "Build"}, Group: g})
	var started api.WorkstreamTasks
	o.do(t, "abir", "POST", path+"/tasks/start_all", -1, api.TasksStartAll{Group: one.Group.ID}, 200, &started)
	if len(started.Tasks) != 2 || started.Tasks[0].State != factory.TaskRunning {
		t.Fatalf("start all started %+v", started.Tasks)
	}
	o.settle()
	for _, task := range o.tasks(t, "santosh", path) {
		want := factory.TaskDone
		if task.ID == chat.ID {
			want = factory.TaskUpNext
		}
		if task.State != want {
			t.Fatalf("%s is %q after start all", task.Title, task.State)
		}
	}

	// The owner's Stop ends a run that is going.
	hold := o.task(t, "santosh", path, api.TaskCreate{Title: "Hold", DoneBy: afi})
	o.do(t, "abir", "PATCH", path+"/tasks/"+hold.ID, hold.Rev, map[string]any{"state": "running"}, 200, &running)
	if !o.h.taskTurnRunning(running.WS, hold.ID) {
		t.Fatal("the started task has no run")
	}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+hold.ID, running.Rev, map[string]any{"state": "stopped"}, 200, nil)
	o.settle()
	if o.h.taskTurnRunning(running.WS, hold.ID) {
		t.Fatal("Stop left the run going")
	}
	if got := o.one(t, path, hold.ID); got.State != factory.TaskStopped {
		t.Fatalf("stopped %+v", got)
	}
	close(release)
}

func (o *wsOrg) one(t *testing.T, path, id string) api.WorkstreamTask {
	t.Helper()
	var out api.WorkstreamTask
	o.do(t, "santosh", "GET", path+"/tasks/"+id, -1, nil, 200, &out)
	return out
}

func (o *wsOrg) threadSays(t *testing.T, path, id, text string) bool {
	t.Helper()
	var events api.WorkstreamTaskEvents
	o.do(t, "santosh", "GET", path+"/tasks/"+id+"/events", -1, nil, 200, &events)
	for _, e := range events.Events {
		if strings.Contains(e.Text, text) {
			return true
		}
	}
	return false
}
