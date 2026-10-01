package cellsync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestPublishCreatesThenAdvances(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	h1 := r.publishFirst(map[string]string{"a": "one"})
	if got := r.head(cellID); got.Head != h1 || got.Lease.Fence != 1 || r.drv.Fence != 1 || r.drv.Head != h1 {
		t.Fatalf("after create: cell %+v, driving %+v", got, r.drv)
	}
	before := r.puts()
	h2 := r.seal(map[string]string{"a": "one", "b": "two"})
	if err := r.pub.Publish(ctx, r.drv, h2, r.info()); err != nil {
		t.Fatal(err)
	}
	if got := r.head(cellID); got.Head != h2 || r.drv.Head != h2 {
		t.Fatalf("after advance: cell %+v, driving %+v", got, r.drv)
	}
	if r.puts() != before+1 {
		t.Fatalf("second publish sent %d frames, want 1", r.puts()-before)
	}
}

func TestPartialUploadNeverMovesHead(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	h1 := r.publishFirst(map[string]string{"a": "one"})
	durable := r.head(cellID).DurableAt

	r.engA.MaxObjects = 1
	h2 := r.seal(map[string]string{"a": "one", "b": "two", "c": "three"})
	r.mem.FailAfter(1, blobstore.ErrUnreachable)
	if err := r.pub.Publish(ctx, r.drv, h2, r.info()); !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("publish = %v, want the store error", err)
	}
	if got := r.head(cellID); got.Head != h1 || got.DurableAt != durable {
		t.Fatalf("head moved after a partial upload: %+v", got)
	}
	if r.drv.Head != h1 {
		t.Fatalf("driving changed on failure: %+v", r.drv)
	}
	if err := r.pub.Publish(ctx, r.drv, h2, r.info()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := r.head(cellID); got.Head != h2 {
		t.Fatalf("retry left head at %s", got.Head)
	}
}

func TestRefusedAfterPartialUploadLeavesNoOrphanHead(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	h1 := r.publishFirst(map[string]string{"a": "one"})
	r.engA.MaxObjects = 1
	h2 := r.seal(map[string]string{"a": "one", "b": "two", "c": "three"})

	r.mem.FailAfter(1, blobstore.ErrUnreachable) // frame 1 lands, frame 2 fails
	if err := r.pub.Publish(ctx, r.drv, h2, r.info()); err == nil {
		t.Fatal("the partial upload succeeded")
	}
	r.takeOver()
	r.bPublishes(h1)

	err := r.pub.Publish(ctx, r.drv, h2, r.info())
	if !errors.Is(err, ErrSuperseded) {
		t.Fatalf("publish = %v, want ErrSuperseded", err)
	}
	if got := r.head(cellID); got.Head != otherHead || got.Lease.Device != devB {
		t.Fatalf("directory after refusal: %+v", got)
	}
	r.requireHeld(h2)

	sent := r.puts()
	id, err := Brancher{Dir: r.dirA, Publisher: r.pub, NewID: r.newID}.Branch(ctx, r.drv, h2, 1, r.info())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.pub.Upload(ctx, r.drv.Cell, h2); err != nil || r.puts() != sent {
		t.Fatalf("the branch re-sent objects: %d puts, err %v", r.puts()-sent, err)
	}
	if got := r.head(id); got.Head != h2 || got.ParentCell != cellID {
		t.Fatalf("branch %+v", got)
	}
}

// requireHeld fails unless the store holds every object head needs.
func (r *rig) requireHeld(head string) {
	r.t.Helper()
	rids, err := r.engA.Want(context.Background(), r.cellA, head)
	if err != nil || len(rids) != 0 {
		r.t.Fatalf("engine is incomplete: %v %v", rids, err)
	}
	var all []string
	for _, op := range r.mem.Log() {
		all = append(all, op.RIDs...)
	}
	have, err := r.mem.Has(context.Background(), all)
	if err != nil {
		r.t.Fatal(err)
	}
	for i, ok := range have {
		if !ok {
			r.t.Fatalf("object %s missing from the store", all[i])
		}
	}
}

