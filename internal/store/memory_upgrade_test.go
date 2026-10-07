package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

// ── opening a database written before owners existed ─────────────────────────
//
// THE RELEASED BUILD'S memories TABLE HAS NO owner COLUMN. Every database on
// disk today was written by it, so the first open of the owner-aware build is
// exactly this shape — and it is not the shape the migration helper's own tests
// cover, because those build their fixture through the CURRENT schema and so
// start with the column already in place. That gap is what let a release ship
// whose open died with `no such column: owner`.
//
// The regression below builds the legacy table with its own hand-written DDL,
// on purpose: it is the one description of the old table that cannot drift when
// today's schema changes.

// legacyMemoriesDDL is the memories table and its search view exactly as the
// released v0.7.1 build created them — no `owner` column, no owner index, and a
// materialized (not external-content) memories_fts over memory_id/title/text/tags.
// Every statement is one the old build really ran.
const legacyMemoriesTableDDL = `
CREATE TABLE memories (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    scope       TEXT NOT NULL,
    title       TEXT NOT NULL,
    text        TEXT NOT NULL,
    tags        TEXT NOT NULL DEFAULT '[]',
    status      TEXT NOT NULL DEFAULT 'active',
    use_count   INTEGER NOT NULL DEFAULT 0,
    miss_count  INTEGER NOT NULL DEFAULT 0,
    created_seq    INTEGER NOT NULL,
    updated_seq    INTEGER NOT NULL,
    source_session TEXT NOT NULL DEFAULT '',
    source_seq     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX memories_status_updated ON memories (status, updated_seq DESC);
CREATE INDEX memories_status_scope ON memories (status, scope, updated_seq DESC);
`

// legacyMemoriesFTSDDL is the search view the same schema made: a materialized
// FTS5 table, not external content, over the id and the three searchable fields.
const legacyMemoriesFTSDDL = `
CREATE VIRTUAL TABLE memories_fts USING fts5(
    memory_id UNINDEXED,
    title,
    text,
    tags
);
`

// legacyMemoriesDDL is the two together, which is the ordinary released store.
const legacyMemoriesDDL = legacyMemoriesTableDDL + legacyMemoriesFTSDDL

// legacyMemoryFixture is one row of the pre-owner table, named so the proof can
// say which fact it is looking at.
type legacyMemoryFixture struct {
	id, memoryType, scope, title, text, tags, status string
	useCount, missCount                              int
	createdSeq, updatedSeq                           int64
	sourceSession                                    string
	sourceSeq                                        int64
}

// legacyMemoryRows is the fixture every open-regression test starts from: one
// row of each scope, a superseded row (history that must survive), and a row
// whose scope this build does not know (written by a later or hand-edited
// store, which the migration also has to place).
func legacyMemoryRows() []legacyMemoryFixture {
	return []legacyMemoryFixture{
		{
			id: "mem_user", memoryType: MemoryPreference, scope: MemoryScopeUser,
			title: "Keeps the receipts", text: "Keeps the receipts for the tax year.",
			tags: `["finance"]`, status: MemoryActive, useCount: 3, missCount: 1,
			createdSeq: 11, updatedSeq: 12, sourceSession: "session-old", sourceSeq: 7,
		},
		{
			id: "mem_env", memoryType: MemoryFact, scope: MemoryScopeEnv,
			title: "Runs on the amber box", text: "The build box has no IPv6.",
			tags: `["infra"]`, status: MemoryActive, useCount: 0, missCount: 0,
			createdSeq: 21, updatedSeq: 22, sourceSession: "session-other", sourceSeq: 9,
		},
		{
			id: "mem_project", memoryType: MemoryDecision, scope: MemoryScopeProject,
			title: "Deploys on Tuesdays", text: "Deploys to the amber cluster every Tuesday.",
			tags: `["deploy"]`, status: MemoryActive, useCount: 5, missCount: 2,
			createdSeq: 31, updatedSeq: 32, sourceSession: "session-project", sourceSeq: 4,
		},
		{
			id: "mem_superseded", memoryType: MemoryCorrection, scope: MemoryScopeUser,
			title: "Used the old registry", text: "Used the old registry for releases.",
			tags: `["release"]`, status: MemorySuperseded, useCount: 1, missCount: 0,
			createdSeq: 41, updatedSeq: 42, sourceSession: "session-old", sourceSeq: 8,
		},
		{
			id: "mem_future_scope", memoryType: MemoryFact, scope: "team",
			title: "A scope from a later build", text: "A row whose scope this build does not know.",
			tags: `[]`, status: MemoryActive, useCount: 0, missCount: 0,
			createdSeq: 51, updatedSeq: 52, sourceSession: "", sourceSeq: 0,
		},
	}
}

