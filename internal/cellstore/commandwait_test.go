package cellstore

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// TestP2FinishedCaptureDoesNotJoinGroupCommit proves P2/P5/C28: an opened
// chat's baseline finishes before its first call, and an unrelated disk flush
// cannot make that call wait after the screened capture is complete.
func TestP2FinishedCaptureDoesNotJoinGroupCommit(t *testing.T) {
	c := newCell(t)
	s := blockingCapture{make(chan struct{}, 2), make(chan struct{}), make(chan TurnInfo, 2)}
	r, err := NewRecorder(&stubExec{}, s, c, filepath.Join(t.TempDir(), "wal"), Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	r.Warm(t.Context())
	<-s.entered
	close(s.release)
	if err := r.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-s.calls
	// Hold the real group's flush slot, as a slow fsync would, while leaving
	// record writes and the completed capture free to proceed.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case commits.flushing <- struct{}{}:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer func() { <-commits.flushing }()
	ran := false
	err = r.Around(ctx, executor.Call{Tool: "bash", Args: []byte("once"), Deferred: true}, executor.EffectExternal, AgentRun, func() ([]byte, bool) { ran = true; return []byte("saved output"), false })
	if err != nil || !ran {
		t.Fatalf("completed baseline joined disk flush: ran=%v err=%v", ran, err)
	}
	_, recovery, err := OpenWAL(r.wal.path)
	if err != nil || len(recovery.Completed) != 1 || string(recovery.Completed[0].Stdout) != "saved output" {
		t.Fatalf("completion was not written before return: %+v %v", recovery, err)
	}
}

// TestP5AnEarlySyncCannotPayForAnUnfinishedCapture proves P5/C28: a group
// that syncs before capture completes must not clear the completed snapshot's
// debt before its local turn records are made durable.
func TestP5AnEarlySyncCannotPayForAnUnfinishedCapture(t *testing.T) {
	c := newCell(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	select {
	case commits.flushing <- struct{}{}:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer func() { <-commits.flushing }()
	t.Cleanup(func() { engineCommits.Lock(); delete(engineCommits.cells, c.Root); engineCommits.Unlock() })
	var captured atomic.Bool
	var after atomic.Int32
	e := Engine{DataRoot: t.TempDir()}
	e.Transport = transportFunc(func(ctx context.Context, _ Target, op Op) ([]byte, error) {
		switch op.(type) {
		case capturePlanOp:
			return []byte(`{"token":"screen-this","full":true,"paths":[]}`), nil
		case sealOp:
			if err := flushEngines(ctx, c.Root); err != nil {
				return nil, err
			}
			captured.Store(true)
			return []byte(`{"snapshot":"completed","durable":false}`), nil
		case syncOp:
			if captured.Load() {
				after.Add(1)
			}
			return []byte(`{"durable":true}`), nil
		default:
			return nil, fmt.Errorf("unexpected %s", op.Verb())
		}
	})
	if _, err := e.Seal(ctx, c, TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := flushEngines(ctx, c.Root); err != nil {
		t.Fatal(err)
	}
	if after.Load() == 0 {
		t.Fatal("the completed snapshot was acknowledged only by a sync before it existed")
	}
}
