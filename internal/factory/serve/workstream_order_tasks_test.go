package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A STANDING ORDER'S RUN, MADE A TASK BY THE DESK OF THE PERSON WHO HOLDS IT,
// IS EVERYONE'S TASK: santosh's order adds a running task named for its run;
// the workstream's owner of incoming work (abir) owns it, sees it in the list
// with where it came from, answers its Ask, and santosh reads which answer was
// chosen so his clock can act on it.
func TestAStandingOrdersRunIsATaskTheOwnerAnswers(t *testing.T) {
	o, path := taskOrg(t, "edit")
	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	rules := ws.Rules
	rules.DefaultOwner = factory.RulesOwner{Kind: "person", ID: "abir", Name: "abir"}
	rules.KeepsRunning.Orders = []string{"a1b2"}
	o.do(t, "santosh", "PATCH", path, ws.Rev, api.WorkstreamPatch{Rules: &rules}, 200, nil)

	came := factory.TaskCameFrom{Kind: factory.TaskByOrder, OrderID: "a1b2", Name: "Every Monday at 09:00", Run: 17}
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft the release notes", Request: "Draft the release notes from last week's done tasks.",
		DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, CameFrom: &came,
		Group: &factory.TaskGroup{ID: "order:a1b2", Name: "Every Monday at 09:00", Kind: "order"}})
	if made.Owner.ID != "abir" || made.CameFrom != came || made.Group == nil || made.Group.Kind != "order" {
		t.Fatalf("the run's task = %+v", made)
	}
	ask := factory.TaskAsk{ID: "order:17", Title: "Run Draft the release notes now?", Detail: "It asks before each run.",
		Options: []factory.TaskAskOption{{ID: "go", Label: "Go", Recommended: true}, {ID: "skip", Label: "Skip this one"}, {ID: "not_right", Label: "Not right"}}}
	needs := factory.TaskNeeds
	var asked api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"state": needs, "ask": ask}, 200, &asked)

	var seen api.WorkstreamTask
	for _, x := range o.tasks(t, "abir", path) {
		if x.ID == made.ID {
			seen = x
		}
	}
	if seen.CameFrom.Run != 17 || seen.State != factory.TaskNeeds || seen.Ask == nil || seen.Ask.ID != "order:17" {
		t.Fatalf("abir sees %+v", seen)
	}
	// Only the owner answers: santosh, who holds the order, cannot.
	o.do(t, "santosh", "POST", path+"/tasks/"+made.ID+"/answer", -1, map[string]any{"option": "go"}, 403, nil)
	o.do(t, "abir", "POST", path+"/tasks/"+made.ID+"/answer", -1, map[string]any{"option": "go"}, 200, nil)

	var events api.WorkstreamTaskEvents
	o.do(t, "santosh", "GET", path+"/tasks/"+made.ID+"/events", -1, nil, 200, &events)
	// The answer is the thread's last answered line; a server with no model
	// key adds why the task waits after it.
	var last api.WorkstreamTaskEvent
	for _, e := range events.Events {
		if e.Kind == factory.TaskEventAnswered {
			last = e
		}
	}
	if last.Kind != factory.TaskEventAnswered || last.Option != "go" || last.By.Name == "" {
		t.Fatalf("the answer santosh reads = %+v", last)
	}
}
