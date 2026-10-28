package syncsetup

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestFenceF1StartupRenewsExistingHold(t *testing.T) {
	r := newDriveRig(t)
	sealed, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	created, err := r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{Head: sealed.Turn.ID})
	mustNil(t, err)
	mustNil(t, cellstore.NotePublished(r.cell, sealed.Turn.ID))
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer drive.Close(t.Context())
	after, err := r.a.Dir.Cell(t.Context(), r.cell.ID)
	mustNil(t, err)
	if after.Cell.Lease.Fence != created.Cell.Lease.Fence {
		t.Fatalf("F1 opening own live hold changed fence: %d -> %d", created.Cell.Lease.Fence, after.Cell.Lease.Fence)
	}
}

func TestFenceF1IdleExpiredHoldRenewsWithoutAcquiring(t *testing.T) {
	r := newDriveRig(t)
	sealed, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	created, err := r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{Head: sealed.Turn.ID})
	mustNil(t, err)
	mustNil(t, cellstore.NotePublished(r.cell, sealed.Turn.ID))
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer drive.Close(t.Context())
	r.clock.Advance(directory.LeaseTTL + time.Second)
	mustNil(t, drive.CheckLease(t.Context()))
	if drive.Gate() != nil || !drive.HoldsLease(t.Context()) {
		t.Fatal("F1 idle owner was retired instead of renewing its unchanged fence")
	}
	view, err := r.a.Dir.Cell(t.Context(), r.cell.ID)
	mustNil(t, err)
	if view.Cell.Lease.Fence != created.Cell.Lease.Fence || view.Cell.Lease.Expires <= view.Now {
		t.Fatalf("F1 idle renewal changed fence or stayed expired: %+v", view)
	}
}

// Restart retains a live lease; a clean quit intentionally releases it for
// immediate continuation elsewhere and therefore needs a new lease on reopen.
func TestFenceF1IdleWatchReconnectAndProcessResume(t *testing.T) {
	connections := make(chan net.Conn, 16)
	var acquires, releases atomic.Int32
	r := newDriveRig(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasSuffix(req.URL.Path, "/acquire") {
				acquires.Add(1)
			}
			if strings.HasSuffix(req.URL.Path, "/release") {
				releases.Add(1)
			}
			if req.URL.Path == "/v1/dir/watch" && len(req.URL.Query()["hold"]) == 0 {
				next.ServeHTTP(fenceReconnectWriter{w, connections}, req)
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	sealed, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	created, err := r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{Head: sealed.Turn.ID})
	mustNil(t, err)
	mustNil(t, cellstore.NotePublished(r.cell, sealed.Turn.ID))
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer drive.Close(t.Context())
	var conn net.Conn
	select {
	case conn = <-connections:
	case <-time.After(10 * time.Second):
		t.Fatal("F1 initial watch missing")
	}
	mustNil(t, conn.Close())
	select {
	case <-connections:
	case <-time.After(10 * time.Second):
		t.Fatal("F1 watch did not reconnect")
	}
	mustNil(t, drive.CheckLease(t.Context()))
	view, err := r.a.Dir.Cell(t.Context(), r.cell.ID)
	mustNil(t, err)
	if view.Cell.Lease.Fence != created.Cell.Lease.Fence || acquires.Load() != 0 || releases.Load() != 0 {
		t.Fatalf("F1 reconnect changed hold: %+v; acquires=%d releases=%d", view.Cell.Lease, acquires.Load(), releases.Load())
	}
	// End the old process's loops without its clean-quit release.
	drive.cancel()
	<-drive.done
	resumed, err := openSync(t, r.homeA).Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer resumed.Close(t.Context())
	view, err = r.a.Dir.Cell(t.Context(), r.cell.ID)
	mustNil(t, err)
	if view.Cell.Lease.Fence != created.Cell.Lease.Fence || acquires.Load() != 0 || releases.Load() != 0 {
		t.Fatalf("F1 idle restart changed hold: %+v; acquires=%d releases=%d", view.Cell.Lease, acquires.Load(), releases.Load())
	}
}

type fenceReconnectWriter struct {
	http.ResponseWriter
	connections chan net.Conn
}

func (w fenceReconnectWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil {
		w.connections <- c
	}
	return c, rw, err
}

func TestFenceF1FailedIdleRenewalRefusesWork(t *testing.T) {
	r := newDriveRig(t)
	wire := &fenceRenewalControl{Client: r.a.Dir}
	r.a.Dir = wire
	sealed, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{Head: sealed.Turn.ID})
	mustNil(t, err)
	mustNil(t, cellstore.NotePublished(r.cell, sealed.Turn.ID))
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer drive.Close(t.Context())
	r.clock.Advance(directory.LeaseTTL + time.Second)
	wire.refuse.Store(true)
	mustNil(t, drive.CheckLease(t.Context()))
	if drive.Gate() == nil || drive.HoldsLease(t.Context()) {
		t.Fatal("F1 expired hold with failed renewal authorized work")
	}
	wire.refuse.Store(false)
	if !drive.HoldsLease(t.Context()) || drive.Gate() != nil {
		t.Fatal("F1 recovered idle owner could not renew its original fence")
	}
}

func TestFenceF1LateIdleReadCannotResurrectClosedHold(t *testing.T) {
	r := newDriveRig(t)
	wire := &fenceRenewalControl{Client: r.a.Dir, read: make(chan struct{}), resume: make(chan struct{})}
	defer func() {
		select {
		case <-wire.resume:
		default:
			close(wire.resume)
		}
	}()
	r.a.Dir = wire
	sealed, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{Head: sealed.Turn.ID})
	mustNil(t, err)
	mustNil(t, cellstore.NotePublished(r.cell, sealed.Turn.ID))
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer drive.Close(t.Context())
	r.clock.Advance(directory.LeaseTTL + time.Second)
	wire.delay.Store(true)
	done := make(chan error, 1)
	go func() { done <- drive.CheckLease(t.Context()) }()
	select {
	case <-wire.read:
	case <-time.After(10 * time.Second):
		t.Fatal("F1 ownership read missing")
	}
	mustNil(t, drive.Close(t.Context()))
	beats := wire.beats.Load()
	close(wire.resume)
	select {
	case err := <-done:
		mustNil(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("F1 late ownership read stuck")
	}
	view, err := r.a.Dir.Cell(t.Context(), r.cell.ID)
	mustNil(t, err)
	if view.Cell.Lease.Expires != 0 || wire.beats.Load() != beats {
		t.Fatalf("F1 late read renewed a closed hold: %+v", view.Cell.Lease)
	}
}

type fenceRenewalControl struct {
	directory.Client
	refuse, delay atomic.Bool
	beats         atomic.Int32
	read, resume  chan struct{}
}

func (c *fenceRenewalControl) Cell(ctx context.Context, id string) (directory.CellView, error) {
	v, err := c.Client.Cell(ctx, id)
	if c.delay.CompareAndSwap(true, false) {
		close(c.read)
		select {
		case <-c.resume:
		case <-ctx.Done():
			return v, ctx.Err()
		}
	}
	return v, err
}
func (c *fenceRenewalControl) Heartbeat(ctx context.Context, id string, b directory.Beat) (directory.CellView, error) {
	c.beats.Add(1)
	if c.refuse.Load() {
		return directory.CellView{}, directory.ErrUnreachable
	}
	return c.Client.Heartbeat(ctx, id, b)
}
