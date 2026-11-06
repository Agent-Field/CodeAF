package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
)

// Contract (c): frozen SQL from the schema-6 writer and the schema-2 merge base.
// These fixtures deliberately do not call the current schema builders.
const schemaSevenTwoDDL = `
CREATE TABLE collections (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL,
 created_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE memberships (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact','crew','process')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES collections(id) ON DELETE CASCADE,
 added_at INTEGER NOT NULL DEFAULT 0,
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id)
);

CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id);
CREATE INDEX memberships_target ON memberships(target_collection);
`

const schemaSevenCoreDDL = `
CREATE TABLE collections (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL,
 created_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE memberships (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact','crew','process','factory_job')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES collections(id) ON DELETE CASCADE,
 added_at INTEGER NOT NULL DEFAULT 0,
 factory TEXT NOT NULL DEFAULT '',
 CHECK ((kind='factory_job' AND length(factory)>0) OR (kind!='factory_job' AND factory='')),
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id,factory)
);

CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id,factory);
CREATE INDEX memberships_target ON memberships(target_collection);

`

const schemaSevenThreeDDL = `CREATE TABLE workstreams (
 id TEXT PRIMARY KEY REFERENCES collections(id) ON DELETE CASCADE,
 why TEXT NOT NULL DEFAULT '', origin_chat_id TEXT NOT NULL DEFAULT '', agent_id TEXT NOT NULL,
 autonomy TEXT NOT NULL DEFAULT 'propose' CHECK(autonomy IN ('propose','small','off')),
 wake_morning INTEGER NOT NULL DEFAULT 0, wake_related_issues INTEGER NOT NULL DEFAULT 0,
 budget_usd REAL CHECK(budget_usd IS NULL OR budget_usd>0),
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, started_at INTEGER, done_at INTEGER,
 how_it_went TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX workstreams_client ON workstreams(client_id) WHERE client_id!='';
CREATE TABLE workstream_jobs (
 member_seq INTEGER PRIMARY KEY REFERENCES memberships(seq) ON DELETE CASCADE,
 workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 factory TEXT NOT NULL, job TEXT NOT NULL
);
CREATE UNIQUE INDEX workstream_job_owner ON workstream_jobs(factory,job);
CREATE TRIGGER workstream_job_insert AFTER INSERT ON memberships
 WHEN NEW.kind='factory_job' AND EXISTS(SELECT 1 FROM workstreams WHERE id=NEW.collection_id)
 BEGIN INSERT INTO workstream_jobs VALUES(NEW.seq,NEW.collection_id,NEW.factory,NEW.ref_id); END;
CREATE TRIGGER workstream_job_update AFTER UPDATE ON memberships
 BEGIN
 DELETE FROM workstream_jobs WHERE member_seq=OLD.seq;
 INSERT INTO workstream_jobs SELECT NEW.seq,NEW.collection_id,NEW.factory,NEW.ref_id
 WHERE NEW.kind='factory_job' AND EXISTS(SELECT 1 FROM workstreams WHERE id=NEW.collection_id);
 END;
CREATE TRIGGER workstream_job_promote AFTER INSERT ON workstreams
 BEGIN INSERT INTO workstream_jobs SELECT seq,collection_id,factory,ref_id FROM memberships
 WHERE collection_id=NEW.id AND kind='factory_job'; END;
CREATE TABLE workstream_pages (
 id TEXT PRIMARY KEY, workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 parent_id TEXT REFERENCES workstream_pages(id) ON DELETE CASCADE,
 title TEXT NOT NULL, owner TEXT NOT NULL CHECK(owner IN ('you','agent')),
 read_by_jobs INTEGER NOT NULL DEFAULT 0, position INTEGER NOT NULL CHECK(position>=0),
 version INTEGER NOT NULL CHECK(version>0), updated_at INTEGER NOT NULL,
 updated_by TEXT NOT NULL CHECK(updated_by IN ('you','agent')), updated_from TEXT,
 doc TEXT NOT NULL, overview INTEGER NOT NULL DEFAULT 0 CHECK(overview IN (0,1)),
 deleted_at INTEGER, deleted_root TEXT
);
CREATE INDEX workstream_page_tree ON workstream_pages(workstream_id,parent_id,position);
CREATE UNIQUE INDEX workstream_overview ON workstream_pages(workstream_id) WHERE overview=1;
CREATE TABLE workstream_page_versions (
 page_id TEXT NOT NULL REFERENCES workstream_pages(id) ON DELETE CASCADE,
 version INTEGER NOT NULL, saved_at INTEGER NOT NULL, saved_by TEXT NOT NULL CHECK(saved_by IN ('you','agent')),
 source TEXT, title TEXT NOT NULL, doc TEXT NOT NULL, bytes INTEGER NOT NULL,
 PRIMARY KEY(page_id,version)
);
CREATE TABLE workstream_records (
 id TEXT PRIMARY KEY, workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('decision','question','goahead')), text TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('proposed','kept','dropped','superseded')),
 actor TEXT NOT NULL CHECK(actor IN ('you','agent')), source TEXT, created_at INTEGER NOT NULL,
 decided_at INTEGER, supersedes TEXT REFERENCES workstream_records(id), superseded_by TEXT REFERENCES workstream_records(id),
 goahead TEXT, goahead_id TEXT
);
CREATE INDEX workstream_record_list ON workstream_records(workstream_id,created_at);
CREATE TABLE workstream_goaheads (
 id TEXT PRIMARY KEY, workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 scope TEXT NOT NULL CHECK(scope IN ('job','wave','milestone')),
 factory TEXT, job INTEGER, wave INTEGER, milestone_id TEXT,
 cap_usd REAL NOT NULL CHECK(cap_usd>0 AND cap_usd<=10000), ends_at INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('active','ended')),
 end_reason TEXT CHECK(end_reason IN ('expired','revoked','workstream_done')),
 granted_at INTEGER NOT NULL, ended_at INTEGER,
 CHECK((scope='job' AND factory IS NOT NULL AND length(factory)>0 AND job IS NOT NULL AND job>0 AND wave IS NULL AND milestone_id IS NULL)
 OR (scope='wave' AND wave IS NOT NULL AND wave>0 AND factory IS NULL AND job IS NULL AND milestone_id IS NULL)
 OR (scope='milestone' AND milestone_id IS NOT NULL AND length(milestone_id)>0 AND factory IS NULL AND job IS NULL AND wave IS NULL)),
 CHECK(ends_at>granted_at AND ends_at<=granted_at+7776000000)
);
CREATE INDEX workstream_goahead_list ON workstream_goaheads(workstream_id,state);
CREATE TABLE workstream_plans (
 workstream_id TEXT PRIMARY KEY REFERENCES workstreams(id) ON DELETE CASCADE,
 version INTEGER NOT NULL CHECK(version>0), updated_at INTEGER NOT NULL,
 updated_by TEXT NOT NULL CHECK(updated_by IN ('you','agent')),
 milestones TEXT NOT NULL, waves TEXT NOT NULL
);

CREATE TRIGGER IF NOT EXISTS workstream_collection_keep BEFORE DELETE ON collections
 WHEN EXISTS(SELECT 1 FROM workstreams WHERE id=OLD.id)
 BEGIN SELECT RAISE(ABORT,'mark a workstream done instead'); END;

`

