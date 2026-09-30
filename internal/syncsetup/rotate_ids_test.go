package syncsetup

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// exportUnder seals everything a head reaches under one identity's keys into a
// fresh store and answers the store and the head's remote id.
func (r *driveRig) exportUnder(id identity.Identity) (blobstore.Store, string) {
	r.t.Helper()
	store := blobstore.NewMemory()
	sync := r.engine.Sync(cellstore.SyncKeys{CellKey: id.CellKey(), Dedup: id.DedupSecret()}, cellstore.LedgerName("test", id.ID()))
	pub := cellsync.Publisher{Engine: sync, Store: store}
	ex, err := pub.Upload(context.Background(), r.cell, r.head())
	if err != nil {
		r.t.Fatal(err)
	}
	return store, ex.HeadRID
}

// TestKeepsSnapshotIDs (and convergence after a rotation) is the fact a rotation rests on: keys change every remote
// id, never a turn id. The same turn sealed under two identities has two
// different remote ids, and a device holding only the new identity's keys takes
// it back to the same turn id and the same bytes.
func TestKeepsSnapshotIDs(t *testing.T) {
	r := newDriveRig(t)
	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	c.mustSay()
	waitFor(t, "the turns to be durable", func() bool { return r.directoryHead(r.cell.ID) == r.head() })

	next, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	_, oldRID := r.exportUnder(r.a.Identity)
	store, newRID := r.exportUnder(next)
	if oldRID == newRID {
		t.Fatalf("the head has one remote id %s under both identities", oldRID)
	}

	workB := t.TempDir()
	cellB, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	engB := cellstore.EngineFor(workB)
	engB.DataRoot, engB.Binary, engB.Transport = t.TempDir(), r.bin, cellstore.Spawn{Binary: r.bin}
	sync := engB.Sync(cellstore.SyncKeys{CellKey: next.CellKey(), Dedup: next.DedupSecret()}, cellstore.LedgerName("test", next.ID()))
	fetch := cellsync.Fetcher{Engine: sync, Store: store, Inbox: sync.Inbox}
	if err := fetch.Fetch(context.Background(), cellB, r.head()); err != nil {
		t.Fatalf("the new identity's keys did not take the old turn id back: %v", err)
	}
	assertSameTree(t, r.work, workB)

	// Convergence: machine B, holding only what it took back, seals the same
	// turn under the same identity to the same remote id, so a device that is
	// paired after the rotation adds nothing the relay already has.
	again := engB.Sync(cellstore.SyncKeys{CellKey: next.CellKey(), Dedup: next.DedupSecret()}, cellstore.LedgerName("elsewhere", next.ID()))
	ex, err := (&cellsync.Publisher{Engine: again, Store: blobstore.NewMemory()}).Upload(context.Background(), cellB, r.head())
	if err != nil || ex.HeadRID != newRID {
		t.Fatalf("machine B sealed the turn as %s (%v), machine A as %s", ex.HeadRID, err, newRID)
	}
}
