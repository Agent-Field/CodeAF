package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// CHANGE WHO DOES IT (org-space DESIGN §4.5, L1 acceptance 4). santosh owns
// the workstream and abir can edit it. The owner hands a task from Afi to a
// person in the workstream and the thread names both; someone outside it, an
// agent the workstream has not added, a running task and a Factory task are
// each refused, and abir, who neither owns the task nor looks after the
// workstream, cannot change it.
func TestChangeWhoDoesItHandsTheTaskOnAndSaysSo(t *testing.T) {
	o, path := taskOrg(t, "edit")
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Run five interviews", DoneBy: afi})

	abir := factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "abir", Name: "abir"}
	o.do(t, "abir", "PATCH", path+"/tasks/"+task.ID, task.Rev, map[string]any{"done_by": abir}, 403, nil)
	stranger := factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "mallory", Name: "Mallory"}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, task.Rev, map[string]any{"done_by": stranger}, 422, nil)
	agent := factory.TaskDoneBy{Kind: factory.TaskByAgent, AgentID: "ag-copy", Name: "Copywriter"}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, task.Rev, map[string]any{"done_by": agent}, 422, nil)

	var handed api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, task.Rev, map[string]any{"done_by": abir}, 200, &handed)
	if handed.DoneBy.Kind != factory.TaskByPerson || handed.DoneBy.ID != "abir" || handed.Owner.ID != "santosh" {
		t.Fatalf("handed %+v (owner and doer are changed separately)", handed)
	}
	if !o.threadSays(t, path, task.ID, "from Afi to abir") {
		t.Fatal("the thread does not say who it was handed from and to")
	}

	// Once the workstream has the agent, it can be picked; its name is the list's.
	o.h.config.WorkstreamAgents = func(factory.OrgWorkstream) []WorkstreamAgent {
		return []WorkstreamAgent{{ID: "ag-copy", Name: "Copywriter", Crew: true}}
	}
	var toAgent api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, handed.Rev, map[string]any{"done_by": factory.TaskDoneBy{Kind: factory.TaskByAgent, AgentID: "ag-copy", Name: "x"}}, 200, &toAgent)
	if toAgent.DoneBy.Name != "Copywriter" || !toAgent.DoneBy.Crew {
		t.Fatalf("the agent doer is %+v", toAgent.DoneBy)
	}

	// A running task is stopped first.
	o.afiAtWork(t)
	run := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft the notes", DoneBy: afi})
	var running api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+run.ID, run.Rev, map[string]any{"state": "running"}, 200, &running)
	o.do(t, "santosh", "PATCH", path+"/tasks/"+run.ID, running.Rev, map[string]any{"done_by": abir}, 403, nil)
}
