package lane

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	"github.com/Agent-Field/aforge-v2/internal/filelock"
)

// ── THE THIRTY MINUTES NOBODY SENT ANYTHING (issue #264) ────────────────────
//
// On 2026-09-01 a chat process issued no model call at all for 29m49s. The
// network was fine. The belief file's own gate was held, [ledger.keep] reached
// [ledger.compact] on the first observation of the process, and the exclusive
// flock underneath it was taken while the ledger's mutex was held — so every
// encode after it, which reads that same mutex, waited on somebody else's
// filesystem.
//
// THESE ARE PROPERTY TESTS AND NOT SCENARIOS. The defect is not a case, it is a
// shape: any door of this ledger that reaches a lock while holding the mutex
// the chooser reads stops the whole process. So the bound is asserted on the
// DOORS — a sighting, a read — with the file's gate held by somebody else for
// the whole test.

// sendBound is how long a door of this ledger may take while another process
// holds the belief file's lock. The work under it is arithmetic over a few
// hundred map entries, so this is four orders of magnitude of headroom; what it
// really separates is FINITE from the thirty minutes that were measured.
const sendBound = 2 * time.Second

// heldGate takes the belief file's own gate exclusively and hands back the
// release, exactly as another aforge in the middle of a compaction holds it.
//
// It is a second open file description of the same path, which is what makes it
// a real conflict rather than a re-entrant one: flock is a property of the
// description and not of the process (internal/filelock).
func heldGate(t *testing.T, path string) func() {
	t.Helper()
	gate, err := os.OpenFile(path+lockSuffix, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the gate: %v", err)
	}
	if err := filelock.Lock(gate, true, false); err != nil {
		t.Fatalf("hold the gate: %v", err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_ = filelock.Unlock(gate)
		_ = gate.Close()
	}
	t.Cleanup(release)
	return release
}

// within runs work and fails the test when it has not finished inside the
// bound. It never waits for a goroutine it has given up on: the whole failure
// under test is one that never returns.
func within(t *testing.T, bound time.Duration, what string, work func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	select {
	case <-done:
	case <-time.After(bound):
		t.Fatalf("%s did not finish in %s while the belief file's lock was held elsewhere", what, bound)
	}
}

// settles waits for a condition the writer reaches on its own time.
func settles(t *testing.T, bound time.Duration, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s did not happen within %s", what, bound)
}

// aSighting is one finished answer, as the send path hands it over.
func aSighting(lane string, at time.Time) Sighting {
	return Sighting{
		ID:     ID{Model: "vendor/model", Lane: lane},
		TTFT:   400 * time.Millisecond,
		Gen:    time.Second,
		Tokens: 64,
		At:     at,
	}
}

// TestASightingNeverWaitsOnTheBeliefFilesLock is #264 itself.
//
// The gate is held for the whole test. A sighting is what the send path folds
// in the moment an answer finishes, and it must come back whatever anybody else
// is doing to the file: a belief is advisory and the wire is not.
func TestASightingNeverWaitsOnTheBeliefFilesLock(t *testing.T) {
	path := sharedFile(t)
	defer heldGate(t, path)()

	beliefs := newLedger()
	beliefs.keepIn(newStore().at(path))

	within(t, sendBound, "one sighting", func() { beliefs.Note(aSighting("A", noon)) })
}

// TestAReadIsNeverBehindAWriteThatCannotTakeTheLock is the second half, and it
// is the one that made the incident a whole-process freeze rather than one slow
// call.
//
// The chooser reads this ledger on EVERY encode. A write that stops inside the
// mutex therefore stops every model call in the process, which is exactly what
// was measured: the ledger file froze at the moment of the failure while
// everything that did not need the mutex kept moving.
func TestAReadIsNeverBehindAWriteThatCannotTakeTheLock(t *testing.T) {
	path := sharedFile(t)
	defer heldGate(t, path)()

	beliefs := newLedger()
	beliefs.keepIn(newStore().at(path))

	// The write goes first and is deliberately not waited for: on the shape
	// this test was written against it never returns.
	go beliefs.Note(aSighting("A", noon))
	time.Sleep(20 * time.Millisecond)

	within(t, sendBound, "the chooser's read", func() { beliefs.Beliefs("vendor/model") })
	within(t, sendBound, "a second sighting", func() { beliefs.Note(aSighting("B", noon)) })
}

// TestACompactionThatCannotTakeTheLockIsCountedAndSaid is the "never silent"
// half of the law.
//
// A write that was deferred and said nothing is the same freeze with the
// symptom removed: the person in the incident had a surface saying "waiting"
// and a log saying nothing at all. So a deferral is counted, and it says so
// once in the call log — the file somebody reading an incident already has
// open.
func TestACompactionThatCannotTakeTheLockIsCountedAndSaid(t *testing.T) {
	path := sharedFile(t)
	logs := t.TempDir()
	calllog.Open(logs)
	t.Cleanup(calllog.Close)
	defer heldGate(t, path)()

	beliefs := newLedger()
	beliefs.keepIn(newStore().at(path))
	beliefs.Note(aSighting("A", noon))

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go beliefs.Persist(ctx)

	settles(t, sendBound, "the deferral being counted", func() bool { return beliefs.Deferred() > 0 })
	settles(t, sendBound, "the deferral being said", func() bool {
		written, err := os.ReadFile(calllog.Path())
		return err == nil && strings.Contains(string(written), deferredNote)
	})
}

// TestWhatCouldNotBeCompactedIsWrittenWhenTheLockIsFree is the other half of
// the same promise: deferred is not dropped.
//
// Every observation is in the journal before the writer is ever signalled, so
// what a held lock costs is a compaction and never a belief — and the moment
// the lock is free the state file catches up on its own, with nobody's request
// behind it.
func TestWhatCouldNotBeCompactedIsWrittenWhenTheLockIsFree(t *testing.T) {
	path := sharedFile(t)
	release := heldGate(t, path)

	beliefs := newLedger()
	beliefs.keepIn(newStore().at(path))
	for _, lane := range []string{"A", "B", "C"} {
		beliefs.Note(aSighting(lane, noon))
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go beliefs.Persist(ctx)

	settles(t, sendBound, "the deferral being counted", func() bool { return beliefs.Deferred() > 0 })
	release()

	settles(t, 5*time.Second, "the state file catching up", func() bool {
		return len(readState(path).Beliefs) == 3
	})
	// And the journal it folded in is empty, which is what says the compaction
	// really happened rather than the state having been written beside a log
	// that would replay it all over again.
	settles(t, 5*time.Second, "the journal being emptied", func() bool {
		info, err := os.Stat(journalPath(path))
		return err == nil && info.Size() == 0
	})
}

// TestAProcessWithNoWriterLosesNothing states the fallback in the one place it
// can be read.
//
// Nothing in this package runs a goroutine of its own, so a process that never
// starts [ledger.Persist] never compacts. That has to cost a longer replay and
// nothing else: the observations are appended before the writer is signalled,
// so a cold ledger over the same file holds every one of them.
func TestAProcessWithNoWriterLosesNothing(t *testing.T) {
	path := sharedFile(t)

	writing := newLedger()
	writing.keepIn(newStore().at(path))
	for _, lane := range []string{"A", "B", "C"} {
		writing.Note(aSighting(lane, noon))
	}

	cold := newLedger()
	cold.keepIn(newStore().at(path))
	if got := len(cold.Beliefs("vendor/model")); got != 3 {
		t.Fatalf("a ledger opened over the same file knows %d of the three lanes written with no writer running", got)
	}
}
