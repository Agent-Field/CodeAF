package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/store"
)

// C18 preserves legacy memory writes without a portable sidecar when the engine is unavailable.
func TestC18CellsOffMustNotWriteMemorySidecar(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(false))
	dir := t.TempDir()
	brain, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	brain.SetMemoryLedger(CellMemories)
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = Place{Dir: dir}.Transcript()
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
		c.Memory = brain
	})
	defer a.Close()
	_, err = brain.AddMemory(store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeUser, Title: "preference", Text: "use tabs", SourceSession: filepath.Base(dir)})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "memories.jsonl")
	if raw, err := os.ReadFile(path); err == nil {
		t.Fatalf("no engine wrote extra memory sidecar %s (%d bytes)", path, len(raw))
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
