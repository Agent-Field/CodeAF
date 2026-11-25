package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// AN ANSWER NEVER MARKS RUNNING WHAT NOTHING CAN RUN (test findings
// 2026-11-23 P0 #2). Bob answered Afi's question on a server with no model
// key, and his Inbox showed "Afi is doing this · Working 14s" with nothing
// running until the lease cleared it minutes later. The answer is kept, and
// the task waits Up next with a line that says why.
func TestAnAnswerOnAServerThatCannotRunAfiLeavesTheTaskWaiting(t *testing.T) {
	o, path := taskOrg(t, "edit")
	ws := strings.TrimPrefix(path, "/v1/workstreams/")
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Venue shortlist", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	d := &agentDoor{h: o.h, id: ws, asker: "santosh"}
	if _, err := d.Work(context.Background(), "ws_task_update", json.RawMessage(`{"task":"`+task.ID+`","ask":{"title":"Which city?","options":[{"label":"Austin","recommended":true},{"label":"Denver"}]}}`)); err != nil {
		t.Fatal(err)
	}
	got := o.one(t, path, task.ID)
	if got.Ask == nil {
		t.Fatalf("no Ask on %+v", got)
	}
	o.do(t, "santosh", "POST", path+"/tasks/"+task.ID+"/answer", -1, api.TaskAnswer{Option: got.Ask.Options[0].ID}, 200, nil)
	after := o.one(t, path, task.ID)
	if after.State != factory.TaskUpNext {
		t.Fatalf("after the answer the task is %q, want up next: nothing here can run Afi", after.State)
	}
	if after.Ask != nil {
		t.Fatalf("the answer was not kept: %+v", after.Ask)
	}
	if !o.threadSays(t, path, task.ID, NoRunnerAnsweredLine) {
		t.Fatalf("the thread does not say why the task waits")
	}
}

// THE ANSWER KEPT IS CARRIED ON ONCE A KEY IS SET: the thread promised "Afi
// carries on once this organization has a model key", so setting the key
// starts Afi again on the parked task, told the answer it was given, as the
// person who answered.
func TestAnAnswerKeptWithoutAKeyIsCarriedOnWhenTheKeyIsSet(t *testing.T) {
	o, path := taskOrg(t, "edit")
	ws := strings.TrimPrefix(path, "/v1/workstreams/")
	started := make(chan AgentTurn, 2)
	o.h.config.WorkstreamAgent = func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		started <- turn
		<-ctx.Done()
		return AgentTurnEnd{}, nil
	}
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Venue shortlist", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	d := &agentDoor{h: o.h, id: ws, asker: "santosh"}
	if _, err := d.Work(context.Background(), "ws_task_update", json.RawMessage(`{"task":"`+task.ID+`","ask":{"title":"Which city?","options":[{"label":"Austin","recommended":true},{"label":"Denver"}]}}`)); err != nil {
		t.Fatal(err)
	}
	got := o.one(t, path, task.ID)
	o.do(t, "santosh", "POST", path+"/tasks/"+task.ID+"/answer", -1, api.TaskAnswer{Option: got.Ask.Options[0].ID}, 200, nil)
	if after := o.one(t, path, task.ID); after.State != factory.TaskUpNext {
		t.Fatalf("after the answer the task is %q, want up next", after.State)
	}
	select {
	case turn := <-started:
		t.Fatalf("Afi ran with no key: %+v", turn)
	default:
	}

	o.do(t, "owner", "PUT", "/v1/org/keys/openrouter", -1, api.OrgKeyPut{Key: testOrgKey}, 200, nil)
	select {
	case turn := <-started:
		if turn.Task == nil || turn.Asker != "santosh" || !strings.Contains(turn.Text, "answered your question “Which city?”: Austin") {
			t.Fatalf("the resumed run = asker %q, text %q", turn.Asker, turn.Text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setting the key did not carry on the parked answer")
	}
	if after := o.one(t, path, task.ID); after.State != factory.TaskRunning {
		t.Fatalf("after the key the task is %q, want running", after.State)
	}
	if !o.threadSays(t, path, task.ID, taskResumedWithKeyLine) {
		t.Fatal("the thread does not say Afi carried on")
	}
	o.h.stopTaskTurn(ws, task.ID)
	o.settle()

	// A second key change finds nothing parked: the task is not started twice.
	o.do(t, "owner", "PUT", "/v1/org/keys/openrouter", -1, api.OrgKeyPut{Key: testOrgKey}, 200, nil)
	select {
	case turn := <-started:
		t.Fatalf("a task already carried on was started again: %+v", turn.Task)
	case <-time.After(50 * time.Millisecond):
	}
}
