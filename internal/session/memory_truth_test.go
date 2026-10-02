package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellindex"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// memoryCell is a cell that has spoken once, beside a graph.db that holds two
// memories the conversation wrote: one kept, one forgotten.
func memoryCell(t *testing.T, ledger bool) (cell.Cell, string) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	session.RunScriptedSession(t, c.Root, "remember how the parser migrates")
	brain := openGraph(t)
	defer brain.Close()
	if ledger {
		brain.SetMemoryLedger(session.CellMemories)
		session.CellMemories.Bind(c.ID, c.Root, nil)
	}
	kept, err := brain.AddMemory(store.Memory{Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
		Title: "parser order", Text: "migrate the parser before the printer", SourceSession: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := brain.AddMemory(store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeUser,
		Title: "old habit", Text: "uses tabs", SourceSession: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := brain.ForgetMemoryFromSession(gone.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	return c, kept.ID
}

func openGraph(t *testing.T) *store.Store {
	t.Helper()
	brain, err := store.Open(home.Join("graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	return brain
}

func dropGraph(t *testing.T) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(home.Join("graph.db" + suffix)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func assertMemoriesBack(t *testing.T, c cell.Cell, keptID string) {
	t.Helper()
	dropGraph(t)
	built, err := cellindex.Rebuild(c, "")
	if err != nil || len(built) == 0 {
		t.Fatalf("Rebuild built %v, err %v", built, err)
	}
	brain := openGraph(t)
	defer brain.Close()
	active, err := brain.ListMemories("", 10)
	if err != nil || len(active) != 1 || active[0].ID != keptID || active[0].Text != "migrate the parser before the printer" {
		t.Fatalf("active memories after rebuild = %+v, err %v", active, err)
	}
	all, _ := brain.MemoryEventsOf(c.ID)
	if len(all) != 3 {
		t.Fatalf("journal holds %d memory events after rebuild, want 3 (add, add, forget)", len(all))
	}
	if again, err := cellindex.Rebuild(c, ""); err != nil || len(again) != 0 {
		t.Fatalf("second Rebuild built %v, err %v", again, err)
	}
}

// Memories a chat wrote are sealed in its cell; deleting graph.db loses none.
func TestMemoriesSurviveTheGraphBeingDeleted(t *testing.T) {
	c, keptID := memoryCell(t, true)
	if _, err := os.Stat(filepath.Join(c.Root, ".cell", "memories.jsonl")); err != nil {
		t.Fatalf("the ledger is not under .cell/: %v", err)
	}
	assertMemoriesBack(t, c, keptID)
}

// A graph.db that already holds a chat's memories, from before ledgers, is
// carried into the cell the next time it opens, and then survives deletion.
func TestAnExistingGraphIsSealedIntoTheCell(t *testing.T) {
	c, keptID := memoryCell(t, false)
	if events, _ := session.ReadMemoryEvents(c.Root); len(events) != 0 {
		t.Fatalf("no ledger was installed, yet the cell holds %d events", len(events))
	}
	brain := openGraph(t)
	if err := session.SealMemories(c.Root, brain); err != nil {
		t.Fatal(err)
	}
	brain.Close()
	if events, _ := session.ReadMemoryEvents(c.Root); len(events) != 3 {
		t.Fatalf("sealed %d events, want 3", len(events))
	}
	assertMemoriesBack(t, c, keptID)
}
