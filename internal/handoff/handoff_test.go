package handoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// backFromB is the common scene: A started the chat, B continued it and let go.
func backFromB(t *testing.T) (w *world, a, b *device) {
	t.Helper()
	w = newWorld(t)
	a, b = w.device(devA), w.device(devB)
	drv := a.start(map[string]string{"a.txt": "one"})
	a.release(drv)
	b.work(map[string]string{"a.txt": "one", "b.txt": "from b"})
	w.trace.steps = nil
	return w, a, b
}

func TestTakeKeepsLocalEditsInBranch(t *testing.T) {
	w, a, _ := backFromB(t)
	head := w.cellRec(chatID).Head
	if err := os.WriteFile(filepath.Join(a.root(chatID), "a.txt"), []byte("hand edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.local.dirty = true

	taken, err := a.taker().Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	if taken.Kept == "" {
		t.Fatal("Kept is empty; the edit was not branched")
	}
	want := []string{"seal", "branch", "fetch", "acquire"}
	if !reflect.DeepEqual(w.trace.steps, want) {
		t.Fatalf("order = %v, want %v (edits are kept before anything is fetched)", w.trace.steps, want)
	}
	if got := tree(t, taken.Cell.Root); got["b.txt"] != "from b" || got["a.txt"] != "one" {
		t.Fatalf("root = %v, want B's head materialized", got)
	}
	rec := w.cellRec(taken.Kept)
	if rec.ParentCell != chatID || rec.OrphanTurns != 1 {
		t.Fatalf("branch record = parent %q orphan %d, want parent %s orphan 1", rec.ParentCell, rec.OrphanTurns, chatID)
	}
	if taken.Driving.Head != head {
		t.Fatalf("driving head = %s, want %s", taken.Driving.Head, head)
	}
	if got := fetchBranch(t, w, rec.Head); got["a.txt"] != "hand edit" {
		t.Fatalf("branch holds %v, want the hand edit", got)
	}
}

// fetchBranch materializes a head on a third device and reads it back.
func fetchBranch(t *testing.T, w *world, head string) map[string]string {
	t.Helper()
	c := w.device("dev_c")
	cl := c.cell("branch-view")
	if err := c.taker().Fetch.Fetch(context.Background(), cl, head); err != nil {
		t.Fatal(err)
	}
	return tree(t, cl.Root)
}

func TestTakeCleanExistingRootIsNotBranched(t *testing.T) {
	_, a, _ := backFromB(t)
	taken, err := a.taker().Take(context.Background(), chatID)
	if err != nil || taken.Kept != "" {
		t.Fatalf("Take = kept %q, err %v; a clean root needs no branch", taken.Kept, err)
	}
}

func TestTakeBackAfterAToBAndAgain(t *testing.T) {
	w, a, b := backFromB(t)
	taken, err := a.taker().Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	if taken.Driving.Fence != 3 || taken.Kept != "" {
		t.Fatalf("fence %d kept %q, want fence 3 and nothing kept", taken.Driving.Fence, taken.Kept)
	}
	if got := tree(t, taken.Cell.Root); got["b.txt"] != "from b" {
		t.Fatalf("root = %v, want B's work", got)
	}
	a.release(taken.Driving)
	head := b.work(map[string]string{"b.txt": "second b"})
	again, err := a.taker().Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Driving.Head != head || tree(t, again.Cell.Root)["b.txt"] != "second b" {
		t.Fatalf("second take = head %s tree %v, want %s and B's newest", again.Driving.Head, tree(t, again.Cell.Root), head)
	}
	if got := w.cellRec(chatID).Lease.Device; got != devA {
		t.Fatalf("lease holder %s, want %s", got, devA)
	}
}

func TestTakeFetchFailureTakesNoLease(t *testing.T) {
	w, a, _ := backFromB(t)
	before := w.cellRec(chatID).Lease
	a.store.down = blobstore.ErrUnreachable

	_, err := a.taker().Take(context.Background(), chatID)
	if !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("err = %v, want the store's", err)
	}
	if w.trace.has("acquire") || w.cellRec(chatID).Lease != before {
		t.Fatalf("lease touched: trace %v, lease %+v", w.trace.steps, w.cellRec(chatID).Lease)
	}
}

func TestTakeMissingObjectNamesItAndLeavesRootClean(t *testing.T) {
	w, _, _ := backFromB(t)
	c := w.device("dev_c")
	c.store.hide = true

	_, err := c.taker().Take(context.Background(), chatID)
	if err == nil || c.store.Missing == "" || !strings.Contains(err.Error(), c.store.Missing) {
		t.Fatalf("err = %v, want it to name object %q", err, c.store.Missing)
	}
	if !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("err = %v, want blobstore.ErrNotFound underneath", err)
	}
	if _, statErr := os.Stat(c.root(chatID)); !os.IsNotExist(statErr) {
		t.Fatalf("root exists after a failed take: %v", tree(t, c.root(chatID)))
	}
	if w.trace.has("acquire") {
		t.Fatal("a lease was taken for a head that could not be fetched")
	}
}

