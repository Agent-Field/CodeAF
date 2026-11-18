package handoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// The tests here state what an open that overlaps its fetch with its lease
// acquire owes a person: the overlap may only ever happen when taking the lease
// cannot fence anybody, and a refused acquire never turns into a forced one
// aimed at somebody the person did not choose.

// overlappingTaker is this device's Taker wired the way the fast open path
// wires one: the head is staged first (StagePrepare), the lease acquire may run
// beside that fetch (ConcurrentClaim), and a refused acquire re-reads the record
// (Revalidate). Staging does what a fetch of the chat's state does, and the
// install puts the staged folder in place once the lease is held.
func (d *device) overlappingTaker() Taker {
	tk := d.taker()
	fetch := tk.Fetch
	tk.StagePrepare = func(ctx context.Context, stage cell.Cell, head string) error {
		return fetch.Fetch(ctx, stage, head)
	}
	tk.InstallState = func(_ context.Context, stage, target cell.Cell, _ string) error {
		if err := os.MkdirAll(filepath.Dir(target.Root), 0o700); err != nil {
			return err
		}
		return Swap(stage.Root, target.Root)
	}
	tk.ConcurrentClaim = true
	dir := d.dir
	tk.Revalidate = func(ctx context.Context) (directory.CellView, error) { return dir.Cell(ctx, chatID) }
	return tk
}

// acquireCount is how many times the take asked the directory for the lease.
func (w *world) acquireCount() int {
	w.trace.mu.Lock()
	defer w.trace.mu.Unlock()
	n := 0
	for _, s := range w.trace.steps {
		if s == "acquire" {
			n++
		}
	}
	return n
}

// Contract O1: a take that fails before the chat opens leaves a live holder
// exactly as it was. A holder in the middle of a turn on another computer keeps
// its lease, its fence and its right to publish, because nothing was acquired.
func TestAFailedStageLeavesALiveHolderTheWriter(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	drv := a.start(map[string]string{"a.txt": "one"}) // A is live, mid-chat
	before := w.cellRec(chatID)

	tk := b.overlappingTaker()
	tk.StagePrepare = func(context.Context, cell.Cell, string) error { return blobstore.ErrUnreachable }
	_, err := tk.Take(context.Background(), chatID)
	if !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("Take = %v, want the failed fetch", err)
	}

	after := w.cellRec(chatID)
	if after.Lease.Device != devA || after.Lease.Fence != before.Lease.Fence || after.Lease.Expires == 0 {
		t.Fatalf("lease after a take that failed to fetch = %+v, want A still holding at fence %d", after.Lease, before.Lease.Fence)
	}
	if n := w.acquireCount(); n != 0 {
		t.Fatalf("the take asked for the lease %d times before it had anything to open; want none", n)
	}
	pub := &cellsync.Publisher{Engine: a.eng, Store: a.store, Dir: a.dir}
	next := a.eng.Seal(a.cell(chatID), w.withMeta(map[string]string{"a.txt": "two"}))
	if err := pub.Publish(context.Background(), &drv, next, cellsync.PublishInfo{Class: "chat"}); err != nil {
		t.Fatalf("A's next turn after B's failed take = %v, want it published as before", err)
	}
}

// Contract O1: a lease that merely lapsed is still somebody's. Its holder may
// only be late with a heartbeat (a closed lid, a stalled network) and mid-turn
// when it comes back, so taking it is fenced behind a fetch that succeeded too:
// a take that fails to arrive leaves that holder its fence and its right to
// publish.
func TestAFailedStageLeavesALapsedHolderItsFence(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	drv := a.start(map[string]string{"a.txt": "one"})
	a.lapse() // A's lease ran out; A has not been heard from, and has not been replaced
	before := w.cellRec(chatID)

	tk := b.overlappingTaker()
	tk.StagePrepare = func(context.Context, cell.Cell, string) error { return blobstore.ErrUnreachable }
	if _, err := tk.Take(context.Background(), chatID); !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("Take = %v, want the failed fetch", err)
	}
	if n := w.acquireCount(); n != 0 {
		t.Fatalf("the take asked for the lease %d times before it had anything to open; want none", n)
	}
	if got := w.cellRec(chatID); got.Lease.Device != devA || got.Lease.Fence != before.Lease.Fence {
		t.Fatalf("lease = %+v, want A's at fence %d", got.Lease, before.Lease.Fence)
	}
	pub := &cellsync.Publisher{Engine: a.eng, Store: a.store, Dir: a.dir}
	next := a.eng.Seal(a.cell(chatID), w.withMeta(map[string]string{"a.txt": "two"}))
	if err := pub.Publish(context.Background(), &drv, next, cellsync.PublishInfo{Class: "chat"}); err != nil {
		t.Fatalf("A's turn after coming back from its lapse = %v, want it published as before", err)
	}
}

