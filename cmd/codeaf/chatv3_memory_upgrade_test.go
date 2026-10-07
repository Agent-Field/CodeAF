package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/store"
)

// The memories table the released build left on disk: no owner column, and an
// index set the owner-aware build's schema must not assume. It is spelled here
// rather than read from the store because this test has to keep describing the
// OLD shape after the current schema moves on.
const releasedMemoriesDDL = `
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
CREATE VIRTUAL TABLE memories_fts USING fts5(
    memory_id UNINDEXED,
    title,
    text,
    tags
);
`

// THE LAUNCH A PERSON ACTUALLY MAKES. Someone who has been running codeaf for
// weeks opens the new build, and the start path asks [v3Memory] for their
// brain. It must not answer nil — which is what it did, printing "memory is off
// for this session: initialize memories schema: no such column: owner" — and
// the note they had kept must still be in it.
func TestLaunchingOnADatabaseFromBeforeOwnersKeepsTheNote(t *testing.T) {
	fresh := t.TempDir()
	t.Setenv("HOME", fresh)
	root := filepath.Join(fresh, ".codeaf")
	t.Setenv(home.EnvVar, root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}

	// The database the released build wrote, with one note the person kept.
	raw, err := sql.Open("sqlite", defaultChatDB())
	if err != nil {
		t.Fatalf("create the old database: %v", err)
	}
	if _, err := raw.Exec(releasedMemoriesDDL); err != nil {
		t.Fatalf("create the old memories table: %v", err)
	}
	if _, err := raw.Exec(`
		INSERT INTO memories (id, type, scope, title, text, tags, status, use_count,
		    miss_count, created_seq, updated_seq, source_session, source_seq)
		VALUES ('mem_kept', 'preference', 'user', 'Keeps the receipts',
		    'Keeps the receipts for the tax year.', '["finance"]', 'active',
		    2, 0, 11, 12, 'session-old', 7)`); err != nil {
		t.Fatalf("insert the old note: %v", err)
	}
	// The released build kept its own search view, so the old database carries
	// one; the launch must leave it answering under the new owner filter.
	if _, err := raw.Exec(`
		INSERT INTO memories_fts (memory_id, title, text, tags)
		SELECT id, title, text, replace(replace(replace(tags, '[', ''), ']', ''), '"', '')
		FROM memories WHERE id = 'mem_kept' AND status = 'active'`); err != nil {
		t.Fatalf("index the old note: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	profile := t.TempDir()
	brain := v3Memory(profile)
	if brain == nil {
		t.Fatal("launching on a database from before owners opened no brain")
	}
	rows, err := brain.ListMemories([]string{store.OwnerUser}, 20)
	if err != nil {
		t.Fatalf("read the person's memories: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the person's shelf = %d rows, want the one they kept", len(rows))
	}
	kept := rows[0]
	if kept.ID != "mem_kept" || kept.Title != "Keeps the receipts" ||
		kept.Text != "Keeps the receipts for the tax year." {
		t.Fatalf("the note came back as %+v, want it preserved", kept)
	}
	if kept.Owner != store.OwnerUser {
		t.Fatalf("the note's owner = %q, want %q", kept.Owner, store.OwnerUser)
	}
	if kept.SourceSession != "session-old" || kept.SourceSeq != 7 {
		t.Fatalf("the note's provenance = (%q, %d), want it preserved", kept.SourceSession, kept.SourceSeq)
	}
	// Search on the launched brain answers the note the person kept, under the
	// owner the upgrade gave it.
	found, err := brain.SearchMemories([]string{store.OwnerUser}, "receipts", 5)
	if err != nil {
		t.Fatalf("search the launched brain: %v", err)
	}
	if len(found) != 1 || found[0].ID != "mem_kept" {
		t.Fatalf("search on the launched brain = %v, want the kept note", found)
	}
	if err := brain.Close(); err != nil {
		t.Fatal(err)
	}

	// The owner index must survive the launch, because the retrieval reads are
	// written against it and the schema can no longer declare it.
	check, err := sql.Open("sqlite", defaultChatDB())
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var name string
	if err := check.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'memories_owner_active'`).Scan(&name); err != nil {
		t.Fatalf("the launch did not leave the owner index: %v", err)
	}

	// And the second launch reads the same note rather than doing the upgrade
	// twice.
	brain = v3Memory(profile)
	if brain == nil {
		t.Fatal("the second launch opened no brain")
	}
	defer brain.Close()
	again, err := brain.ListMemories([]string{store.OwnerUser}, 20)
	if err != nil {
		t.Fatalf("read on the second launch: %v", err)
	}
	if len(again) != 1 || again[0].Text != kept.Text {
		t.Fatalf("the second launch = %+v, want the same note", again)
	}
}
