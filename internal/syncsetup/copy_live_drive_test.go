package syncsetup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// createGate holds a drive's first Create until the test lets it through, so
// the background copier can create and release the record first. That is the
// order a large chat produced on 2026-11-16: the copier and the drive both
// uploaded the same 400 MB, and the copier's Create landed first.
type createGate struct {
	directory.Client
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (g *createGate) Create(ctx context.Context, id string, in directory.CellInit) (directory.CellView, error) {
	g.once.Do(func() { close(g.entered) })
	select {
	case <-g.release:
	case <-ctx.Done():
		return directory.CellView{}, ctx.Err()
	}
	return g.Client.Create(ctx, id, in)
}

// A chat this window is driving, whose first publish is still uploading, can be
// shared first by the background copier, which creates its directory record
// and gives the lease back at once. The live drive must read that free lease
// as its own chat not published by it yet, not as a chat that left this
// computer: it keeps admitting work, its first publish takes the record over,
// and the next send confirms the hold. Before the fix the drive called the
// chat "available to continue here" and refused every later send for as long
// as the engine ran.
func TestBackgroundCopyOfALiveDriveDoesNotDisplaceIt(t *testing.T) {
	r := newDriveRig(t)
	ctx := t.Context()
	gate := &createGate{Client: r.a.Dir, entered: make(chan struct{}), release: make(chan struct{})}
	window := &Sync{Home: r.a.Home, Identity: r.a.Identity, Device: r.a.Device,
		Dir: gate, Store: r.a.Store, Ledger: r.a.Ledger, Relay: r.a.Relay, Interval: r.a.Interval}
	var viewers []Viewer
	var mu sync.Mutex
	drive, err := window.Drive(ctx, r.engine, r.cell, DriveOptions{DeviceName: "A", OnViewer: func(v Viewer) {
		mu.Lock()
		defer mu.Unlock()
		viewers = append(viewers, v)
	}})
	mustNil(t, err)
	var let sync.Once
	letThrough := func() { let.Do(func() { close(gate.release) }) }
	// Cleanups run last first: the gate opens before Close joins the publish.
	t.Cleanup(func() { _ = drive.Close(context.Background()) })
	t.Cleanup(letThrough)

	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"hello"}`+"\n")
	_, err = drive.Store(r.engine).Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	mustNil(t, cellstore.Await(ctx, r.cell))
	<-gate.entered

	// The copier finds no record, uploads, creates it and releases it.
	mustNil(t, r.a.PublishCopy(ctx, r.engine, r.cell, "A", "chat"))
	if v, err := r.a.Dir.Cell(ctx, r.cell.ID); err != nil || v.Cell.Lease.Expires > v.Now {
		t.Fatalf("fixture: the copier left no released record: %+v, %v", v, err)
	}

	// The next send asks who holds the chat while the first publish is still
	// in flight, and so does every tool call that send runs.
	// A check that retires the hold waits for the publish in flight, which
	// the gate keeps, so it is given a moment and then failed, not awaited.
	checked := make(chan error, 1)
	go func() { checked <- drive.CheckLease(ctx) }()
	select {
	case err := <-checked:
		mustNil(t, err)
	case <-time.After(5 * time.Second):
		letThrough()
		t.Fatal("a tool call's ownership check retired the hold of the window driving the chat")
	}
	if err := drive.Gate(); err != nil {
		t.Fatalf("a tool call's ownership check displaced the window driving the chat: %v", err)
	}
	if _, err := drive.ConfirmWork(ctx); err != nil {
		t.Fatal(err)
	}
	if err := drive.Gate(); err != nil {
		t.Fatalf("the copier's release displaced the window driving the chat: %v", err)
	}

	letThrough()
	waitFor(t, "the drive to hold its chat", func() bool {
		held, err := drive.ConfirmWork(ctx)
		return err == nil && held
	})
	if err := drive.Gate(); err != nil {
		t.Fatalf("work refused after the drive confirmed its hold: %v", err)
	}
	// The turn sealed here is what the other computer reads, on the record it
	// lists, not on a branch made for work that was never displaced.
	waitFor(t, "the sealed turn on the chat's record", func() bool { return r.directoryHead(r.cell.ID) == r.head() })
	if branches, err := branchMapOf(r.homeA); err != nil || branches.Resolve(r.cell.ID) != r.cell.ID {
		t.Fatalf("the chat's turns were moved to a branch: %v", err)
	}
	if _, err := r.b.Dir.Acquire(ctx, r.cell.ID, directory.AcquireOpts{}); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("the other computer can take a chat this window drives: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(viewers) != 0 {
		t.Fatalf("the window was told it no longer drives the chat: %+v", viewers)
	}
}