const schemaSevenFourDDL = `CREATE TABLE workstream_wakes (
 id TEXT PRIMARY KEY, workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 event_key TEXT NOT NULL, event_json TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('running','settled','failed')),
 summary TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', cost_usd REAL,
 started_at INTEGER NOT NULL, settled_at INTEGER, dismissed_at INTEGER,
 UNIQUE(workstream_id,event_key)
);
CREATE INDEX workstream_wake_list ON workstream_wakes(workstream_id,started_at);
CREATE TABLE workstream_wake_items (
 id TEXT PRIMARY KEY, wake_id TEXT NOT NULL REFERENCES workstream_wakes(id) ON DELETE CASCADE,
 position INTEGER NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('move','draft','page','decision','question')),
 state TEXT NOT NULL CHECK(state IN ('applied','undone','proposed','dismissed')),
 why TEXT NOT NULL DEFAULT '', item_json TEXT NOT NULL, undo_json TEXT,
 note TEXT NOT NULL DEFAULT '', applied_at INTEGER, undone_at INTEGER
);
CREATE INDEX workstream_wake_item_list ON workstream_wake_items(wake_id,position);

`

const schemaSevenFiveDDL = `CREATE TABLE workstream_sources (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('file','repo','issue','link','kept')),
 target TEXT NOT NULL,
 title TEXT NOT NULL,
 path TEXT, repo TEXT, number INTEGER, url TEXT, ref TEXT, why TEXT,
 read_by_jobs INTEGER NOT NULL DEFAULT 0,
 proposed INTEGER NOT NULL DEFAULT 0,
 added_by TEXT NOT NULL CHECK(added_by IN ('you','agent')),
 added_at INTEGER NOT NULL,
 UNIQUE(workstream_id,kind,target)
);
CREATE INDEX workstream_sources_order ON workstream_sources(workstream_id,seq);

`

