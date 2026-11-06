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
	if userVersion(t, s) != schemaVersion {
		t.Fatal("first write did not migrate to the current schema")
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
