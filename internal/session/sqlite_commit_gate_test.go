package session

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/plandb/plandbtest"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// THE GATE IS SQLITE'S OWN COMMIT REFUSAL AND NOTHING ELSE: the store's error
// is the COMMIT's, wearing [plandb.ErrCommitUncertain] over the driver's
// SQLITE_CONSTRAINT_COMMITHOOK; the refused transaction is rolled back; a
// commit carrying another key, on the same handle, while armed, goes through;
// and a store at a path no gate serves is never hooked.
func TestTheCommitGateIsSQLitesOwnRefusalOfOneTransaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	gate := plandbtest.GateNote(t, path, "g-armed")
	seedPlanStore(t, path, "ask-chat", plandb.TaskSpec{ID: "child", Title: "Write handler", Description: "replace the lookup"})
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	if _, err := store.AddPersonNoteKeyed("child", "other words", "g-other"); err != nil {
		t.Fatalf("an unarmed key on the gated handle = %v", err)
	}
	_, err = store.AddPersonNoteKeyed("child", "same words", "g-armed")
	var refusal *sqlite.Error
	if !errors.Is(err, plandb.ErrCommitUncertain) || !errors.As(err, &refusal) || refusal.Code() != sqlite3.SQLITE_CONSTRAINT_COMMITHOOK {
		t.Fatalf("the armed commit = %v, want ErrCommitUncertain over SQLite's commit-hook refusal", err)
	}
	if gate.Refused() != 1 {
		t.Fatalf("refused %d commits, want 1", gate.Refused())
	}
	if keys := personNoteKeys(store.Notes("child", 0)); keys != "g-other" {
		t.Fatalf("person notes after the refused commit = %q, want only g-other", keys)
	}
	gate.Disarm()
	if _, err := store.AddPersonNoteKeyed("child", "later words", "g-later"); err != nil {
		t.Fatalf("the handle after the refused commit = %v", err)
	}
	if keys := personNoteKeys(store.Notes("child", 0)); keys != "g-other|g-later" {
		t.Fatalf("person notes after the healthy commit = %q", keys)
	}

	elsewhere := filepath.Join(t.TempDir(), planStoreFilename)
	seedPlanStore(t, elsewhere, "ask-chat", plandb.TaskSpec{ID: "child", Title: "Write handler", Description: "replace the lookup"})
	other, err := plandb.Open(elsewhere, "", "", "", "")
	if err != nil {
		t.Fatalf("open elsewhere: %v", err)
	}
	defer other.Close()
	gate.Arm()
	if _, err := other.AddPersonNoteKeyed("child", "same words", "g-armed"); err != nil {
		t.Fatalf("a store at a path no gate serves = %v", err)
	}
	if gate.Refused() != 1 {
		t.Fatalf("a commit at another path was refused")
	}
}
