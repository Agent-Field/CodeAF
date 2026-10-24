package cellstore

import (
	"context"
	"errors"
	"os"
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

// TestE2WarmStartsBeforeACommandAndCloseJoinsIt proves E2 and C9: a newly
// opened chat starts capture without a tool, and neither an early tool nor
// close can race its screen. No command is needed to finish the first capture.
func TestE2WarmStartsBeforeACommandAndCloseJoinsIt(t *testing.T) {
	c := newCell(t)
	s := blockingCapture{make(chan struct{}, 2), make(chan struct{}), make(chan TurnInfo, 2)}
	r, err := NewRecorder(&stubExec{}, s, c, filepath.Join(t.TempDir(), "wal"), Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	r.Warm(t.Context())
	<-s.entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ran := false
	err = r.Around(ctx, executor.Call{Tool: "bash"}, executor.EffectExternal, AgentRun, func() ([]byte, bool) { ran = true; return nil, false })
	if !errors.Is(err, context.Canceled) || ran {
		t.Fatalf("tool raced cold screen: %v ran=%v", err, ran)
	}
	if err := r.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("close abandoned capture: %v", err)
	}
	close(s.release)
	if err := r.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	in := <-s.calls
	if len(in.Calls) != 0 {
		t.Fatalf("warm capture fabricated calls: %+v", in)
	}
}

// TestC28CommandWaitDoesNotCommitBeforeBoundary proves C28/E1: ordinary
// command ownership checks wait for capture, but a disk-flush error belongs to
// the explicit movement boundary, not to each following command.
func TestC28CommandWaitDoesNotCommitBeforeBoundary(t *testing.T) {
	c := newCell(t)
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, nil, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "not-a-directory")
	commits.mark(c.Root, path, false)
	defer func() { commits.mu.Lock(); delete(commits.pending, commitPath{c.Root, path}); commits.mu.Unlock() }()
	if err := Captured(t.Context(), c); err != nil {
		t.Fatalf("command forced disk flush: %v", err)
	}
	if err := Await(t.Context(), c); err == nil {
		t.Fatal("move hid disk flush error")
	}
}

// TestE2ExitJoinsAnOpenedSeatWithoutAnAgent proves E2/C28: an error after
// seat construction cannot abandon a first capture just because no Agent.Close
// was installed. The common process exit also waits and reports cancellation.
func TestE2ExitJoinsAnOpenedSeatWithoutAnAgent(t *testing.T) {
	c := newCell(t)
	s := blockingCapture{make(chan struct{}, 2), make(chan struct{}), make(chan TurnInfo, 2)}
	r, err := NewRecorder(&stubExec{}, s, c, filepath.Join(t.TempDir(), "wal"), Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	r.Warm(t.Context())
	<-s.entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("exit abandoned opened seat: %v", err)
	}
	close(s.release)
	if err := r.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
}
