package plandb

// Tests for a person's note carrying its sender's opaque key, written in the
// same transaction as the note and read back by the immutable note id.

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"
)

// A keyed note is on disk with its key the moment it exists, survives a
// reopen, and two notes with equal text and different keys stay two notes with
// two ids. A repeat of one key is not folded into the first.
func TestKeyedPersonNoteKeepsItsKeyBesideEqualTextAcrossAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	planAdd(t, store, planSpec("a", "Alpha"))

	first, err := store.AddPersonNoteKeyed("a", "same words", "cid-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AddPersonNoteKeyed("a", "same words", "cid-2")
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := store.AddPersonNoteKeyed("a", "same words", "cid-1")
	if err != nil {
		t.Fatal(err)
	}
	unkeyed, err := store.AddPersonNote("a", "same words")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.AddNote("a", "w1", "same words")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.ID == repeat.ID || first.ID == unkeyed.ID {
		t.Fatalf("ids %q %q %q %q are not distinct", first.ID, second.ID, repeat.ID, unkeyed.ID)
	}
	if first.ClientID != "cid-1" || second.ClientID != "cid-2" || unkeyed.ClientID != "" || worker.ClientID != "" {
		t.Fatalf("returned keys = %q %q %q %q", first.ClientID, second.ClientID, unkeyed.ClientID, worker.ClientID)
	}
	store.Close()

	reopened := planReopen(t, path)
	defer reopened.Close()
	got := reopened.Notes("a", 0)
	want := []struct{ id, key string }{{first.ID, "cid-1"}, {second.ID, "cid-2"}, {repeat.ID, "cid-1"}, {unkeyed.ID, ""}, {worker.ID, ""}}
	if len(got) != len(want) {
		t.Fatalf("notes = %#v", got)
	}
	for i, note := range got {
		if note.ID != want[i].id || note.ClientID != want[i].key {
			t.Fatalf("note %d = %q key %q, want %q key %q", i, note.ID, note.ClientID, want[i].id, want[i].key)
		}
	}
}

// A refused keyed note leaves nothing behind: the key never outlives a note
// that was not stored.
func TestRefusedKeyedPersonNoteStoresNothing(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	if _, err := store.AddPersonNoteKeyed("ghost", "words", "cid"); err == nil {
		t.Fatal("a keyed note on an unknown task was accepted")
	}
	if _, err := store.AddPersonNoteKeyed("a", "  ", "cid"); err == nil {
		t.Fatal("a keyed empty note was accepted")
	}
	if notes := store.Notes("a", 0); len(notes) != 0 {
		t.Fatalf("notes after two refusals = %#v", notes)
	}
}

// A store written before the column opens, migrates on the writable open, reads
// its old notes with no key, and then keeps a keyed note. A read-only reader of
// the same old file neither migrates it nor fails.
func TestNotesFromBeforeTheClientIDColumnReadUnknownAndAReadOnlyReaderDoesNotMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	planAdd(t, store, planSpec("a", "Alpha"))
	old, err := store.AddPersonNote("a", "from before the column")
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`ALTER TABLE notes DROP COLUMN client_id`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	if _, err := ReadTasks(path); err != nil {
		t.Fatalf("a read-only read of an old store failed: %v", err)
	}
	if notes := readNotesReadOnly(t, path); len(notes) != 1 || notes[0].ID != old.ID || notes[0].ClientID != "" {
		t.Fatalf("read-only notes = %#v, want the old note with no key", notes)
	}
	if columnPresent(t, path) {
		t.Fatal("a read-only reader added the client_id column")
	}

	migrated := planReopen(t, path)
	defer migrated.Close()
	if !columnPresent(t, path) {
		t.Fatal("the writable open did not add the client_id column")
	}
	if notes := migrated.Notes("a", 0); len(notes) != 1 || notes[0].ID != old.ID || notes[0].ClientID != "" {
		t.Fatalf("migrated notes = %#v, want the old note with no key", notes)
	}
	keyed, err := migrated.AddPersonNoteKeyed("a", "after the column", "cid-9")
	if err != nil {
		t.Fatal(err)
	}
	if notes := migrated.Notes("a", 0); len(notes) != 2 || notes[0].ClientID != "" || notes[1].ID != keyed.ID || notes[1].ClientID != "cid-9" {
		t.Fatalf("notes after the keyed write = %#v", notes)
	}
}

func readNotesReadOnly(t *testing.T, path string) []Note {
	t.Helper()
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	notes, err := loadNotes(tx)
	if err != nil {
		t.Fatalf("read-only loadNotes: %v", err)
	}
	return notes
}

func columnPresent(t *testing.T, path string) bool {
	t.Helper()
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	found, err := hasColumn(tx, "notes", "client_id")
	if err != nil {
		t.Fatal(err)
	}
	return found
}
