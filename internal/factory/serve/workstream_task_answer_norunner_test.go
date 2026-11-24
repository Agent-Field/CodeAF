package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
