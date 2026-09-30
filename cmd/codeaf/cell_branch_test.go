package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

const (
	parentID = "01J0000000000000000000000A"
	branchID = "01J0000000000000000000000B"
)

// branchRig is device A driving a chat whose death branch device B made.
type branchRig struct {
	t      *testing.T
	clock  *directorytest.FakeClock
	dir    *directory.Memory
	store  *blobstore.Memory
	engA   *cellsync.FakeEngine
	parent cell.Cell
	drv    *cellsync.Driving
	door   branchDoor
}

type fixedDriver struct{ d *cellsync.Driving }

func (f fixedDriver) Driving(context.Context, cell.Cell) (*cellsync.Driving, error) { return f.d, nil }

func newBranchRig(t *testing.T) *branchRig {
	t.Helper()
	ctx := context.Background()
	r := &branchRig{t: t, clock: directorytest.NewFakeClock(), store: blobstore.NewMemory(), engA: cellsync.NewFakeEngine(t.TempDir())}
	r.dir = directory.NewMemory(r.clock.Now)
	dirA := r.dir.For("dev_a")
	r.parent = cell.Cell{ID: parentID, Root: t.TempDir()}
	if err := os.WriteFile(filepath.Join(r.parent.Root, "parent.txt"), []byte("parent"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.drv = &cellsync.Driving{Cell: r.parent}
	pub := &cellsync.Publisher{Engine: r.engA, Store: r.store, Dir: dirA}
	head := r.engA.Seal(r.parent, map[string]string{"parent.txt": "parent"})
	if err := pub.Publish(ctx, r.drv, head, cellsync.PublishInfo{Class: "chat", Size: 1}); err != nil {
		t.Fatal(err)
	}
	r.makeBranch(map[string]string{"branch.txt": "branch"})
	r.door = branchDoor{Dir: dirA, Driver: fixedDriver{r.drv}}
	return r
}

// makeBranch is device B's death branch: its objects reach the store, then the record.
func (r *branchRig) makeBranch(files map[string]string) {
	r.t.Helper()
	ctx := context.Background()
	engB := cellsync.NewFakeEngine(r.t.TempDir())
	bc := cell.Cell{ID: branchID, Root: r.t.TempDir()}
	head := engB.Seal(bc, files)
	ex, err := engB.Export(ctx, bc, head)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, f := range ex.Frames {
		raw, _ := os.ReadFile(f.Path)
		if _, err := r.store.PutFrame(ctx, raw); err != nil {
			r.t.Fatal(err)
		}
	}
	_, err = r.dir.For("dev_b").Create(ctx, branchID, directory.CellInit{Head: head, Class: "chat", ParentCell: parentID, OrphanTurns: 1})
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *branchRig) rec(id string) directory.Cell {
	r.t.Helper()
	v, err := r.dir.For("dev_a").Cell(context.Background(), id)
	if err != nil {
		r.t.Fatal(err)
	}
	return v.Cell
}

// loseLease lets A's lease lapse and gives it to device C.
func (r *branchRig) loseLease() {
	r.t.Helper()
	r.clock.Advance(directory.LeaseTTL + 1)
	if _, err := r.dir.For("dev_c").Acquire(context.Background(), parentID, directory.AcquireOpts{}); err != nil {
		r.t.Fatal(err)
	}
}

func TestCellDiscard(t *testing.T) {
	t.Run("discard archives", func(t *testing.T) {
		r := newBranchRig(t)
		before := r.rec(parentID).Head
		if err := r.door.discard(context.Background(), r.parent, branchID, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if !r.rec(branchID).Archived || r.rec(parentID).Head != before {
			t.Fatal("discard must archive the branch and leave the parent alone")
		}
	})

	t.Run("discard does not run without the parent's lease", func(t *testing.T) {
		r := newBranchRig(t)
		r.loseLease()
		before := r.rec(parentID).Head
		if err := r.door.discard(context.Background(), r.parent, branchID, &bytes.Buffer{}); !errors.Is(err, directory.ErrFenceStale) {
			t.Errorf("discard: err = %v, want ErrFenceStale", err)
		}
		if r.rec(branchID).Archived || r.rec(parentID).Head != before {
			t.Fatal("a device without the lease changed something")
		}
	})

	t.Run("a stranger's branch is refused", func(t *testing.T) {
		r := newBranchRig(t)
		if _, err := r.dir.For("dev_b").Create(context.Background(), "other", directory.CellInit{Head: "h", Class: "chat"}); err != nil {
			t.Fatal(err)
		}
		if err := r.door.discard(context.Background(), r.parent, "other", &bytes.Buffer{}); err == nil {
			t.Fatal("discarded a cell that is not a branch of this chat")
		}
	})
}

func TestCellDiscardNeedsSyncSetUp(t *testing.T) {
	t.Setenv("CODEAF_SYNC_URL", "")
	if err := runCellIn([]string{"discard", branchID, mustCellRoot(t)}, &bytes.Buffer{}, t.TempDir()); err == nil {
		t.Error("discard ran on a device with no sync")
	}
}

// TestThereIsNoCellMerge pins that the verb is absent, not broken: it is not in
// the usage line and asking for it is the usage line.
func TestThereIsNoCellMerge(t *testing.T) {
	if strings.Contains(cellUsage, "merge") {
		t.Errorf("the usage line still offers merge: %s", cellUsage)
	}
	err := runCellIn([]string{"merge", branchID, mustCellRoot(t)}, &bytes.Buffer{}, t.TempDir())
	if err == nil || err.Error() != cellUsage {
		t.Errorf("cell merge answered %v, want the usage line", err)
	}
}

func mustCellRoot(t *testing.T) string {
	t.Helper()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	return c.Root
}
