package cellsync

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// fakeLiveness is a watch socket the test opens and closes by hand.
type fakeLiveness struct {
	mu    sync.Mutex
	kept  []directory.Hold
	sock  *fakeKeeping
	start bool // whether the socket is healthy from the first Keep
}

type fakeKeeping struct {
	healthy atomic.Bool
	changes chan struct{}
}

func newFakeLiveness(healthy bool) *fakeLiveness {
	return &fakeLiveness{start: healthy, sock: &fakeKeeping{changes: make(chan struct{}, 1)}}
}

func (l *fakeLiveness) Keep(cell string, fence uint64) Keeping {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.kept = append(l.kept, directory.Hold{Cell: cell, Fence: fence})
	l.sock.healthy.Store(l.start)
	return l.sock
}

// flip sets the socket's health and signals the change, as a feed does.
func (l *fakeLiveness) flip(healthy bool) {
	l.sock.healthy.Store(healthy)
	select {
	case l.sock.changes <- struct{}{}:
	default:
	}
}

func (k *fakeKeeping) Healthy() bool            { return k.healthy.Load() }
func (k *fakeKeeping) Changes() <-chan struct{} { return k.changes }
func (k *fakeKeeping) Release()                 {}

// heldRig is a rig whose batcher runs its heartbeat loop under a fake sleeper
// with l vouching, on a lease device A holds and has nothing pending on.
type heldRig struct {
	*rig
	b    *Batcher
	sl   *fakeSleeper
	stop func()
}

func runHeld(t *testing.T, l Liveness) *heldRig {
	t.Helper()
	r := newRig(t)
	b := r.batcher()
	b.Liveness = l
	sl := newFakeSleeper(r.clock)
	b.Sleep = sl.Sleep
	r.publishFirst(map[string]string{"a": "0"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	sl.settle(1)
	t.Cleanup(func() { cancel(); <-done })
	return &heldRig{rig: r, b: b, sl: sl}
}

// ticks lets n heartbeat intervals pass, each fully handled before the next.
func (h *heldRig) ticks(n int) {
	for range n {
		h.sl.advance(directory.HeartbeatEvery)
		h.sl.settle(1)
	}
}

// beatsReach waits for the directory to have heard n heartbeats, and fails if it
// does not within a real second.
func (h *heldRig) beatsReach(n int32) {
	h.t.Helper()
	for deadline := time.Now().Add(time.Second); h.dirA.beats.Load() < n; {
		if time.Now().After(deadline) {
			h.t.Fatalf("%d heartbeats, want %d", h.dirA.beats.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// A vouching socket is the proof of life: a chat idle for hours sends nothing.
func TestNoBeatsWhileSocketVouches(t *testing.T) {
	h := runHeld(t, newFakeLiveness(true))
	h.ticks(240) // two idle hours
	if got := h.dirA.beats.Load(); got != 0 {
		t.Fatalf("%d heartbeats in two idle hours with the socket up, want none", got)
	}
}

// The moment the socket stops vouching, the lease is on its stored expiry
// alone, so a beat goes at once; the next ticks keep it alive while it is down.
func TestBeatsResumeWhenSocketDown(t *testing.T) {
	l := newFakeLiveness(true)
	h := runHeld(t, l)
	h.ticks(3)
	l.flip(false)
	h.beatsReach(1) // before any tick: the change is answered at once
	h.ticks(2)
	if got := h.dirA.beats.Load(); got != 3 {
		t.Fatalf("%d heartbeats after the socket went down and two ticks, want 3", got)
	}
	l.flip(true)
	h.ticks(3)
	if got := h.dirA.beats.Load(); got != 3 {
		t.Fatalf("%d heartbeats, want none more once the socket vouches again", got)
	}
}

// A change in the pending count is a fact only a beat carries, so one goes at
// the next tick even with the socket healthy, and only one.
func TestBeatOnPendingChange(t *testing.T) {
	h := runHeld(t, newFakeLiveness(true))
	h.ticks(2)
	// The flush loop is held still, so the noted turns stay pending.
	h.b.flushMu.Lock()
	t.Cleanup(h.b.flushMu.Unlock)
	note(h.b, h.rig.seal(map[string]string{"a": "1"}), h.rig.seal(map[string]string{"a": "2"}))
	h.ticks(3)
	if got := h.dirA.beats.Load(); got != 1 {
		t.Fatalf("%d heartbeats, want exactly the one that carried the pending count", got)
	}
	if got := h.head(cellID).Lease.Pending; got != 2 {
		t.Fatalf("directory shows %d pending, want 2", got)
	}
}

// A relay that never said it vouches is an old one: beat as always.
func TestBeatsOnRelayWithoutVouching(t *testing.T) {
	h := runHeld(t, newFakeLiveness(false))
	h.ticks(4)
	if got := h.dirA.beats.Load(); got != 4 {
		t.Fatalf("%d heartbeats in 4 ticks without vouching, want 4", got)
	}
}

// vouchingStream is a socket that vouches, speaks once and then stays quiet.
type vouchingStream struct {
	spoke bool
	done  chan struct{}
}

func (s *vouchingStream) Next(ctx context.Context) (dirwatch.Frame, error) {
	if !s.spoke {
		s.spoke = true
		return dirwatch.Frame{Version: 1}, nil
	}
	select {
	case <-ctx.Done():
	case <-s.done:
	}
	return dirwatch.Frame{}, ctx.Err()
}
func (s *vouchingStream) Ping(context.Context) error { return nil }
func (s *vouchingStream) Vouching() bool             { return true }
func (s *vouchingStream) Close()                     { close(s.done) }

func vouchingDial(context.Context, []directory.Hold) (dirwatch.Stream, error) {
	return &vouchingStream{done: make(chan struct{})}, nil
}

// The socket names at most as many holds as it is allowed; a chat past the cap
// is never vouched for, so its heartbeats go on.
func TestBeatsForCellsBeyondMaxHolds(t *testing.T) {
	holder := dirwatch.Hold("beyond-max-holds", 1, vouchingDial)
	first := holder.Take(directory.Hold{Cell: "someone-else", Fence: 1}) // takes the only place
	defer first.Release()
	h := runHeld(t, holdSocket{holder})
	h.ticks(4)
	if got := h.dirA.beats.Load(); got != 4 {
		t.Fatalf("%d heartbeats in 4 ticks past the cap, want 4", got)
	}
}

// The same socket with room left vouches, and the heartbeats stop.
func TestBeatsStopForCellsWithinMaxHolds(t *testing.T) {
	holder := dirwatch.Hold("within-max-holds", 2, vouchingDial)
	h := runHeld(t, holdSocket{holder})
	probe := holder.Take(directory.Hold{Cell: cellID, Fence: h.drv.Fence})
	defer probe.Release()
	for deadline := time.Now().Add(time.Second); !probe.Healthy(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the socket never vouched")
		}
	}
	before := h.dirA.beats.Load()
	h.ticks(10)
	if got := h.dirA.beats.Load() - before; got != 0 {
		t.Fatalf("%d heartbeats with the socket vouching, want none", got)
	}
}

// holdSocket adapts the holder as the wiring in syncsetup does.
type holdSocket struct {
	h *dirwatch.Holder[directory.Hold]
}

func (s holdSocket) Keep(cell string, fence uint64) Keeping {
	return s.h.Take(directory.Hold{Cell: cell, Fence: fence})
}
