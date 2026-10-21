//go:build engine

package syncsetup

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
)

// TestEngineImportRoundTripIntoSecondCell is the byte-identical round trip with
// the real engine: what machine A published is fetched by machine B from the
// relay alone, imported into a second cell, and restored to the same bytes and
// the same modes.
func TestEngineImportRoundTripIntoSecondCell(t *testing.T) {
	r := newDriveRig(t)
	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	c.mustSay()
	waitFor(t, "the turns to be durable", func() bool { return r.directoryHead(r.cell.ID) == r.head() })

	workB := t.TempDir()
	cellB, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	engB := cellstore.EngineFor(workB)
	engB.DataRoot, engB.Binary, engB.Transport = t.TempDir(), r.bin, cellstore.Spawn{Binary: r.bin}
	sync := engB.Sync(cellstore.SyncKeys{CellKey: r.b.Identity.CellKey(), Dedup: r.b.Identity.DedupSecret()}, r.b.Ledger)
	fetch := cellsync.Fetcher{Engine: sync, Store: r.b.Store, Inbox: sync.Inbox}
	if err := fetch.Fetch(context.Background(), cellB, r.head()); err != nil {
		t.Fatal(err)
	}
	assertSameTree(t, r.work, workB)
}
