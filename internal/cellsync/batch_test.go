package cellsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// fakeInner is a Stage 0 store that seals instantly in the fake engine, with
// no network at all.
type fakeInner struct {
	r *rig
	n int
}

func (f *fakeInner) Seal(context.Context, cell.Cell, cellstore.TurnInfo) (cellstore.Sealed, error) {
	f.n++
	head := f.r.seal(map[string]string{"a": fmt.Sprint(f.n)})
	return cellstore.Sealed{Turn: cellstore.Turn{ID: head}}, nil
}

func TestPublishingNeverBlocksSeal(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r.store.set(nil, func(ctx context.Context) { // the store hangs
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-ctx.Done():
		}
	})
	note(b, r.seal(map[string]string{"a": "one"}))
	flushed := make(chan struct{})
	go func() { b.flush(context.Background()); close(flushed) }()
	<-entered // a flush is now stuck inside the store

	sealed := make(chan error, 1)
	p := &Publishing{Inner: &fakeInner{r: r}, Batcher: b}
	go func() { _, err := p.Seal(context.Background(), r.cellA, cellstore.TurnInfo{}); sealed <- err }()
	select {
	case err := <-sealed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a seal waited on the network")
	}
	if b.Pending() != 2 {
		t.Fatalf("pending = %d, want 2", b.Pending())
	}
	close(release)
	<-flushed
}

func TestBatcherPublishesNewestOnly(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	var flushes []Flush
	b.OnFlush = func(f Flush) { flushes = append(flushes, f) }
	h1 := r.seal(map[string]string{"a": "1"})
	h2 := r.seal(map[string]string{"a": "2"})
	h3 := r.seal(map[string]string{"a": "3"})
	note(b, h1, h2, h3)

	if d := b.flush(context.Background()); d != b.Interval {
		t.Fatalf("next flush in %v, want %v", d, b.Interval)
	}
	if got := r.head(cellID); got.Head != h3 {
		t.Fatalf("head %s, want the newest %s", got.Head, h3)
	}
	if len(flushes) != 1 || flushes[0].Head != h3 || flushes[0].Turns != 3 || flushes[0].Frames != 1 || flushes[0].Objects == 0 {
		t.Fatalf("flushes = %+v", flushes)
	}
	if b.Pending() != 0 || r.puts() != 1 {
		t.Fatalf("pending %d, puts %d", b.Pending(), r.puts())
	}
}

func TestBatcherHeartbeatCarriesPending(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	note(b, r.seal(map[string]string{"a": "1"}))
	b.flush(context.Background())
	note(b, r.seal(map[string]string{"a": "2"}), r.seal(map[string]string{"a": "3"}))

	r.clock.Advance(directory.HeartbeatEvery)
	b.beat(context.Background())
	got := r.head(cellID)
	if got.Lease.Pending != 2 || got.Lease.Expires <= r.clock.Now().UnixMilli() {
		t.Fatalf("lease after heartbeat: %+v", got.Lease)
	}
}

func TestStoreFailureKeepsSealing(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	h1 := r.publishFirst(map[string]string{"a": "0"})
	r.store.set(blobstore.ErrFull, nil)
	inner := &fakeInner{r: r}
	p := &Publishing{Inner: inner, Batcher: b}
	seal := func() {
		if _, err := p.Seal(context.Background(), r.cellA, cellstore.TurnInfo{}); err != nil {
			t.Fatalf("seal failed while the store was full: %v", err)
		}
	}

	var waits []time.Duration
	for i := 0; i < 6; i++ {
		seal()
		waits = append(waits, b.flush(context.Background()))
	}
	s := time.Second
	want := []time.Duration{10 * s, 20 * s, 40 * s, 60 * s, 60 * s, 60 * s}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("backoff %v, want %v", waits, want)
		}
	}
	if b.Pending() != 6 || r.head(cellID).Head != h1 {
		t.Fatalf("pending %d, head %s", b.Pending(), r.head(cellID).Head)
	}

	r.store.set(nil, nil)
	if d := b.flush(context.Background()); d != b.Interval || b.Pending() != 0 {
		t.Fatalf("after recovery: wait %v, pending %d", d, b.Pending())
	}
}

