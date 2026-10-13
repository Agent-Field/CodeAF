package records

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestDraftDatabasePathAndMissingOverlay(t *testing.T) {
	s, scope, _, _ := setup(t)
	scope.Data = svc.DataDraft
	for _, member := range []string{"me", "raj"} {
		t.Run(member, func(t *testing.T) {
			scope.Caller.Member = member
			_, err := s.Create(context.Background(), scope, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "scratch"}})
			needCode(t, err, bridge.CodeNotFound)
			if err.Error() != "not_found: no draft" {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(s.cfg.Root, "crm", "records.db")); !os.IsNotExist(err) {
				t.Fatal("missing draft created real records", err)
			}
			path := filepath.Join(s.cfg.Root, "crm", "drafts", member, "overlay.db")
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("missing draft created overlay", err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte{}, 0600); err != nil {
				t.Fatal(err)
			}
			filename, err := s.shapeDatabaseFilename(scope)
			if err != nil || filename != filepath.Join("drafts", member, "overlay.db") {
				t.Fatalf("path = %s, %v", filename, err)
			}
		})
	}
	for _, member := range []string{"", ".", "..", "../me", "/me", "me/other", "me\\other"} {
		scope.Caller.Member = member
		_, err := s.shapeDatabaseFilename(scope)
		needCode(t, err, bridge.CodeInvalid)
	}
}

func TestDraftCRUDIsIsolatedPerMember(t *testing.T) {
	s, scope, _, _ := setup(t)
	ctx := context.Background()
	scope.Data = svc.DataReal
	before, err := s.Query(ctx, scope, bridge.QueryParams{Type: "company"})
	if err != nil {
		t.Fatal(err)
	}
	realPath := filepath.Join(s.cfg.Root, scope.App, "records.db")
	original, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"me", "raj"} {
		path := filepath.Join(s.cfg.Root, scope.App, "drafts", member, "overlay.db")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, member := range []string{"me", "raj"} {
		t.Run(member, func(t *testing.T) {
			draft := scope
			draft.Data = svc.DataDraft
			draft.Caller.Member = member
			created, err := s.Create(ctx, draft, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": member + " draft"}})
			if err != nil {
				t.Fatal(err)
			}
			row, err := s.Get(ctx, draft, bridge.GetRecordParams{Type: "company", ID: created.ID})
			if err != nil || row.CreatedBy != member {
				t.Fatalf("get = %+v %v", row, err)
			}
			other := draft
			if member == "me" {
				other.Caller.Member = "raj"
			} else {
				other.Caller.Member = "me"
			}
			_, err = s.Get(ctx, other, bridge.GetRecordParams{Type: "company", ID: created.ID})
			needCode(t, err, bridge.CodeNotFound)
			changed, err := s.Update(ctx, draft, bridge.UpdateRecordParams{Type: "company", ID: created.ID, Set: map[string]any{"name": member + " edited"}})
			if err != nil || changed.Rows[0].Fields["name"] != member+" edited" {
				t.Fatalf("update = %+v %v", changed, err)
			}
			page, err := s.Query(ctx, draft, bridge.QueryParams{Type: "company"})
			if err != nil || page.Total != before.Total+1 {
				t.Fatalf("query = %+v %v", page, err)
			}
			_, err = s.Delete(ctx, draft, bridge.DeleteRecordParams{Type: "company", ID: created.ID})
			if err != nil {
				t.Fatal(err)
			}
			page, err = s.Query(ctx, draft, bridge.QueryParams{Type: "company"})
			if err != nil || page.Total != before.Total {
				t.Fatalf("after delete = %+v %v", page, err)
			}
			after, err := os.ReadFile(realPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(original, after) {
				t.Fatal("draft CRUD changed records.db")
			}
		})
	}
}
