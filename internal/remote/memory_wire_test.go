package remote

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// MEMORY CROSSES THE WIRE THE WAY THE SURFACE REACHES IT: the commands through
// the conversation's own engine, which decides where a line holds and writes the
// receipt, and the place's moves and project reading through the same engine.
// The far end is a real session over a real store, because what is under test is
// that the default road (a *remote.Agent) reaches the line a local session would
// keep. No model is called: an explicit save asks no model anything.
func TestMemoryCrossesTheWireToTheRealSession(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/memory-wire",
		APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Test only.", PromptProfile: "full",
		Memory: brain, MemoryProjectKey: "wire", MemoryProjectName: "wire-repo"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = far.Close() })
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Memory: engineMemoryRows{brain}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	commands, ok := any(loop.Client.Agent()).(MemoryCommands)
	if !ok || !commands.Remembers() {
		t.Fatal("the ordinary engine handle has no memory commands")
	}
	receipt, err := commands.Remember("use tabs in this repository")
	if err != nil || receipt != "remembered · use tabs in this repository · for this project" {
		t.Fatalf("/remember over the wire = (%q, %v)", receipt, err)
	}
	receipt, err = commands.RememberAlways("no emoji anywhere", true)
	if err != nil || receipt != "remembered · no emoji anywhere · for you everywhere" {
		t.Fatalf("/always from home over the wire = (%q, %v)", receipt, err)
	}
	if owner := commands.MemoryProject(); owner != store.OwnerProject("wire") {
		t.Fatalf("the engine's project over the wire = %q", owner)
	}
	lines, err := commands.Memories("tabs")
	if err != nil || len(lines) != 1 || lines[0].Where != "for this project" {
		t.Fatalf("the matching lines over the wire = (%+v, %v)", lines, err)
	}
	moved, err := commands.MoveMemory(lines[0].ID, store.MemoryScopeUser)
	if err != nil || moved != "now for you everywhere" {
		t.Fatalf("a move over the wire = (%q, %v)", moved, err)
	}
	row, _, _ := brain.MemoryRecord(lines[0].ID)
	if row.Owner != store.OwnerUser {
		t.Fatalf("the moved line = %+v", row)
	}
	// AND THE PLACE'S FIX THROUGH THE ENGINE'S STORE carries only the words.
	if err := loop.Client.UpdateMemory(lines[0].ID, "use tabs everywhere"); err != nil {
		t.Fatalf("a fix over the wire: %v", err)
	}
	row, _, _ = brain.MemoryRecord(lines[0].ID)
	if row.Text != "use tabs everywhere" || row.Title != "use tabs everywhere" {
		t.Fatalf("the fixed line = %+v", row)
	}
}

// WITH NO STORE ON THE ENGINE, EVERY MEMORY DOOR IS REFUSED IN THE ONE SENTENCE,
// rather than answering an empty list that would read as "nothing kept" when the
// truth is "nothing can be kept".
func TestTheMemoryDoorsWithNoStoreAreRefused(t *testing.T) {
	far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/memory-off",
		APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Test only.", PromptProfile: "full"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = far.Close() })
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	for _, method := range []string{MethodMemoryRememberAlways, MethodMemoryAlways, MethodMemoryMove} {
		if _, err := loop.Client.call(nil, method, MemoryAlwaysArgs{Text: "use tabs"}); err == nil || !strings.Contains(err.Error(), memoryGoneWord) {
			t.Fatalf("%s with no store = %v", method, err)
		}
	}
	if err := loop.Client.UpdateMemory("mem_x", "words"); err == nil || !strings.Contains(err.Error(), memoryGoneWord) {
		t.Fatalf("the place's fix with no store = %v", err)
	}
}
