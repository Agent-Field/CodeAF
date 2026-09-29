package cellsync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// TestBranchCreatesChildWithParent is L12: three turns are sealed while the
// upload is blocked, another device takes over, and on return the turns become
// a child cell while the old head is untouched.
func TestBranchCreatesChildWithParent(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	h1 := r.publishFirst(map[string]string{"a": "0"})
	var branches, by []string
	b.OnSuperseded = func(s Superseded) { branches, by = append(branches, s.Branch), append(by, s.By) }

	r.store.set(blobstore.ErrUnreachable, nil)
	var last string
	for _, v := range []string{"1", "2", "3"} {
		last = r.seal(map[string]string{"a": v})
		note(b, last)
		b.flush(context.Background())
	}
	if b.Pending() != 3 {
		t.Fatalf("pending = %d", b.Pending())
	}
	r.takeOver()
	r.bPublishes(h1)
	r.store.set(nil, nil)

	b.flush(context.Background())
	if len(branches) != 1 || by[0] != devB {
		t.Fatalf("OnSuperseded calls: %v by %v", branches, by)
	}
	child := r.head(branches[0])
	if child.Head != last || child.ParentCell != cellID || child.OrphanTurns != 3 ||
		child.Lease.Device != devA || child.Lease.Fence != 1 {
		t.Fatalf("child = %+v", child)
	}
	if old := r.head(cellID); old.Head != otherHead || old.Lease.Device != devB {
		t.Fatalf("old cell was touched: %+v", old)
	}
	if b.Pending() != 0 || r.drv.ID() != branches[0] || r.drv.Cell.ID != cellID {
		t.Fatalf("driving %+v pending %d", r.drv, b.Pending())
	}
	if got := r.branch.Resolve(cellID); got != branches[0] {
		t.Fatalf("branch map resolves the chat to %s", got)
	}

	// The turns are readable from the store through the branch's head.
	f, c := fetcherFor(t, r.mem)
	if err := f.Fetch(context.Background(), c, child.Head); err != nil {
		t.Fatalf("the branch head is not in the store: %v", err)
	}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, map[string]string{"a": "3"}) {
		t.Fatalf("branch files %v", got)
	}
}

func TestTakeoverDuringUploadBranches(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	h1 := r.publishFirst(map[string]string{"a": "0"})
	var branches, by []string
	b.OnSuperseded = func(s Superseded) { branches, by = append(branches, s.Branch), append(by, s.By) }
	h2 := r.seal(map[string]string{"a": "1"})
	note(b, h2)

	var once sync.Once
	r.store.set(nil, func(context.Context) { // B takes over while A is mid-upload
		once.Do(func() { r.takeOver(); r.bPublishes(h1) })
	})
	b.flush(context.Background())

	if len(branches) != 1 {
		t.Fatalf("OnSuperseded calls: %v", branches)
	}
	if old := r.head(cellID); old.Head != otherHead {
		t.Fatalf("B's head was disturbed: %+v", old)
	}
	if got := r.head(branches[0]); got.Head != h2 || got.ParentCell != cellID || got.OrphanTurns != 1 {
		t.Fatalf("branch = %+v", got)
	}
}

func TestBranchMapPersists(t *testing.T) {
	home := t.TempDir()
	m, err := OpenBranchMap(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Resolve("old"); got != "old" {
		t.Fatalf("an unbranched id resolved to %q", got)
	}
	if err := m.Set("old", "new1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set("new1", "new2"); err != nil {
		t.Fatal(err)
	}

	again, err := OpenBranchMap(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Resolve("old"); got != "new2" {
		t.Fatalf("after reopen old resolves to %q", got)
	}
	raw, err := os.ReadFile(filepath.Join(home, "v3", "sync", "branches.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		V   int
		Map map[string]string
	}
	if err := json.Unmarshal(raw, &file); err != nil || file.V != 1 || file.Map["old"] != "new1" {
		t.Fatalf("branches.json = %s (%v)", raw, err)
	}
}

func TestBranchWithoutMapStillCreates(t *testing.T) {
	r := newRig(t)
	h1 := r.publishFirst(map[string]string{"a": "0"})
	id, err := Brancher{Dir: r.dirA, NewID: r.newID}.Branch(context.Background(), r.drv, h1, 2, r.info())
	if err != nil {
		t.Fatal(err)
	}
	if got := r.head(id); got.OrphanTurns != 2 || got.ParentCell != cellID {
		t.Fatalf("branch = %+v", got)
	}
	if _, err := r.dir.For(devB).Acquire(context.Background(), id); err != directory.ErrLeaseHeld {
		t.Fatalf("the branch is held by A: %v", err)
	}
}
