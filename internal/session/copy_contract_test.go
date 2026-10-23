package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/store"
)

// TestC29CopyCarriesTruthAndReplacesTheListRow proves C29.1/C29.3: the new
// build lists the copy once, with the same title/order, dials, journals and memory.
func TestC29CopyCarriesTruthAndReplacesTheListRow(t *testing.T) {
	bucket := t.TempDir()
	original := filepath.Join(bucket, "old-session")
	if err := os.MkdirAll(filepath.Join(original, "tasks"), 0700); err != nil {
		t.Fatal(err)
	}
	transcript := `{"type":"session","version":1,"id":"old-session"}` + "\n" + `{"type":"message","role":"user","content":"all my earlier words"}` + "\n"
	if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(transcript), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "tasks", "one.jsonl"), []byte("whole task journal"), 0600); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 0)
	if err := SaveMeta(original, Meta{ID: "old-session", Title: "The older conversation", Effort: "high", Approval: "allow", LastUserAt: at, Workspace: bucket}); err != nil {
		t.Fatal(err)
	}
	brain, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	_, err = brain.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "An older chat's remembered preference.", SourceSession: "old-session"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := cell.CopyLegacy(t.Context(), original, func(ctx context.Context, c cell.Cell) error { return PrepareCopy(ctx, original, c, brain) })
	if err != nil {
		t.Fatal(err)
	}
	meta, err := LoadMeta(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Effort != "high" || meta.Approval != "allow" || meta.Title != "The older conversation" || !meta.LastUserAt.Equal(at) {
		t.Fatalf("copy lost truth: %+v", meta)
	}
	events, err := ReadMemoryEvents(c.Root)
	if err != nil || len(events) != 1 {
		t.Fatalf("memory did not copy: %v %v", events, err)
	}
	raw, err := os.ReadFile(filepath.Join(c.Root, ".cell", "tasks", "one.jsonl"))
	if err != nil || string(raw) != "whole task journal" {
		t.Fatal("task journal did not copy")
	}
	rows := ReadWorld(filepath.Dir(bucket)).Sessions()
	_ = rows
	recent := RecentSessions(bucket, 10)
	if len(recent) != 1 || recent[0].File != filepath.Join(c.Root, cell.TranscriptPath) {
		t.Fatalf("list did not replace original: %+v", recent)
	}
	if got, err := os.ReadFile(filepath.Join(original, "transcript.jsonl")); err != nil || string(got) != transcript {
		t.Fatal("copy edited original")
	}
}
