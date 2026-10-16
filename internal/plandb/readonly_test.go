package plandb

import (
	"os"
	"path/filepath"
	"testing"
)

// A read of another conversation's store never creates one, sees the committed
// tasks in the store's order beside a writer that holds the write lock, and
// leaves the file as it found it.
func TestReadTasksReadsBesideAWriterAndNeverCreatesAStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	if _, err := ReadTasks(path); err == nil {
		t.Fatal("a missing store was readable")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a read created a store: %v", err)
	}
	store, err := Open(path, "program", "1", "fix parser", "fix the parser crash")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx, err := store.beginWrite()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got, err := ReadTasks(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.RootID != "1" || len(got.Tasks) != 1 || got.Tasks[0].Title != "fix parser" {
		t.Fatalf("read = %+v", got)
	}
}
