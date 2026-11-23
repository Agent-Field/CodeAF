//go:build engine

package cellstore

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
)

// TestSyncEngineRoundTripRealEngine runs the real CLI verbs: seal a cell, export
// it, record the frames as published, and see a second export send nothing. It
// skips while the engine build has no sync verbs.
func TestSyncEngineRoundTripRealEngine(t *testing.T) {
	bin := celltest.Engine(t)
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
			return store.target(c2)
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

// TestARepeatedExportKeepsTheWaitingFramesRealEngine runs the real export twice
// into one outbox with nothing published between: the second answers the frames
// already waiting without sealing them again, so no frame file is born twice.
func TestARepeatedExportKeepsTheWaitingFramesRealEngine(t *testing.T) {
	bin := celltest.Engine(t)
	c := newCell(t)
	for i := range 40 {
		body := strings.Repeat(string(rune('a'+i%26)), 64<<10)
		if err := os.WriteFile(filepath.Join(c.Root, "f"+strconv.Itoa(i)+".txt"), []byte(body+strconv.Itoa(i)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := Engine{Binary: bin, DataRoot: t.TempDir(), Transport: Spawn{Binary: bin}}
	sealed, err := store.Seal(context.Background(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	outbox := t.TempDir()
	e := SyncEngine{
		Transport: Spawn{Binary: bin},
		Keys:      SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)},
		Ledger:    LedgerName("http://relay", "id_test"),
		Target:    func(c2 cell.Cell) Target { return store.target(c2) },
		Outbox:    func(cell.Cell) string { return outbox },
		MaxFrame:  256 << 10,
	}
	first, err := e.Export(context.Background(), c, sealed.Turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Frames) < 2 {
		t.Fatalf("fixture needs several frames, got %d", len(first.Frames))
	}
	born := map[string]os.FileInfo{}
	for _, f := range first.Frames {
		info, err := os.Stat(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		born[f.Path] = info
	}
	second, err := e.Export(context.Background(), c, sealed.Turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Frames) != len(first.Frames) || second.Bytes != first.Bytes {
		t.Fatalf("second export = %d frames %d bytes, want the first's %d frames %d bytes", len(second.Frames), second.Bytes, len(first.Frames), first.Bytes)
	}
	rewritten := 0
	for path, before := range born {
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) {
			rewritten++
		}
	}
	if rewritten != 0 {
		t.Fatalf("a repeated export rewrote %d of %d waiting frames", rewritten, len(born))
	}
}
