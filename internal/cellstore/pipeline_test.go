package cellstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

type blockingCapture struct {
	entered, release chan struct{}
	calls            chan TurnInfo
}

func (s blockingCapture) Seal(ctx context.Context, c cell.Cell, in TurnInfo) (Sealed, error) {
	s.entered <- struct{}{}
	select {
	case <-s.release:
	case <-ctx.Done():
		return Sealed{}, ctx.Err()
	}
	s.calls <- in
	return Sealed{}, nil
}

// TestD3CommandReturnsAndNextCallWaits proves D3 and D4 without clock sleeps:
// the first call returns with a durable completion while capture is blocked;
// no later tool can run over that capture, and its output remains in the WAL.
func TestD3CommandReturnsAndNextCallWaits(t *testing.T) {
	c := newCell(t)
	s := blockingCapture{make(chan struct{}, 2), make(chan struct{}), make(chan TurnInfo, 2)}
	wal := filepath.Join(t.TempDir(), "wal")
	r, err := NewRecorder(&stubExec{}, s, c, wal, Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Around(t.Context(), executor.Call{Tool: "bash", Args: []byte("first")}, executor.EffectExternal, AgentRun, func() ([]byte, bool) { return []byte("first output"), false }); err != nil {
		t.Fatal(err)
	}
	<-s.entered
	_, recovery, err := OpenWAL(wal)
	if err != nil || len(recovery.Completed) != 1 || string(recovery.Completed[0].Stdout) != "first output" {
		t.Fatalf("unsealed result not durable: %+v %v", recovery, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ran := false
	err = r.Around(ctx, executor.Call{Tool: "bash"}, executor.EffectExternal, AgentRun, func() ([]byte, bool) { ran = true; return nil, false })
	if !errors.Is(err, context.Canceled) || ran {
		t.Fatalf("next call passed pending capture: %v ran=%v", err, ran)
	}
	if err := Await(ctx, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("move boundary dropped pending seal: %v", err)
	}
	close(s.release)
	if err := r.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	in := <-s.calls
	if len(in.Calls) != 1 || string(in.Calls[0].Stdout) != "first output" {
		t.Fatalf("capture lost result: %+v", in)
	}
}

// TestD4CloseSurfacesPendingCapture proves D4: process exit surfaces a pending
// capture rather than silently abandoning it when its wait is cancelled.
func TestD4CloseSurfacesPendingCapture(t *testing.T) {
	c := newCell(t)
	s := blockingCapture{make(chan struct{}, 1), make(chan struct{}), make(chan TurnInfo, 1)}
	r, err := NewRecorder(&stubExec{}, s, c, filepath.Join(t.TempDir(), "wal"), Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	r.pending = []Executed{exec1("bash", "out")}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// The worker ignores a turn's cancellation, but closing waits on the caller's
	// deadline and tells the person that saving is still pending.
	r.Settle(t.Context())
	<-s.entered
	if err := r.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("exit hid a pending capture: %v", err)
	}
	close(s.release)
	if err := r.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type failedCapture struct{ err error }

func (s failedCapture) Seal(context.Context, cell.Cell, TurnInfo) (Sealed, error) {
	return Sealed{}, s.err
}

// TestD4FailedCaptureBlocksAMove proves D4 and L9: even after a background
// worker stops, a failed save cannot authorize moving the earlier head and its
// completed external call remains durable rather than being run again.
func TestD4FailedCaptureBlocksAMove(t *testing.T) {
	c := newCell(t)
	fault := errors.New("capture refused")
	wal := filepath.Join(t.TempDir(), "wal")
	r, err := NewRecorder(&stubExec{}, failedCapture{fault}, c, wal, Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Around(t.Context(), executor.Call{Tool: "bash"}, executor.EffectExternal, AgentRun, func() ([]byte, bool) { return []byte("done once"), false }); err != nil {
		t.Fatal(err)
	}
	if err := r.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := Await(t.Context(), c); !errors.Is(err, fault) {
		t.Fatalf("move ignored the failed save: %v", err)
	}
	if err := r.Drain(t.Context()); !errors.Is(err, fault) {
		t.Fatalf("close ignored the failed save: %v", err)
	}
	_, recovery, err := OpenWAL(wal)
	if err != nil || len(recovery.Completed) != 1 {
		t.Fatalf("failed save lost its completion: %+v %v", recovery, err)
	}
}
