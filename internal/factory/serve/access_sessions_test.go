package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func keepContractEntry(t *testing.T, path string, e api.AgentEntry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func restartAccessOrg(t *testing.T, o *wsOrg) {
	t.Helper()
	o.h.Close()
	o.h = New(o.seam, o.cfg)
	t.Cleanup(o.h.Close)
}

func TestContractJ1SessionLifecycle(t *testing.T) {
	o, path := taskOrg(t, "edit")
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Research", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	ws := task.WS
	afiID := "afi:" + ws + ":santosh"
	taskID := "task:" + ws + ":" + task.ID
	keepContractEntry(t, filepath.Join(o.h.agentDir(agentSeat{ws: ws, person: "santosh"}), "entries.jsonl"), api.AgentEntry{Seq: 1, Text: "Afi's first message"})
	keepContractEntry(t, o.h.taskWorkFile(taskRunKey{ws, task.ID}), api.AgentEntry{Seq: 1, Text: "Task's first message"})
	restartAccessOrg(t, o)
	for _, want := range []struct{ id, visibility string }{{afiID, acl.Private}, {taskID, acl.Owner}} {
		row, found, err := o.st.Resource("session", want.id)
		if err != nil || !found || row.Parent.ID != ws || row.Parent.Kind != "workstream" ||
			row.Owner.Kind != "person" || row.Owner.ID != "santosh" || row.Visibility != want.visibility {
			t.Fatalf("session %s = %+v, found %v, err %v", want.id, row, found, err)
		}
		if want.id == afiID {
			if row.CreatedBy.Kind != "person" || row.CreatedBy.ID != "santosh" {
				t.Fatalf("Afi's creator = %+v", row.CreatedBy)
			}
			row.Visibility = acl.Owner
			if err := o.st.PutResource(row); err != nil {
				t.Fatal(err)
			}
		}
	}
	restartAccessOrg(t, o)
	row, found, err := o.st.Resource("session", afiID)
	if err != nil || !found || row.Visibility != acl.Owner {
		t.Fatalf("backfill changed the existing row: %+v, %v", row, err)
	}
	o.do(t, "abir", "GET", path+"/agent", -1, nil, 200, nil)
	o.do(t, "abir", "GET", "/v1/resources/session/afi:"+ws+":abir", -1, nil, 200, nil)
	first := o.task(t, "santosh", path, api.TaskCreate{Title: "Write", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	o.h.addTaskWorkEntry(taskRunKey{ws, first.ID}, api.AgentEntry{Text: "Working now"})
	o.do(t, "abir", "GET", "/v1/resources/session/task:"+ws+":"+first.ID, -1, nil, 200, nil)

	o.do(t, "santosh", "DELETE", path+"/tasks/"+task.ID, task.Rev, nil, 200, nil)
	restartAccessOrg(t, o)
	o.do(t, "santosh", "GET", "/v1/resources/session/"+taskID, -1, nil, 404, nil)
	o.do(t, "owner", "DELETE", path+"?purge=1", -1, nil, 204, nil)
	rows, err := o.st.Resources("session")
	if err != nil || len(rows) != 0 {
		t.Fatalf("after workstream removal: %+v, %v", rows, err)
	}
}

func TestContractJ2RegistrationPreservesLegacyRoutes(t *testing.T) {
	o, path := taskOrg(t, "edit")
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Research", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	keepContractEntry(t, o.h.taskWorkFile(taskRunKey{task.WS, task.ID}), api.AgentEntry{Seq: 1, Text: "Task's line"})
	for _, who := range []string{"santosh", "abir", "owner"} {
		keepContractEntry(t, filepath.Join(o.h.agentDir(agentSeat{ws: task.WS, person: who}), "entries.jsonl"), api.AgentEntry{Seq: 1, By: who, Text: "Personal line"})
	}
	paths := []string{path + "/agent", path + "/tasks/" + task.ID, path + "/tasks/" + task.ID + "/work"}
	people := []string{"santosh", "abir", "owner"}
	before := map[[2]string]string{}
	for _, who := range people {
		if _, found, err := o.st.Resource("session", "afi:"+task.WS+":"+who); err != nil || found {
			t.Fatalf("Afi was already registered for %s: %v", who, err)
		}
		for _, endpoint := range paths {
			r := o.raw(t, who, "GET", endpoint, nil)
			if r.Code != 200 {
				t.Fatalf("%s %s: %d %s", who, endpoint, r.Code, r.Body)
			}
			before[[2]string{who, endpoint}] = r.Body.String()
		}
	}
	restartAccessOrg(t, o)
	for _, who := range people {
		for _, endpoint := range paths {
			r := o.raw(t, who, "GET", endpoint, nil)
			if r.Code != 200 || r.Body.String() != before[[2]string{who, endpoint}] {
				t.Fatalf("%s %s changed: %d %s", who, endpoint, r.Code, r.Body)
			}
		}
	}
	o.do(t, "owner", "GET", "/v1/resources/session/afi:"+task.WS+":santosh", -1, nil, 404, nil)
}

func TestContractJ3SessionAccessAndGrants(t *testing.T) {
	o, path := taskOrg(t, "edit")
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Research", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	ws := strings.TrimPrefix(path, "/v1/workstreams/")
	afiID := "afi:" + ws + ":santosh"
	taskID := "task:" + ws + ":" + task.ID
	o.do(t, "santosh", "GET", path+"/agent", -1, nil, 200, nil)
	o.h.addTaskWorkEntry(taskRunKey{ws, task.ID}, api.AgentEntry{Text: "Working now"})
	const base = "/v1/resources/session"
	var listed api.ResourceList
	o.do(t, "abir", "GET", base, -1, nil, 200, &listed)
	if len(listed.Resources) != 1 || listed.Resources[0].ID != taskID {
		t.Fatalf("member's sessions = %+v", listed)
	}
	o.do(t, "abir", "GET", base+"/"+afiID, -1, nil, 404, nil)
	o.do(t, "mia", "GET", base+"/"+taskID, -1, nil, 404, nil)
	o.do(t, "mia", "GET", base+"/"+afiID+"/grants", -1, nil, 404, nil)
	o.do(t, "santosh", "POST", base+"/"+afiID+"/grants", -1, personGrant("mia", "view"), 201, nil)
	var grants api.ResourceGrants
	o.do(t, "mia", "GET", base+"/"+afiID+"/grants", -1, nil, 200, &grants)
	if !slices.ContainsFunc(grants.Grants, func(g api.GrantTuple) bool { return g.Subject.ID == "mia" && g.Live }) {
		t.Fatalf("Mia's grant missing: %+v", grants)
	}
}
