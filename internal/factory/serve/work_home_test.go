package serve

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"testing"
)

func TestWorkHomeContainsOnlyMembershipEvenForOperator(t *testing.T) {
	o := newWSOrg(t)
	mine := o.create(t, "owner", api.WorkstreamNew{Name: "AgentField"})
	other := o.create(t, "mia", api.WorkstreamNew{Name: "Private work"})
	o.task(t, "owner", "/v1/workstreams/"+mine.ID, api.TaskCreate{Title: "Release", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	o.task(t, "mia", "/v1/workstreams/"+other.ID, api.TaskCreate{Title: "Not mine", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	var got struct {
		Workstreams []api.WorkstreamSummary  `json:"workstreams"`
		Tasks       []factory.WorkstreamTask `json:"tasks"`
	}
	o.do(t, "owner", "GET", "/v1/work", -1, nil, 200, &got)
	if len(got.Workstreams) != 1 || got.Workstreams[0].ID != mine.ID || len(got.Tasks) != 1 || got.Tasks[0].Title != "Release" {
		t.Fatalf("home leaked other work: %+v", got)
	}
}

func TestOverviewSharesAskAnswerAndPlanCountsWithTasks(t *testing.T) {
	o, path := taskOrg(t, "edit")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Release notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, Group: &factory.TaskGroup{ID: "release", Name: "Release 0.9", Kind: "batch"}})
	var got struct {
		Tasks     []factory.WorkstreamTask `json:"tasks"`
		PlanCount int                      `json:"plan_count"`
	}
	o.do(t, "santosh", "GET", path+"/overview", -1, nil, 200, &got)
	if got.PlanCount != 1 || len(got.Tasks) != 1 {
		t.Fatalf("plan: %+v", got)
	}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"ask": factory.TaskAsk{Title: "Publish?", Options: []factory.TaskAskOption{{ID: "yes", Label: "Publish notes"}}}}, 200, nil)
	o.do(t, "santosh", "GET", path+"/overview", -1, nil, 200, &got)
	if got.Tasks[0].Ask == nil || got.PlanCount != 0 {
		t.Fatalf("ask: %+v", got)
	}
	o.do(t, "santosh", "POST", path+"/tasks/"+made.ID+"/answer", -1, api.TaskAnswer{Option: "yes"}, 200, nil)
	got.Tasks = nil
	o.do(t, "santosh", "GET", path+"/overview", -1, nil, 200, &got)
	if got.Tasks[0].Ask != nil {
		t.Fatal("answered Ask remains on Overview")
	}
	o.do(t, "mia", "GET", path+"/overview", -1, nil, 404, nil)
}

func TestOverviewRequestShowsOnlyTheSentTaskAcrossMembership(t *testing.T) {
	o := newWSOrg(t)
	source := o.create(t, "santosh", api.WorkstreamNew{Name: "Marketing"})
	dest := o.create(t, "mia", api.WorkstreamNew{Name: "AgentField"})
	o.task(t, "mia", "/v1/workstreams/"+dest.ID, api.TaskCreate{Title: "Do not disclose", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	o.do(t, "santosh", "POST", "/v1/workstreams/"+dest.ID+"/requests", -1, factory.TaskRequest{Title: "Build for screenshots", FromWS: source.ID}, 201, nil)
	var got api.WorkOverview
	o.do(t, "santosh", "GET", "/v1/workstreams/"+source.ID+"/overview", -1, nil, 200, &got)
	if len(got.AskedOf) != 1 || got.AskedOf[0].Title != "Build for screenshots" || len(got.Tasks) != 0 {
		t.Fatalf("request leaked or missing: %+v", got)
	}
}

func TestPlanStartLeavesUntickedTasksUpNext(t *testing.T) {
	o, path := taskOrg(t, "edit")
	group := &factory.TaskGroup{ID: "release", Name: "Release 0.9", Kind: "batch"}
	ticked := o.task(t, "santosh", path, api.TaskCreate{Title: "Write notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, Group: group})
	unticked := o.task(t, "santosh", path, api.TaskCreate{Title: "Write docs", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, Group: group})
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		_, err := turn.Door.(workDoor).Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done"}`))
		return AgentTurnEnd{}, err
	})
	var started api.WorkstreamTasks
	o.do(t, "santosh", "POST", path+"/tasks/start_all", -1, api.TasksStartAll{Group: group.ID, IDs: []string{ticked.ID}}, 200, &started)
	if len(started.Tasks) != 1 || started.Tasks[0].ID != ticked.ID {
		t.Fatalf("started unticked work: %+v", started)
	}
	o.settle()
	if got := o.one(t, path, unticked.ID); got.State != factory.TaskUpNext {
		t.Fatalf("unticked task became %s", got.State)
	}
}

func TestOverviewEmptyCollectionsAreArrays(t *testing.T) {
	o, path := taskOrg(t, "edit")
	var got map[string]json.RawMessage
	o.do(t, "santosh", "GET", path+"/overview", -1, nil, 200, &got)
	for _, key := range []string{"tasks", "children", "people", "decisions", "files", "asked_of", "standing_orders"} {
		if len(got[key]) == 0 || got[key][0] != '[' {
			t.Fatalf("%s must be an array, got %s", key, got[key])
		}
	}
}

func TestOverviewShowsFilesBeforeFilesTabIsOpened(t *testing.T) {
	o, path := taskOrg(t, "edit")
	var got api.WorkOverview
	o.do(t, "santosh", "GET", path+"/overview", -1, nil, 200, &got)
	if len(got.Files) == 0 || got.Files[0].Name != "Overview" {
		t.Fatalf("the workstream's existing page is missing from Files: %+v", got.Files)
	}
}
