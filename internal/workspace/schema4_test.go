package workspace

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func workstreamSchemaThreeFixture(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.db.Exec("DROP TABLE workstream_suggestion_edits; DROP TABLE workstream_suggestions; DROP TABLE workstream_sources; DROP TABLE workstream_wake_items; DROP TABLE workstream_wakes; PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
	s.version = 3
}

func TestWakeI_SchemaThreeMigratesAdditively(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.db")
	s := openTestStore(t, path)
	seedWorkstream(t, s, "w", "Watching")
	if _, err := s.db.Exec(`
 INSERT INTO workstream_pages(id,workstream_id,title,owner,position,version,updated_at,updated_by,doc) VALUES ('p','w','Notes','agent',0,1,100,'agent','{}');
 INSERT INTO workstream_page_versions(page_id,version,saved_at,saved_by,title,doc,bytes) VALUES ('p',1,100,'agent','Notes','{}',2);
 INSERT INTO workstream_records(id,workstream_id,kind,text,state,actor,created_at) VALUES ('r','w','decision','Keep it','kept','agent',100);
 INSERT INTO workstream_plans(workstream_id,version,updated_at,updated_by,milestones,waves) VALUES ('w',1,100,'you','[]','[]');`); err != nil {
		t.Fatal(err)
	}
	workstreamSchemaThreeFixture(t, s)
	queries := []string{"SELECT * FROM workstreams", "SELECT * FROM workstream_pages", "SELECT * FROM workstream_page_versions", "SELECT * FROM workstream_records", "SELECT * FROM workstream_plans"}
	snapshot := func() [][]string {
		all := [][]string{}
		for _, q := range queries {
			rows, err := s.db.Query(q)
			if err != nil {
				t.Fatal(err)
			}
			columns, _ := rows.Columns()
			for rows.Next() {
				cells := make([]sql.NullString, len(columns))
				args := make([]any, len(cells))
				for i := range args {
					args[i] = &cells[i]
				}
				if err := rows.Scan(args...); err != nil {
					t.Fatal(err)
				}
				values := []string{}
				for _, cell := range cells {
					values = append(values, cell.String)
				}
				all = append(all, values)
			}
			rows.Close()
		}
		return all
	}
	before := snapshot()
	if err := s.Write(context.Background(), func(*sql.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if userVersion(t, s) != 6 || !reflect.DeepEqual(snapshot(), before) {
		t.Fatal("migration lost existing rows")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM workstream_wakes").Scan(&count); err != nil || count != 0 {
		t.Fatalf("wakes: %d %v", count, err)
	}
}

func TestWakeI_NewerSchemaIsRefusedWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	s := openTestStore(t, path)
	createTestCollection(t, s, "Future")
	if _, err := s.db.Exec("PRAGMA user_version=7"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(path)
	if opened != nil {
		opened.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "version 7") {
		t.Fatalf("newer schema: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("newer file changed")
	}
}

func TestWakeI_FailedFirstWriteRollsBackTheMigration(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "rollback.db"))
	seedWorkstream(t, s, "w", "Watching")
	workstreamSchemaThreeFixture(t, s)
	err := s.Write(context.Background(), func(*sql.Tx) error { return ErrInvalid })
	if err != ErrInvalid || userVersion(t, s) != 3 {
		t.Fatal("failed write migrated", err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('workstream_wakes','workstream_wake_items')").Scan(&n); err != nil || n != 0 {
		t.Fatal("partial migration", n, err)
	}
}
