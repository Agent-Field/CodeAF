package serve

import (
	"bufio"
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTaskDeleteAndUndoRestoreTheWaits(t *testing.T) {
	o, path := taskOrg(t, "edit")
	a := o.task(t, "santosh", path, api.TaskCreate{Title: "Research", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh"}})
	b := o.task(t, "santosh", path, api.TaskCreate{Title: "Write", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, WaitingOn: []string{a.ID}})
	o.do(t, "abir", "DELETE", path+"/tasks/"+a.ID, a.Rev, nil, 403, nil)
	o.do(t, "santosh", "DELETE", path+"/tasks/"+a.ID, a.Rev, nil, 200, nil)
	if all := o.tasks(t, "santosh", path); len(all) != 1 || len(all[0].WaitingOn) != 0 || all[0].Starts != factory.TaskStartsByHand {
		t.Fatalf("after delete: %+v", all)
	}
	o.do(t, "santosh", "GET", path+"/tasks/"+a.ID, -1, nil, 404, nil)
	o.do(t, "abir", "POST", path+"/tasks/"+a.ID+"/restore", -1, nil, 403, nil)
	o.do(t, "santosh", "POST", path+"/tasks/"+a.ID+"/restore", -1, nil, 200, nil)
	var got api.WorkstreamTask
	o.do(t, "santosh", "GET", path+"/tasks/"+b.ID, -1, nil, 200, &got)
	if len(got.WaitingOn) != 1 || got.WaitingOn[0] != a.ID || got.Starts != "" {
		t.Fatalf("Undo waits: %+v", got)
	}
}

func TestDeletingATaskKeepsItsFilesAndRemovesItsInboxQuestion(t *testing.T) {
	o, path := taskOrg(t, "edit")
	a := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	var file api.WorkstreamFile
	o.do(t, "santosh", "POST", path+"/files", -1, api.FileNew{Kind: factory.FileDocument, Name: "Release draft", BodyMD: "Keep these words", FromTask: a.ID}, 201, &file)
	var asked api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+a.ID, a.Rev, map[string]any{"ask": factory.TaskAsk{Title: "Publish?", Options: []factory.TaskAskOption{{ID: "yes", Label: "Publish"}}}}, 200, &asked)
	o.do(t, "santosh", "DELETE", path+"/tasks/"+a.ID, asked.Rev, nil, 200, nil)
	var files api.WorkstreamFiles
	o.do(t, "santosh", "GET", path+"/files", -1, nil, 200, &files)
	if !slices.ContainsFunc(files.Files, func(f factory.WorkstreamFile) bool { return f.ID == file.ID && f.FromTask == a.ID }) {
		t.Fatalf("task's file vanished: %+v", files)
	}
	var home struct {
		Tasks []factory.WorkstreamTask `json:"tasks"`
	}
	o.do(t, "santosh", "GET", "/v1/work", -1, nil, 200, &home)
	if slices.ContainsFunc(home.Tasks, func(t factory.WorkstreamTask) bool { return t.ID == a.ID }) {
		t.Fatal("deleted Ask remains in the Inbox's task source")
	}
	o.now = o.now.Add(31 * time.Second)
	o.do(t, "santosh", "POST", path+"/tasks/"+a.ID+"/restore", -1, nil, 409, nil)
}

func TestTaskDeleteNeedsAStopAndOwnerRights(t *testing.T) {
	o, path := taskOrg(t, "edit")
	a := o.task(t, "santosh", path, api.TaskCreate{Title: "Work", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "abir"}, Owner: &factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: "abir", Name: "Abir"}})
	var cur api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+a.ID, a.Rev, map[string]any{"state": "running"}, 200, &cur)
	var refused struct {
		Error string `json:"error"`
	}
	o.do(t, "abir", "DELETE", path+"/tasks/"+a.ID, cur.Rev, nil, 403, &refused)
	if refused.Error != "Stop this task before deleting it" {
		t.Fatalf("running refusal: %q", refused.Error)
	}
	o.do(t, "abir", "PATCH", path+"/tasks/"+a.ID, cur.Rev, map[string]any{"state": "stopped"}, 200, &cur)
	o.do(t, "santosh", "DELETE", path+"/tasks/"+a.ID, a.Rev, nil, 412, nil)
	o.do(t, "santosh", "DELETE", path+"/tasks/"+a.ID, cur.Rev, nil, 200, nil)
	o.do(t, "abir", "POST", path+"/tasks/"+a.ID+"/restore", -1, nil, 200, nil)
	o.do(t, "abir", "DELETE", path+"/tasks/"+a.ID, cur.Rev+1, nil, 200, nil)
}

func TestTaskUndoPreservesNewerWaitEdits(t *testing.T) {
	o, path := taskOrg(t, "edit")
	a := o.task(t, "santosh", path, api.TaskCreate{Title: "A", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh"}})
	b := o.task(t, "santosh", path, api.TaskCreate{Title: "B", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh"}, WaitingOn: []string{a.ID}})
	c := o.task(t, "santosh", path, api.TaskCreate{Title: "C", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh"}})
	o.do(t, "santosh", "DELETE", path+"/tasks/"+a.ID, a.Rev, nil, 200, nil)
	o.do(t, "santosh", "PATCH", path+"/tasks/"+b.ID, b.Rev+1, map[string]any{"waiting_on": []string{c.ID}}, 200, nil)
	o.do(t, "santosh", "POST", path+"/tasks/"+a.ID+"/restore", -1, nil, 200, nil)
	var got api.WorkstreamTask
	o.do(t, "santosh", "GET", path+"/tasks/"+b.ID, -1, nil, 200, &got)
	if !slices.Equal(got.WaitingOn, []string{c.ID}) {
		t.Fatalf("Undo overwrote the new wait: %+v", got)
	}
}

func TestTaskDeleteNotifiesTheRequestingWorkstream(t *testing.T) {
	o := newWSOrg(t)
	dest := o.create(t, "abir", api.WorkstreamNew{Name: "Engineering"})
	source := o.create(t, "santosh", api.WorkstreamNew{Name: "Marketing"})
	var sent api.RequestSent
	o.do(t, "santosh", "POST", "/v1/workstreams/"+dest.ID+"/requests", -1, factory.TaskRequest{Title: "Build release", FromWS: source.ID}, 201, &sent)
	task := o.tasks(t, "abir", "/v1/workstreams/"+dest.ID)[0]
	srv := httptest.NewServer(o.h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+o.tokens["santosh"])
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("events status: %d", res.StatusCode)
	}
	o.do(t, "abir", "DELETE", "/v1/workstreams/"+dest.ID+"/tasks/"+task.ID, task.Rev, nil, 200, nil)
	scan := bufio.NewScanner(res.Body)
	for scan.Scan() {
		if strings.Contains(scan.Text(), source.ID) {
			return
		}
	}
	t.Fatalf("the requesting workstream did not hear the deletion: %v", scan.Err())
}
