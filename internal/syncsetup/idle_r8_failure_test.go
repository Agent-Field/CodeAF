package syncsetup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

type r8ReleaseDirectory struct {
	directory.Client
	failure error
	commit  bool
	clock   *directorytest.FakeClock
	readErr error
}

func (d *r8ReleaseDirectory) Cell(ctx context.Context, id string) (directory.CellView, error) {
	if d.readErr != nil {
		return directory.CellView{}, d.readErr
	}
	return d.Client.Cell(ctx, id)
}

func (d *r8ReleaseDirectory) Release(ctx context.Context, id string, fence uint64) error {
	if d.failure != nil && !d.commit {
		return d.failure
	}
	if err := d.Client.Release(ctx, id, fence); err != nil {
		return err
	}
	return d.failure
}

func TestIdleRelease_R8_UncertainReleaseRetakesOnSend(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cause  error
		commit bool
	}{
		{"lost reply before commit", context.DeadlineExceeded, false},
		{"lost reply after commit", context.DeadlineExceeded, true},
		{"connection reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, dir := r8IdleDrive(t)
			fence := d.batcher.Fence()
			dir.failure, dir.commit = tc.cause, tc.commit
			finished, err := d.releaseIdle(t.Context(), d.activity.Load())
			if finished || !errors.Is(err, tc.cause) || !d.IdleReleased() || d.Gate() == nil {
				t.Fatalf("uncertain release=%v error=%v released=%v gate=%v", finished, err, d.IdleReleased(), d.Gate())
			}
			if _, _, holding := d.RenewalStatus(); holding {
				t.Fatal("uncertain release still claims a renewing hold")
			}
			if _, _, err := d.sync.SendLease(d.cell.ID); !os.IsNotExist(err) {
				t.Fatalf("uncertain release kept send receipt: %v", err)
			}
			// A later refused retry cannot undo an earlier uncertain request.
			dir.failure, dir.commit = &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, false
			_, err = d.releaseIdle(t.Context(), d.activity.Load())
			if !errors.Is(err, syscall.ECONNREFUSED) || !d.IdleReleased() {
				t.Fatalf("refused retry revived uncertain release: %v", err)
			}
			dir.failure = nil
			confirmed, err := d.ConfirmWork(t.Context())
			if err != nil || !confirmed || d.Gate() != nil || d.IdleReleased() || d.batcher.Fence() != fence+1 {
				t.Fatalf("send did not retake: confirmed=%v error=%v fence=%d gate=%v", confirmed, err, d.batcher.Fence(), d.Gate())
			}
			_, receipt, err := d.sync.SendLease(d.cell.ID)
			if err != nil || receipt != fence+1 {
				t.Fatalf("retaken receipt=%d error=%v", receipt, err)
			}
		})
	}
}

// r8IdleDrive has no background loops or network. The real local engine proves
// the saved state, while the memory directory and clock control ownership.
func r8IdleDrive(t *testing.T) (*Drive, *r8ReleaseDirectory) {
	t.Helper()
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	celltest.RequireEngine(t)
	clock := directorytest.NewFakeClock()
	d := sameComputerDrive(t, clock.Now)
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	mustNil(t, err)
	c.ID = d.cell.ID
	d.cell, d.batcher.Driving.Cell = c, c
	d.sync.Store = blobstore.NewMemory()
	bin := celltest.Engine(t)
	d.engine = cellstore.Engine{Binary: bin, DataRoot: t.TempDir(), Transport: cellstore.Spawn{Binary: bin}}
	d.files, err = d.sync.files(d.engine, d.cell)
	mustNil(t, err)
	sealed, err := d.engine.Seal(t.Context(), d.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	mustNil(t, cellstore.Captured(t.Context(), d.cell))
	_, err = d.sync.Dir.Publish(t.Context(), d.cell.ID, directory.Publish{Fence: d.batcher.Fence(), Head: sealed.Turn.ID})
	mustNil(t, err)
	d.batcher.Driving.Head = sealed.Turn.ID
	dir := &r8ReleaseDirectory{Client: d.sync.Dir, clock: clock}
	d.sync.Dir, d.batcher.Publisher.Dir = dir, dir
	d.batcher.Sleep = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	d.devices = &DeviceObserver{home: d.sync.Home, binding: deviceBindingFor(Resolve(d.sync.Home).URL, d.sync.Identity, d.sync.Device), state: DeviceSnapshot{Complete: true, Devices: map[string]directory.Device{"A": {}, "B": {}}}}
	d.idleCheck = func() bool { return true }
	return d, dir
}

func TestIdleRelease_R8_RetrySettlesAfterRecovery(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			d, dir := r8IdleDrive(t)
			var waits []time.Duration
			dir.failure = &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
			if uncertain {
				dir.failure = context.DeadlineExceeded
			}
			d.opt.Sleep = func(ctx context.Context, wait time.Duration) error {
				waits = append(waits, wait)
				dir.clock.Advance(wait)
				if len(waits) == 7 {
					dir.failure = nil
				}
				if len(waits) > 7 {
					t.Fatal("idle release did not settle on recovery")
				}
				if _, _, holding := d.RenewalStatus(); holding {
					_, err := dir.Heartbeat(ctx, d.cell.ID, directory.Beat{Fence: 1})
					mustNil(t, err)
				} else if !uncertain {
					t.Fatal("a refused release stopped renewing during retry backoff")
				}
				return nil
			}
			d.releaseWhenIdle(t.Context(), d.activity.Load())
			want := []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute, time.Minute}
			if !slices.Equal(waits, want) || !d.releaseDone.Load() || !d.IdleReleased() {
				t.Fatalf("recovery did not settle at bounded backoff: waits=%v done=%v released=%v", waits, d.releaseDone.Load(), d.IdleReleased())
			}
			v, err := dir.Cell(t.Context(), d.cell.ID)
			mustNil(t, err)
			if v.Cell.Lease.Fence != 1 || v.Cell.Lease.Expires != 0 {
				t.Fatalf("retry did not release the original hold: %+v", v.Cell.Lease)
			}
		})
	}
}

