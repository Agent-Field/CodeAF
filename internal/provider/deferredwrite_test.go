package provider

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE WIRE, WITH SOMEBODY ELSE HOLDING THE BELIEF FILE (issue #264) ───────
//
// The scenarios in hedge_test.go script a ledger, which is what makes them
// about the watch. These two are about the opposite thing: the REAL ledger,
// writing a real file, with the file's lock held by somebody who is not going
// to give it back — and the claim is that the wire does not notice.
//
// It is the incident of 2026-09-01 reduced to a test. That process sent nothing
// at all for 29m49s because a belief write took an exclusive flock while
// holding the mutex every encode reads.

// wireBound is how long a turn may take while the belief file's lock is held
// elsewhere. The lanes below answer in milliseconds, so what this really
// separates is a turn that happens from one that never comes back.
const wireBound = 5 * time.Second

// heldBeliefFile takes the real belief file's gate exclusively, as another
// aforge mid-compaction holds it, for the rest of the test.
//
// The path is the one a shipped binary uses, resolved under the temporary home
// the rig has already installed — a test that locked the real one would freeze
// whoever is running the suite.
func heldBeliefFile(t *testing.T) {
	t.Helper()
	path := lanes.StorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("make the state directory: %v", err)
	}
	gate, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the gate: %v", err)
	}
	if err := filelock.Lock(gate, true, false); err != nil {
		t.Fatalf("hold the gate: %v", err)
	}
	t.Cleanup(func() {
		_ = filelock.Unlock(gate)
		_ = gate.Close()
	})
}

// TestTurnsKeepGoingWhileTheBeliefFilesLockIsHeld is #264's acceptance, at the
// door the incident happened at.
//
// The rig's scripted ledger is put back so that the REAL one is what folds the
// answers in — the belief file, its journal and its lock, exactly as a shipped
// binary writes them — and then the lock is taken away. Two turns, because the
// shape that was measured was a first request that went out and a second that
// never did.
func TestTurnsKeepGoingWhileTheBeliefFilesLockIsHeld(t *testing.T) {
	rig := newLaneRig(t, "locked/belief-file",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	// The real ledger, over the real store, under the rig's temporary home.
	lanes.Default().SetLedger(nil)
	heldBeliefFile(t)

	for turn := 1; turn <= 2; turn++ {
		done := make(chan error, 1)
		go func() {
			_, err := rig.client.CompleteWithMessages(talking(), userMessages("hello"))
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("turn %d: %v", turn, err)
			}
		case <-time.After(wireBound):
			t.Fatalf("turn %d did not come back in %s while the belief file's lock was held elsewhere", turn, wireBound)
		}
	}
	// AND THE OBSERVATIONS ARE NOT LOST. The state file is the half a held lock
	// costs; the journal beside it is written with a lock nobody waits on, and
	// it is where both turns' sightings are.
	journal := filepath.Join(filepath.Dir(lanes.StorePath()), "lanes.log")
	written, err := os.ReadFile(journal)
	if err != nil || len(written) == 0 {
		t.Fatalf("the journal holds nothing (%v); the turns' sightings were dropped rather than deferred", err)
	}
}

// stuckLedger is a ledger whose write never comes back, which is what an
// uninterruptible file lock looked like from the arm goroutine's side.
type stuckLedger struct {
	scriptedLedger
	entered sync.Once
	inside  chan struct{}
	release chan struct{}
}

func (l *stuckLedger) Note(sighting lanes.Sighting) {
	l.entered.Do(func() { close(l.inside) })
	<-l.release
	l.scriptedLedger.Note(sighting)
}

// TestACancelledRaceEndsEvenWhenAnArmCannotReport is why the race's loop grew a
// context case.
//
// An arm folds its answer into the belief BEFORE it reports back to the race
// (client.go's noteVelocity), so an arm stuck in that write never posts a
// result — and a loop waiting only on arms is a loop nothing can leave. That is
// how Escape left a turn in `stopping` for minutes in the reported run. The
// write itself is fixed in `internal/lane`; this is the second door, which is
// that a cancelled context ends the race whatever the arms are doing.
func TestACancelledRaceEndsEvenWhenAnArmCannotReport(t *testing.T) {
	rig := newLaneRig(t, "cancelled/stuck-arm",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	stuck := &stuckLedger{
		scriptedLedger: scriptedLedger{beliefs: map[lanes.ID]lanes.Belief{}},
		inside:         make(chan struct{}),
		release:        make(chan struct{}),
	}
	lanes.Default().SetLedger(stuck)
	// The arm is let go at the end whatever happened, so the suite never leaves
	// a goroutine parked on a channel nobody closes.
	t.Cleanup(func() { close(stuck.release) })

	ctx, stop := context.WithCancel(talking())
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))
	done := make(chan error, 1)
	go func() {
		_, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
		done <- err
	}()

	select {
	case <-stuck.inside:
	case <-time.After(wireBound):
		t.Fatal("the arm never reached the belief write, so this test proved nothing")
	}
	stop()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled turn came back with an answer")
		}
	case <-time.After(wireBound):
		t.Fatalf("the cancelled turn did not end in %s", wireBound)
	}
}
