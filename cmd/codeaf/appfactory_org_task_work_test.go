package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A TASK'S WORKING CONVERSATION REACHES THE DESK. The engine says org_task_work
// in hello; a server task.work line is relayed as factory.org_workstream_task_work
// with its connection, workstream and task, and never as the private
// factory.org_workstream_agent the Afi pane folds into its own conversation.
func TestATasksWorkingConversationIsRelayed(t *testing.T) {
	o, wire, id, _ := orgRelay(t)
	if !strings.Contains(wire.hello, `"`+appwire.FeatureOrgTaskWork+`"`) {
		t.Fatalf("hello lacks %s: %s", appwire.FeatureOrgTaskWork, wire.hello)
	}
	work := make(chan appwire.FactoryOrgTaskWork, 4)
	pane := make(chan appwire.FactoryOrgWorkstreamAgent, 4)
	h := wire.backend.factories
	h.mu.Lock()
	h.orgTaskWork = func(e appwire.FactoryOrgTaskWork) { work <- e }
	h.orgAgent = func(e appwire.FactoryOrgWorkstreamAgent) { pane <- e }
	h.mu.Unlock()
	var teams appwire.OrgTeams
	wire.must("factory.teams", map[string]any{"connection": id}, &teams)
	nextOpening(t, o)
	o.frames <- "id: 5\nevent: task.work\ndata: {\"id\":\"ws_2\",\"task\":\"tk_1\",\"entry\":{\"seq\":2,\"at\":\"2026-11-17T10:00:00Z\",\"kind\":\"person\",\"by\":\"marco\",\"text\":\"use the shorter title\"}}\n\n"
	select {
	case e := <-work:
		if e.Connection != id || e.ID != "ws_2" || e.Task != "tk_1" || e.Entry == nil || e.Entry.By != "marco" || e.Entry.Text != "use the shorter title" {
			t.Fatalf("relayed %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no factory.org_workstream_task_work arrived")
	}
	select {
	case e := <-pane:
		t.Fatalf("a task's line reached the private pane: %+v", e)
	default:
	}
}
