package store

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"os"
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

// An attempt that FALLS is the fault this function cannot see coming: the
// goroutine that opens the transaction sends its outcome on a channel two
// people are waiting on, and a panic guard.Go absorbs used to end it with
// nothing sent. The caller then waited out its whole bound and blamed the lock
// — ErrBusy, for a lock nobody was holding — and the rollback goroutine it
// started on the way out waited on that channel for the life of the process,
// one leaked goroutine per fault.
//
// The outcome leaves from a defer now, so the fall arrives at whoever is
// waiting, named as itself.
func TestAnAttemptThatFallsReachesItsCallerAndFreesTheRollback(t *testing.T) {
	// guard.Note writes the fault and its stack through the standard logger,
	// which in a test is the test's own output.
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	// The fall lands while the caller is still waiting, which is the ordinary
	// case: it is told what happened rather than told the store is busy.
	tx, rolledBack, err := beginWithin(func() (*sql.Tx, error) {
		panic("the driver fell over inside BEGIN")
	}, writeLockWait)
	if tx != nil {
		_ = tx.Rollback()
		t.Fatal("an attempt that fell handed back a transaction")
	}
	if err == nil {
		t.Fatal("an attempt that fell reported no error at all")
	}
	if errors.Is(err, ErrBusy) {
		t.Fatalf("a fallen attempt was reported as a busy lock: %v", err)
	}
	if !errors.Is(err, errAttemptFell) {
		t.Fatalf("the caller was told %v, want the fault the attempt fell with", err)
	}
	if rolledBack != nil {
		t.Fatal("a rollback was started for an attempt the caller was still waiting on")
	}

	// And the fall that lands AFTER the caller has given up, where the only one
	// still waiting is the rollback goroutine. It ends because the send comes,
	// and it says so by closing — the one way a goroutine's exit can be watched
	// for rather than inferred from a count that anything else in the process
	// could move.
	_, rolledBack, err = beginWithin(func() (*sql.Tx, error) {
		time.Sleep(100 * time.Millisecond)
		panic("the driver fell over on its way out")
	}, 20*time.Millisecond)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("an attempt abandoned on the clock should say busy, said: %v", err)
	}
	if rolledBack == nil {
		t.Fatal("the abandoned attempt started no rollback to take the lock back")
	}
	select {
	case <-rolledBack:
	case <-time.After(5 * time.Second):
		t.Fatal("the rollback is still waiting on an attempt that will never send")
	}
}
