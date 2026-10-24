package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestC29FirstSavedTurnIncludesTheEarlierConversation proves C29.1/C29.4 with
// the supplied engine: rewind reaches the copy's first folder state and retains
// the entire earlier conversation, while the original remains unchanged.
func TestC29FirstSavedTurnIncludesTheEarlierConversation(t *testing.T) {
	celltest.RequireEngine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(cellstore.StopSpawned)
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("at copy time"), 0600); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(home.Dir(), "v3", "projects", "old", "old-session")
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	journal := `{"type":"session","version":1,"id":"old-session"}` + "\n" + `{"type":"message","role":"user","content":"my whole earlier conversation"}` + "\n"
	if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(original, session.Meta{ID: "old-session", Title: "Older words", LastUserAt: time.Now(), Workspace: work}); err != nil {
		t.Fatal(err)
	}
	c, err := copyOlderChat(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	head, err := cellstore.Head(c)
	if err != nil || head == nil {
		t.Fatalf("copy has no saved first turn: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("later"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := cellstore.EngineFor(work)
	if _, err := engine.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Rewind(t.Context(), c, head.Turn.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(work, "hello.txt"))
	if err != nil || string(data) != "at copy time" {
		t.Fatalf("rewind lost first folder state: %s %v", data, err)
	}
	data, err = os.ReadFile(filepath.Join(c.Root, cell.TranscriptPath))
	if err != nil || !strings.Contains(string(data), "my whole earlier conversation") {
		t.Fatal("rewind lost the earlier conversation")
	}
	data, err = os.ReadFile(filepath.Join(original, "transcript.jsonl"))
	if err != nil || string(data) != journal {
		t.Fatal("using copy changed original")
	}
	// Retire only this fixture's engine routes before TempDir cleanup.
	_ = cellstore.Await(context.Background(), c)
}
