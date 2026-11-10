package handoff

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestIdleOpen_C6_C8_ReleasedAndLapsedChatNeedsOnlyRelay(t *testing.T) {
	for _, released := range []bool{true, false} {
		t.Run(map[bool]string{true: "released", false: "lapsed"}[released], func(t *testing.T) {
			w := newWorld(t)
			a, b := w.device(devA), w.device(devB)
			drive := a.start(map[string]string{"journal": "final answer", "work/file": "saved"})
			if released {
				a.release(drive)
			} else {
				w.clock.Advance(directory.LeaseTTL + time.Second)
			}
			// A has no live drive or server in this fixture. B can only read
			// the relay's immutable objects and acquire its directory hold.
			taker := b.taker()
			taker.FreeOnly = true
			if !released {
				if _, err := taker.Take(t.Context(), chatID); !errors.Is(err, directory.ErrLeaseHeld) {
					t.Fatalf("lapsed hold bypassed Continue here: %v", err)
				}
				// The confirmed continuation still needs only the relay.
				taker.FreeOnly = false
			}
			got, err := taker.Take(t.Context(), chatID)
			if err != nil {
				t.Fatal(err)
			}
			files := tree(t, got.Cell.Root)
			if files["journal"] != "final answer" || files["work/file"] != "saved" || w.cellRec(chatID).Lease.Device != devB {
				t.Fatalf("C6/C8 incoming state: %v", files)
			}
		})
	}
}

func TestIdleOpen_C7_C11_LiveOldHolderIsNeverTaken(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	a.start(map[string]string{"journal": "old build still open"})
	before := w.cellRec(chatID)
	taker := b.taker()
	taker.FreeOnly = true
	if _, err := taker.Take(t.Context(), chatID); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("C7/C11 live holder: %v", err)
	}
	if got := w.cellRec(chatID); got.Lease != before.Lease || got.Head != before.Head {
		t.Fatal("C7/C11 open changed the old holder")
	}
}

func TestIdleOpen_C12_HolderRetakingDuringFetchWins(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	drive := a.start(map[string]string{"journal": "saved"})
	a.release(drive)
	before := w.cellRec(chatID)
	b.onFetc = func() {
		_, err := a.dir.Acquire(t.Context(), chatID, directory.AcquireOpts{RequireConditional: true, Expect: &directory.LeaseExpectation{Device: devA, Fence: drive.Fence}})
		if err != nil {
			t.Fatal(err)
		}
	}
	taker := b.taker()
	taker.FreeOnly = true
	seen := &directory.LeaseExpectation{Device: before.Lease.Device, Fence: before.Lease.Fence}
	if _, err := taker.TakeSeen(t.Context(), chatID, seen); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("C12 renewed holder was forced: %v", err)
	}
	if got := w.cellRec(chatID); got.Lease.Device != devA || got.Lease.Fence != before.Lease.Fence+1 || got.Head != before.Head {
		t.Fatalf("C12 renewed ownership changed: %+v", got)
	}
}