const schemaSevenSixDDL = `CREATE TABLE workstream_suggestions (
 id TEXT PRIMARY KEY, workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 page_id TEXT NOT NULL REFERENCES workstream_pages(id) ON DELETE CASCADE,
 base_version INTEGER NOT NULL, why TEXT NOT NULL, actor TEXT NOT NULL, source TEXT,
 created_at INTEGER NOT NULL
);
CREATE INDEX workstream_suggestion_page ON workstream_suggestions(page_id,created_at);
CREATE TABLE workstream_suggestion_edits (
 suggestion_id TEXT NOT NULL REFERENCES workstream_suggestions(id) ON DELETE CASCADE,
 id TEXT NOT NULL, position INTEGER NOT NULL, block TEXT NOT NULL, at_offset INTEGER NOT NULL,
 del TEXT NOT NULL, ins TEXT NOT NULL, block_text TEXT NOT NULL, why TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('proposed','kept','dropped')), decided_at INTEGER,
 PRIMARY KEY(suggestion_id,id)
);
`

const schemaSevenRows = `
INSERT INTO collections(seq,id,name,created_at) VALUES
 (4,'root','Root',1234),(9,'nested','Nested',2345),(12,'former','Former owner',3456);
INSERT INTO memberships(seq,collection_id,kind,ref_id,session_id,target_collection,added_at) VALUES
 (2,'root','conversation','chat','',NULL,4567),
 (6,'root','task','3','chat',NULL,5678),
 (11,'root','collection','nested','','nested',6789),
 (15,'nested','collection','former','','former',7000),
 (16,'nested','crew','crew','',NULL,7100),
 (17,'nested','process','chat:p1','chat',NULL,7200),
 (18,'former','conversation','agent','',NULL,7300),
 (19,'nested','artifact','/tmp/report.md','',NULL,7400),
 (20,'nested','standing','retired','',NULL,7500);
INSERT INTO collections(seq,id,name) VALUES (100,'gone','Deleted');
DELETE FROM collections WHERE id='gone';
INSERT INTO memberships(seq,collection_id,kind,ref_id,session_id) VALUES (200,'root','conversation','gone','');
DELETE FROM memberships WHERE ref_id='gone';
`

const schemaSevenThreeRows = `
INSERT INTO workstreams(id,agent_id,created_at,updated_at,client_id) VALUES ('former','agent',3456,4000,'client');
INSERT INTO memberships(seq,collection_id,kind,ref_id,session_id,added_at,factory) VALUES
 (21,'former','factory_job','7','',7600,'factory-a'),(22,'root','factory_job','7','',7700,'factory-a');
INSERT INTO workstream_pages(id,workstream_id,title,owner,position,version,updated_at,updated_by,doc,overview)
 VALUES ('page','former','Overview','you',0,1,4000,'you','{}',1);
INSERT INTO workstream_page_versions(page_id,version,saved_at,saved_by,title,doc,bytes)
 VALUES ('page',1,4000,'you','Overview','{}',2);
INSERT INTO workstream_records(id,workstream_id,kind,text,state,actor,created_at)
 VALUES ('record','former','decision','Keep','kept','you',4000);
INSERT INTO workstream_goaheads(id,workstream_id,scope,wave,cap_usd,ends_at,state,granted_at)
 VALUES ('grant','former','wave',1,1,5000,'active',4000);
INSERT INTO workstream_plans(workstream_id,version,updated_at,updated_by,milestones,waves)
 VALUES ('former',1,4000,'you','[]','[]');
`
const schemaSevenFourRows = `
INSERT INTO workstream_wakes(id,workstream_id,event_key,event_json,state,started_at)
 VALUES ('wake','former','morning:2026-11-03','{}','settled',4000);
INSERT INTO workstream_wake_items(id,wake_id,position,kind,state,item_json)
 VALUES ('item','wake',0,'page','applied','{}');
`
const schemaSevenFiveRows = `
INSERT INTO workstream_sources(id,workstream_id,kind,target,title,added_by,added_at)
 VALUES ('source','former','link','https://example.com','Example','you',4000);
`
const schemaSevenSixRows = `
INSERT INTO workstream_suggestions(id,workstream_id,page_id,base_version,why,actor,created_at)
 VALUES ('suggestion','former','page',1,'Clarify','agent',4000);
INSERT INTO workstream_suggestion_edits(suggestion_id,id,position,block,at_offset,del,ins,block_text,why,state)
 VALUES ('suggestion','edit',0,'[0]',0,'Old','New','Old','Clearer','proposed');
`

