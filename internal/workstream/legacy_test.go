package workstream

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// Part A (a): domain reads through older schemas are empty, never migrations.
func TestPartALegacyWorkstreamReadsDoNotWrite(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			created, added := "", ""
			if version == 2 {
				created = ", created_at INTEGER NOT NULL DEFAULT 0"
				added = ", added_at INTEGER NOT NULL DEFAULT 0"
			}
			_, err = db.Exec(fmt.Sprintf(`CREATE TABLE collections(seq INTEGER PRIMARY KEY,id TEXT UNIQUE,name TEXT%s);
CREATE TABLE memberships(seq INTEGER PRIMARY KEY,collection_id TEXT,kind TEXT,ref_id TEXT,session_id TEXT,target_collection TEXT%s);
PRAGMA application_id=1095123788; PRAGMA user_version=%d;
INSERT INTO collections(seq,id,name) VALUES (1,'collection','Existing');`, created, added, version))
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			w, err := workspace.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			s := New(w, Options{})
			ctx := context.Background()
			all, err := s.List(ctx)
			if err != nil || all == nil || len(all) != 0 {
				t.Fatalf("legacy list: %+v %v", all, err)
			}
			for name, read := range map[string]func() error{
				"get":      func() error { _, err := s.Get(ctx, "collection"); return err },
				"plan":     func() error { _, err := s.Plan(ctx, "collection"); return err },
				"pages":    func() error { _, err := s.Pages(ctx, "collection"); return err },
				"page":     func() error { _, err := s.Page(ctx, "missing", true); return err },
				"history":  func() error { _, err := s.PageHistory(ctx, "missing", 50); return err },
				"records":  func() error { _, err := s.Records(ctx, "collection", "", ""); return err },
				"goaheads": func() error { _, err := s.GoAheads(ctx, "collection", true); return err },
			} {
				if err := read(); !errors.Is(err, ErrNotFound) {
					t.Fatalf("legacy %s: %v", name, err)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("domain read changed legacy file")
			}
		})
	}
}