func TestTakeRefusedWhenDirectoryUnreachable(t *testing.T) {
	w, a, _ := backFromB(t)
	a.local.dirty = true
	a.dir.down = directory.ErrUnreachable

	_, err := a.taker().Take(context.Background(), chatID)
	if !errors.Is(err, directory.ErrUnreachable) {
		t.Fatalf("err = %v, want directory.ErrUnreachable", err)
	}
	if len(w.trace.steps) != 0 {
		t.Fatalf("steps %v ran; nothing may change without the directory", w.trace.steps)
	}
}

func TestTakeLosesRace(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	drv := a.start(map[string]string{"a.txt": "one"})
	a.release(drv)
	ran := false
	a.hooks = []func(context.Context, cell.Cell) error{func(context.Context, cell.Cell) error { ran = true; return nil }}
	// B wins the acquire while A is still fetching.
	a.onFetc = func() {
		if _, err := b.dir.Client.Acquire(context.Background(), chatID); err != nil {
			t.Fatal(err)
		}
	}
	before := w.cellRec(chatID).Lease

	taken, err := a.taker().Take(context.Background(), chatID)
	if !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("err = %v, want ErrLeaseHeld", err)
	}
	if ran || taken.Kept != "" || taken.Driving.Fence != 0 {
		t.Fatalf("hooks ran %v, taken %+v; a lost race changes nothing", ran, taken)
	}
	if got := w.cellRec(chatID).Lease; got.Device != devB || got.Fence != before.Fence+1 {
		t.Fatalf("lease = %+v, want B's, untouched by A", got)
	}
}

func TestTakeRefetchesMovedHead(t *testing.T) {
	w, a, b := backFromB(t)
	stale := w.cellRec(chatID).Head
	var moved string
	a.onFetc = func() { moved = b.work(map[string]string{"b.txt": "moved"}) }
	tk := a.taker()

	taken, err := tk.Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	heads := tk.Fetch.(*tracedFetch).heads
	if !reflect.DeepEqual(heads, []string{stale, moved}) {
		t.Fatalf("fetched %v, want the stale head then %s", heads, moved)
	}
	if taken.Driving.Head != moved || tree(t, taken.Cell.Root)["b.txt"] != "moved" {
		t.Fatalf("opened head %s tree %v, want the moved head", taken.Driving.Head, tree(t, taken.Cell.Root))
	}
}

func TestTakeAfterHooksRunInOrderBeforeOpen(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	// The head carries no meta.json; only a hook makes the folder openable, so
	// a successful take proves the hooks ran before cell.OpenAt.
	w.bare = true
	a.release(a.start(nil))
	var order []string
	b.hooks = []func(context.Context, cell.Cell) error{
		func(context.Context, cell.Cell) error { order = append(order, "first"); return nil },
		func(_ context.Context, c cell.Cell) error {
			order = append(order, "second")
			meta := filepath.Join(c.Root, cell.MetaPath)
			if err := os.MkdirAll(filepath.Dir(meta), 0o700); err != nil {
				return err
			}
			return os.WriteFile(meta, []byte(w.meta), 0o600)
		},
	}
	if _, err := b.taker().Take(context.Background(), chatID); err != nil {
		t.Fatalf("Take: %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("hook order = %v", order)
	}
}

func TestTakeFailingHookFailsBeforeOpenAndFreesLease(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	w.bare = true // the folder cannot be opened, so an open error would show if hooks did not come first
	a.release(a.start(nil))
	boom := errors.New("vault refused")
	var ran []string
	b.hooks = []func(context.Context, cell.Cell) error{
		func(context.Context, cell.Cell) error { ran = append(ran, "one"); return nil },
		func(context.Context, cell.Cell) error { ran = append(ran, "two"); return boom },
		func(context.Context, cell.Cell) error { ran = append(ran, "three"); return nil },
	}

	_, err := b.taker().Take(context.Background(), chatID)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the hook's error (and not an open error: hooks come first)", err)
	}
	if !reflect.DeepEqual(ran, []string{"one", "two"}) {
		t.Fatalf("hooks ran %v, want the first two only", ran)
	}
	if got := w.cellRec(chatID).Lease.Expires; got != 0 {
		t.Fatalf("lease expires %d after a failed take, want it released", got)
	}
}