func schemaSevenFixture(t *testing.T, version int) string {
	t.Helper()
	ddl, rows := schemaSevenTwoDDL, schemaSevenRows
	if version >= 3 {
		ddl = schemaSevenCoreDDL + schemaSevenThreeDDL
		rows += schemaSevenThreeRows
	}
	if version >= 4 {
		ddl += schemaSevenFourDDL
		rows += schemaSevenFourRows
	}
	if version >= 5 {
		ddl += schemaSevenFiveDDL
		rows += schemaSevenFiveRows
	}
	if version >= 6 {
		ddl += schemaSevenSixDDL
		rows += schemaSevenSixRows
	}
	path := filepath.Join(t.TempDir(), "collections.db")
	db, err := sql.Open("sqlite", sqlitefile.URI(path, "_pragma=foreign_keys(1)"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ddl + rows + fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d;", applicationID, version)); err != nil {
		t.Fatal(err)
	}
	return path
}

func schemaSevenFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func schemaSevenQuery(t *testing.T, db *sql.DB, query string) [][]any {
	t.Helper()
	rows, err := db.Query(query)
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
		values, pointers := make([]any, len(columns)), make([]any, len(columns))
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

func schemaSevenSchema(t *testing.T, s *Store) [][]any {
	t.Helper()
	schema := schemaSevenQuery(t, s.db, "SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name")
	// ALTER TABLE quotes renamed table identifiers; the logical shape is identical.
	for _, row := range schema {
		if text, ok := row[3].(string); ok {
			row[3] = strings.ReplaceAll(text, `"`, "")
		}
	}
	return schema
}

func schemaSevenSequences(t *testing.T, s *Store) [][]any {
	t.Helper()
	return schemaSevenQuery(t, s.db, "SELECT name,seq FROM sqlite_sequence WHERE name IN ('collections','memberships') ORDER BY name")
}

// Contract (c), G2: every supported predecessor reads without writing, migrates
// on the requested successful write, preserves every row and cursor high-water
// mark, permits former-owner deletion, and performs no DDL on subsequent writes.
func TestSchemaSevenMigratesAndPreservesGenericData(t *testing.T) {
	for _, version := range []int{2, 3, 4, 5, 6} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ctx := context.Background()
			path := schemaSevenFixture(t, version)
			before := schemaSevenFile(t, path)
			s := openTestStore(t, path)
			wantCollections := schemaSevenQuery(t, s.db, "SELECT seq,id,name,created_at FROM collections ORDER BY seq")
			projection := "seq,collection_id,kind,ref_id,session_id,target_collection,added_at,''"
			if version >= 3 {
				projection = "seq,collection_id,kind,ref_id,session_id,target_collection,added_at,factory"
			}
			wantMembers := schemaSevenQuery(t, s.db, "SELECT "+projection+" FROM memberships ORDER BY seq")
			wantSequences := schemaSevenSequences(t, s)
			if !reflect.DeepEqual(wantSequences, [][]any{{"collections", int64(100)}, {"memberships", int64(200)}}) {
				t.Fatalf("fixture lacks deleted-row gaps: %v", wantSequences)
			}
			if version == 6 {
				// Every frozen legacy table contains data; cleanup is exercised with foreign keys on.
				for _, row := range schemaSevenQuery(t, s.db, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT IN ('collections','memberships','sqlite_sequence')") {
					count := schemaSevenQuery(t, s.db, "SELECT count(*) FROM "+row[0].(string))
					if count[0][0].(int64) == 0 {
						t.Fatalf("empty fixture table: %s", row[0])
					}
				}
			}
			collections, err := s.Collections(ctx)
			if err != nil || len(collections) != 3 {
				t.Fatalf("collections: %v, %v", collections, err)
			}
			summaries, err := s.List(ctx, nil)
			if err != nil || len(summaries) != 3 || summaries[0].CreatedAt != 1234 {
				t.Fatalf("summaries: %+v, %v", summaries, err)
			}
			wantRefs := map[string][]Ref{}
			wantTrees := map[string]Tree{}
			for _, c := range collections {
				refs, err := s.Members(ctx, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				wantRefs[c.ID] = refs
				tree, err := s.Tree(ctx, c.ID, 3, 200, "")
				if err != nil {
					t.Fatal(err)
				}
				wantTrees[c.ID] = tree
				expected := []Ref{}
				for _, row := range wantMembers {
					if row[1] == c.ID {
						expected = append(expected, Ref{Kind: Kind(row[2].(string)), ID: row[3].(string), SessionID: row[4].(string), Factory: row[7].(string)})
					}
				}
				if !reflect.DeepEqual(refs, expected) {
					t.Fatalf("read refs: %v, want %v", refs, expected)
				}
			}
			if version >= 3 {
				parents, err := s.CollectionsFor(ctx, Ref{Kind: FactoryJobKind, ID: "7", Factory: "factory-a"})
				if err != nil || !reflect.DeepEqual(parents, []Collection{{ID: "root", Name: "Root"}, {ID: "former", Name: "Former owner"}}) {
					t.Fatalf("shared job: %v, %v", parents, err)
				}
			}
			if userVersion(t, s) != version || !reflect.DeepEqual(schemaSevenFile(t, path), before) {
				t.Fatal("open/read modified predecessor")
			}
			// Renaming to the existing name is still a successful public write and
			// lets us compare every original row without editing its expected values.
			if err := s.Rename(ctx, "root", "Root"); err != nil {
				t.Fatal(err)
			}
			if userVersion(t, s) != 7 {
				t.Fatal("first write did not install schema 7")
			}
			if got := schemaSevenQuery(t, s.db, "SELECT seq,id,name,created_at FROM collections ORDER BY seq"); !reflect.DeepEqual(got, wantCollections) {
				t.Fatalf("collections changed: %v", got)
			}
			if got := schemaSevenQuery(t, s.db, "SELECT seq,collection_id,kind,ref_id,session_id,target_collection,added_at,factory FROM memberships ORDER BY seq"); !reflect.DeepEqual(got, wantMembers) {
				t.Fatalf("memberships changed: %v", got)
			}
			if got := schemaSevenSequences(t, s); !reflect.DeepEqual(got, wantSequences) {
				t.Fatalf("high-water marks changed: %v", got)
			}
			if got, err := s.List(ctx, nil); err != nil || !reflect.DeepEqual(got, summaries) {
				t.Fatalf("summaries changed: %v, %v", got, err)
			}
			for _, c := range collections {
				if got, err := s.Members(ctx, c.ID); err != nil || !reflect.DeepEqual(got, wantRefs[c.ID]) {
					t.Fatalf("refs changed: %v, %v", got, err)
				}
				if got, err := s.Tree(ctx, c.ID, 3, 200, ""); err != nil || !reflect.DeepEqual(got, wantTrees[c.ID]) {
					t.Fatalf("tree changed: %v, %v", got, err)
				}
			}
			fresh := openTestStore(t, filepath.Join(t.TempDir(), "fresh.db"))
			if err := fresh.ensureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			if userVersion(t, fresh) != 7 {
				t.Fatal("blank file did not initialize as schema 7")
			}
			if got, want := schemaSevenSchema(t, s), schemaSevenSchema(t, fresh); !reflect.DeepEqual(got, want) {
				t.Fatalf("migration left legacy objects or wrong schema: %v; want %v", got, want)
			}
			schema := schemaSevenSchema(t, s)
			var schemaCookie int
			if err := s.db.QueryRow("PRAGMA schema_version").Scan(&schemaCookie); err != nil {
				t.Fatal(err)
			}
			if err := s.Rename(ctx, "root", "Root again"); err != nil {
				t.Fatal(err)
			}
			var afterCookie int
			if err := s.db.QueryRow("PRAGMA schema_version").Scan(&afterCookie); err != nil {
				t.Fatal(err)
			}
			if got := schemaSevenSchema(t, s); !reflect.DeepEqual(got, schema) || afterCookie != schemaCookie {
				t.Fatal("second write ran DDL")
			}
			// IF EXISTS makes the predecessor cleanup safe when repeated or partially absent.
			if _, err := s.db.Exec(schemaSevenCleanup); err != nil {
				t.Fatal(err)
			}
			if got := schemaSevenSchema(t, s); !reflect.DeepEqual(got, schema) {
				t.Fatal("repeated cleanup changed schema")
			}
			if _, err := s.Delete(ctx, "former"); err != nil {
				t.Fatalf("former owner is not deletable: %v", err)
			}
			c, err := s.Create(ctx, "After")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Add(ctx, c.ID, Ref{Kind: FactoryJobKind, ID: "7", Factory: "factory-a"}); err != nil {
				t.Fatal(err)
			}
			var collectionSeq, memberSeq int
			if err := s.db.QueryRow("SELECT seq FROM collections WHERE id=?", c.ID).Scan(&collectionSeq); err != nil {
				t.Fatal(err)
			}
			if err := s.db.QueryRow("SELECT seq FROM memberships WHERE collection_id=?", c.ID).Scan(&memberSeq); err != nil {
				t.Fatal(err)
			}
			if collectionSeq <= 100 || memberSeq <= 200 {
				t.Fatalf("reused retired sequences: %d, %d", collectionSeq, memberSeq)
			}
		})
	}
}