func TestStoreDownDirectoryUp(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	h1 := r.publishFirst(map[string]string{"a": "0"})
	r.store.set(blobstore.ErrUnreachable, nil)
	h2, h3 := r.seal(map[string]string{"a": "1"}), r.seal(map[string]string{"a": "2"})
	note(b, h2, h3)
	b.flush(context.Background())

	for i := 0; i < 5; i++ { // 50 s: longer than a lease
		r.clock.Advance(directory.HeartbeatEvery)
		b.beat(context.Background())
		if _, err := r.dir.For(devB).Acquire(context.Background(), cellID); !errors.Is(err, directory.ErrLeaseHeld) {
			t.Fatalf("beat %d: takeover = %v, the lease should be kept", i, err)
		}
	}
	got := r.head(cellID)
	if got.Head != h1 || got.Lease.Pending != 2 || b.Pending() != 2 {
		t.Fatalf("head %s pending %d/%d", got.Head, got.Lease.Pending, b.Pending())
	}

	r.store.set(nil, nil)
	b.flush(context.Background())
	if got := r.head(cellID); got.Head != h3 || b.Pending() != 0 {
		t.Fatalf("the next tick did not publish: head %s, pending %d", got.Head, b.Pending())
	}
}

func TestDirectoryDownThenFenceStale(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	h1 := r.publishFirst(map[string]string{"a": "0"})
	var branches, by []string
	b.OnSuperseded = func(s Superseded) { branches, by = append(branches, s.Branch), append(by, s.By) }
	h2 := r.seal(map[string]string{"a": "1"})
	note(b, h2)

	r.dirA.set(directory.ErrUnreachable)
	b.flush(context.Background()) // uploads may go through; the head cannot move
	if r.head(cellID).Head != h1 {
		t.Fatal("the head moved with the directory down")
	}
	r.takeOver()
	r.bPublishes(h1)
	r.dirA.set(nil)

	b.flush(context.Background())
	if len(branches) != 1 {
		t.Fatalf("OnSuperseded calls: %v", branches)
	}
	if got := r.head(cellID); got.Head != otherHead {
		t.Fatalf("the old cell's head moved to %s", got.Head)
	}
	if got := r.head(branches[0]); got.Head != h2 || got.ParentCell != cellID || got.OrphanTurns != 1 {
		t.Fatalf("branch = %+v", got)
	}
}

func TestBatcherRidesOutRelayRestart(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	r.publishFirst(map[string]string{"a": "0"})
	h2 := r.seal(map[string]string{"a": "1"})
	note(b, h2)
	var superseded int
	b.OnSuperseded = func(Superseded) { superseded++ }

	r.dirA.set(directory.ErrUnreachable) // the relay restarts
	r.clock.Advance(directory.HeartbeatEvery)
	b.beat(context.Background())
	if d := b.flush(context.Background()); d != 2*b.Interval {
		t.Fatalf("backoff = %v", d)
	}
	r.dirA.set(nil) // back before the lease lapsed
	r.clock.Advance(directory.HeartbeatEvery)
	b.beat(context.Background())
	b.flush(context.Background())

	got := r.head(cellID)
	if got.Head != h2 || got.Lease.Fence != 1 || got.Lease.Device != devA || superseded != 0 {
		t.Fatalf("cell %+v, superseded %d", got, superseded)
	}
}

func TestSkewIsSurfacedOnce(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	var shown []error
	b.OnError = func(err error) { shown = append(shown, err) }
	r.publishFirst(map[string]string{"a": "0"})
	note(b, r.seal(map[string]string{"a": "1"}))

	r.dirA.set(wireauth.ErrSkew)
	for i := 0; i < 3; i++ {
		if d := b.flush(context.Background()); d != b.Interval {
			t.Fatalf("skew was retried on the backoff: %v", d)
		}
		b.beat(context.Background())
	}
	if len(shown) != 1 || !errors.Is(shown[0], wireauth.ErrSkew) {
		t.Fatalf("OnError calls: %v", shown)
	}

	r.dirA.set(nil) // a fixed clock makes the next skew news again
	b.flush(context.Background())
	r.dirA.set(wireauth.ErrSkew)
	note(b, r.seal(map[string]string{"a": "2"}))
	b.flush(context.Background())
	if len(shown) != 2 {
		t.Fatalf("OnError calls after recovery: %v", shown)
	}
}

func TestCloseFlushesAndReleases(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	h1 := r.seal(map[string]string{"a": "1"})
	note(b, h1)
	if err := b.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := r.head(cellID)
	if got.Head != h1 || got.Lease.Expires != 0 {
		t.Fatalf("after close: %+v", got)
	}
	if _, err := r.dir.For(devB).Acquire(context.Background(), cellID); err != nil {
		t.Fatalf("a released lease is takeable at once: %v", err)
	}
}

