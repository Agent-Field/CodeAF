package workspace

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func workstreamSchemaFourFixture(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.db.Exec("DROP TABLE workstream_suggestion_edits; DROP TABLE workstream_suggestions; DROP TABLE workstream_sources; PRAGMA user_version=4"); err != nil {
		t.Fatal(err)
	}
	s.version = 4
}

// WP-12 (i): every existing table and value survives the additive migration;
// failed first writes and all reads leave schemas three and four unchanged.
func TestWorkstreamSourcesSchemaFiveMigration(t *testing.T) {
	t.Run("schema three", func(t *testing.T) { workstreamSourcesMigration(t, 3) })
	t.Run("schema four", func(t *testing.T) { workstreamSourcesMigration(t, 4) })
}

func workstreamSourcesMigration(t *testing.T, version int) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sources.db")
	s := openTestStore(t, path)
	seedWorkstream(t, s, "stream", "Sources")
	if err := s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
INSERT INTO memberships(collection_id,kind,ref_id,session_id,factory) VALUES ('stream','factory_job','7','','f');
INSERT INTO workstream_pages(id,workstream_id,title,owner,position,version,updated_at,updated_by,doc) VALUES ('page','stream','Overview','you',0,1,100,'you','{"type":"doc","content":[]}');
INSERT INTO workstream_page_versions(page_id,version,saved_at,saved_by,title,doc,bytes) VALUES ('page',1,100,'you','Overview','{"type":"doc","content":[]}',27);
INSERT INTO workstream_goaheads(id,workstream_id,scope,wave,cap_usd,ends_at,state,granted_at) VALUES ('grant','stream','wave',1,1,200,'active',100);
INSERT INTO workstream_records(id,workstream_id,kind,text,state,actor,created_at,goahead,goahead_id) VALUES ('record','stream','goahead','Keep this','kept','agent',100,'{"scope":"wave","wave":1,"cap_usd":1,"ends_at":"1970-01-01T00:00:00.2Z"}','grant');
INSERT INTO workstream_plans(workstream_id,version,updated_at,updated_by,milestones,waves) VALUES ('stream',1,100,'you','[]','[]');`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tables := []string{"collections", "memberships", "workstreams", "workstream_jobs", "workstream_pages", "workstream_page_versions", "workstream_records", "workstream_goaheads", "workstream_plans"}
	if version == 4 {
		if _, err := s.db.Exec(`
INSERT INTO workstream_wakes(id,workstream_id,event_key,event_json,state,summary,error,cost_usd,started_at,settled_at,dismissed_at)
 VALUES ('wake','stream','morning:2026-11-03','{"kind":"morning","slot":"2026-11-03"}','settled','Review this.','',0.04,100,200,300);
INSERT INTO workstream_wake_items(id,wake_id,position,kind,state,why,item_json,undo_json,note,applied_at,undone_at)
 VALUES ('wake-item','wake',0,'decision','undone','Keep the context.','{"kind":"decision","text":"Review this."}','{"record_id":"record"}','Changed since.',150,250);`); err != nil {
			t.Fatal(err)
		}
		workstreamSchemaFourFixture(t, s)
		tables = append(tables, "workstream_wakes", "workstream_wake_items")
	} else {
		workstreamSchemaThreeFixture(t, s)
	}
	before := map[string][][]any{}
	for _, table := range tables {
		before[table] = workstreamStoredRows(t, s.db, table)
		if len(before[table]) == 0 {
			t.Fatalf("empty migration fixture: %s", table)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	bytesBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, path)
	if _, err := s.Members(ctx, "stream"); err != nil {
		t.Fatal(err)
	}
	if err := s.Read(ctx, func(_ *sql.Tx, gotVersion int) error {
		if gotVersion != version {
			t.Fatalf("read schema %d, want %d", gotVersion, version)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("first write refused")
	if err := s.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO workstream_sources(id,workstream_id,kind,target,title,added_by,added_at) VALUES ('rollback','stream','link','https://example.com','Example','you',100)"); err != nil {
			return err
		}
		return refused
	}); !errors.Is(err, refused) {
		t.Fatal(err)
	}
	bytesAfter, err := os.ReadFile(path)
	if err != nil || string(bytesAfter) != string(bytesBefore) || userVersion(t, s) != version {
		t.Fatalf("read/refusal changed schema %d: %v", version, err)
	}
	if err := s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO workstream_sources(id,workstream_id,kind,target,title,added_by,added_at) VALUES ('source','stream','link','https://example.com','Example','you',100)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if userVersion(t, s) != 6 {
		t.Fatal("first write did not migrate")
	}
	for _, table := range tables {
		if got := workstreamStoredRows(t, s.db, table); !reflect.DeepEqual(got, before[table]) {
			t.Fatalf("migration changed %s: %v", table, got)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	opened := openTestStore(t, path)
	if userVersion(t, opened) != 6 {
		t.Fatal("current schema did not reopen")
	}
}

// WP-12 (i): missing sources in schema five and future versions are refused.
func TestWorkstreamSourcesSchemaFiveRequiresItsTable(t *testing.T) {
	for _, future := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "invalid.db")
		s := openTestStore(t, path)
		createTestCollection(t, s, "Sources")
		query := "DROP TABLE workstream_sources"
		if future {
			query = "PRAGMA user_version=7"
		}
		if _, err := s.db.Exec(query); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := Open(path)
		if opened != nil {
			opened.Close()
		}
		if err == nil || future && !strings.Contains(err.Error(), "unsupported collections database") || !future && !strings.Contains(err.Error(), "workstream_sources") {
			t.Fatalf("invalid schema opened: %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(before) != string(after) {
			t.Fatalf("refusal changed invalid schema: %v", err)
		}
	}
}
