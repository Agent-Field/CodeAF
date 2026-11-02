package syncsetup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type r1ReleaseGate struct {
	directory.Client
	created chan struct{}
	release chan struct{}
}

func (d *r1ReleaseGate) Create(ctx context.Context, id string, in directory.CellInit) (directory.CellView, error) {
	v, err := d.Client.Create(ctx, id, in)
	if err == nil {
		close(d.created)
		select {
		case <-d.release:
		case <-ctx.Done():
			return v, ctx.Err()
		}
	}
	return v, err
}

// M2/M3: opening between background Create and Release must leave an active
// hold, so the other computer cannot acquire the open chat.
func TestR1_3BackgroundPublicationCannotReleaseConcurrentOpen(t *testing.T) {
	r := newDriveRig(t)
	_, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	mustNil(t, cellstore.Await(t.Context(), r.cell))
	gate := &r1ReleaseGate{Client: r.a.Dir, created: make(chan struct{}), release: make(chan struct{})}
	publisher := &Sync{Home: r.a.Home, Identity: r.a.Identity, Device: r.a.Device,
		Dir: gate, Store: r.a.Store, Ledger: r.a.Ledger, Relay: r.a.Relay}
	published := make(chan error, 1)
	go func() { published <- publisher.PublishCopy(t.Context(), r.engine, r.cell, "A", "chat") }()
	<-gate.created
	opened := make(chan error, 1)
	var drive *Drive
	go func() {
		var err error
		drive, err = r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{DeviceName: "A"})
		opened <- err
	}()
	select {
	case err := <-opened:
		opened <- err
	case <-time.After(100 * time.Millisecond):
	}
	close(gate.release)
	mustNil(t, <-published)
	mustNil(t, <-opened)
	t.Cleanup(func() { _ = drive.Close(context.Background()) })
	if _, err := r.b.Dir.Acquire(t.Context(), r.cell.ID, directory.AcquireOpts{}); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("other computer can acquire a chat opened during publication: %v", err)
	}
}
