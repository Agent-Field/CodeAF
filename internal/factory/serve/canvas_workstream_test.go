package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// listsCanvas is whether who's canvas list has id, and the row when it does.
func (o *wsOrg) listsCanvas(t *testing.T, who, id string) (map[string]any, bool) {
	t.Helper()
	var list struct {
		Canvases []map[string]any `json:"canvases"`
	}
	o.do(t, who, "GET", "/v1/canvases", -1, nil, 200, &list)
	for _, row := range list.Canvases {
		if row["id"] == id {
			return row, true
		}
	}
	return nil, false
}

// A CANVAS MADE IN A TEAM'S WORKSTREAM IS THE TEAM'S (D11): santosh makes it
// from Launch checklist, owned by Platform; abir, in Platform, lists it with
// the workstream named and writes in it; mia, outside, never sees it. A share
// added to the workstream afterwards reaches the canvas too, the way a page
// in a Notion teamspace follows the teamspace.
func TestACanvasMadeInATeamWorkstreamIsTheTeams(t *testing.T) {
	o := canvasOrg(t)
	team := api.WorkstreamOwner{Kind: "team", ID: "tm_platform"}
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch checklist", Owner: &team})

	var cv map[string]any
	o.do(t, "santosh", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "Untitled canvas", Workstream: ws.ID}, 201, &cv)
	id := cv["id"].(string)
	owner, _ := cv["owner"].(map[string]any)
	if cv["workstream"] != ws.ID || owner["kind"] != "team" || owner["id"] != "tm_platform" || cv["my_role"] != "full" {
		t.Fatalf("made: %v", cv)
	}

	row, ok := o.listsCanvas(t, "abir", id)
	if !ok || row["workstream"] != ws.ID || row["my_role"] != "full" {
		t.Fatalf("abir's row = %v %v", row, ok)
	}
	o.do(t, "abir", "POST", "/v1/canvases/"+id+"/ops", -1, ops(`{"t":"put","id":"f_abir00000000","type":"frame","props":{"kind":"board","name":"Abir"}}`), 200, nil)
	if _, ok := o.listsCanvas(t, "mia", id); ok {
		t.Fatal("mia, outside Platform, sees the team's canvas")
	}
	o.do(t, "mia", "GET", "/v1/canvases/"+id, -1, nil, 404, nil)

	// Sharing the workstream with mia as a viewer lets her read the canvas, not write it.
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "mia", Role: "view"}), 200, nil)
	if row, ok := o.listsCanvas(t, "mia", id); !ok || row["my_role"] != "view" {
		t.Fatalf("mia after the workstream was shared = %v %v", row, ok)
	}
	o.do(t, "mia", "POST", "/v1/canvases/"+id+"/ops", -1, ops(`{"t":"put","id":"f_mia000000000","type":"frame","props":{"kind":"board","name":"Mia"}}`), 403, nil)
}

// ONLY SOMEONE WHO CAN EDIT A WORKSTREAM PUTS A CANVAS IN IT: a view-only
// member is refused with 403 and nothing is made; someone it is not shared
// with is told it does not exist; and owner, shares or private beside a
// workstream are refused, because the workstream decides them.
func TestAViewerCannotPutACanvasInAWorkstream(t *testing.T) {
	o := canvasOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch checklist"})
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "abir", Role: "view"}), 200, nil)

	w := o.raw(t, "abir", "POST", "/v1/canvases", api.CanvasNew{Title: "Untitled canvas", Workstream: ws.ID})
	if w.Code != 403 {
		t.Fatalf("a viewer put a canvas in the workstream: %d %s", w.Code, w.Body)
	}
	var list struct {
		Canvases []map[string]any `json:"canvases"`
	}
	o.do(t, "santosh", "GET", "/v1/canvases", -1, nil, 200, &list)
	if len(list.Canvases) != 0 {
		t.Fatalf("a refused canvas was made: %v", list.Canvases)
	}
	o.do(t, "mia", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "x", Workstream: ws.ID}, 404, nil)
	o.do(t, "santosh", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "x", Workstream: ws.ID, Private: true}, 400, nil)

	// Nor may a viewer move their own canvas into it; an editor may, and take it out again.
	var mine map[string]any
	o.do(t, "abir", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "Abir's"}, 201, &mine)
	id := mine["id"].(string)
	in := ws.ID
	o.do(t, "abir", "PATCH", "/v1/canvases/"+id, -1, api.CanvasPatch{Workstream: &in}, 403, nil)
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "abir", Role: "edit"}), 200, nil)
	var moved map[string]any
	o.do(t, "abir", "PATCH", "/v1/canvases/"+id, -1, api.CanvasPatch{Workstream: &in}, 200, &moved)
	if moved["workstream"] != ws.ID {
		t.Fatalf("moved = %v", moved)
	}
	if _, ok := o.listsCanvas(t, "santosh", id); !ok {
		t.Fatal("the workstream's owner does not see the canvas moved into it")
	}
}