// writeLegacyDatabase lays down a database in the released shape. It is raw
// SQL on purpose: no store code runs, so nothing today can quietly repair the
// fixture into the shape the test is supposed to prove is upgradeable.
func writeLegacyDatabase(t *testing.T, path string) {
	writeLegacyDatabaseWithFTS(t, path, true)
}

// writeLegacyDatabaseWithFTS is the same fixture with the lexical index left out
// entirely, which is the shape of a store written on a build whose SQLite had no
// FTS5 (or whose view was hand-removed). The memory rows are identical; only the
// search tier differs.
func writeLegacyDatabaseWithFTS(t *testing.T, path string, withFTS bool) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("create legacy database: %v", err)
	}
	defer raw.Close()
	ddl := legacyMemoriesTableDDL
	if withFTS {
		ddl = legacyMemoriesDDL
	}
	if _, err := raw.Exec(ddl); err != nil {
		t.Fatalf("create legacy memories table: %v", err)
	}

	for _, row := range legacyMemoryRows() {
		if _, err := raw.Exec(`
			INSERT INTO memories
			    (id, type, scope, title, text, tags, status, use_count, miss_count,
			     created_seq, updated_seq, source_session, source_seq)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.id, row.memoryType, row.scope, row.title, row.text, row.tags, row.status,
			row.useCount, row.missCount, row.createdSeq, row.updatedSeq, row.sourceSession, row.sourceSeq); err != nil {
			t.Fatalf("insert legacy row %s: %v", row.id, err)
		}
		if !withFTS {
			continue
		}
		// The old build kept the search view itself, and this is its own
		// statement (v0.7.1's refreshMemoryFTS), tags punctuation stripped and
		// only active rows present: the fixture must hold the index the way the
		// released build left it, not the way this build would rebuild it.
		if _, err := raw.Exec(`
			INSERT INTO memories_fts (memory_id, title, text, tags)
			SELECT id, title, text, replace(replace(replace(tags, '[', ''), ']', ''), '"', '')
			FROM memories WHERE id = ? AND status = ?`, row.id, MemoryActive); err != nil {
			t.Fatalf("index legacy row %s: %v", row.id, err)
		}
	}
}

// addLegacyOwnerColumn simulates the step a store reached before the crash this
// fix is about: the column exists (with the empty default an ALTER gives it),
// the rows are owned, and the caller decides whether the interrupted upgrade
// got as far as the index.
func addLegacyOwnerColumn(t *testing.T, path string, withIndex bool) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`ALTER TABLE memories ADD COLUMN owner TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatalf("add the owner column: %v", err)
	}
	if _, err := raw.Exec(`UPDATE memories SET owner = 'user' WHERE scope = 'user'`); err != nil {
		t.Fatal(err)
	}
	if withIndex {
		if _, err := raw.Exec(`CREATE INDEX memories_owner_active ON memories (owner, updated_seq DESC) WHERE status = 'active'`); err != nil {
			t.Fatal(err)
		}
	}
}

// memoriesTableColumns is what the open left the table looking like.
func memoriesTableColumns(t *testing.T, graph *Store) map[string]bool {
	t.Helper()
	columns := map[string]bool{}
	rows, err := graph.db.Query(`PRAGMA table_info(memories)`)
	if err != nil {
		t.Fatalf("read memories columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan memories column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read memories columns: %v", err)
	}
	return columns
}

