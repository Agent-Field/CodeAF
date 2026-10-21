//go:build engine

package cellsync

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// TestAFreshLedgerSendsOnlyTheTurnRealEngine is the same rule on the real
// engine, whose frames the Rust side must accept after the split: the store
// holds a 1 MiB chat another ledger published, a second ledger publishes one
// small turn on top, and only the turn crosses the wire.
func TestAFreshLedgerSendsOnlyTheTurnRealEngine(t *testing.T) {
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	ctx := context.Background()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	local := cellstore.Engine{Binary: bin, DataRoot: t.TempDir(), Transport: cellstore.Spawn{Binary: bin}}
	engineFor := func(name string) cellstore.SyncEngine {
		return cellstore.SyncEngine{
			Transport: cellstore.Spawn{Binary: bin},
			Keys:      cellstore.SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)},
			Ledger:    cellstore.LedgerName("http://relay-"+name, "id_test"),
			Target: func(c2 cell.Cell) cellstore.Target {
				return cellstore.Target{DataDir: local.LocalDir(c2), Tree: c2.Root}
			},
			Outbox: func(cell.Cell) string { return t.TempDir() },
		}
	}
	chat := make([]byte, 1<<20)
	_, _ = rand.Read(chat)
	if err := os.WriteFile(filepath.Join(c.Root, "chat"), chat, 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := local.Seal(ctx, c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	mem := blobstore.NewMemory()
	if _, err := (&Publisher{Engine: engineFor("a"), Store: mem}).Upload(ctx, c, first.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, "turn"), []byte("one small turn"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := local.Seal(ctx, c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}

	counters := &blobstore.Counters{}
	fresh := engineFor("b")
	whole, err := fresh.Export(ctx, c, second.Turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Publisher{Engine: fresh, Store: blobstore.Counting{Inner: mem, C: counters}}).Upload(ctx, c, second.Turn.ID); err != nil {
		t.Fatal(err)
	}
	t.Logf("bytes sent: %d before, %d after", whole.Bytes, counters.BytesUp.Load())
	if got := counters.BytesUp.Load(); got > askFloor {
		t.Fatalf("a fresh ledger sent %d bytes for a small turn", got)
	}
	if again, err := fresh.Export(ctx, c, second.Turn.ID); err != nil || again.Objects != 0 {
		t.Fatalf("the ledger still lacks %d objects (%v)", again.Objects, err)
	}
}
