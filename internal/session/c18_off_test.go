package session

import (
	"github.com/Agent-Field/codeaf/internal/store"
	"os"
	"path/filepath"
	"testing"
)

// C18 preserves legacy memory writes without a portable sidecar when cells are off.
func TestC18CellsOffMustNotWriteMemorySidecar(t *testing.T) {
	t.Setenv("CODEAF_CELLS", "0")
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
		t.Fatalf("cells off wrote extra memory sidecar %s (%d bytes)", path, len(raw))
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
