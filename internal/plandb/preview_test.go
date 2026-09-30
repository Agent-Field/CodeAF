package plandb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootPreviewReadsBesideAWriterAndNeverCreatesAStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run brief.db")
	if _, err := ReadRootPreview(path); err == nil {
		t.Fatal("missing store was readable")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read created a store: %v", err)
	}
	store, err := Open(path, "program", "1", "fix parser", "fix the parser crash")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// A live worker holding the write transaction must not block the picker.
	tx, err := store.beginWrite()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got, err := ReadRootPreview(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "fix parser" || got.Description != "fix the parser crash" || !got.CreatedAt.Equal(store.Task("1").CreatedAt) {
		t.Fatalf("wrong saved root: %+v", got)
	}
}
