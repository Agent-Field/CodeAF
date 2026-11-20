package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
)

func TestQACarriedProcessHistoryBecomesPageableWithoutChangingItsExecutionFolder(t *testing.T) {
	root, from := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, cell.StateDir, "files"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, cell.TranscriptPath), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := inventory.Record(root, func(inv *inventory.Inventory) { inv.ChatDir = from }); err != nil {
		t.Fatal(err)
	}
	source := processStore{dir: filepath.Join(root, cell.StateDir, "files")}
	dir, head, err := source.open(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	n := JobNotice{ID: 1, State: JobDone, Kind: JobKindCommand, Command: "echo complete", Started: time.Now().Add(-time.Minute), Ended: time.Now(), Dir: "/sender/project", LogPath: filepath.Join(from, "logs", "jobs", "1.log"), StartedBy: JobStartedByChat}
	_, err = source.put(dir, &head, n, false)
	dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RestoreCarriedProcessHistory(ctx, root); err != nil {
		t.Fatal(err)
	}
	page, err := ReadProcessPage(ctx, filepath.Join(root, cell.TranscriptPath), ProcessPageAsk{})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("restored page = %+v, %v", page, err)
	}
	got, found, err := ReadProcessRecord(filepath.Join(root, cell.TranscriptPath), 1)
	if err != nil || !found || !got.Started.Equal(n.Started) || got.Dir != n.Dir || got.StartedBy != n.StartedBy || got.LogPath != filepath.Join(root, "logs", "jobs", "1.log") {
		t.Fatalf("restored record = %+v, %v", got, err)
	}
	if err := RestoreCarriedProcessHistory(ctx, root); err != nil {
		t.Fatal(err)
	}
	again, err := ReadProcessPage(ctx, filepath.Join(root, cell.TranscriptPath), ProcessPageAsk{})
	if err != nil || len(again.Rows) != 1 {
		t.Fatal("retry duplicated or lost the process")
	}
}

func TestQACarriedProcessHistoryIgnoresAnOldLocalBranchStore(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, cell.TranscriptPath), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := inventory.Record(root, func(inv *inventory.Inventory) { inv.ChatDir = "/source/chat" }); err != nil {
		t.Fatal(err)
	}
	old := processStore{dir: root}
	dir, head, err := old.open(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.put(dir, &head, JobNotice{ID: 99, State: JobDone, Kind: JobKindCommand, Command: "old local branch"}, false)
	dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RestoreCarriedProcessHistory(ctx, root); err != nil {
		t.Fatal(err)
	}
	page, err := ReadProcessPage(ctx, filepath.Join(root, cell.TranscriptPath), ProcessPageAsk{})
	if err != nil || len(page.Rows) != 0 {
		t.Fatalf("old local store leaked into received history: %+v, %v", page, err)
	}
}
