package serve

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// MOVING FROM MY SPACE LEAVES ONE DECISIONS LINE, AND ITS UNDO TAKES IT BACK
// (org-space DESIGN §2.2). santosh moves a document from their space into
// Pantry: it is Pantry's, and Pantry's Decisions say who moved what in. Undo
// within its window takes the file back and withdraws the line. Once the
// window has gone, only whoever looks after Pantry moves it to a space.
func TestAMoveFromMySpaceIsOneDecisionAndUndoTakesItBack(t *testing.T) {
	o := newWSOrg(t)
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	o.h.config.Now = func() time.Time { return now }
	pantry := o.create(t, "abir", api.WorkstreamNew{Name: "Pantry"})
	edit := []factory.Share{{Kind: "person", ID: "santosh", Role: "edit"}}
	o.do(t, "abir", "PUT", "/v1/workstreams/"+pantry.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &edit, Rev: &pantry.Rev}, 200, nil)
	var space api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &space)
	mine := "/v1/workstreams/" + space.Workstream.ID + "/files"
	theirs := "/v1/workstreams/" + pantry.ID + "/files"
	var doc api.WorkstreamFile
	o.do(t, "santosh", "POST", mine, -1, api.FileNew{Kind: "document", Name: "Launch canvas", BodyMD: "Ship it on Tuesday"}, 201, &doc)

	o.do(t, "santosh", "POST", mine+"/move", -1, api.FilesMove{Files: []string{doc.ID}, To: pantry.ID}, 200, nil)
	moveLine := func() factory.WorkstreamDecision {
		t.Helper()
		ws := o.get(t, "abir", pantry.ID)
		for _, d := range ws.Decisions {
			if d.From != nil && d.From.Kind == factory.DecisionFromMove {
				return d
			}
		}
		t.Fatalf("Pantry's Decisions hold no move: %+v", ws.Decisions)
		return factory.WorkstreamDecision{}
	}
	if d := moveLine(); d.State != factory.DecisionKept || d.By != "santosh" || d.Text != "Priya Nair moved “Launch canvas” in from their space" {
		t.Fatalf("the move's line = %+v", d)
	}

	// Undo, a few seconds later: back in the space, the line withdrawn.
	now = now.Add(8 * time.Second)
	o.do(t, "santosh", "POST", theirs+"/move", -1, api.FilesMove{Files: []string{doc.ID}, To: space.Workstream.ID}, 200, nil)
	if d := moveLine(); d.State != factory.DecisionWithdrawn || d.ChangedBy == nil || d.ChangedBy.ID != "santosh" {
		t.Fatalf("after Undo the line = %+v", d)
	}

	// Moved in again and left past its Undo: santosh may not take it back,
	// abir, who looks after Pantry, may.
	o.do(t, "santosh", "POST", mine+"/move", -1, api.FilesMove{Files: []string{doc.ID}, To: pantry.ID}, 200, nil)
	now = now.Add(factory.MoveUndoWindow + time.Second)
	o.do(t, "santosh", "POST", theirs+"/move", -1, api.FilesMove{Files: []string{doc.ID}, To: space.Workstream.ID}, 403, nil)
	// abir cannot reach santosh's space at all.
	o.do(t, "abir", "POST", theirs+"/move", -1, api.FilesMove{Files: []string{doc.ID}, To: space.Workstream.ID}, 404, nil)
}
