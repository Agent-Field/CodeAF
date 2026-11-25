package serve

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

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
		if strings.HasPrefix(turn.Task.Title, "Hold") {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return AgentTurnEnd{}, nil
		}
		if _, err := d.Work(ctx, "ws_task_comment", json.RawMessage(`{"task":"`+turn.Task.ID+`","text":"Done: `+turn.Task.Title+`"}`)); err != nil {
			return AgentTurnEnd{}, err
		}
		_, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done","handoff":"Made the table."}`))
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
	if turn := asked["Draft the notes"]; turn.Asker != "abir" || !strings.HasPrefix(turn.Text, "Draft the notes\n\nTwo paragraphs.") {
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

	// Start all: every agent task of the group runs, Afi's, a chat's and a named agent's.
	g := &factory.TaskGroup{Name: "Launch", Kind: "batch"}
	one := o.task(t, "santosh", path, api.TaskCreate{Title: "Write the post", DoneBy: afi, Group: g})
	o.task(t, "santosh", path, api.TaskCreate{Title: "Pick the image", DoneBy: afi, Group: g})
	// E2 F7: a chat's task and a named agent's start with the rest, each in its
	// own seat, from the task's brief and never the chat's conversation.
	o.h.config.WorkstreamAgents = func(factory.OrgWorkstream) []WorkstreamAgent {
		return []WorkstreamAgent{{ID: "ag-copy", Name: "Copywriter", Brief: "Writes launch copy."}}
	}
	o.task(t, "santosh", path, api.TaskCreate{Title: "Ship the build", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByChat, ChatID: "c1", Name: "Build"}, Group: g})
	copyTask := o.task(t, "santosh", path, api.TaskCreate{Title: "Write the tagline", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAgent, AgentID: "ag-copy", Name: "whatever the caller said"}, Group: g})
	if copyTask.DoneBy.Name != "Copywriter" {
		t.Fatalf("an agent doer keeps the list's name, not the caller's: %+v", copyTask.DoneBy)
	}
	var started api.WorkstreamTasks
	o.do(t, "abir", "POST", path+"/tasks/start_all", -1, api.TasksStartAll{Group: one.Group.ID}, 200, &started)
	if len(started.Tasks) != 4 || started.Tasks[0].State != factory.TaskRunning {
		t.Fatalf("start all started %+v", started.Tasks)
	}
	o.settle()
	if turn := asked["Write the tagline"]; !strings.HasPrefix(turn.Text, "You are doing this task as Copywriter.\nWhat Copywriter is for: Writes launch copy.") {
		t.Fatalf("the agent was asked %q", turn.Text)
	}
	if turn := asked["Ship the build"]; !strings.HasPrefix(turn.Text, "You are doing this task as Build.") {
		t.Fatalf("the chat's agent was asked %q", turn.Text)
	}
	for _, task := range o.tasks(t, "santosh", path) {
		want := factory.TaskDone
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

	// A reopen sends what waited on it back to waiting, and its run ends with it.
	sign := o.task(t, "santosh", path, api.TaskCreate{Title: "Sign the form", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "santosh"}})
	var signed api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+sign.ID, sign.Rev, map[string]any{"state": "done"}, 200, &signed)
	after := o.task(t, "santosh", path, api.TaskCreate{Title: "Hold after signing", DoneBy: afi, WaitingOn: []string{sign.ID}})
	o.do(t, "abir", "PATCH", path+"/tasks/"+after.ID, after.Rev, map[string]any{"state": "running"}, 200, &running)
	if !o.h.taskTurnRunning(running.WS, after.ID) {
		t.Fatal("the task started after signing has no run")
	}
	o.do(t, "abir", "PATCH", path+"/tasks/"+sign.ID, signed.Rev, map[string]any{"state": "up_next", "state_reason": "wrong form"}, 200, nil)
	// Polled rather than settled: a run nobody stopped would hold settle forever.
	for end := time.Now().Add(3 * time.Second); o.h.taskTurnRunning(running.WS, after.ID); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			close(release)
			t.Fatal("a reopen sent the task back to waiting and left Afi working on it")
		}
	}
	if got := o.one(t, path, after.ID); got.State == factory.TaskRunning {
		t.Fatalf("after the reopen %+v", got)
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

// afiAtWork serves the organization's Afi with a run that keeps working until
// it is stopped, for a test that needs a task Afi has really taken: since
// #2695 a person's "running" is refused unless Afi takes the task. Every run
// still going ends with the test.
func (o *wsOrg) afiAtWork(t *testing.T) {
	t.Helper()
	o.serverMode(t, func(ctx context.Context, _ AgentTurn) (AgentTurnEnd, error) {
		<-ctx.Done()
		return AgentTurnEnd{}, nil
	})
	t.Cleanup(func() {
		o.h.agentDesk.mu.Lock()
		for _, stop := range o.h.agentDesk.tasks {
			stop()
		}
		o.h.agentDesk.mu.Unlock()
		o.settle()
	})
}
