package serve

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// lastCanvasAudience is who the newest canvas.changed event for id was sent to.
func lastCanvasAudience(t *testing.T, o *wsOrg, id string) canvasLive {
	t.Helper()
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range slices.Backward(events) {
		var stored canvasLive
		if e.Kind == api.EventCanvasChanged && json.Unmarshal(e.Data, &stored) == nil && stored.ID == id {
			return stored
		}
	}
	t.Fatalf("no canvas.changed event for %s among %d events", id, len(events))
	return canvasLive{}
}

// Contract F2, F3, F4. A move changes where a canvas lives and who hears of it, and a share never does.
func TestAccessMoveCanvasBetweenMySpaceAndWorkstream(t *testing.T) {
	o := newWSOrg(t)
	pantry := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	o.do(t, "santosh", "POST", accessWorkstreamPath(pantry.ID)+"/grants", -1, personGrant("abir", "edit"), 201, nil)
	var space api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &space)
	const canvas = "cv_demo01234567"
	if _, err := o.st.EditCanvases(func(all factory.OrgCanvases) (bool, error) {
		all[canvas] = factory.OrgCanvas{ID: canvas, Workstream: space.Workstream.ID, Owner: factory.WorkstreamOwner{Kind: "person", ID: "santosh"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	path := "/v1/resources/canvas/" + canvas
	o.do(t, "abir", "GET", path, -1, nil, 404, nil)

	// The refusals first: each says why and changes nothing.
	o.do(t, "santosh", "POST", path+"/move", -1, api.ResourceMove{}, 422, nil)
	o.do(t, "santosh", "POST", path+"/move", -1, api.ResourceMove{To: space.Workstream.ID}, 409, nil)
	o.do(t, "mia", "POST", path+"/move", -1, api.ResourceMove{To: pantry.ID}, 404, nil)
	o.do(t, "santosh", "POST", "/v1/resources/workstream/"+pantry.ID+"/move", -1, api.ResourceMove{To: space.Workstream.ID}, 409, nil)
	viewOnly := o.create(t, "abir", api.WorkstreamNew{Name: "Read only for santosh"})
	o.do(t, "abir", "POST", accessWorkstreamPath(viewOnly.ID)+"/grants", -1, personGrant("santosh", "view"), 201, nil)
	o.do(t, "santosh", "POST", path+"/move", -1, api.ResourceMove{To: viewOnly.ID}, 403, nil)
	o.do(t, "abir", "POST", path+"/move", -1, api.ResourceMove{To: pantry.ID}, 404, nil)

	// Into Pantry: its people reach it, and abir hears it arrive.
	var moved api.ResourceMoved
	o.do(t, "santosh", "POST", path+"/move", -1, api.ResourceMove{To: pantry.ID}, 200, &moved)
	if moved.Resource.Parent == nil || moved.Resource.Parent.ID != pantry.ID || len(moved.Dropped) != 0 {
		t.Fatalf("moved = %+v", moved)
	}
	var got api.OrgResource
	o.do(t, "abir", "GET", path, -1, nil, 200, &got)
	if got.Parent == nil || got.Parent.ID != pantry.ID || got.MyRole != "edit" {
		t.Fatalf("abir's view of the moved canvas = %+v", got)
	}
	o.do(t, "mia", "GET", path, -1, nil, 404, nil)
	if heard := lastCanvasAudience(t, o, canvas); !slices.Contains(heard.Persons, "abir") || !slices.Contains(heard.Persons, "santosh") {
		t.Fatalf("the move was heard by %+v", heard.itemAudience)
	}
	// abir may edit Pantry but does not manage the canvas, so cannot move it out.
	o.do(t, "abir", "POST", path+"/move", -1, api.ResourceMove{To: viewOnly.ID}, 403, nil)

	// An own grant on the canvas widens it; moving it back into a private space drops that grant,
	// and everyone who reached it only at Pantry or through the grant hears it go.
	var grant api.AccessGrant
	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("mia", "view"), 201, &grant)
	o.do(t, "mia", "GET", path, -1, nil, 200, nil)
	o.do(t, "santosh", "POST", path+"/move", -1, api.ResourceMove{To: space.Workstream.ID}, 200, &moved)
	if moved.Resource.Parent == nil || moved.Resource.Parent.ID != space.Workstream.ID || len(moved.Dropped) != 1 || moved.Dropped[0].ID != grant.ID {
		t.Fatalf("moved back = %+v", moved)
	}
	for _, who := range []string{"abir", "mia"} {
		o.do(t, who, "GET", path, -1, nil, 404, nil)
	}
	var listed api.ResourceGrants
	o.do(t, "santosh", "GET", path+"/grants", -1, nil, 200, &listed)
	if len(listed.Grants) != 1 || listed.Grants[0].Source != "owner" || listed.Grants[0].Subject.ID != "santosh" {
		t.Fatalf("grants after the move = %+v", listed.Grants)
	}
	heard := lastCanvasAudience(t, o, canvas)
	for _, who := range []string{"abir", "mia", "santosh"} {
		if !slices.Contains(heard.Persons, who) {
			t.Fatalf("%s did not hear the move out: %+v", who, heard.itemAudience)
		}
	}
	var audit struct {
		Events []record.Event `json:"events"`
	}
	// The record shows a move only to those who may still see the canvas: mia may not.
	o.do(t, "santosh", "GET", "/v1/audit", -1, nil, 200, &audit)
	if !slices.ContainsFunc(audit.Events, func(e record.Event) bool {
		return e.Kind == record.ResourceAccessChanged && e.Verb == "moved" && e.Handle == "santosh"
	}) {
		t.Fatalf("the move is missing from the record: %+v", audit.Events)
	}
}