// Contract (c): a failed requested write rolls back DDL, data and version bytes.
func TestSchemaSevenFailedFirstWriteLeavesPredecessorUntouched(t *testing.T) {
	for _, version := range []int{2, 6} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := schemaSevenFixture(t, version)
			before := schemaSevenFile(t, path)
			s := openTestStore(t, path)
			schema := schemaSevenSchema(t, s)
			if err := s.Rename(context.Background(), "root", "Nested"); err != ErrNameTaken {
				t.Fatalf("expected name conflict: %v", err)
			}
			if userVersion(t, s) != version || !reflect.DeepEqual(schemaSevenFile(t, path), before) || !reflect.DeepEqual(schemaSevenSchema(t, s), schema) {
				t.Fatal("failed write migrated predecessor")
			}
		})
	}
}

// Contract (c): future versions and foreign application IDs are refused without
// modifying the file, including a file that changed after this handle opened.
func TestSchemaSevenRefusesUnknownDatabases(t *testing.T) {
	for _, pragma := range []string{"PRAGMA user_version=8", "PRAGMA application_id=12345"} {
		t.Run(pragma, func(t *testing.T) {
			path := schemaSevenFixture(t, 6)
			s := openTestStore(t, path)
			if _, err := s.db.Exec(pragma); err != nil {
				t.Fatal(err)
			}
			before := schemaSevenFile(t, path)
			opened, err := Open(path)
			if opened != nil {
				opened.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "unsupported collections database") {
				t.Fatalf("open accepted unknown database: %v", err)
			}
			if _, err := s.Collections(context.Background()); err == nil || !strings.Contains(err.Error(), "unsupported collections database") {
				t.Fatalf("read accepted unknown database: %v", err)
			}
			if err := s.Rename(context.Background(), "root", "Changed"); err == nil || !strings.Contains(err.Error(), "unsupported collections database") {
				t.Fatalf("write accepted unknown database: %v", err)
			}
			if !reflect.DeepEqual(schemaSevenFile(t, path), before) {
				t.Fatal("refusal changed file")
			}
		})
	}
}

// Contract (c): schemas 3–6 are recognized by the generic tables alone.
func TestSchemaSevenReadsCleanPredecessorShapes(t *testing.T) {
	for _, version := range []int{3, 4, 5, 6} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "clean.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(schemaSevenCoreDDL + schemaSevenRows + fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d;", applicationID, version)); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			before := schemaSevenFile(t, path)
			s := openTestStore(t, path)
			if got, err := s.Members(context.Background(), "root"); err != nil || len(got) != 3 {
				t.Fatalf("clean predecessor: %v, %v", got, err)
			}
			if !reflect.DeepEqual(schemaSevenFile(t, path), before) {
				t.Fatal("read changed clean predecessor")
			}
			if err := s.Rename(context.Background(), "root", "Root"); err != nil {
				t.Fatal(err)
			}
			if userVersion(t, s) != 7 {
				t.Fatal("clean predecessor not upgraded")
			}
		})
	}
}