func TestPublishSupersededIsErrSuperseded(t *testing.T) {
	r := newRig(t)
	h1 := r.publishFirst(map[string]string{"a": "one"})
	r.takeOver()
	h2 := r.seal(map[string]string{"a": "two"})
	err := r.pub.Publish(context.Background(), r.drv, h2, r.info())
	if !errors.Is(err, ErrSuperseded) || !errors.Is(err, directory.ErrFenceStale) {
		t.Fatalf("publish = %v", err)
	}
	if r.drv.Head != h1 || r.head(cellID).Head != h1 {
		t.Fatalf("a superseded publish moved a head: %+v", r.drv)
	}
}

// checkRaceOutcome accepts the two orders the race can end in, and nothing
// else. A publish renews its own lease, so when the owner's publish lands first
// the other device's plain take is refused and the head moved; when the take
// lands first the owner's publish is superseded or had already gone up.
func checkRaceOutcome(t *testing.T, run int, r *rig, h1, h2 string, pubErr, takeErr error) {
	t.Helper()
	got := r.head(cellID)
	if errors.Is(takeErr, directory.ErrLeaseHeld) {
		if pubErr != nil || got.Head != h2 || got.Lease.Device != devA {
			t.Fatalf("run %d: owner renewed first, yet publish %v left %+v", run, pubErr, got)
		}
		return
	}
	if takeErr != nil || got.Lease.Device != devB || got.Lease.Fence != 2 {
		t.Fatalf("run %d: B did not end up holding the lease: %v %+v", run, takeErr, got.Lease)
	}
	if pubErr == nil && got.Head != h2 || pubErr != nil && (!errors.Is(pubErr, ErrSuperseded) || got.Head != h1) {
		t.Fatalf("run %d: publish %v left head %s", run, pubErr, got.Head)
	}
}

func TestTwoDevicesRaceAfterLeaseExpiry(t *testing.T) {
	for i := 0; i < 40; i++ {
		r := newRig(t)
		h1 := r.publishFirst(map[string]string{"a": "one"})
		h2 := r.seal(map[string]string{"a": "two"})
		r.clock.Advance(directory.LeaseTTL + time.Second)

		var pubErr, takeErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); pubErr = r.pub.Publish(context.Background(), r.drv, h2, r.info()) }()
		go func() {
			defer wg.Done()
			_, takeErr = r.dir.For(devB).Acquire(context.Background(), cellID, directory.AcquireOpts{})
		}()
		wg.Wait()

		checkRaceOutcome(t, i, r, h1, h2, pubErr, takeErr)
	}
}

// A device that took a chat holds objects the relay already has. Publishing a
// small edit on top of them must send the edit alone, not the chat again.
func TestPublishAfterTakeSendsOnlyTheEdit(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	big := strings.Repeat("x", 64<<10)
	head := r.publishFirst(map[string]string{"big": big})

	f, c := fetcherFor(t, r.mem)
	if err := f.Fetch(ctx, c, head); err != nil {
		t.Fatal(err)
	}
	engB := f.Engine.(*FakeEngine)
	edited := engB.Seal(c, map[string]string{"big": big, "note": "edit"})

	var counters blobstore.Counters
	pub := &Publisher{Engine: engB, Store: blobstore.Counting{Inner: r.mem, C: &counters}, Dir: r.dirA}
	if _, err := pub.Upload(ctx, c, edited); err != nil {
		t.Fatal(err)
	}
	if got := counters.ObjectsUp.Load(); got != 2 {
		t.Fatalf("sent %d objects, want the edit's blob and its snapshot", got)
	}
	if got := counters.BytesUp.Load(); got >= int64(len(big)) {
		t.Fatalf("sent %d bytes, the size of the chat taken (%d) or more", got, len(big))
	}
}
