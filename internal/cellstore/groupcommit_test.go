package cellstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestC28GroupCommitWritesBeforeItsClockFires proves C28/E1: ordered records
// are visible after each write, with no fsync until the shared 250ms tick.
func TestC28GroupCommitWritesBeforeItsClockFires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls")
	var synced []string
	q := newCommitQueue(func(p string) error { synced = append(synced, p); return nil })
	var tick func()
	q.after = func(d time.Duration, f func()) {
		if d != 250*time.Millisecond {
			t.Fatalf("group interval %s", d)
		}
		if tick != nil {
			t.Fatal("one timer per group")
		}
		tick = f
	}
	for _, line := range []string{"intent\n", "group\n", "done\n"} {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.WriteString(line); err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
		q.mark("chat", path, false)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "intent\ngroup\ndone\n" {
		t.Fatalf("ordered writes %q %v", raw, err)
	}
	if len(synced) != 0 {
		t.Fatalf("command synced: %v", synced)
	}
	tick()
	if !reflect.DeepEqual(synced, []string{path}) {
		t.Fatalf("one group sync: %v", synced)
	}
}

// TestC28BoundaryReportsFailedSyncAndRetainsDebt proves C28/E1: handoff and
// exit cannot silently acknowledge a failed group commit; retry keeps the debt.
func TestC28BoundaryReportsFailedSyncAndRetainsDebt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls")
	if err := os.WriteFile(path, []byte("done\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("disk refused sync")
	q := newCommitQueue(func(string) error { return fault })
	q.after = func(time.Duration, func()) {}
	q.mark("chat", path, true)
	if err := q.flush(t.Context(), "chat"); !errors.Is(err, fault) {
		t.Fatalf("boundary hid failure: %v", err)
	}
	if len(q.pending) == 0 {
		t.Fatal("failed sync acknowledged data")
	}
	q.syncPath = func(string) error { return nil }
	if err := q.flush(t.Context(), "chat"); err != nil {
		t.Fatal(err)
	}
	if len(q.pending) != 0 {
		t.Fatal("successful retry did not drain")
	}
}

// TestC28ConcurrentWriteRemainsOwed proves C28/E1: a write occurring during a
// group fsync is not mistaken for bytes already committed by that generation.
func TestC28ConcurrentWriteRemainsOwed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var q *commitQueue
	count := 0
	q = newCommitQueue(func(string) error {
		count++
		if count == 1 {
			q.mark("chat", path, false)
		}
		return nil
	})
	q.after = func(time.Duration, func()) {}
	q.mark("chat", path, false)
	if err := q.flush(t.Context(), "chat"); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("concurrent generation synced %d times", count)
	}
}

// TestC28CancelledBoundaryKeepsDebt proves C28/E1: a cancelled close waiting
// behind disk IO reports cancellation and leaves its records pending.
func TestC28CancelledBoundaryKeepsDebt(t *testing.T) {
	q := newCommitQueue(func(string) error { return nil })
	q.after = func(time.Duration, func()) {}
	q.mark("chat", "calls", false)
	q.flushing <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := q.flush(ctx, "chat"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled flush: %v", err)
	}
	if len(q.pending) != 1 {
		t.Fatal("cancelled boundary lost records")
	}
}

// TestC28TornTailKeepsEarlierHistoryAndFutureWrites proves C28/L12: a power
// cut may discard its final append, but neither an earlier turn nor the next
// complete call can become unreadable because of that tail.
func TestC28TornTailKeepsEarlierHistoryAndFutureWrites(t *testing.T) {
	c := newCell(t)
	fake := &fakeEngine{}
	e := fake.engine(t)
	first, err := e.Seal(t.Context(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(rel(c, TurnsPath), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"V":1,"id":"torn` + "\n" + `{"V":1,"id":"partial`)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	head, err := Head(c)
	if err != nil || head == nil || head.Turn.ID != first.Turn.ID {
		t.Fatalf("torn turn damaged head: %+v %v", head, err)
	}
	second, err := e.Seal(t.Context(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Turn.Parent != first.Turn.ID {
		t.Fatal("next seal lost previous parent")
	}
	head, err = Head(c)
	if err != nil || head.Turn.ID != second.Turn.ID {
		t.Fatalf("new head after torn tail: %+v %v", head, err)
	}
	wal := filepath.Join(t.TempDir(), "calls")
	w, _, err := OpenWAL(wal)
	if err != nil {
		t.Fatal(err)
	}
	i := Intent{V: walV, ArgsHash: "first", Started: 1, SideEffect: "external"}
	if err = w.Begin(i); err != nil {
		t.Fatal(err)
	}
	f, err = os.OpenFile(wal, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"V":2,"op":"done` + "\n" + `{"V":2,"op":"partial`)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	w, rec, err := OpenWAL(wal)
	if err != nil || len(rec.Incomplete) != 1 {
		t.Fatalf("earlier intent lost: %+v %v", rec, err)
	}
	i.ArgsHash = "next"
	i.Started = 2
	if err = w.Begin(i); err != nil {
		t.Fatal(err)
	}
	_, rec, err = OpenWAL(wal)
	if err != nil || len(rec.Incomplete) != 2 {
		t.Fatalf("new record hidden by torn tail: %+v %v", rec, err)
	}
}

// TestC28SealCannotPublishBeforeRecordFlush proves C28/E1/L12: the engine
// cannot return a sealed turn to its publishing wrapper until the call/turn
// records reach disk, even though the recorder has not compacted the WAL yet.
func TestC28SealCannotPublishBeforeRecordFlush(t *testing.T) {
	c := newCell(t)
	fake := &fakeEngine{}
	e := fake.engine(t)
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, nil, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "not-a-directory")
	defer func() { commits.mu.Lock(); delete(commits.pending, commitPath{c.Root, path}); commits.mu.Unlock() }()
	e.Run = func(ctx context.Context, dir string, environ []string, argv ...string) ([]byte, error) {
		// Introduce the flush failure only after the pre-capture dependency flush.
		out, err := fake.run(ctx, dir, environ, argv...)
		commits.mark(c.Root, path, false)
		return out, err
	}
	_, err := e.Seal(t.Context(), c, TurnInfo{})
	if err == nil || !strings.Contains(err.Error(), "save call records") {
		t.Fatalf("publisher received an unflushed turn: %v", err)
	}
}
