package parts

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

func TestCanvasStatusVersionsAndInvalidBounds(t *testing.T) {
	s, scope, hub := setup(t)
	ctx := context.Background()
	original := `{"title":"Hiring","status":"Draft","frame":"16:9","boxes":[]}`
	_, err := s.CreateDoc(ctx, scope, bridge.CreateDocParams{Doc: "q4", Surface: bridge.SurfaceCanvas, Title: "Board", Parts: []bridge.CreateDocPart{{Part: "slides/1.json", Text: original}}})
	if err != nil {
		t.Fatal(err)
	}
	initial := len(hub.events)
	_, err = s.Write(ctx, scope, bridge.WritePartParams{Doc: "q4", Part: "slides/1.json", BaseVersion: 1, Kind: bridge.PartVersionKindNudge, Text: `{"title":"Hiring","frame":"16:9","boxes":[{"id":"a","type":"text","x":1270,"y":0,"w":20,"h":20}]}`})
	requireCode(t, err, bridge.CodeInvalid)
	body, err := os.ReadFile(filepath.Join(s.cfg.Root, scope.App, "content/q4/slides/1.json"))
	if err != nil || string(body) != original || len(hub.events) != initial {
		t.Fatalf("body=%q err=%v", body, err)
	}
	final := `{"title":"Hiring","status":"Final","frame":"16:9","boxes":[]}`
	p, err := s.Write(ctx, scope, bridge.WritePartParams{Doc: "q4", Part: "slides/1.json", BaseVersion: 1, Kind: bridge.PartVersionKindAsk, Text: final})
	if err != nil || p.Status != "Final" {
		t.Fatalf("part=%+v err=%v", p, err)
	}
	p, err = s.Get(ctx, scope, bridge.GetPartParams{Doc: "q4", Part: "slides/1.json", Version: 1})
	if err != nil || p.Status != "Draft" {
		t.Fatalf("historical=%+v err=%v", p, err)
	}
	p, err = s.Restore(ctx, scope, bridge.RestorePartParams{Doc: "q4", Part: "slides/1.json", Version: 1})
	if err != nil || p.Version != 3 || p.Status != "Draft" {
		t.Fatalf("restore=%+v err=%v", p, err)
	}
	doc, err := s.GetDoc(ctx, scope, bridge.GetDocParams{Doc: "q4"})
	if err != nil || doc.Parts[0].Status != "Draft" || doc.Parts[0].Title != "Hiring" {
		t.Fatalf("doc=%+v err=%v", doc, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(s.cfg.Root, scope.App, "parts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var stored string
	if err = db.QueryRow(`SELECT body FROM part_versions WHERE version=2`).Scan(&stored); err != nil || stored != final {
		t.Fatalf("stored=%q err=%v", stored, err)
	}
}

func TestRestoreReleasesLockOnFailure(t *testing.T) {
	s, scope, _ := setup(t)
	ctx := context.Background()
	textDoc(t, s, scope, "q4")
	collab := &fakeCollab{}
	s.all.Collab = collab
	_, err := s.Restore(ctx, scope, bridge.RestorePartParams{Doc: "q4", Part: "section-1.md", Version: 99})
	requireCode(t, err, bridge.CodeNotFound)
	if collab.locks != 1 || collab.releases != 1 {
		t.Fatalf("collab=%+v", collab)
	}
	collab.err = conflict("held by another member")
	_, err = s.Restore(ctx, scope, bridge.RestorePartParams{Doc: "q4", Part: "section-1.md", Version: 1})
	requireCode(t, err, bridge.CodeConflict)
	if collab.locks != 2 || collab.releases != 1 {
		t.Fatalf("collab=%+v", collab)
	}
}
