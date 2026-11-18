package cell

import (
	"errors"
	"os"
	"testing"
	"time"
)

// #2781: AN OPEN MUST BE ABLE TO TELL A FAILURE NOBODY WILL RETRY FROM AN ARRIVAL
// THAT IS STILL ON ITS WAY, WITHOUT WAITING FOR EITHER. [AbandonedArrival] is the
// look: it names only the failure that will stay refused unless somebody starts
// the fetch again, and it never blocks behind a live owner the way
// [WaitWorkspace] does.

func abandonedWithin(t *testing.T, cellRoot, workDir string) (time.Duration, error) {
	t.Helper()
	start := time.Now()
	err := AbandonedArrival(cellRoot, workDir)
	return time.Since(start), err
}

func TestAbandonedArrivalNamesAFailureWhoseOwnerIsGone(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-probe-abandoned")
	const reason = "the files could not be fetched"
	owner := startArrivalOwner(t, cellRoot)
	owner.say(t, "fail "+reason)
	owner.leave(t)

	for _, when := range []string{"just after the owner left", "asked again"} {
		took, err := abandonedWithin(t, cellRoot, workDir)
		if !errors.Is(err, ErrWorkspaceArrivalAbandoned) {
			t.Fatalf("%s: %v; want the abandoned-arrival refusal", when, err)
		}
		if got, ok := AbandonedReason(err); !ok || got != reason {
			t.Fatalf("%s: reason = %q, %v; want %q", when, got, ok, reason)
		}
		if took > time.Second {
			t.Fatalf("%s: the look took %s; it must never wait", when, took)
		}
		if _, statErr := os.Stat(marker); statErr != nil {
			t.Fatalf("%s: looking must leave the failure on disk: %v", when, statErr)
		}
	}
	// A failure is as abandoned in ten minutes as it is now: the files have not
	// arrived, and nothing about the clock changes that.
	backdate(t, marker, 10*time.Minute)
	if _, err := abandonedWithin(t, cellRoot, workDir); !errors.Is(err, ErrWorkspaceArrivalAbandoned) {
		t.Fatalf("ten minutes later: %v; want the abandoned-arrival refusal", err)
	}
}

func TestAbandonedArrivalNamesAnArrivalThisProcessGaveUp(t *testing.T) {
	cellRoot, workDir, _ := arrivalCell(t, "cell-probe-gave-up")
	a := BeginWorkspaceArrival(cellRoot, workDir, nil, nil)
	if err := AbandonedArrival(cellRoot, workDir); err != nil {
		t.Fatalf("a running arrival of this process = %v; want nothing", err)
	}
	a.Abandon("the restart could not go on")
	err := AbandonedArrival(cellRoot, workDir)
	if got, ok := AbandonedReason(err); !errors.Is(err, ErrWorkspaceArrivalAbandoned) || !ok || got != "the restart could not go on" {
		t.Fatalf("after Abandon = %v (reason %q); want the abandoned-arrival refusal carrying its reason", err, got)
	}
}

// NOTHING THAT A LIVE OWNER CAN STILL FINISH IS NAMED, in any process: starting a
// fetch over it would race the first, and waiting for it is what the open must
// not do.
func TestAbandonedArrivalNamesNothingAnOwnerCanStillFinish(t *testing.T) {
	t.Run("no arrival at all", func(t *testing.T) {
		cellRoot, workDir, _ := arrivalCell(t, "cell-probe-none")
		if err := AbandonedArrival(cellRoot, workDir); err != nil {
			t.Fatalf("a chat with no arrival = %v; want nothing", err)
		}
		if err := AbandonedArrival(t.TempDir()); err != nil {
			t.Fatalf("a folder that is no chat's = %v; want nothing", err)
		}
	})
	t.Run("running, owner in another process", func(t *testing.T) {
		cellRoot, workDir, marker := arrivalCell(t, "cell-probe-running")
		startArrivalOwner(t, cellRoot)
		took, err := abandonedWithin(t, cellRoot, workDir)
		if err != nil || took > time.Second {
			t.Fatalf("a running arrival with a live owner = %v after %s; want nothing, at once", err, took)
		}
		if _, statErr := os.Stat(marker); statErr != nil {
			t.Fatalf("looking must not remove a live owner's marker: %v", statErr)
		}
	})
	t.Run("failed, owner in another process still there to retry", func(t *testing.T) {
		cellRoot, workDir, _ := arrivalCell(t, "cell-probe-failed-alive")
		owner := startArrivalOwner(t, cellRoot)
		owner.say(t, "fail the fetch failed")
		if _, err := abandonedWithin(t, cellRoot, workDir); err != nil {
			t.Fatalf("a failure whose owner can still retry it = %v; want nothing", err)
		}
	})
	t.Run("running, owner in this process", func(t *testing.T) {
		cellRoot, workDir, _ := arrivalCell(t, "cell-probe-mine")
		a := BeginWorkspaceArrival(cellRoot, workDir, nil, nil)
		t.Cleanup(a.Complete)
		if err := AbandonedArrival(cellRoot, workDir); err != nil {
			t.Fatalf("a running arrival of this process = %v; want nothing", err)
		}
		a.Fail(errors.New("the fetch failed"))
		if err := AbandonedArrival(cellRoot, workDir); err != nil {
			t.Fatalf("a failure of this process that it can retry = %v; want nothing", err)
		}
	})
	t.Run("running, owner gone: its marker is cleared, as the gate has always done", func(t *testing.T) {
		cellRoot, workDir, marker := arrivalCell(t, "cell-probe-crashed")
		owner := startArrivalOwner(t, cellRoot)
		owner.leave(t)
		if err := AbandonedArrival(cellRoot, workDir); err != nil {
			t.Fatalf("a crashed running arrival = %v; want nothing (the gate removes it)", err)
		}
		if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
			t.Fatalf("a dead owner's running marker must be removed; stat err = %v", statErr)
		}
	})
}

// ONCE A NEW ARRIVAL REPLACES THE FAILURE AND ITS FILES LAND, NOTHING IS LEFT TO
// NAME.
func TestAbandonedArrivalIsClearedByANewArrivalThatLands(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-probe-replaced")
	first := startArrivalOwner(t, cellRoot)
	first.say(t, "fail the first fetch failed")
	first.leave(t)
	if _, err := abandonedWithin(t, cellRoot, workDir); !errors.Is(err, ErrWorkspaceArrivalAbandoned) {
		t.Fatalf("before the restart: %v; want the abandoned-arrival refusal", err)
	}

	second := startArrivalOwner(t, cellRoot)
	if err := AbandonedArrival(cellRoot, workDir); err != nil {
		t.Fatalf("while the new arrival runs = %v; want nothing", err)
	}
	second.say(t, "complete")
	if err := AbandonedArrival(cellRoot, workDir); err != nil {
		t.Fatalf("after the files landed = %v; want nothing", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("a completed arrival leaves no marker; stat err = %v", statErr)
	}
}