// Contract O1: a take of a chat somebody is driving fetches first and acquires
// after, in series, as it always did. Nothing the person did not yet decide is
// fenced while the fetch is still running.
func TestALiveHoldersLeaseIsNotTouchedWhileTheFetchRuns(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	a.start(map[string]string{"a.txt": "one"})

	tk := b.overlappingTaker()
	stage := tk.StagePrepare
	var asked bool
	tk.StagePrepare = func(ctx context.Context, staged cell.Cell, head string) error {
		err := stage(ctx, staged, head)
		// Give a lease request that was sent beside the fetch time to land.
		deadline := time.Now().Add(150 * time.Millisecond)
		for time.Now().Before(deadline) && w.acquireCount() == 0 {
			time.Sleep(2 * time.Millisecond)
		}
		asked = w.acquireCount() > 0
		return err
	}
	taken, err := tk.Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	if asked {
		t.Fatal("the lease of a live holder was asked for while the fetch was still running")
	}
	if taken.Driving.Fence != 2 || w.cellRec(chatID).Lease.Device != devB {
		t.Fatalf("fence %d, holder %s; want B at fence 2 once the fetch is done", taken.Driving.Fence, w.cellRec(chatID).Lease.Device)
	}
}

// signalDir tells the test when the take asks for the lease.
type signalDir struct {
	directory.Client
	once sync.Once
	seen chan struct{}
}

func (d *signalDir) Acquire(ctx context.Context, id string, o directory.AcquireOpts) (directory.CellView, error) {
	d.once.Do(func() { close(d.seen) })
	return d.Client.Acquire(ctx, id, o)
}

// Contract O1, the half that keeps the speed: a chat nobody holds (released)
// still has its lease asked for beside the fetch, because taking it can fence
// nobody. The fetch here cannot finish until the lease request has been sent,
// so a take that waited for the fetch first could not open at all.
func TestAFreeChatHasItsLeaseAskedForBesideTheFetch(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	a.release(a.start(map[string]string{"a.txt": "one"}))

	tk := b.overlappingTaker()
	sig := &signalDir{Client: b.dir, seen: make(chan struct{})}
	tk.Dir = sig
	stage := tk.StagePrepare
	tk.StagePrepare = func(ctx context.Context, staged cell.Cell, head string) error {
		select {
		case <-sig.seen:
		case <-time.After(5 * time.Second):
			return errors.New("the lease was not asked for while the fetch ran")
		}
		return stage(ctx, staged, head)
	}
	taken, err := tk.Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	if w.cellRec(chatID).Lease.Device != devB || tree(t, taken.Cell.Root)["a.txt"] != "one" {
		t.Fatalf("holder %s tree %v; want B holding A's files", w.cellRec(chatID).Lease.Device, tree(t, taken.Cell.Root))
	}
}

// Contract O12: a forced take covers the holder and the turn the person saw, and
// nobody else. When the acquire is refused because the record moved, the take
// looks again, and if the record now names a different holder it is refused
// like any other lost race: the new holder keeps its lease, and nothing is
// fetched or acquired a second time on the person's behalf.
func TestARefusedClaimNeverForcesAHolderThePersonDidNotChoose(t *testing.T) {
	for _, withCard := range []bool{false, true} {
		name := map[bool]string{false: "no card", true: "card names A"}[withCard]
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			a, b, c := w.device(devA), w.device(devB), w.device("dev_c")
			a.release(a.start(map[string]string{"a.txt": "one"}))
			seenView, err := b.dir.Cell(context.Background(), chatID) // B's reading: A's, released
			if err != nil {
				t.Fatal(err)
			}
			// C takes the chat before B asks for it and is driving it now.
			if _, err := c.taker().Take(context.Background(), chatID); err != nil {
				t.Fatal(err)
			}
			held := w.cellRec(chatID)
			w.trace.steps = nil

			tk := b.overlappingTaker()
			tk.View = &seenView
			var seen *directory.LeaseExpectation
			if withCard {
				seen = &directory.LeaseExpectation{Device: seenView.Cell.Lease.Device, Fence: seenView.Cell.Lease.Fence}
			}
			_, err = tk.TakeSeen(context.Background(), chatID, seen)
			if !errors.Is(err, directory.ErrLeaseHeld) {
				t.Fatalf("TakeSeen = %v, want ErrLeaseHeld: C took the chat after the reading B acted on", err)
			}
			if got := w.cellRec(chatID); got.Lease.Device != "dev_c" || got.Lease.Fence != held.Lease.Fence || got.Lease.Expires == 0 {
				t.Fatalf("lease = %+v, want C still driving at fence %d", got.Lease, held.Lease.Fence)
			}
			if n := w.acquireCount(); n != 1 {
				t.Fatalf("the take asked for the lease %d times, want 1 (the refused one)", n)
			}
			if _, statErr := os.Stat(b.root(chatID)); !os.IsNotExist(statErr) {
				t.Fatalf("a refused take left a root behind (%v)", statErr)
			}
		})
	}
}

