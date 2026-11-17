package serve

import (
	"bytes"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"testing"
)

func TestFoldMovesWorkAndUndoRestoresBothWorkstreams(t *testing.T) {
	o := newWSOrg(t)
	a := o.create(t, "santosh", api.WorkstreamNew{Name: "Research", Pages: []api.WorkstreamPage{{ID: "notes", Title: "Notes", Kind: "page", BodyMD: "Keep this"}}, Decisions: []api.WorkstreamDecision{{Text: "Ship weekly", State: "kept"}}})
	b := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch"})
	task := o.task(t, "santosh", "/v1/workstreams/"+a.ID, api.TaskCreate{Title: "Read notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "Santosh"}})
	var files api.WorkstreamFiles
	o.do(t, "santosh", "GET", "/v1/workstreams/"+a.ID+"/files", -1, nil, 200, &files)
	var got api.Workstream
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+a.ID, a.Rev, map[string]any{"fold_into": b.ID}, 200, &got)
	movedTasks := o.tasks(t, "santosh", "/v1/workstreams/"+b.ID)
	if len(movedTasks) != 1 || movedTasks[0].ID != task.ID || movedTasks[0].WS != b.ID {
		t.Fatalf("moved tasks = %+v", movedTasks)
	}
	if got.ID != b.ID || len(got.Decisions) != 1 {
		t.Fatalf("fold answer = %+v", got)
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+a.ID, -1, nil, 404, nil)
	var moved api.WorkstreamFiles
	o.do(t, "santosh", "GET", "/v1/workstreams/"+b.ID+"/files", -1, nil, 200, &moved)
	if len(moved.Files) < len(files.Files) {
		t.Fatalf("files did not move: %+v", moved)
	}
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+b.ID, got.Rev, map[string]any{"unfold": a.ID}, 200, &got)
	o.do(t, "santosh", "GET", "/v1/workstreams/"+a.ID, -1, nil, 200, &got)
	if got.Name != "Research" {
		t.Fatalf("undo = %+v", got)
	}
}

func TestImportBringsUploadWithItsTaskLinks(t *testing.T) {
	o := newWSOrg(t)
	pkg := map[string]any{
		"from_local_id": "local-chat", "name": "Launch",
		"files": []any{map[string]any{"file": map[string]any{"id": "fl_input", "kind": "upload", "name": "input.txt", "made_by": map[string]any{"kind": "person", "name": "You"}, "from_task": "t1", "used_by": []string{"t1"}}, "data": "QQ=="}},
		"tasks": []any{map[string]any{"id": "t1", "title": "Read input", "owner": map[string]any{"kind": "person", "id": "you", "name": "You"}, "done_by": map[string]any{"kind": "person", "id": "you", "name": "You"}, "came_from": map[string]any{"kind": "person", "id": "you", "name": "You"}, "state": "up_next", "waiting_on": []string{}, "results": []any{}}},
	}
	var ws api.Workstream
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, pkg, 201, &ws)
	var files api.WorkstreamFiles
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID+"/files", -1, nil, 200, &files)
	f := fileNamed(t, files.Files, "input.txt")
	if f.FromTask != "t1" || len(f.UsedBy) != 1 || f.UsedBy[0] != "t1" || f.Upload.Size != 1 {
		t.Fatalf("imported links = %+v", f)
	}
	data, err := o.st.Blob(f.Upload.Blob)
	if err != nil || string(data) != "A" {
		t.Fatalf("bytes = %q, %v", data, err)
	}
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, pkg, 200, &ws)
	pkg["to"] = ws.ID
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, pkg, 200, &ws)
}

func TestFoldUndoNeverOverwritesLaterWork(t *testing.T) {
	o := newWSOrg(t)
	a := o.create(t, "santosh", api.WorkstreamNew{Name: "Research"})
	b := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch"})
	var got api.Workstream
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+a.ID, a.Rev, map[string]any{"fold_into": b.ID}, 200, &got)
	name := "Launch edited"
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+b.ID, got.Rev, api.WorkstreamPatch{Name: &name}, 200, &got)
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+b.ID, got.Rev, map[string]any{"unfold": a.ID}, 409, nil)
	o.do(t, "santosh", "GET", "/v1/workstreams/"+b.ID, -1, nil, 200, &got)
	if got.Name != name {
		t.Fatalf("Undo replaced later work: %+v", got)
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+a.ID, -1, nil, 404, nil)
}

func TestImportBringsAnUploadLargerThanTheOrdinaryJSONBody(t *testing.T) {
	o := newWSOrg(t)
	data := bytes.Repeat([]byte("A"), 2<<20)
	pkg := map[string]any{"from_local_id": "large", "name": "Files", "files": []any{map[string]any{"file": map[string]any{"id": "large-input", "kind": "upload", "name": "large.txt", "from_chat": "chat1", "made_by": map[string]any{"kind": "person", "name": "You"}}, "data": data}}}
	var ws api.Workstream
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, pkg, 201, &ws)
	var files api.WorkstreamFiles
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID+"/files", -1, nil, 200, &files)
	file := fileNamed(t, files.Files, "large.txt")
	if file.Upload.Size != int64(len(data)) || file.FromChat != "chat1" {
		t.Fatalf("imported file = %+v", file)
	}
}

func TestFoldKeepsTheDestinationPlanAndUndoReturnsTheSourcePlan(t *testing.T) {
	o := newWSOrg(t)
	a := o.create(t, "santosh", api.WorkstreamNew{Name: "Research"})
	b := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch"})
	for id, name := range map[string]string{a.ID: "Research plan", b.ID: "Launch plan"} {
		o.do(t, "santosh", "PUT", "/v1/workstreams/"+id+"/plan", 0, map[string]any{"plan": map[string]any{"name": name}}, 200, nil)
	}
	var folded api.Workstream
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+a.ID, a.Rev, map[string]any{"fold_into": b.ID}, 200, &folded)
	var dest map[string]any
	o.do(t, "santosh", "GET", "/v1/workstreams/"+b.ID+"/plan", -1, nil, 200, &dest)
	if dest["plan"].(map[string]any)["name"] != "Launch plan" {
		t.Fatalf("destination plan = %+v", dest)
	}
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+b.ID, folded.Rev, map[string]any{"unfold": a.ID}, 200, nil)
	var source map[string]any
	o.do(t, "santosh", "GET", "/v1/workstreams/"+a.ID+"/plan", -1, nil, 200, &source)
	if source["plan"].(map[string]any)["name"] != "Research plan" {
		t.Fatalf("restored source plan = %+v", source)
	}
}
