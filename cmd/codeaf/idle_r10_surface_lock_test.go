package main

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestIdleRelease_R10_RetryableRefusalIsNotHeldElsewhere(t *testing.T) {
	err := appEngineError(&executor.WorkRefusal{Line: "couldn't confirm taking this chat back from the relay; try again", Retryable: true})
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeUnreachable || wire.Detail != nil {
		t.Fatalf("uncertain take-back offered a move: %+v", err)
	}
}

func TestIdleRelease_R10_NeverReleasedRenameAvoidsLiveDriveMutex(t *testing.T) {
	live := &liveDrive{}
	live.install(&syncsetup.Drive{}, nil, make(chan struct{}))
	a, err := session.New(session.Config{Workspace: t.TempDir(), ProfileDir: t.TempDir(), Model: "test/model", APIKey: "fixture", BaseURL: "http://127.0.0.1:1", System: "SYSTEM", Seat: executor.Gated(executor.Stance{Class: executor.HostBound}, func() error { return nil }, nil, nil, driveSettingsGate(live))})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	done := make(chan error, 1)
	blocked := func() bool {
		live.mu.Lock()
		defer live.mu.Unlock()
		go func() {
			err := a.RenameByPerson("ordinary local title")
			live.SavedMetadata()
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
			return false
		case <-time.After(driveWorkWithin + 100*time.Millisecond):
			return true
		}
	}()
	if blocked {
		if err := <-done; err != nil {
			t.Error(err)
		}
		t.Fatal("ordinary durable write acquired a new unbounded startup mutex before its released-only check")
	}
}

func TestIdleRelease_R10_ReleasedRenameStillAdmitsWhileLiveMutexIsBusy(t *testing.T) {
	f := newIdleAppContract(t)
	before := f.released(t)
	var book *driveBook
	f.b.locked(func() { book = f.b.drives })
	live := book.renewalsOf(f.agent.ID)
	if live == nil {
		t.Fatal("no installed drive")
	}
	done := make(chan error, 1)
	completed, err := func() (bool, error) {
		live.mu.Lock()
		defer live.mu.Unlock()
		go func() {
			_, err := f.b.Rename(f.agent.ID, "kept while catch-up is busy")
			done <- err
		}()
		// Rename acknowledges only after publishing the new facts. Both
		// admission and that acknowledgement must finish with the lock held.
		select {
		case err := <-done:
			return true, err
		case <-time.After(10 * time.Second):
			return false, nil
		}
	}()
	if !completed {
		t.Fatal("busy live mutex blocked rename admission or its acknowledgement")
	}
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.own.Dir.Cell(t.Context(), f.agent.ID)
	if err != nil || after.Cell.Lease.Fence <= before.Cell.Lease.Fence {
		t.Fatalf("busy live mutex skipped released-window admission: before=%d after=%+v error=%v", before.Cell.Lease.Fence, after.Cell.Lease, err)
	}
}
