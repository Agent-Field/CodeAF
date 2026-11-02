package handoff

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// TestC7ConcurrentForcedTakesHaveOneWinner proves C7: moving a chat preserves its history and its single holder.
func TestC7ConcurrentForcedTakesHaveOneWinner(t *testing.T) {
	w := newWorld(t)
	a, b, c := w.device(devA), w.device(devB), w.device("dev_c")
	a.start(map[string]string{"a.txt": "one"})
	cWon := false
	b.onFetc = func() {
		if cWon {
			return
		}
		cWon = true
		taken, err := c.taker().Take(context.Background(), chatID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("C successfully took active A at fence %d", taken.Driving.Fence)
	}
	taken, err := b.taker().Take(context.Background(), chatID)
	if !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("B must lose after C wins; B got fence=%d error=%v, final holder=%s", taken.Driving.Fence, err, w.cellRec(chatID).Lease.Device)
	}
}

// TestC7CardCannotDisplaceAHolderThatArrivedBeforeConfirmation proves C7:
// consent to move A's chat does not authorize displacing C's later lease.
func TestC7CardCannotDisplaceAHolderThatArrivedBeforeConfirmation(t *testing.T) {
	w := newWorld(t)
	a, b, c := w.device(devA), w.device(devB), w.device("dev_c")
	a.start(map[string]string{"a.txt": "one"})
	old := w.cellRec(chatID).Lease
	if _, err := c.taker().Take(context.Background(), chatID); err != nil {
		t.Fatal(err)
	}
	_, err := b.taker().TakeSeen(context.Background(), chatID, &directory.LeaseExpectation{Device: old.Device, Fence: old.Fence})
	if !errors.Is(err, directory.ErrLeaseHeld) || w.cellRec(chatID).Lease.Device != "dev_c" {
		t.Fatalf("stale card changed holder: %v, %+v", err, w.cellRec(chatID).Lease)
	}
}

func TestTakeF2SameHolderSameHeadFenceChange(t *testing.T) {
	for _, change := range []string{"fence", "head", "holder"} {
		t.Run(change, func(t *testing.T) {
			w := newWorld(t)
			a, b := w.device(devA), w.device(devB)
			a.start(map[string]string{"a.txt": "one"})
			old := w.cellRec(chatID)
			seen := &directory.LeaseExpectation{Device: old.Lease.Device, Fence: old.Lease.Fence, Head: old.Head}
			switch change {
			case "fence":
				_, err := a.dir.Acquire(context.Background(), chatID, directory.AcquireOpts{})
				if err != nil {
					t.Fatal(err)
				}
			case "head":
				a.work(map[string]string{"a.txt": "two"})
			case "holder":
				if _, err := w.device("dev_c").taker().Take(context.Background(), chatID); err != nil {
					t.Fatal(err)
				}
			}
			_, err := b.taker().TakeSeen(context.Background(), chatID, seen)
			if change == "fence" && err != nil {
				t.Fatalf("F2 harmless fence bump refused: %v", err)
			}
			if change != "fence" && !errors.Is(err, directory.ErrLeaseHeld) {
				t.Fatalf("F2 %s must conflict: %v", change, err)
			}
		})
	}
}
