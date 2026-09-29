package cellstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestSeatIsPlainWhenCellsAreOff(t *testing.T) {
	t.Setenv("CODEAF_CELLS", "")
	seat, err := SeatFor(executor.Sandboxed, newCell(t), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := seat.(executor.Stance); !ok {
		t.Fatalf("flag off built %T, want a plain stance with no recorder", seat)
	}
}

func TestAPlainUserFolderIsNotSealed(t *testing.T) {
	t.Setenv("CODEAF_CELLS", "1")
	workspace := t.TempDir()
	seat, err := SeatFor(executor.HostBound, newCell(t), workspace, nil)
	if !errors.Is(err, ErrNotSealable) {
		t.Fatalf("err = %v, want ErrNotSealable", err)
	}
	if _, ok := seat.(executor.Stance); !ok {
		t.Fatalf("an unsealable folder still needs its class: got %T", seat)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, repositoryMarker)); statErr == nil {
		t.Fatal("a user's folder was made a repository")
	}
}

func TestSealTreeIsTheWorkspaceForARepositoryAndAnOwnedFolder(t *testing.T) {
	c := newCell(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, repositoryMarker), 0o700); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(c.Root, "work")
	for _, workspace := range []string{repo, owned} {
		tree, err := sealTree(c, workspace)
		if err != nil || tree.Root != workspace || tree.ID != c.ID {
			t.Fatalf("sealTree(%s) = %+v, %v", workspace, tree, err)
		}
	}
}

// Whatever a tool call does inside, it leaves one receipt row, and each seal is
// chained to the last by parent: a call that runs a process, one that only
// writes a file, and one that fails.
func TestSealedSeatRecordsEachToolCallAsOneReceipt(t *testing.T) {
	c := newCell(t)
	rec, fake := newRecorder(t, c, &stubExec{}, filepath.Join(t.TempDir(), "wal"))
	seat := sealed{Stance: executor.Stance{Class: executor.HostBound}, rec: rec}
	for _, failed := range []bool{false, true} {
		err := seat.Around(context.Background(), "bash", []byte(`{"command":"x"}`), func() ([]byte, bool) { return []byte("out"), failed })
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.count("turn-end"); got != 2 {
		t.Fatalf("%d seals for two calls", got)
	}
	head, _ := Head(c)
	call := head.Receipt.Calls[0]
	if len(head.Receipt.Calls) != 1 || call.Tool != "bash" || call.Exit != 1 || call.SideEffect != "external" || head.Turn.Parent == "" {
		t.Fatalf("head %+v", head)
	}
}

func TestACallWhoseIntentCannotBeLoggedDoesNotRun(t *testing.T) {
	c := newCell(t)
	dir := filepath.Join(t.TempDir(), "log")
	rec, _ := newRecorder(t, c, &stubExec{}, filepath.Join(dir, "wal"))
	// The log's folder becomes a file, so nothing more can be appended to it.
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ran := false
	err := rec.Around(context.Background(), "bash", nil, executor.EffectLocal, func() ([]byte, bool) { ran = true; return nil, false })
	if err == nil || ran {
		t.Fatalf("err = %v, ran = %v; an unlogged call must not run", err, ran)
	}
}
