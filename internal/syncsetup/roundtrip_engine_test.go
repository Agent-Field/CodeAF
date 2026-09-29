//go:build engine

package syncsetup

import (
	"context"
	"os"
	"path/filepath"
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

// assertSameTree compares every file outside the cell's own directory.
func assertSameTree(t *testing.T, a, b string) {
	t.Helper()
	seen := 0
	err := filepath.WalkDir(a, func(path string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(a, path)
		if err != nil || d.IsDir() || rel == cell.StateDir || filepath.Dir(rel) == cell.StateDir {
			return err
		}
		seen++
		want, _ := os.ReadFile(path)
		got, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s: imported %q (%v), want %q", rel, got, err, want)
		}
		wi, _ := os.Stat(path)
		gi, _ := os.Stat(filepath.Join(b, rel))
		if gi != nil && gi.Mode() != wi.Mode() {
			t.Errorf("%s: mode %v, want %v", rel, gi.Mode(), wi.Mode())
		}
		return nil
	})
	if err != nil || seen == 0 {
		t.Fatalf("compared %d files, err %v", seen, err)
	}
}