// Contract O12: the computer taking the chat is not a holder it may force
// either. If this computer's own record has taken the lease since the reading
// (another window of it), the take is a lost race, not a displacement of itself.
func TestARefusedClaimNeverForcesTheCallersOwnNewerLease(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	a.release(a.start(map[string]string{"a.txt": "one"}))
	seenView, err := b.dir.Cell(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	// Another window of B's own computer takes the lease first.
	other, err := b.dir.Client.Acquire(context.Background(), chatID, directory.AcquireOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.trace.steps = nil

	tk := b.overlappingTaker()
	tk.View = &seenView
	_, err = tk.TakeSeen(context.Background(), chatID, nil)
	if !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("TakeSeen = %v, want ErrLeaseHeld", err)
	}
	if got := w.cellRec(chatID); got.Lease.Fence != other.Cell.Lease.Fence || got.Lease.Expires == 0 {
		t.Fatalf("lease = %+v, want the other window's still at fence %d", got.Lease, other.Cell.Lease.Fence)
	}
}

// movedTurnUnderTheSameFence is B publishing one more turn after A read the
// record and before A acts on it, under the fence B already held, then letting
// go again. It answers the head that is now the chat's.
func movedTurnUnderTheSameFence(t *testing.T, w *world, b *device, read directory.CellView) string {
	t.Helper()
	cl := b.cell(chatID)
	moved := b.eng.Seal(cl, w.withMeta(map[string]string{"b.txt": "moved on"}))
	drv := cellsync.Driving{Cell: cl, Head: read.Cell.Head, Fence: read.Cell.Lease.Fence}
	pub := &cellsync.Publisher{Engine: b.eng, Store: b.store, Dir: b.dir.Client}
	if err := pub.Publish(context.Background(), &drv, moved, cellsync.PublishInfo{Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	b.release(drv)
	return moved
}

// Contract O12: a card that named a turn covers that turn. The same holder
// publishing a newer one after the card was drawn is not what the person
// chose, so the take stops where the stale head was found rather than fetching
// and acquiring the newer one on their behalf.
func TestARefusedClaimStopsWhenTheCardsTurnMoved(t *testing.T) {
	w, a, b := backFromB(t)
	read, err := a.dir.Cell(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	movedTurnUnderTheSameFence(t, w, b, read)
	w.trace.steps = nil

	tk := a.overlappingTaker()
	tk.View = &read
	seen := &directory.LeaseExpectation{Device: read.Cell.Lease.Device, Fence: read.Cell.Lease.Fence, Head: read.Cell.Head}
	_, err = tk.TakeSeen(context.Background(), chatID, seen)
	if !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("TakeSeen = %v, want ErrLeaseHeld: the card's turn is no longer the chat's", err)
	}
	if n := w.acquireCount(); n != 1 {
		t.Fatalf("the take asked for the lease %d times, want 1 (the refused one)", n)
	}
	if heads := tk.Fetch.(*tracedFetch).heads; len(heads) != 1 {
		t.Fatalf("fetched %v, want one fetch of the card's turn and no second one", heads)
	}
}

// Contract O12, the half that keeps working: the same holder at the same fence
// publishing a newer turn after the reading, with no turn named on the card, is
// the ordinary stale-reading fallback. The newer turn is opened.
func TestARefusedClaimFollowsTheSameHoldersNewerTurn(t *testing.T) {
	w, a, b := backFromB(t)
	read, err := a.dir.Cell(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	moved := movedTurnUnderTheSameFence(t, w, b, read)

	tk := a.overlappingTaker()
	tk.View = &read
	seen := &directory.LeaseExpectation{Device: read.Cell.Lease.Device, Fence: read.Cell.Lease.Fence}
	taken, err := tk.TakeSeen(context.Background(), chatID, seen)
	if err != nil {
		t.Fatal(err)
	}
	if taken.Driving.Head != moved || tree(t, taken.Cell.Root)["b.txt"] != "moved on" {
		t.Fatalf("opened head %s tree %v, want the newer turn", taken.Driving.Head, tree(t, taken.Cell.Root))
	}
}
