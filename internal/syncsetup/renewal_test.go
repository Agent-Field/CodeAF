package syncsetup

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
)

type renewalStatusReader interface {
	RenewalStatus() (time.Time, time.Duration, bool)
}

// refusingRenewals puts a real connection refusal on the relay boundary.
type refusingRenewals struct {
	directory.Client
	address string
	down    atomic.Bool
}

func (r *refusingRenewals) Heartbeat(ctx context.Context, id string, beat directory.Beat) (directory.CellView, error) {
	if r.down.Load() {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", r.address)
		if conn != nil {
			conn.Close()
		}
		return directory.CellView{}, fmt.Errorf("%w: %v", directory.ErrUnreachable, err)
	}
	return r.Client.Heartbeat(ctx, id, beat)
}

// Contract D1 keeps the first failed renewal locally until a real renewal succeeds.
func TestHeldDriveReportsRefusedRenewalsAndRecovery(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(URLVar, "http://relay.invalid")
	local := t.TempDir()
	id, err := identity.Ensure(local)
	mustNil(t, err)
	dev, err := identity.Device(local)
	mustNil(t, err)
	memory := directory.NewMemory(time.Now)
	owner, other := memory.For(dev.ID()), memory.For("another-computer")
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	mustNil(t, err)
	_, err = owner.Create(t.Context(), c.ID, directory.CellInit{})
	mustNil(t, err)
	syncer := &Sync{Home: local, Identity: id, Device: dev, Dir: owner, Store: blobstore.NewMemory()}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	mustNil(t, err)
	address := listener.Addr().String()
	mustNil(t, listener.Close())
	relay := &refusingRenewals{Client: owner, address: address}
	syncer.Dir = relay
	ticks, ready := make(chan struct{}), make(chan struct{}, 1)
	drive, err := syncer.Drive(t.Context(), cellstore.Engine{}, c, DriveOptions{Sleep: func(ctx context.Context, _ time.Duration) error {
		ready <- struct{}{}
		select {
		case <-ticks:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	mustNil(t, err)
	defer drive.Close(t.Context())
	reader, ok := any(drive).(renewalStatusReader)
	if !ok {
		t.Fatal("the held drive cannot report renewal failures")
	}
	<-ready
	tick := func() {
		t.Helper()
		select {
		case ticks <- struct{}{}:
		case <-time.After(5 * time.Second):
			t.Fatal("renewal tick did not start")
		}
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("renewal tick did not finish")
		}
	}
	before := time.Now()
	relay.down.Store(true)
	tick()
	waitFor(t, "failed renewal", func() bool { since, _, _ := reader.RenewalStatus(); return !since.IsZero() })
	since, period, held := reader.RenewalStatus()
	if !held || period != directory.HeartbeatEvery || since.Before(before) || since.After(time.Now()) {
		t.Fatalf("renewal status = %v, %v, %v", since, period, held)
	}
	tick()
	if next, _, _ := reader.RenewalStatus(); !next.Equal(since) {
		t.Fatalf("a repeated failure changed the first failure from %v to %v", since, next)
	}
	relay.down.Store(false)
	tick()
	waitFor(t, "renewal recovery", func() bool { since, _, held := reader.RenewalStatus(); return held && since.IsZero() })
	view, err := other.Cell(t.Context(), c.ID)
	mustNil(t, err)
	_, err = other.Acquire(t.Context(), c.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence}})
	mustNil(t, err)
	tick()
	waitFor(t, "displacement", func() bool { _, _, held := reader.RenewalStatus(); return !held })
}
