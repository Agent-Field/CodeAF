package session

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestAClosingSessionReleasesItsHoldOnceWhileItsJournalIsLocked(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		name := "ordinary close"
		if deletion {
			name = "deletion close"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal.jsonl")
			calls := 0
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
				cfg.SessionFile = path
				cfg.ReleaseHold = func() error {
					calls++
					if !InUse(path) {
						t.Error("the hold was released after the journal lock")
					}
					return nil
				}
			})
			if agent.config.ReleaseHold != nil {
				t.Fatal("a session config copy can inherit the journal owner's release")
			}
			closeAgent := agent.Close
			if deletion {
				closeAgent = agent.CloseForDeletion
			}
			if err := closeAgent(); err != nil {
				t.Fatal(err)
			}
			if err := closeAgent(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || InUse(path) {
				t.Fatalf("release calls = %d, journal held = %v; want 1, false", calls, InUse(path))
			}
		})
	}
}

func TestAFailedSaveStillReleasesTheHoldAndJournalAndReportsBothErrors(t *testing.T) {
	saveErr, releaseErr := errors.New("save failed"), errors.New("release failed")
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	calls := 0
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.SessionFile = path
		cfg.Seat = failedSavingSeat{Seat: executor.Host, err: saveErr}
		cfg.ReleaseHold = func() error {
			calls++
			if !InUse(path) {
				t.Error("a failed save unlocked the journal before releasing its hold")
			}
			return releaseErr
		}
	})
	err := agent.Close()
	var unsaved *UnsavedChatError
	if !errors.Is(err, saveErr) || !errors.Is(err, releaseErr) || !errors.As(err, &unsaved) || unsaved.Transcript != path {
		t.Fatalf("close lost a saving or release error: %v", err)
	}
	if calls != 1 || InUse(path) {
		t.Fatalf("release calls = %d, journal held = %v; want 1, false", calls, InUse(path))
	}
}

func TestARefusedJournalOpenerNeverReleasesTheOwnersHold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	calls := 0
	owner, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.SessionFile = path })
	_, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: path,
		ReleaseHold: func() error { calls++; return nil },
	}, &scriptedCompleter{})
	if !errors.Is(err, ErrSessionLocked) || calls != 0 || !InUse(path) {
		t.Fatalf("refused open = %v, release calls = %d, journal held = %v", err, calls, InUse(path))
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAFailedJournalReplayReleasesItsHoldBeforeUnlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"session\",\"version\":999}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	releaseErr := errors.New("release failed")
	_, _, err := openSessionFile(path, t.TempDir(), "test/model", "", func() error {
		calls++
		if !InUse(path) {
			t.Error("failed replay released the journal lock before its hold")
		}
		return releaseErr
	})
	if !errors.Is(err, errNewerFormat) || !errors.Is(err, releaseErr) || calls != 1 || InUse(path) {
		t.Fatalf("failed replay = %v, release calls = %d, journal held = %v", err, calls, InUse(path))
	}
}

func TestADeferredJournalCloseKeepsItsHoldUntilTheWorkersStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	var calls atomic.Int32
	file, _, err := openSessionFile(path, t.TempDir(), "test/model", "", func() error {
		if !InUse(path) {
			t.Error("deferred close released the journal lock before its hold")
		}
		calls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stopped := make(chan struct{})
	agent := &Agent{file: file, closingBeltDone: stopped}
	agent.finishCloseWhenStopped()
	if calls.Load() != 0 || !InUse(path) {
		t.Fatal("a worker that is still stopping lost its hold or journal")
	}
	close(stopped)
	deadline := time.Now().Add(5 * time.Second)
	for InUse(path) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 1 || InUse(path) {
		t.Fatalf("deferred release calls = %d, journal held = %v; want 1, false", calls.Load(), InUse(path))
	}
}

func TestAChildSessionCannotInheritItsParentsJournalRelease(t *testing.T) {
	calls := 0
	parent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.SessionFile = filepath.Join(t.TempDir(), "parent.jsonl")
		cfg.ReleaseHold = func() error { calls++; return nil }
	})
	cfg := parent.config
	if cfg.ReleaseHold != nil {
		t.Fatal("the parent's config retained its journal release")
	}
	cfg.SessionFile = filepath.Join(t.TempDir(), "child.jsonl")
	cfg.ReleaseHold = func() error { calls++; return nil }
	child, err := parent.newChildAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !InUse(parent.config.SessionFile) {
		t.Fatal("a derived config released the parent conversation's hold")
	}
	if err := parent.Close(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("parent release calls = %d; want 1", calls)
	}
}