// indexOnMemories answers whether one named index exists over the memories
// table — the assertion that the ordering bug moved out of the schema and into
// a migration cannot be made any other way.
func indexOnMemories(t *testing.T, graph *Store, name string) bool {
	t.Helper()
	var got string
	err := graph.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'memories' AND name = ?`, name).Scan(&got)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("read sqlite_master for %s: %v", name, err)
	}
	return got == name
}

// THE UPGRADE THE RELEASE BROKE. Opening a database from before owners existed
// must add the column, own every row the way its scope proves, and leave the
// notes, their provenance, status and scope exactly as they were.
func TestOpeningADatabaseFromBeforeOwnersKeepsEveryNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	writeLegacyDatabase(t, path)

	graph, err := Open(path)
	if err != nil {
		t.Fatalf("opening a database from before owners existed: %v", err)
	}
	defer graph.Close()

	if !indexOnMemories(t, graph, "memories_owner_active") {
		t.Fatal("the open did not leave the owner index on memories")
	}
	if columns := memoriesTableColumns(t, graph); !columns["owner"] {
		t.Fatalf("the open did not add the owner column: %v", columns)
	}

	// Every row is still there and still says what it said, whatever its status.
	for _, want := range legacyMemoryRows() {
		got, ok, err := graph.MemoryRecord(want.id)
		if err != nil || !ok {
			t.Fatalf("read %s after the upgrade: (%v, %v, %v)", want.id, got, ok, err)
		}
		if got.Title != want.title || got.Text != want.text {
			t.Errorf("%s lost its note: (%q, %q), want (%q, %q)", want.id, got.Title, got.Text, want.title, want.text)
		}
		if got.Type != want.memoryType {
			t.Errorf("%s type = %q, want %q", want.id, got.Type, want.memoryType)
		}
		if got.Scope != want.scope {
			t.Errorf("%s scope = %q, want %q", want.id, got.Scope, want.scope)
		}
		if got.Status != want.status {
			t.Errorf("%s status = %q, want %q", want.id, got.Status, want.status)
		}
		// A quarantined row carries the marker the migration APPENDS; every
		// other row's tags are byte-for-byte what the old build wrote.
		wantTags := legacyTags(t, want.tags)
		if want.scope == MemoryScopeProject {
			wantTags = append(wantTags, OwnerLegacyTag)
		}
		if !reflect.DeepEqual(got.Tags, wantTags) {
			t.Errorf("%s tags = %v, want %v", want.id, got.Tags, wantTags)
		}
		if got.UseCount != want.useCount || got.MissCount != want.missCount {
			t.Errorf("%s counters = (%d, %d), want (%d, %d)", want.id, got.UseCount, got.MissCount, want.useCount, want.missCount)
		}
		if got.CreatedSeq != want.createdSeq || got.UpdatedSeq != want.updatedSeq {
			t.Errorf("%s seqs = (%d, %d), want (%d, %d)", want.id, got.CreatedSeq, got.UpdatedSeq, want.createdSeq, want.updatedSeq)
		}
		if got.SourceSession != want.sourceSession || got.SourceSeq != want.sourceSeq {
			t.Errorf("%s provenance = (%q, %d), want (%q, %d)", want.id, got.SourceSession, got.SourceSeq, want.sourceSession, want.sourceSeq)
		}
	}

	// And the owners are the ones the scopes prove — never a guess.
	for id, want := range map[string]string{
		"mem_user":         OwnerUser,
		"mem_env":          OwnerMachine,
		"mem_project":      OwnerLegacyProject,
		"mem_superseded":   OwnerUser,
		"mem_future_scope": OwnerLegacyProject,
	} {
		got, ok, err := graph.MemoryRecord(id)
		if err != nil || !ok {
			t.Fatalf("read %s for its owner: (%v, %v, %v)", id, got, ok, err)
		}
		if got.Owner != want {
			t.Errorf("%s owner = %q, want %q", id, got.Owner, want)
		}
	}
	// The unprovable project row carries the quarantine marker on purpose: a
	// memory's source is never cleaned off it, and a person can search for the
	// rows that came in blind.
	project, _, err := graph.MemoryRecord("mem_project")
	if err != nil {
		t.Fatal(err)
	}
	if !containsStubTag(project.Tags, OwnerLegacyTag) {
		t.Errorf("mem_project tags = %v, want the legacy marker", project.Tags)
	}
	if !containsStubTag(project.Tags, "deploy") {
		t.Errorf("mem_project tags = %v, want the original tag kept", project.Tags)
	}

	// The active rows are readable through the ordinary public read, and the
	// quarantined note is not: the owner filter is the permission model, and an
	// upgrade must not hand a model a row whose owner nobody could prove.
	visible, err := graph.ListMemories(nil, 0)
	if err != nil {
		t.Fatalf("list memories after the upgrade: %v", err)
	}
	if len(visible) != 4 {
		t.Fatalf("active memories = %v, want the four active rows", memoryIDs(visible))
	}
	own, err := graph.ListMemories([]string{OwnerUser}, 0)
	if err != nil {
		t.Fatalf("list the person's memories: %v", err)
	}
	if len(own) != 1 || own[0].ID != "mem_user" {
		t.Fatalf("the person's shelf = %v, want mem_user alone", memoryIDs(own))
	}
	quarantined, err := graph.ListMemories([]string{OwnerLegacyProject}, 0)
	if err != nil {
		t.Fatalf("list the quarantine: %v", err)
	}
	if len(quarantined) != 2 {
		t.Fatalf("the quarantine = %v, want the unprovable project row and the unknown scope", memoryIDs(quarantined))
	}
	// The lexical tier answered from the rows the old build indexed, under the
	// new owner filter: the note a person would search for is the one they get.
	hits, err := graph.SearchMemories([]string{OwnerUser}, "receipts", 5)
	if err != nil {
		t.Fatalf("search after the upgrade: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "mem_user" {
		t.Fatalf("search for 'receipts' = %v, want the preserved note", memoryIDs(hits))
	}
	if hits[0].Text != "Keeps the receipts for the tax year." {
		t.Fatalf("the search hit lost its text: %q", hits[0].Text)
	}
	// The words the old build indexed are still the words that match, and the
	// owner filter decides which of them a search may return: "amber" is in this
	// machine's row and in the quarantined project row, and neither search sees
	// the other's.
	for _, want := range []struct {
		owners []string
		query  string
		id     string
	}{
		{[]string{OwnerUser}, "finance", "mem_user"},           // the tag the old build wrote, punctuation and all
		{[]string{OwnerMachine}, "amber", "mem_env"},           // this machine's own truth
		{[]string{OwnerLegacyProject}, "amber", "mem_project"}, // the quarantine, to its own owner only
		{[]string{OwnerUser}, "amber", ""},                     // the person has no such line
	} {
		got, err := graph.SearchMemories(want.owners, want.query, 5)
		if err != nil {
			t.Fatalf("search %q under %v: %v", want.query, want.owners, err)
		}
		if want.id == "" {
			if len(got) != 0 {
				t.Fatalf("search %q under %v = %v, want nothing", want.query, want.owners, memoryIDs(got))
			}
			continue
		}
		if len(got) != 1 || got[0].ID != want.id {
			t.Fatalf("search %q under %v = %v, want %s", want.query, want.owners, memoryIDs(got), want.id)
		}
	}
	// A quarantined row is not searchable under any owner a model can name.
	blind, err := graph.SearchMemories([]string{OwnerUser, OwnerMachine, OwnerProject("alpha")}, "Tuesday", 5)
	if err != nil {
		t.Fatalf("search across real owners: %v", err)
	}
	if len(blind) != 0 {
		t.Fatalf("the quarantine answered a model's search: %v", memoryIDs(blind))
	}
}

func TestOpeningADatabaseFromBeforeOwnersIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	writeLegacyDatabase(t, path)

	graph, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	first := map[string]Memory{}
	for _, row := range legacyMemoryRows() {
		got, ok, err := graph.MemoryRecord(row.id)
		if err != nil || !ok {
			t.Fatalf("first read %s: (%v, %v, %v)", row.id, got, ok, err)
		}
		first[row.id] = got
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		graph, err := Open(path)
		if err != nil {
			t.Fatalf("open %d after the upgrade: %v", attempt+2, err)
		}
		for _, row := range legacyMemoryRows() {
			got, ok, err := graph.MemoryRecord(row.id)
			if err != nil || !ok {
				t.Fatalf("read %s on open %d: (%v, %v, %v)", row.id, attempt+2, got, ok, err)
			}
			if !reflect.DeepEqual(got, first[row.id]) {
				t.Fatalf("%s changed on open %d:\nfirst %+v\nlater %+v", row.id, attempt+2, first[row.id], got)
			}
		}
		if err := graph.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A STORE THAT ALREADY HAS THE COLUMN STILL NEEDS THE INDEX. Removing the index
// from [memoriesSchema] is only half the repair: a database that carries the
// owner column but not the index — the interrupted upgrade, the hand-edited
// store — would otherwise never get it, because [migrateMemoriesOwner] returns
// early once the column is there.
func TestOpeningAnAlreadyOwnedDatabaseWithoutTheIndexAddsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "already-owned.db")
	writeLegacyDatabase(t, path)

	// Simulate the interrupted upgrade: add the column and own the rows the way
	// the migration would, and stop short of the index — exactly the state a
	// store reaches when a build dies between the ALTER and the CREATE INDEX.
	addLegacyOwnerColumn(t, path, false)

	graph, err := Open(path)
	if err != nil {
		t.Fatalf("opening an already-owned database without the index: %v", err)
	}
	defer graph.Close()

	if !indexOnMemories(t, graph, "memories_owner_active") {
		t.Fatal("the open did not add the owner index to a database that already had the column")
	}
	got, ok, err := graph.MemoryRecord("mem_superseded")
	if err != nil || !ok || got.Owner != OwnerUser {
		t.Fatalf("the already-owned row = (%+v, %v, %v), want its owner untouched", got, ok, err)
	}
}

// AN OLDER BUILD IS STILL RUNNING WHILE THE NEW ONE IS INSTALLED, so the mixed
// shape is not hypothetical: the old build opens the upgraded store, never sets
// the owner column it can see, and writes its rows with the empty default.
// Those rows carry a real scope and must be owned by it at the next open,
// without touching a single owner that is already set.
func TestOpeningADatabaseAPreOwnerBuildWroteIntoOwnsItsEmptyRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.db")
	writeLegacyDatabase(t, path)
	addLegacyOwnerColumn(t, path, true)

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// One row already carries an owner a person re-homed; the repair must leave
	// it exactly where it is.
	if _, err := raw.Exec(`UPDATE memories SET owner = 'project:alpha' WHERE id = 'mem_user'`); err != nil {
		t.Fatal(err)
	}
	// And the rows the older build wrote: no owner in the INSERT at all, which
	// is what landing the column's empty default means.
	for id, scope := range map[string]string{
		"mem_oldbuild_user":    MemoryScopeUser,
		"mem_oldbuild_env":     MemoryScopeEnv,
		"mem_oldbuild_project": MemoryScopeProject,
	} {
		if _, err := raw.Exec(`
			INSERT INTO memories (id, type, scope, title, text, tags, status, created_seq, updated_seq)
			VALUES (?, 'fact', ?, 'Written by the old build', 'A row the older build wrote.', '[]', 'active', 61, 62)`,
			id, scope); err != nil {
			t.Fatalf("insert row %s the old build would have written: %v", id, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	graph, err := Open(path)
	if err != nil {
		t.Fatalf("opening a store an older build wrote into: %v", err)
	}
	defer graph.Close()

	for id, want := range map[string]string{
		"mem_oldbuild_user":    OwnerUser,
		"mem_oldbuild_env":     OwnerMachine,
		"mem_oldbuild_project": OwnerLegacyProject,
	} {
		got, ok, err := graph.MemoryRecord(id)
		if err != nil || !ok {
			t.Fatalf("read %s: (%v, %v, %v)", id, got, ok, err)
		}
		if got.Owner != want {
			t.Errorf("%s owner = %q, want %q", id, got.Owner, want)
		}
	}
	// The empty project row is quarantined exactly as the first migration would
	// have quarantined it, marker and all.
	quarantined, _, err := graph.MemoryRecord("mem_oldbuild_project")
	if err != nil {
		t.Fatal(err)
	}
	if !containsStubTag(quarantined.Tags, OwnerLegacyTag) {
		t.Errorf("the old build's project row tags = %v, want the legacy marker", quarantined.Tags)
	}
	// THE NONEMPTY OWNER IS UNTOUCHED. Re-homing a row is the person's act, and
	// a pass that "repaired" it back to `user` would silently widen it.
	kept, _, err := graph.MemoryRecord("mem_user")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Owner != "project:alpha" {
		t.Fatalf("an already-owned row moved to %q, want project:alpha", kept.Owner)
	}
	// The older build's rows are readable under the owner the scope proves, and
	// the quarantine is still invisible to a retrieval.
	visible, err := graph.ListMemories([]string{OwnerUser}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].ID != "mem_oldbuild_user" {
		t.Fatalf("the person's shelf = %v, want the old build's user row", memoryIDs(visible))
	}
	before := map[string]Memory{}
	for _, id := range []string{"mem_user", "mem_oldbuild_user", "mem_oldbuild_env", "mem_oldbuild_project"} {
		got, _, err := graph.MemoryRecord(id)
		if err != nil {
			t.Fatal(err)
		}
		before[id] = got
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	graph, err = Open(path)
	if err != nil {
		t.Fatalf("reopen after the mixed-version repair: %v", err)
	}
	defer graph.Close()
	for id, want := range before {
		got, _, err := graph.MemoryRecord(id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s changed on the second open:\nfirst  %+v\nsecond %+v", id, want, got)
		}
	}
}

// A STORE WITH NO LEXICAL INDEX IS STILL THE PERSON'S MEMORY, and this is
// PRE-EXISTING behavior, deliberately unchanged by the owner repair: the open
// builds the empty search view when FTS5 is available but does not re-index rows
// the file already held, so search answers nothing while every note stays
// readable and owner-scoped. A Rebuild replays the journal and fills the view.
// An ordinary upgraded store carried its released index with it, so the repair
// has no reason to rebuild one, and nothing here broadens it to try.
func TestOpeningADatabaseWithoutItsLexicalIndexKeepsEveryNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-fts.db")
	writeLegacyDatabaseWithFTS(t, path, false)

	graph, err := Open(path)
	if err != nil {
		t.Fatalf("opening a database with no lexical index: %v", err)
	}
	defer graph.Close()

	if !indexOnMemories(t, graph, "memories_owner_active") {
		t.Fatal("the open did not leave the owner index on memories")
	}
	kept, ok, err := graph.MemoryRecord("mem_user")
	if err != nil || !ok {
		t.Fatalf("read the preserved note: (%v, %v, %v)", kept, ok, err)
	}
	if kept.Owner != OwnerUser || kept.Text != "Keeps the receipts for the tax year." {
		t.Fatalf("the note = %+v, want its words and the owner its scope proves", kept)
	}
	if hits, err := graph.SearchMemories([]string{OwnerUser}, "receipts", 5); err != nil || len(hits) != 0 {
		t.Fatalf("search without a lexical index = (%v, %v), want the quiet tier", memoryIDs(hits), err)
	}
}

// A BRAND NEW STORE STILL GETS THE INDEX. The index moved out of the schema
// because the schema runs before the column is guaranteed; it must not move out
// of reach of the first launch, which is what a migration that only ran on
// legacy shapes would do.
func TestANewStoreCarriesTheOwnerIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatalf("open a new store: %v", err)
	}
	defer graph.Close()
	if !indexOnMemories(t, graph, "memories_owner_active") {
		t.Fatal("a brand new store has no owner index")
	}
}

// legacyTags reads the fixture's JSON tag list the way the store reads a row's.
func legacyTags(t *testing.T, raw string) []string {
	t.Helper()
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		t.Fatalf("fixture tags %q are not JSON: %v", raw, err)
	}
	return tags
}
