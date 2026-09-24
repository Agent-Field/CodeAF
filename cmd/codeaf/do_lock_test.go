package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/lease"
)

// An errand that cannot be the brain says who has the role and stops.
//
// The silent version of this cost two full benchmark grids: `do` runs whose
// store was already spoken for posted their command, watched a journal nobody
// was serving, and burned 25-40 minutes each at nodes:0, spend $0.00, with one
// repeated progress line and nothing anywhere naming the lock. The wait is now
// bounded and the diagnostic names the holder, the store, and the way out.
func TestDoFailsFastWhenAnotherProcessHoldsTheResidentLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	release, heldBy, err := lease.AcquireResident(path, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("could not stand in for the resident: release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	defer release()

	var stdout, stderr strings.Builder
	started := time.Now()
	err = doErrand(doRequest{
		task:     "write the release note",
		database: path,
		// Every seat is named, so the crew needs no router to reach the lock.
		model: "test/model", planModel: "test/model", checkModel: "test/model",
		// The wall is long and the bound is short on purpose: the thing under
		// test is that the run leaves on the bound rather than on the wall.
		timeout:      5 * time.Minute,
		residentWait: 300 * time.Millisecond,
		stdout:       &stdout,
		stderr:       &stderr,
	})
	elapsed := time.Since(started)
	if err == nil {
		t.Fatalf("the errand deferred to a resident that was never coming and said it worked\nstderr:\n%s", stderr.String())
	}
	if elapsed > 30*time.Second {
		t.Fatalf("the errand waited %s — it burned the wall instead of the bound", elapsed)
	}
	// The diagnostic has to carry the three things a person needs: who holds the
	// role, which store it is held for, and what to do about it.
	for _, want := range []string{strconv.Itoa(lockedPID(t, path)), path, "--db"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the diagnostic does not name %q: %v", want, err)
		}
	}
	if !strings.Contains(stderr.String(), "waiting for the resident (pid") {
		t.Fatalf("the wait was silent:\n%s", stderr.String())
	}
}

// lockedPID reads the pid off the lock the test itself is holding, so the
// assertion above quotes the journal's own answer rather than assuming it.
func lockedPID(t *testing.T, path string) int {
	t.Helper()
	holder, err := lease.ProbeResident(path)
	if err != nil || holder == nil {
		t.Fatalf("probe the lock this test holds = %+v, %v", holder, err)
	}
	return holder.PID
}

// The other half of the same failure: a holder that is serving a different
// database is not slow, it is absent. There is nothing to wait for, and the run
// says so immediately rather than spending its bound finding out.
func TestAResidentServingAnotherStoreIsRefusedImmediately(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "mine.db")
	theirs := filepath.Join(dir, "theirs.db")
	holder := &lease.Resident{PID: 4242, Host: "box", Store: theirs}

	err := residentServesThisStore(holder, mine)
	if err == nil {
		t.Fatal("a resident serving another store was accepted as this errand's brain")
	}
	for _, want := range []string{"4242", "box", theirs, "--db"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the diagnostic does not name %q: %v", want, err)
		}
	}
	// The same store, however named, is the legitimate case and stays legitimate.
	if err := residentServesThisStore(&lease.Resident{PID: 7, Store: mine}, mine); err != nil {
		t.Fatalf("a resident serving this very store was refused: %v", err)
	}
	if err := residentServesThisStore(&lease.Resident{PID: 7}, mine); err != nil {
		t.Fatalf("an older build's lock, which names no store, was refused outright: %v", err)
	}
}
