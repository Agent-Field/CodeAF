package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// The Inbox's verbs over the wire (build spec v2, screens 1 and 2): an undone
// answer opens its Ask again and withdraws the decision it kept, Hand to
// someone else names a person in the workstream, and Take it back makes the
// owner the doer.
func TestAnUndoneAnswerWithdrawsItsDecisionOverTheWire(t *testing.T) {
	o, path := taskOrg(t, "edit")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Rename jobs to tasks in the SDK reference", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	ask := map[string]any{"ask": factory.TaskAsk{Title: "Rename “jobs” to “tasks”?", Reversible: true,
		Options: []factory.TaskAskOption{{ID: "rename", Label: "Rename", Recommended: true, Decides: "The SDK reference says tasks"}}}}
	var asked api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, made.Rev, ask, 200, &asked)
	if asked.Ask == nil || !asked.Ask.Reversible {
		t.Fatalf("the Ask lost reversible: %+v", asked.Ask)
	}
	var answered api.WorkstreamTask
	o.do(t, "santosh", "POST", path+"/tasks/"+made.ID+"/answer", -1, api.TaskAnswer{Option: "rename"}, 200, &answered)
	if len(answered.Undoable) != 1 {
		t.Fatalf("answered %+v", answered.Undoable)
	}
	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	if len(ws.Decisions) != 1 || ws.Decisions[0].State != factory.DecisionKept {
		t.Fatalf("decisions %+v", ws.Decisions)
	}
	o.do(t, "abir", "PATCH", path+"/tasks/"+made.ID, answered.Rev, map[string]any{"undo_answer": asked.Ask.ID}, 403, nil)
	var undone api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, answered.Rev, map[string]any{"undo_answer": asked.Ask.ID}, 200, &undone)
	if undone.State != factory.TaskNeeds || undone.Ask == nil || undone.Ask.ID != asked.Ask.ID {
		t.Fatalf("undone %s %+v", undone.State, undone.Ask)
	}
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	if len(ws.Decisions) != 1 || ws.Decisions[0].State != factory.DecisionWithdrawn {
		t.Fatalf("the decision stayed: %+v", ws.Decisions)
	}
}

func TestHandToSomeoneElseOverTheWireNamesSomeoneInTheWorkstream(t *testing.T) {
	o, path := taskOrg(t, "edit")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Pick the launch screenshots", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	var asked api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"ask": factory.TaskAsk{Title: "Which three screenshots?", Options: []factory.TaskAskOption{{ID: "a", Label: "These"}}}}, 200, &asked)
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, asked.Rev, map[string]any{"ask_hand": factory.TaskAskHand{Ask: asked.Ask.ID, To: factory.PersonRef{ID: "mia"}}}, 422, nil)
	var handed api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, asked.Rev, map[string]any{"ask_hand": factory.TaskAskHand{Ask: asked.Ask.ID, To: factory.PersonRef{ID: "abir"}}}, 200, &handed)
	if handed.Ask != nil || len(handed.Asks) != 1 || handed.Asks[0].Decider.ID != "abir" {
		t.Fatalf("handed %+v %+v", handed.Ask, handed.Asks)
	}
	o.do(t, "abir", "POST", path+"/tasks/"+made.ID+"/answer", -1, api.TaskAnswer{Ask: asked.Ask.ID, Option: "a"}, 200, nil)
}

func TestTakeItBackOverTheWire(t *testing.T) {
	o, path := taskOrg(t, "edit")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Board update draft", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	o.do(t, "abir", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"take_back": true}, 403, nil)
	var back api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"take_back": true}, 200, &back)
	if back.DoneBy.Kind != factory.TaskByPerson || back.DoneBy.ID != "santosh" || back.State != factory.TaskUpNext {
		t.Fatalf("taken back %+v %s", back.DoneBy, back.State)
	}
}