func TestIdleRelease_R8_LongTurnAfterOutageStillReleases(t *testing.T) {
	d, dir := r8IdleDrive(t)
	running := true
	d.idleCheck = func() bool { return !running }
	d.Working(true)
	for range 10 {
		dir.clock.Advance(30 * time.Second)
		_, err := dir.Heartbeat(t.Context(), d.cell.ID, directory.Beat{Fence: 1, Working: true})
		mustNil(t, err)
	}
	dir.clock.Advance(20 * time.Second)
	dir.readErr = &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	mustNil(t, d.ConfirmPreparedWork(t.Context()))
	mustNil(t, d.CheckLease(t.Context()))
	if finished, err := d.releaseIdle(t.Context(), d.activity.Load()); finished || err != nil {
		t.Fatalf("running turn ended idle retries: finished=%v error=%v", finished, err)
	}
	if line, viewing := d.Viewer(); viewing || line != "" {
		t.Fatalf("outage created a permanent viewer: %q", line)
	}
	dir.readErr = nil
	_, err := dir.Heartbeat(t.Context(), d.cell.ID, directory.Beat{Fence: 1, Working: true})
	mustNil(t, err)
	d.Working(false)
	running = false
	available := make(chan Viewer, 1)
	d.opt.OnViewer = func(v Viewer) { available <- v }
	d.opt.Sleep = func(ctx context.Context, wait time.Duration) error {
		dir.clock.Advance(wait)
		return ctx.Err()
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		mustNil(t, d.stopIdle(ctx))
	})
	// Use the real turn-end door, including its capture join and scheduler.
	d.Idle()
	select {
	case viewer := <-available:
		if viewer.Line != chatlist.Available || !d.releaseDone.Load() || !d.idleRequested.Load() {
			t.Fatalf("finished long turn did not release: viewer=%+v done=%v requested=%v", viewer, d.releaseDone.Load(), d.idleRequested.Load())
		}
	case <-t.Context().Done():
		t.Fatal("turn-end Idle did not schedule a release")
	}
	d.idleWorkers.Wait()
	v, err := dir.Cell(t.Context(), d.cell.ID)
	mustNil(t, err)
	if v.Cell.Lease.Fence != 1 || v.Cell.Lease.Expires != 0 {
		t.Fatalf("long turn lapsed instead of releasing: %+v", v.Cell.Lease)
	}
}

func TestIdleRelease_R8_RefusedReleaseKeepsHoldingSameFence(t *testing.T) {
	d, dir := r8IdleDrive(t)
	before := d.sendLeaseSnapshot()
	var notices []Viewer
	d.opt.OnViewer = func(v Viewer) { notices = append(notices, v) }
	dir.failure = fmt.Errorf("%w: %w", directory.ErrUnreachable, &url.Error{Op: "Post", URL: "http://fixture.invalid/release", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}})
	finished, err := d.releaseIdle(t.Context(), d.activity.Load())
	if finished || !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("release finished=%v error=%v", finished, err)
	}
	if d.IdleReleased() || d.Gate() != nil || d.sendLeaseSnapshot() != before {
		t.Fatalf("refused release changed holding state: released=%v gate=%v receipt=%+v", d.IdleReleased(), d.Gate(), d.sendLeaseSnapshot())
	}
	if line, viewer := d.Viewer(); viewer || line != "" || len(notices) != 0 {
		t.Fatalf("refused release changed viewer or emitted Available: line=%q viewer=%v notices=%v", line, viewer, notices)
	}
	if _, _, holding := d.RenewalStatus(); !holding || d.batcher.Fence() != before.Fence {
		t.Fatalf("refused release lost renewal: holding=%v fence=%d", holding, d.batcher.Fence())
	}
	id, fence, err := d.sync.SendLease(d.cell.ID)
	if err != nil || id != before.Cell || fence != before.Fence {
		t.Fatalf("refused release lost send receipt: %q %d %v", id, fence, err)
	}
	dir.failure = nil
	confirmed, err := d.ConfirmWork(t.Context())
	if err != nil || !confirmed || d.batcher.Fence() != before.Fence {
		t.Fatalf("recovered send did not retain fence: confirmed=%v error=%v fence=%d", confirmed, err, d.batcher.Fence())
	}
	finished, err = d.releaseIdle(t.Context(), d.activity.Load())
	if err != nil || !finished || !d.IdleReleased() || len(notices) != 1 || notices[0].Line != chatlist.Available {
		t.Fatalf("normal idle retry did not release: finished=%v error=%v notices=%v", finished, err, notices)
	}
}
