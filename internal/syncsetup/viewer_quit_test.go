package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// pipelinedSeat is the seat the chat door builds: pipelined, warmed, gated.
func pipelinedSeat(t *testing.T, c cell.Cell, work string, over func(cellstore.Engine) cellstore.Store, gate func() error) executor.Seat {
	t.Helper()
	seat, err := cellstore.SeatOver(executor.HostBound, c, work, nil, nil, over, cellstore.WithPipeline(), cellstore.WithWarmCapture())
	if err != nil {
		t.Fatal(err)
	}
	return executor.Gated(seat, gate)
}

// prepareFault is a store whose first screening of the folder fails, the way
// one that ran into a busy folder does, and whose seals and ownership are the
// drive side's own.
type prepareFault struct {
	inner cellstore.Store
	err   error
	once  sync.Once
}

func (s *prepareFault) Seal(ctx context.Context, c cell.Cell, info cellstore.TurnInfo) (cellstore.Sealed, error) {
	return s.inner.Seal(ctx, c, info)
}

func (s *prepareFault) Prepare(ctx context.Context, c cell.Cell) error {
	var fail error
	s.once.Do(func() { fail = s.err })
	if fail != nil {
		return fail
	}
	return s.inner.(interface {
		Prepare(context.Context, cell.Cell) error
	}).Prepare(ctx, c)
}

func (s *prepareFault) Viewing() bool {
	return s.inner.(interface{ Viewing() bool }).Viewing()
}

// quietHeartbeats stops this computer's heartbeats before its chat starts, so
// that its lease lapses when the relay clock moves on instead of being renewed
// first. The returned switch brings them back.
func (r *driveRig) quietHeartbeats() *atomic.Bool {
	var down atomic.Bool
	down.Store(true)
	r.a.Dir = offlineDir{Client: r.a.Dir, down: down.Load}
	return &down
}

// takenByAnotherComputer makes another computer take c's chat once the holder's
// lease lapsed, then tells this window the way the directory's watch does in a
// real run, so it learns it only shows the chat without any call of its own.
func (r *driveRig) takenByAnotherComputer(c *chat, name string, heartbeats *atomic.Bool) {
	r.t.Helper()
	ctx := r.t.Context()
	if err := r.b.putDevice(ctx, name); err != nil {
		r.t.Fatal(err)
	}
	r.clock.advance(directory.LeaseTTL + time.Second)
	if _, err := r.b.Dir.Acquire(ctx, r.cell.ID, directory.AcquireOpts{}); err != nil {
		r.t.Fatalf("the other computer could not take the chat: %v", err)
	}
	heartbeats.Store(false)
	c.drive.becomeViewer(cellsync.Superseded{By: r.b.Device.ID()})
	if _, viewer := c.drive.Viewer(); !viewer {
		r.t.Fatal("this window did not learn it only shows the chat")
	}
}

// C3.1: the chat was taken away while this window showed it and the window has
// nothing of its own unsaved: quitting reports no failure, and the window knows
// it only shows the chat.
func TestAWindowTheChatWasTakenFromQuitsWithoutAFailure(t *testing.T) {
	r := newDriveRig(t)
	ctx := t.Context()
	r.spokenFixture()
	heartbeats := r.quietHeartbeats()
	c := r.startChat(&noticeLog{})
	c.mustSay()
	durable := r.head()
	waitFor(t, "the first turn to be durable", func() bool { return r.directoryHead(r.cell.ID) == durable })
	r.takenByAnotherComputer(c, "live-pc", heartbeats)
	if !c.drive.Viewing() {
		t.Fatal("a window the chat was taken from does not know it only shows the chat")
	}
	if err := executor.Drain(ctx, c.seat); err != nil {
		t.Fatalf("saving the seat of a window that only shows the chat: %v", err)
	}
	if err := c.drive.Close(ctx); err != nil {
		t.Fatalf("closing the drive side of a window that only shows the chat: %v", err)
	}
}

// C3.1: this window opened its own copy of a chat another computer holds.
func TestAWindowOpenedOnAChatAnotherComputerHoldsQuitsWithoutAFailure(t *testing.T) {
	r := newDriveRig(t)
	ctx := t.Context()
	r.spokenFixture()
	c := r.startChat(&noticeLog{})
	c.mustSay()
	durable := r.head()
	waitFor(t, "the first turn to be durable", func() bool { return r.directoryHead(r.cell.ID) == durable })

	copyRoot := filepath.Join(t.TempDir(), r.cell.ID)
	if err := os.CopyFS(copyRoot, os.DirFS(r.cell.Root)); err != nil {
		t.Fatal(err)
	}
	own, err := cell.OpenAt(copyRoot, r.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	eng := cellstore.EngineFor(work)
	eng.DataRoot, eng.Binary, eng.Transport = t.TempDir(), r.bin, cellstore.Spawn{Binary: r.bin}
	drive, err := r.b.Drive(ctx, eng, own, DriveOptions{DeviceName: "laptop"})
	if err != nil {
		t.Fatal(err)
	}
	if !drive.Viewing() {
		t.Fatal("a window opened on a chat another computer holds does not know it only shows the chat")
	}
	seat := pipelinedSeat(t, own, work, func(cellstore.Engine) cellstore.Store { return drive.Store(eng) }, drive.Gate)
	if err := executor.Drain(ctx, seat); err != nil {
		t.Fatalf("saving the seat of a window that only shows the chat: %v", err)
	}
	if err := drive.Close(ctx); err != nil {
		t.Fatalf("closing the drive side of a window that only shows the chat: %v", err)
	}
}

// C3.1 in the state the hand test left: an earlier attempt of this window failed
// while it still drove the chat, the chat was taken away since, and no later
// seal can ever clear that failure because every one is now refused as read-only.
func TestAnEarlierFailureDoesNotFollowAWindowThatOnlyShowsTheChat(t *testing.T) {
	r := newDriveRig(t)
	ctx := t.Context()
	r.spokenFixture()
	heartbeats := r.quietHeartbeats()
	c := r.startChat(&noticeLog{})
	c.mustSay()
	durable := r.head()
	waitFor(t, "the first turn to be durable", func() bool { return r.directoryHead(r.cell.ID) == durable })

	// A second seat of the same window, whose first screening of the folder fails.
	fault := errors.New("the folder was busy while it was screened")
	seat := pipelinedSeat(t, r.cell, r.work, func(cellstore.Engine) cellstore.Store {
		return &prepareFault{inner: c.drive.Store(r.engine), err: fault}
	}, c.drive.Gate)
	waitFor(t, "the failed screening to be reported while this window drives", func() bool {
		return errors.Is(cellstore.Captured(ctx, r.cell), fault)
	})

	r.takenByAnotherComputer(c, "live-pc", heartbeats)
	if err := executor.Drain(ctx, seat); err != nil {
		t.Fatalf("a window that only shows the chat and owes it nothing reported %v at quit", err)
	}
	if err := c.drive.Close(ctx); err != nil {
		t.Fatalf("closing the drive side: %v", err)
	}
}
