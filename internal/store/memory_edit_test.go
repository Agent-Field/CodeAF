package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryShelfTransferIsConditionalAndOwnerIsolated(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	row, err := db.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Title: "Release", Text: "Friday release"})
	if err != nil {
		t.Fatal(err)
	}
	crew, text := OwnerCrew("release"), "Monday release"
	version := uint64(row.UpdatedSeq)
	if err := db.EditMemory(row.ID, MemoryEdit{Action: "patch", Text: &text, Owner: &crew, Version: &version}); err != nil {
		t.Fatal(err)
	}
	if err := db.EditMemory(row.ID, MemoryEdit{Action: "forget", Version: &version, Now: time.Now()}); !errors.Is(err, ErrMemoryStale) {
		t.Fatal(err)
	}
	for _, owner := range []string{OwnerUser, OwnerCrew("unrelated")} {
		if rows, err := db.SearchMemories([]string{owner}, "release", 10); err != nil || len(rows) != 0 {
			t.Fatalf("%s saw moved memory: %v %v", owner, rows, err)
		}
	}
	if rows, err := db.SearchMemories([]string{crew}, "Monday", 10); err != nil || len(rows) != 1 {
		t.Fatalf("crew memory %v %v", rows, err)
	}
}

func TestMemoryUndoCannotRestoreAnInterveningTerminalForget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	app, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	terminal, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	row, err := app.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Title: "Release", Text: "Friday release"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := app.EditMemory(row.ID, MemoryEdit{Action: "forget", Now: now}); err != nil {
		t.Fatal(err)
	}
	if err := terminal.RestoreMemory(row.ID); err != nil {
		t.Fatal(err)
	}
	if err := terminal.ForgetMemory(row.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.EditMemory(row.ID, MemoryEdit{Action: "restore", Now: now.Add(time.Second)}); !errors.Is(err, ErrMemoryUndoEnded) {
		t.Fatal(err)
	}
}
