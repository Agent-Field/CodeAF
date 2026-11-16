package keys

import (
	"os"
	"path/filepath"
	"testing"
)

// A bounded ledger stops noting new files once full, but keeps judging the
// ones it holds and refreshes them when they change.
func TestBoundedLedgerNotesNoMoreThanItsBound(t *testing.T) {
	dir := t.TempDir()
	l := NewBoundedLedger(2)
	for _, name := range []string{"a", "b", "c"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() == name {
				st, _ := l.known(path, e)
				l.note(path, st)
			}
		}
	}
	if len(l.clean) != 2 {
		t.Fatalf("bounded ledger holds %d files, want 2", len(l.clean))
	}
	if _, ok := l.clean[filepath.Join(dir, "c")]; ok {
		t.Fatal("a full ledger noted a new file")
	}
}
