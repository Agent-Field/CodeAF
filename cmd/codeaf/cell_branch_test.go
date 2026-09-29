package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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

// unionMerger is a merge that keeps both sides' files, sealed through the fake engine.
type unionMerger struct {
	eng  *cellsync.FakeEngine
	fail error
}

func (m unionMerger) Merge(_ context.Context, parent, branch cell.Cell, _ string) (string, error) {
	if m.fail != nil {
		return "", m.fail
	}
	files := map[string]string{}
	for _, root := range []string{parent.Root, branch.Root} {
		for p, body := range readTree(root) {
			files[p] = body
		}
	}
	return m.eng.Seal(parent, files), nil
}

func readTree(root string) map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		raw, err := os.ReadFile(p)
		out[rel] = string(raw)
		return err
	})
	return out
}

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
	r.door = branchDoor{
		Dir:     dirA,
		Fetch:   &cellsync.Fetcher{Engine: r.engA, Store: r.store, Inbox: func(c cell.Cell) string { return filepath.Join(t.TempDir(), c.ID) }},
		Driver:  fixedDriver{r.drv},
		Merger:  unionMerger{eng: r.engA},
		Pub:     pub,
		Scratch: func(id string) cell.Cell { return cell.Cell{ID: id, Root: filepath.Join(t.TempDir(), "scratch", id)} },
	}
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
	if _, err := r.dir.For("dev_c").Acquire(context.Background(), parentID); err != nil {
		r.t.Fatal(err)
	}
}

func TestCellMergeDiscard(t *testing.T) {
	t.Run("merge keeps both sides files", func(t *testing.T) {
		r := newBranchRig(t)
		var out bytes.Buffer
		before := r.rec(parentID).Head
		if err := r.door.merge(context.Background(), r.parent, branchID, &out); err != nil {
			t.Fatal(err)
		}
		if got := r.rec(parentID).Head; got == before {
			t.Fatal("the parent's head did not move")
		}
		if !r.rec(branchID).Archived {
			t.Fatal("the merged branch was not archived")
		}
		// A reader on a fresh device sees both sides in the published head.
		reader := cell.Cell{ID: parentID, Root: t.TempDir()}
		f := &cellsync.Fetcher{Engine: cellsync.NewFakeEngine(t.TempDir()), Store: r.store, Inbox: func(c cell.Cell) string { return t.TempDir() }}
		if err := f.Fetch(context.Background(), reader, r.rec(parentID).Head); err != nil {
			t.Fatal(err)
		}
		got := readTree(reader.Root)
		if got["parent.txt"] != "parent" || got["branch.txt"] != "branch" {
			t.Fatalf("merged tree = %v, want both sides", got)
		}
	})

	t.Run("merge conflict leaves the branch alive", func(t *testing.T) {
		r := newBranchRig(t)
		boom := errors.New("conflict in parent.txt")
		r.door.Merger = unionMerger{eng: r.engA, fail: boom}
		before := r.rec(parentID).Head
		if err := r.door.merge(context.Background(), r.parent, branchID, &bytes.Buffer{}); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the conflict", err)
		}
		if r.rec(branchID).Archived || r.rec(parentID).Head != before {
			t.Fatal("a failed merge changed the directory")
		}
	})

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

	t.Run("neither runs without the parent's lease", func(t *testing.T) {
		r := newBranchRig(t)
		r.loseLease()
		before := r.rec(parentID).Head
		puts := len(r.store.Log())
		for name, run := range map[string]func() error{
			"merge":   func() error { return r.door.merge(context.Background(), r.parent, branchID, &bytes.Buffer{}) },
			"discard": func() error { return r.door.discard(context.Background(), r.parent, branchID, &bytes.Buffer{}) },
		} {
			if err := run(); !errors.Is(err, directory.ErrFenceStale) {
				t.Errorf("%s: err = %v, want ErrFenceStale", name, err)
			}
		}
		if r.rec(branchID).Archived || r.rec(parentID).Head != before || len(r.store.Log()) != puts {
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

func TestCellMergeDiscardNeedSyncSetUp(t *testing.T) {
	for _, verb := range []string{"merge", "discard"} {
		err := runCellIn([]string{verb, branchID, mustCellRoot(t)}, &bytes.Buffer{}, t.TempDir())
		if err == nil {
			t.Errorf("%s ran on a device with no sync", verb)
		}
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
