package main

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestQAMovedProcessRebasesOnlyItsManagedLog(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(t.TempDir(), "sender")
	if err := inventory.Record(root, func(inv *inventory.Inventory) { inv.ChatDir = from }); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(root, ".cell", "transcript.jsonl")
	for _, base := range []string{from, filepath.Join(from, ".cell")} {
		old := session.JobNotice{ID: 1, LogPath: filepath.Join(base, "logs", "jobs", "1.log"), Dir: "/original/workspace"}
		got := appMovedProcessRecord(transcript, old)
		if got.LogPath != filepath.Join(root, "logs", "jobs", "1.log") || got.Dir != old.Dir {
			t.Fatalf("rebased = %+v", got)
		}
	}
	for _, path := range []string{filepath.Join(from, "private.txt"), filepath.Join(from, "logs", "jobs", "2.log"), filepath.Join(root, "logs", "jobs", "1.log")} {
		got := appMovedProcessRecord(transcript, session.JobNotice{ID: 1, LogPath: path})
		if got.LogPath != path {
			t.Fatal("an unrelated path was accepted as this job's managed output")
		}
	}
}
