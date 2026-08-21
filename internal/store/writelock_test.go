package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// holdTheWriteLock takes the write lock on path from a second connection — a
// second process, as far as SQLite is concerned — and holds it until the
// returned function is called. It is what a wedged writer looks like from here.
func holdTheWriteLock(t *testing.T, path string) (release func()) {
	t.Helper()
	other, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(10000)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open the second connection: %v", err)
	}
	tx, err := other.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("take the write lock: %v", err)
	}
	// A BEGIN IMMEDIATE alone reserves the lock; the write is here so the hold
	// is a real transaction with something in it, the way a wedged writer is.
	if _, err := tx.Exec(`INSERT INTO events (ts, kind, node_id, payload) VALUES (?, ?, ?, ?)`,
		"2026-01-01T00:00:00Z", "held_by_the_test", "", "{}"); err != nil {
		t.Fatalf("write under the held lock: %v", err)
	}
	released := false
	release = func() {
		if released {
			return
		}
		released = true
		_ = tx.Rollback()
		_ = other.Close()
	}
	t.Cleanup(release)
	return release
}

// A write against a lock somebody else is holding used to take ten seconds —
// SQLite's whole busy_timeout — and the caller could do nothing about it,
// because the driver runs BEGIN IMMEDIATE with a context of its own and throws
// the caller's away. It gives up on its own clock now.
func TestAWriteGivesUpOnAHeldLockInsteadOfStalling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph := openTestStore(t, path)
	holdTheWriteLock(t, path)

	start := time.Now()
	_, err := graph.PostMessage(Message{SessionID: "session", Role: RoleUser, Body: "hello"})
	waited := time.Since(start)

	if err == nil {
		t.Fatal("posting a message won a lock the test is holding")
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("a write that lost the lock should say so with ErrBusy, said: %v", err)
	}
	if waited > writeLockWait+time.Second {
		t.Fatalf("the write waited %s for the lock; the bound is %s", waited, writeLockWait)
	}
	if waited < writeLockWait {
		t.Fatalf("the write gave up after %s, well inside its own %s bound", waited, writeLockWait)
	}
}

// The bound is on the WAIT, never on the transaction. Handing BeginTx a context
// with the deadline on it would make database/sql roll a live transaction back
// under its caller the moment the deadline passed — a slow write would lose its
// work rather than its patience.
func TestTheBoundOnTheWaitIsNotABoundOnTheTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph := openTestStore(t, path)

	tx, err := graph.beginWrite()
	if err != nil {
		t.Fatalf("beginWrite on an uncontended store: %v", err)
	}
	time.Sleep(writeLockWait + 500*time.Millisecond)
	if _, err := tx.Exec(`INSERT INTO events (ts, kind, node_id, payload) VALUES (?, ?, ?, ?)`,
		"2026-01-01T00:00:00Z", "slow_but_alive", "", "{}"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("writing past the wait's bound: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("committing past the wait's bound: %v", err)
	}
}

// An abandoned attempt is still out there waiting, and it may yet win the lock
// after its caller has gone. When it does it is rolled back at once, or it would
// hold the lock nobody is ever going to release.
func TestAnAbandonedAttemptDoesNotKeepTheLockItLaterWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph := openTestStore(t, path)
	release := holdTheWriteLock(t, path)

	if _, err := graph.PostMessage(Message{SessionID: "session", Role: RoleUser, Body: "abandoned"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected the first post to give up busy, got: %v", err)
	}
	// Now the wedged writer lets go, and the abandoned attempt takes the lock it
	// was waiting for. Whether the next write succeeds is whether anything gave
	// that lock back.
	release()

	deadline := time.Now().Add(writeLockWait + 2*time.Second)
	var err error
	for time.Now().Before(deadline) {
		if _, err = graph.PostMessage(Message{SessionID: "session", Role: RoleUser, Body: "after"}); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the lock never came back after the holder released it: %v", err)
}
