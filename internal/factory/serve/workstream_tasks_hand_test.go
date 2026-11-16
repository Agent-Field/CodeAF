package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A PERSON WHO LEAVES HANDS THEIR OPEN WORK ON: abir's unfinished tasks go to
// the fallback (mia), with "Handed to mia" in each thread; done work keeps
// who did it, and santosh's own tasks are untouched.
func TestAPersonWhoLeavesHandsTheirOpenTasksToTheFallback(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	path := "/v1/workstreams/" + ws.ID
	in := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}, {Kind: "person", ID: "mia", Role: "edit"}}
	var shared api.WorkstreamShared
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &in}, 200, &shared)
	rules := shared.Workstream.Rules
	rules.Fallback = &factory.RulesPerson{ID: "mia"}
	o.do(t, "santosh", "PATCH", path, shared.Workstream.Rev, api.WorkstreamPatch{Rules: &rules}, 200, nil)

	open := o.task(t, "abir", path, api.TaskCreate{Title: "Draft the notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	tick := o.task(t, "santosh", path, api.TaskCreate{Title: "Abir approves the layout", Owner: &factory.TaskOwner{Kind: "person", ID: "abir", Name: "abir"},
		DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "abir", Name: "abir"}})
	done := o.task(t, "abir", path, api.TaskCreate{Title: "Old work", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	o.do(t, "abir", "PATCH", path+"/tasks/"+done.ID, done.Rev, map[string]any{"state": "done"}, 200, nil)
	mine := o.task(t, "santosh", path, api.TaskCreate{Title: "Mine", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})

	out := []factory.Share{{Kind: "person", ID: "mia", Role: "edit"}}
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &out}, 200, nil)

	by := map[string]api.WorkstreamTask{}
	for _, x := range o.tasks(t, "santosh", path) {
		by[x.ID] = x
	}
	if got := by[open.ID]; got.Owner.ID != "mia" || got.Rev != open.Rev+1 {
		t.Fatalf("abir's open task = %+v", got.Owner)
	}
	if got := by[tick.ID]; got.Owner.ID != "mia" || got.DoneBy.ID != "mia" {
		t.Fatalf("abir's person task = owner %+v done by %+v", got.Owner, got.DoneBy)
	}
	if got := by[done.ID]; got.Owner.ID != "abir" {
		t.Fatalf("done work changed hands: %+v", got.Owner)
	}
	if got := by[mine.ID]; got.Owner.ID != "santosh" || got.Rev != mine.Rev {
		t.Fatalf("santosh's task changed: %+v", got)
	}
	var events api.WorkstreamTaskEvents
	o.do(t, "santosh", "GET", path+"/tasks/"+open.ID+"/events", -1, nil, 200, &events)
	if n := len(events.Events); n == 0 || events.Events[n-1].Kind != factory.TaskEventHanded || events.Events[n-1].Text != "Handed to mia" {
		t.Fatalf("the thread does not say who took it: %+v", events.Events)
	}
}

// A FILE CHANGED UNDER A RUNNING TASK STOPS IT ON A QUESTION TO ITS OWNER: the
// task that read the brief asks whether to carry on; one up next that also
// read it is left alone, and a change that is only a rename flags nothing.
func TestChangingAFileARunningTaskReadAsksItsOwnerWhetherToCarryOn(t *testing.T) {
	o, path := taskOrg(t, "edit")
	running := o.task(t, "abir", path, api.TaskCreate{Title: "Build the meal grid", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, State: factory.TaskRunning})
	waiting := o.task(t, "abir", path, api.TaskCreate{Title: "Later", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	var doc api.WorkstreamFile
	o.do(t, "santosh", "POST", path+"/files", -1, api.FileNew{Kind: "document", Name: "Layout", BodyMD: "Two columns"}, 201, &doc)
	used := []string{running.ID, waiting.ID}
	o.do(t, "abir", "PATCH", path+"/files/"+doc.ID, doc.Rev, api.FilePatch{UsedBy: &used}, 200, &doc)

	name := "Layout v2"
	o.do(t, "santosh", "PATCH", path+"/files/"+doc.ID, doc.Rev, api.FilePatch{Name: &name}, 200, &doc)
	for _, x := range o.tasks(t, "santosh", path) {
		if x.ID == running.ID && x.State != factory.TaskRunning {
			t.Fatalf("a rename flagged the task: %+v", x)
		}
	}

	o.do(t, "santosh", "PATCH", path+"/files/"+doc.ID, doc.Rev, api.FilePatch{Name: &name, Edited: "person"}, 200, &doc)
	by := map[string]api.WorkstreamTask{}
	for _, x := range o.tasks(t, "santosh", path) {
		by[x.ID] = x
	}
	got := by[running.ID]
	if got.State != factory.TaskNeeds || got.Ask == nil || got.Ask.Title != "Layout v2 changed while this ran. Carry on?" {
		t.Fatalf("the running task = %s %+v", got.State, got.Ask)
	}
	if by[waiting.ID].State != factory.TaskUpNext {
		t.Fatalf("a task not running was flagged: %+v", by[waiting.ID])
	}
	// Only its owner answers; either answer carries on.
	o.do(t, "santosh", "POST", path+"/tasks/"+running.ID+"/answer", -1, api.TaskAnswer{Option: "carry_on"}, 403, nil)
	var answered api.WorkstreamTask
	o.do(t, "abir", "POST", path+"/tasks/"+running.ID+"/answer", -1, api.TaskAnswer{Option: "carry_on"}, 200, &answered)
	if answered.State != factory.TaskRunning {
		t.Fatalf("answered = %+v", answered)
	}
}