func TestBatcherRunFlushesAndBeatsOnSchedule(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	b.Interval = 7 * time.Second // apart from the 10 s heartbeat, so each wake is its own
	sl := newFakeSleeper(r.clock)
	b.Sleep = sl.Sleep
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	sl.settle(2)

	h1 := r.seal(map[string]string{"a": "1"})
	note(b, h1)
	sl.settleOn(NudgeWindow) // the first note beat at once, before any lease exists to renew
	sl.advance(NudgeWindow)
	sl.settle(2)
	sl.advance(6 * time.Second) // flush at 7 s
	sl.settle(2)
	if r.head(cellID).Head != h1 {
		t.Fatal("no flush after one interval")
	}
	h2 := r.seal(map[string]string{"a": "2"})
	note(b, h2)
	sl.settleOn(NudgeWindow) // the noted turn beat at once, at 7 s, and did not wait for 10 s
	if got := r.head(cellID); got.Lease.Pending != 1 || got.Head != h1 {
		t.Fatalf("at 7 s: %+v", got)
	}
	sl.advance(NudgeWindow) // the window closes; the periodic wait starts again
	sl.settle(2)
	sl.advance(6 * time.Second) // flush at 14 s
	sl.settle(2)
	if r.head(cellID).Head != h2 {
		t.Fatal("no second flush")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
}

func TestSupersededDriverStopsAndNotifiesOnce(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	h1 := r.publishFirst(map[string]string{"a": "0"})
	var branches, by []string
	b.OnSuperseded = func(s Superseded) { branches, by = append(branches, s.Branch), append(by, s.By) }
	r.takeOver()
	r.bPublishes(h1)

	h2 := r.seal(map[string]string{"a": "1"})
	note(b, h2)
	for i := 0; i < 3; i++ {
		b.flush(context.Background())
		b.beat(context.Background())
	}
	if len(branches) != 1 || branches[0] == "" || by[0] != devB {
		t.Fatalf("OnSuperseded calls: %q by %q", branches, by)
	}
	if got := r.head(cellID); got.Head != otherHead {
		t.Fatalf("the superseded driver moved the old head to %s", got.Head)
	}
	// The device now drives the branch, and keeps publishing to it alone.
	h3 := r.seal(map[string]string{"a": "2"})
	note(b, h3)
	b.flush(context.Background())
	if got := r.head(branches[0]); got.Head != h3 || r.head(cellID).Head != otherHead {
		t.Fatalf("branch head %s", got.Head)
	}
}

func TestSupersededWithNothingOrphanedNotifiesViewerOnce(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	r.publishFirst(map[string]string{"a": "0"})
	var branches, by []string
	b.OnSuperseded = func(s Superseded) { branches, by = append(branches, s.Branch), append(by, s.By) }
	r.takeOver()
	for i := 0; i < 3; i++ {
		b.beat(context.Background())
	}
	if len(branches) != 1 || branches[0] != "" || by[0] != devB {
		t.Fatalf("OnSuperseded calls: %q by %q", branches, by)
	}
}

// A noted turn reaches the directory's pending count within the coalescing
// window, without waiting for the 10 s heartbeat; a burst costs one beat.
func TestNotedTurnBeatsPromptlyAndCoalesces(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	b.Interval = time.Hour // keep the flush loop out of this test
	sl := newFakeSleeper(r.clock)
	b.Sleep = sl.Sleep
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	sl.settle(2)
	r.publishFirst(map[string]string{"a": "0"})

	note(b, r.seal(map[string]string{"a": "1"}))
	sl.settleOn(NudgeWindow) // the heartbeat loop is in its coalescing window
	if got := r.head(cellID).Lease.Pending; got != 1 {
		t.Fatalf("pending %d right after Note, want 1", got)
	}

	note(b, r.seal(map[string]string{"a": "2"}), r.seal(map[string]string{"a": "3"}))
	if got := r.head(cellID).Lease.Pending; got != 1 {
		t.Fatalf("pending %d inside the window, want the burst held back", got)
	}
	sl.advance(NudgeWindow)
	sl.settleOn(NudgeWindow) // the burst's one beat was sent, and a new window opened
	if got := r.head(cellID).Lease.Pending; got != 3 {
		t.Fatalf("pending %d after the window, want 3", got)
	}
	cancel()
	<-done
}
