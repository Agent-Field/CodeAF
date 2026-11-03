package workspace

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
)

// Part A (a): this literal is schema 2, independent of the schema being written.
const schemaTwoDDL = `
CREATE TABLE collections (seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT NOT NULL UNIQUE,name TEXT NOT NULL,created_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE memberships (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact','crew','process')),
 ref_id TEXT NOT NULL,session_id TEXT NOT NULL,target_collection TEXT REFERENCES collections(id) ON DELETE CASCADE,
 added_at INTEGER NOT NULL DEFAULT 0,
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id) OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id));
CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id);
CREATE INDEX memberships_target ON memberships(target_collection);
PRAGMA application_id=1095123788; PRAGMA user_version=2;
INSERT INTO collections(seq,id,name,created_at) VALUES (4,'aaaa','Product',1234),(9,'bbbb','Nested',2345);
INSERT INTO memberships(seq,collection_id,kind,ref_id,session_id,target_collection,added_at) VALUES
 (2,'aaaa','conversation','chat','',NULL,4567),(6,'aaaa','task','3','chat',NULL,5678),
 (11,'aaaa','collection','bbbb','','bbbb',6789);
`

func writeSchemaTwo(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "two.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaTwoDDL); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPartASchemaTwoReadsWithoutWritingAndMigratesAllRows(t *testing.T) {
	ctx := context.Background()
	path := writeSchemaTwo(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := openTestStore(t, path)
	wantC, wantM := legacyRows(t, s.db)
	list, err := s.List(ctx, &Ref{Kind: ConversationKind, ID: "chat"})
	if err != nil || len(list) != 2 || list[0].CreatedAt != 1234 || list[0].LastAddedAt != 6789 || !*list[0].Holds {
		t.Fatalf("schema 2 read: %+v, %v", list, err)
	}
	tree, err := s.Tree(ctx, "aaaa", 0, 20, "")
	if err != nil || len(tree.Members) != 3 || tree.Members[0].AddedAt != 6789 {
		t.Fatalf("tree: %+v, %v", tree, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("read changed schema 2")
	}
	if err := s.Rename(ctx, "aaaa", "Products"); err != nil {
		t.Fatal(err)
	}
	if userVersion(t, s) != 4 {
		t.Fatal("first write did not migrate to 4")
	}
	gotC, gotM := legacyRows(t, s.db)
	wantC[0] = strings.Replace(wantC[0], "Product|", "Products|", 1)
	if !reflect.DeepEqual(gotC, wantC) || !reflect.DeepEqual(gotM, wantM) {
		t.Fatalf("migration lost rows: %v %v", gotC, gotM)
	}
	rows, err := s.db.Query("SELECT seq,created_at FROM collections ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	var collectionTimes [][2]int64
	for rows.Next() {
		var seq, at int64
		if err := rows.Scan(&seq, &at); err != nil {
			t.Fatal(err)
		}
		collectionTimes = append(collectionTimes, [2]int64{seq, at})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(collectionTimes, [][2]int64{{4, 1234}, {9, 2345}}) {
		t.Fatalf("collection times changed: %+v", collectionTimes)
	}
	rows, err = s.db.Query("SELECT seq,added_at FROM memberships ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	var memberTimes [][2]int64
	for rows.Next() {
		var seq, at int64
		if err := rows.Scan(&seq, &at); err != nil {
			t.Fatal(err)
		}
		memberTimes = append(memberTimes, [2]int64{seq, at})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(memberTimes, [][2]int64{{2, 4567}, {6, 5678}, {11, 6789}}) {
		t.Fatalf("membership times changed: %+v", memberTimes)
	}
	for _, name := range []string{"workstreams", "workstream_jobs", "workstream_pages", "workstream_page_versions", "workstream_records", "workstream_goaheads", "workstream_plans"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n); err != nil || n != 1 {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if err := s.Add(ctx, "aaaa", Ref{Kind: FactoryJobKind, ID: "17", Factory: "factory-a"}); err != nil {
		t.Fatal(err)
	}
}

func TestPartASchemaTwoMigrationRollsBackWithFailedWrite(t *testing.T) {
	path := writeSchemaTwo(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := openTestStore(t, path)
	if err := s.Rename(context.Background(), "aaaa", "Nested"); err != ErrNameTaken {
		t.Fatalf("want name conflict: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || userVersion(t, s) != 2 {
		t.Fatal("failed first write changed file")
	}
}

func TestPartAFactoryJobRefValidation(t *testing.T) {
	for _, tc := range []struct {
		ref Ref
		ok  bool
	}{
		{Ref{Kind: FactoryJobKind, ID: "17", Factory: "f"}, true},
		{Ref{Kind: FactoryJobKind, ID: "0", Factory: "f"}, false},
		{Ref{Kind: FactoryJobKind, ID: "017", Factory: "f"}, false},
		{Ref{Kind: FactoryJobKind, ID: "+17", Factory: "f"}, false},
		{Ref{Kind: FactoryJobKind, ID: "17"}, false},
		{Ref{Kind: FactoryJobKind, ID: "17", Factory: " f"}, false},
		{Ref{Kind: FactoryJobKind, ID: "17", Factory: "f", SessionID: "chat"}, false},
		{Ref{Kind: ConversationKind, ID: "chat", Factory: "f"}, false},
	} {
		if err := tc.ref.Validate(); (err == nil) != tc.ok {
			t.Errorf("%+v: %v", tc.ref, err)
		}
	}
}

// Part A (a): deleted sequence numbers remain retired across both migrations,
// preserving the monotonic keys used by existing listing and member cursors.
func TestPartAMigrationPreservesSequenceHighWatermarks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture func(*testing.T) string
	}{
		{"schema 1", writeLegacyStore}, {"schema 2", writeSchemaTwo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t, tc.fixture(t))
			ctx := context.Background()
			if _, err := s.db.Exec(`INSERT INTO collections(seq,id,name) VALUES (100,'gone','Gone');
DELETE FROM collections WHERE id='gone';
INSERT INTO memberships(seq,collection_id,kind,ref_id,session_id) VALUES (200,'aaaa','conversation','gone','');
DELETE FROM memberships WHERE ref_id='gone';`); err != nil {
				t.Fatal(err)
			}
			c, err := s.Create(ctx, "After")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Add(ctx, c.ID, Ref{Kind: FactoryJobKind, ID: "1", Factory: "f"}); err != nil {
				t.Fatal(err)
			}
			var collectionSeq, memberSeq int64
			if err := s.db.QueryRow("SELECT seq FROM collections WHERE id=?", c.ID).Scan(&collectionSeq); err != nil {
				t.Fatal(err)
			}
			if err := s.db.QueryRow("SELECT seq FROM memberships WHERE collection_id=?", c.ID).Scan(&memberSeq); err != nil {
				t.Fatal(err)
			}
			if collectionSeq <= 100 || memberSeq <= 200 {
				t.Fatalf("reused retired sequences: collection %d member %d", collectionSeq, memberSeq)
			}
		})
	}
}

// WP-F1 (b), Storage: schema 3 is amended in place; either missing deletion
// column identifies an obsolete schema-3 file and must fail at Open.
func TestWorkstreamSchemaThreeRequiresDeletionColumns(t *testing.T) {
	for _, column := range []string{"deleted_at", "deleted_root"} {
		t.Run(column, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "obsolete.db")
			s := openTestStore(t, path)
			createTestCollection(t, s, "Schema three")
			workstreamSchemaThreeFixture(t, s)
			if userVersion(t, s) != 3 {
				t.Fatal("fixture is not schema 3")
			}
			if _, err := s.db.Exec("ALTER TABLE workstream_pages DROP COLUMN " + column); err != nil {
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
			if err == nil || !strings.Contains(err.Error(), column) {
				t.Fatalf("obsolete schema 3 opened without naming %s: %v", column, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatalf("refusal changed obsolete schema 3: %v", err)
			}
		})
	}
}

func workstreamSchemaTwoWriter(t *testing.T, path string) *sql.Conn {
	t.Helper()
	db, err := sql.Open("sqlite", sqlitefile.URI(path, "_pragma=foreign_keys(1)"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var version, foreignKeys int
	if err := conn.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatalf("older writer did not open schema 2: %d, %v", version, err)
	}
	if err := conn.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("older writer has foreign keys disabled: %d, %v", foreignKeys, err)
	}
	return conn
}

func workstreamStoredRows(t *testing.T, db *sql.DB, table string) [][]any {
	t.Helper()
	rows, err := db.Query("SELECT * FROM " + table + " ORDER BY 1,2")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		out = append(out, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// E-B1-a: a connection opened before migration must not cascade away a
// workstream, even when its writer bypasses the current Store.Delete guard.
func TestStabEB1AnOlderWriterCannotDeleteAWorkstream(t *testing.T) {
	ctx := context.Background()
	path := writeSchemaTwo(t)
	older := workstreamSchemaTwoWriter(t, path)
	s := openTestStore(t, path)
	seedWorkstream(t, s, "protected", "Protected")
	if userVersion(t, s) != 4 {
		t.Fatal("workstream creation did not migrate schema 2")
	}
	// The domain store imports workspace, so this fixture uses its shared SQL
	// transaction to create the dependent rows without an import cycle.
	if err := s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO memberships(collection_id,kind,ref_id,session_id,factory) VALUES
 ('protected','conversation','agent-protected','',''),('protected','factory_job','7','','f');
INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection)
 VALUES ('aaaa','collection','protected','','protected');
INSERT INTO workstream_pages(id,workstream_id,title,owner,position,version,updated_at,updated_by,doc)
 VALUES ('page','protected','Notes','you',0,2,200,'you','{"type":"doc","content":[]}');
INSERT INTO workstream_page_versions(page_id,version,saved_at,saved_by,title,doc,bytes) VALUES
 ('page',1,100,'you','Notes','{"type":"doc","content":[]}',27),
 ('page',2,200,'you','Notes','{"type":"doc","content":[]}',27);
INSERT INTO workstream_records(id,workstream_id,kind,text,state,actor,created_at)
 VALUES ('record','protected','decision','Keep this decision','kept','you',100);
INSERT INTO workstream_goaheads(id,workstream_id,scope,wave,cap_usd,ends_at,state,granted_at)
 VALUES ('grant','protected','wave',1,1,200,'active',100);
INSERT INTO workstream_plans(workstream_id,version,updated_at,updated_by,milestones,waves)
 VALUES ('protected',1,100,'you','[]','[]');
`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tables := []struct {
		name  string
		count int
	}{
		{"collections", 3}, {"memberships", 6}, {"workstreams", 1}, {"workstream_jobs", 1},
		{"workstream_pages", 1}, {"workstream_page_versions", 2}, {"workstream_records", 1},
		{"workstream_goaheads", 1}, {"workstream_plans", 1},
	}
	before := make(map[string][][]any, len(tables))
	for _, table := range tables {
		before[table.name] = workstreamStoredRows(t, s.db, table.name)
		if len(before[table.name]) != table.count {
			t.Fatalf("%s fixture has %d rows, want %d", table.name, len(before[table.name]), table.count)
		}
	}
	if _, err := older.ExecContext(ctx, "DELETE FROM collections WHERE id=?", "protected"); err == nil || !strings.Contains(err.Error(), ErrWorkstreamDelete.Error()) {
		t.Fatalf("older writer was not refused with the workstream explanation: %v", err)
	}
	for _, table := range tables {
		if after := workstreamStoredRows(t, s.db, table.name); !reflect.DeepEqual(after, before[table.name]) {
			t.Errorf("refused delete changed %s: before %v, after %v", table.name, before[table.name], after)
		}
	}
}

// E-B1-b: the SQL guard preserves plain collection deletion and its cascades,
// and Store.Delete still returns the friendly workstream refusal.
func TestStabEB1PlainCollectionsStillDelete(t *testing.T) {
	ctx := context.Background()
	path := writeSchemaTwo(t)
	older := workstreamSchemaTwoWriter(t, path)
	s := openTestStore(t, path)
	seedWorkstream(t, s, "protected", "Protected")
	for _, writer := range []string{"older writer", "store"} {
		t.Run(writer, func(t *testing.T) {
			plain := createTestCollection(t, s, "Plain "+writer)
			if err := s.Add(ctx, plain.ID, Ref{Kind: ConversationKind, ID: "plain-chat"}); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(ctx, "aaaa", Ref{Kind: CollectionKind, ID: plain.ID}); err != nil {
				t.Fatal(err)
			}
			var members int
			if err := s.db.QueryRow("SELECT count(*) FROM memberships WHERE collection_id=? OR target_collection=?", plain.ID, plain.ID).Scan(&members); err != nil || members != 2 {
				t.Fatalf("plain membership fixture: %d, %v", members, err)
			}
			if writer == "older writer" {
				result, err := older.ExecContext(ctx, "DELETE FROM collections WHERE id=?", plain.ID)
				if err != nil {
					t.Fatal(err)
				}
				if n, err := result.RowsAffected(); err != nil || n != 1 {
					t.Fatalf("older writer deleted %d collections: %v", n, err)
				}
			} else if parents, err := s.Delete(ctx, plain.ID); err != nil || !reflect.DeepEqual(parents, []string{"aaaa"}) {
				t.Fatalf("plain Store.Delete: %v, %v", parents, err)
			}
			var collections int
			if err := s.db.QueryRow("SELECT count(*) FROM collections WHERE id=?", plain.ID).Scan(&collections); err != nil || collections != 0 {
				t.Fatalf("plain collection survived: %d, %v", collections, err)
			}
			if err := s.db.QueryRow("SELECT count(*) FROM memberships WHERE collection_id=? OR target_collection=?", plain.ID, plain.ID).Scan(&members); err != nil || members != 0 {
				t.Fatalf("plain memberships did not cascade: %d, %v", members, err)
			}
		})
	}
	if _, err := s.Delete(ctx, "protected"); err != ErrWorkstreamDelete {
		t.Fatalf("Store.Delete lost its friendly workstream refusal: %v", err)
	}
}

// E-B1-c: earlier schema-3 files remain readable without modification and gain
// the database guard in the transaction of their next write.
func TestStabEB1AnEarlierSchemaThreeFileGainsTheGuardOnItsNextWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "earlier.db")
	fixture := openTestStore(t, path)
	plain := createTestCollection(t, fixture, "Earlier")
	workstreamSchemaThreeFixture(t, fixture)
	if _, err := fixture.db.Exec("DROP TRIGGER IF EXISTS workstream_collection_keep"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := openTestStore(t, path)
	if userVersion(t, s) != 3 {
		t.Fatal("fixture is not schema 3")
	}
	list, err := s.List(ctx, nil)
	if err != nil || len(list) != 1 || list[0].ID != plain.ID {
		t.Fatalf("earlier schema-3 read: %+v, %v", list, err)
	}
	var triggers int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name='workstream_collection_keep'").Scan(&triggers); err != nil || triggers != 0 {
		t.Fatalf("read installed the guard: %d, %v", triggers, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("open or read changed earlier schema 3: %v", err)
	}
	if err := s.Rename(ctx, plain.ID, "Updated"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name='workstream_collection_keep'").Scan(&triggers); err != nil || triggers != 1 {
		t.Fatalf("first write did not install the guard: %d, %v", triggers, err)
	}
	if err := s.Rename(ctx, plain.ID, "Updated again"); err != nil {
		t.Fatalf("installing the guard again was not idempotent: %v", err)
	}
}
