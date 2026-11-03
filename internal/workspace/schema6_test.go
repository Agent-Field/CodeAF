package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// WP-6d Schema: use the actual DDL for each step, without any later tables.
func writeWorkstreamSuggestionPredecessor(t *testing.T, version int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "collections.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ddl := schemaTables("collections", "memberships") + schemaIndexes + workstreamSchema
	if version >= WorkstreamWakeSchemaVersion {
		ddl += workstreamWakeSchema
	}
	if version >= WorkstreamSourcesSchemaVersion {
		ddl += workstreamSourcesSchema
	}
	if _, err := db.Exec(ddl + fmt.Sprintf(`
PRAGMA application_id=%d; PRAGMA user_version=%d;
INSERT INTO collections(id,name,created_at) VALUES ('existing','Existing',100);
INSERT INTO workstreams(id,agent_id,created_at,updated_at) VALUES ('existing','agent',100,100);
INSERT INTO memberships(collection_id,kind,ref_id,session_id,factory) VALUES ('existing','factory_job','7','','f');
INSERT INTO workstream_pages(id,workstream_id,title,owner,position,version,updated_at,updated_by,doc) VALUES ('page','existing','Notes','you',0,1,100,'you','{"type":"doc","content":[]}');
INSERT INTO workstream_page_versions(page_id,version,saved_at,saved_by,title,doc,bytes) VALUES ('page',1,100,'you','Notes','{"type":"doc","content":[]}',27);
INSERT INTO workstream_records(id,workstream_id,kind,text,state,actor,created_at) VALUES ('record','existing','decision','Keep','kept','you',100);
INSERT INTO workstream_goaheads(id,workstream_id,scope,wave,cap_usd,ends_at,state,granted_at) VALUES ('grant','existing','wave',1,1,200,'active',100);
INSERT INTO workstream_plans(workstream_id,version,updated_at,updated_by,milestones,waves) VALUES ('existing',1,100,'you','[]','[]');
`, applicationID, version)); err != nil {
		t.Fatal(err)
	}
	if version >= WorkstreamWakeSchemaVersion {
		if _, err := db.Exec(`
INSERT INTO workstream_wakes(id,workstream_id,event_key,event_json,state,summary,cost_usd,started_at,settled_at) VALUES ('wake','existing','morning:2026-11-03','{}','settled','Existing wake',0.25,100,200);
INSERT INTO workstream_wake_items(id,wake_id,position,kind,state,why,item_json,undo_json,applied_at) VALUES ('item','wake',0,'page','applied','Existing change','{}','{}',200);
`); err != nil {
			t.Fatal(err)
		}
	}
	if version >= WorkstreamSourcesSchemaVersion {
		if _, err := db.Exec(`
INSERT INTO workstream_sources(id,workstream_id,kind,target,title,url,why,read_by_jobs,proposed,added_by,added_at) VALUES ('source','existing','link','https://example.com','Example','https://example.com','Existing context',1,1,'agent',100);
`); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// WP-6d Schema: opening, reading and a refused first write preserve the file;
// the first committed write upgrades every missing step and keeps all old rows.
func TestWorkstreamSchemaSixIsAnAtomicAdditiveMigration(t *testing.T) {
	for _, version := range []int{3, 4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := writeWorkstreamSuggestionPredecessor(t, version)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			s := openTestStore(t, path)
			tables := []string{"collections", "memberships", "workstreams", "workstream_jobs", "workstream_pages", "workstream_page_versions", "workstream_records", "workstream_goaheads", "workstream_plans"}
			if version >= WorkstreamWakeSchemaVersion {
				tables = append(tables, "workstream_wakes", "workstream_wake_items")
			}
			if version >= WorkstreamSourcesSchemaVersion {
				tables = append(tables, "workstream_sources")
			}
			rows := map[string][][]any{}
			for _, table := range tables {
				rows[table] = workstreamStoredRows(t, s.db, table)
				if len(rows[table]) == 0 {
					t.Fatalf("fixture has no rows in %s", table)
				}
			}
			if list, err := s.List(context.Background(), nil); err != nil || len(list) != 1 || list[0].ID != "existing" {
				t.Fatalf("schema %d list: %+v %v", version, list, err)
			}
			if err := s.Read(context.Background(), func(_ *sql.Tx, got int) error {
				if got != version {
					t.Fatalf("read version %d, want %d", got, version)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) || userVersion(t, s) != version {
				t.Fatal("open or read changed the old file", err)
			}
			refusal := errors.New("refused write")
			if err := s.Write(context.Background(), func(tx *sql.Tx) error {
				if _, err := tx.Exec("INSERT INTO collections(id,name) VALUES ('rollback','Rollback')"); err != nil {
					return err
				}
				return refusal
			}); !errors.Is(err, refusal) {
				t.Fatal(err)
			}
			after, err = os.ReadFile(path)
			if err != nil || string(after) != string(before) || userVersion(t, s) != version {
				t.Fatal("refused write changed the old file", err)
			}
			var n int
			if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('workstream_wakes','workstream_wake_items','workstream_sources','workstream_suggestions','workstream_suggestion_edits')").Scan(&n); err != nil || n != len(tables)-9 {
				t.Fatal("refused write left migration tables", n, err)
			}
			if err := s.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec(`
INSERT INTO workstream_suggestions(id,workstream_id,page_id,base_version,why,actor,created_at) VALUES ('suggestion','existing','page',1,'Clarify','agent',200);
INSERT INTO workstream_suggestion_edits(suggestion_id,id,position,block,at_offset,del,ins,block_text,why,state) VALUES ('suggestion','e1',0,'[0]',0,'Old','New','Old','Clearer','proposed');`)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if userVersion(t, s) != 6 {
				t.Fatal("first write did not install schema 6")
			}
			for _, table := range tables {
				if !reflect.DeepEqual(rows[table], workstreamStoredRows(t, s.db, table)) {
					t.Errorf("migration changed %s", table)
				}
			}
			for _, table := range []string{"workstream_wakes", "workstream_wake_items", "workstream_sources", "workstream_suggestions", "workstream_suggestion_edits"} {
				if _, err := s.db.Exec("SELECT * FROM " + table + " LIMIT 0"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// WP-6d Schema: blank files write 6; future and incomplete current files refuse
// without repair, including wake and source tables inherited from schemas 4/5.
func TestWorkstreamSchemaSixFreshFutureAndDamaged(t *testing.T) {
	for _, damage := range []string{"future", "workstream_suggestions", "workstream_suggestion_edits", "workstream_wakes", "workstream_wake_items", "workstream_sources"} {
		t.Run(damage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "collections.db")
			s := openTestStore(t, path)
			createTestCollection(t, s, "Fresh")
			if userVersion(t, s) != 6 {
				t.Fatal("fresh schema is not 6")
			}
			query := "DROP TABLE " + damage
			switch damage {
			case "future":
				query = "PRAGMA user_version=7"
			case "workstream_suggestions":
				query = "DROP TABLE workstream_suggestion_edits; DROP TABLE workstream_suggestions"
			case "workstream_wakes":
				query = "DROP TABLE workstream_wake_items; DROP TABLE workstream_wakes"
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
				_ = opened.Close()
			}
			if err == nil || damage == "future" && !strings.Contains(err.Error(), "unsupported collections database") || damage != "future" && !strings.Contains(err.Error(), damage) {
				t.Fatal("accepted unsupported file or returned the wrong refusal", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatal("refusal changed file", err)
			}
		})
	}
}

// WP-6d Schema: accepting schemas 4/5 must still verify their wake/source tables.
func TestWorkstreamSuggestionPredecessorsStillRequireTheirTables(t *testing.T) {
	for _, version := range []int{4, 5} {
		tables := []string{"workstream_wakes", "workstream_wake_items"}
		if version >= WorkstreamSourcesSchemaVersion {
			tables = append(tables, "workstream_sources")
		}
		for _, table := range tables {
			t.Run(fmt.Sprintf("%d/%s", version, table), func(t *testing.T) {
				path := writeWorkstreamSuggestionPredecessor(t, version)
				s := openTestStore(t, path)
				query := "DROP TABLE " + table
				if table == "workstream_wakes" {
					query = "DROP TABLE workstream_wake_items; DROP TABLE workstream_wakes"
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
					_ = opened.Close()
				}
				if err == nil || !strings.Contains(err.Error(), table) {
					t.Fatalf("schema %d accepted missing %s: %v", version, table, err)
				}
				after, err := os.ReadFile(path)
				if err != nil || string(after) != string(before) {
					t.Fatal("refusal changed predecessor file", err)
				}
			})
		}
	}
}
