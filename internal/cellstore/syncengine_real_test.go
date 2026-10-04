//go:build engine

package cellstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// TestSyncEngineRoundTripRealEngine runs the real CLI verbs: seal a cell, export
// it, record the frames as published, and see a second export send nothing. It
// skips while the engine build has no sync verbs.
func TestSyncEngineRoundTripRealEngine(t *testing.T) {
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	c := newCell(t)
	if err := os.WriteFile(filepath.Join(c.Root, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Engine{Binary: bin, DataRoot: t.TempDir(), Transport: Spawn{Binary: bin}}
	sealed, err := store.Seal(context.Background(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	e := SyncEngine{
		Transport: Spawn{Binary: bin},
		Keys:      SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)},
		Ledger:    LedgerName("http://relay", "id_test"),
		Target: func(c2 cell.Cell) Target {
			return Target{Tree: store.tree(c2), DataDir: store.LocalDir(c2), CellDir: store.cellDir(c2)}
		},
		Outbox: func(cell.Cell) string { return t.TempDir() },
	}
	first, err := e.Export(context.Background(), c, sealed.Turn.ID)
	if err != nil {
		if strings.Contains(err.Error(), "export") && strings.Contains(err.Error(), "unrecognized") {
			t.Skipf("engine has no sync verbs: %v", err)
		}
		t.Fatal(err)
	}
	if len(first.Frames) == 0 {
		t.Fatal("first export sent nothing")
	}
	paths := make([]string, len(first.Frames))
	for i, f := range first.Frames {
		paths[i] = f.Path
	}
	if err := e.Published(context.Background(), c, paths); err != nil {
		t.Fatal(err)
	}
	second, err := e.Export(context.Background(), c, sealed.Turn.ID)
	if err != nil || len(second.Frames) != 0 {
		t.Fatalf("second export = %+v, %v", second, err)
	}
}
