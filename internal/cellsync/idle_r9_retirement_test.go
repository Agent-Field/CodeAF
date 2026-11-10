package cellsync

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

func TestIdleRelease_R9_RetiredFenceNeverRenewsOrPublishes(t *testing.T) {
	for _, takeover := range []bool{false, true} {
		t.Run(map[bool]string{false: "own expiry", true: "other computer acquired"}[takeover], func(t *testing.T) {
			r := newRig(t)
			r.publishFirst(map[string]string{"journal": "saved"})
			b := r.batcher()
			r.clock.Advance(directory.LeaseTTL + time.Second)
			if takeover {
				r.takeOver()
			}
			b.RetireFence(cellID, 1)
			before := r.head(cellID)
			beats := r.dirA.beats.Load()
			b.beat(t.Context())
			if after := r.head(cellID); r.dirA.beats.Load() != beats || after.Lease != before.Lease {
				t.Fatal("a retired writer sent a revival beat")
			}
			if _, _, holding := b.RenewalStatus(); holding {
				t.Fatal("retired writer claimed a live hold")
			}
			note(b, r.seal(map[string]string{"journal": "unsent"}))
			err := b.drain(t.Context())
			if takeover {
				if err != nil || b.Pending() != 0 || b.Driving.ID() == cellID {
					t.Fatalf("retired takeover did not keep its turns on a branch: error=%v pending=%d drive=%+v", err, b.Pending(), b.Driving)
				}
			} else if !errors.Is(err, cellstore.ErrReadOnly) || !syncwords.UploadPending(err) || b.Pending() != 1 {
				t.Fatalf("retired publication must wait for reopen: error=%v pending=%d", err, b.Pending())
			}
			if after := r.head(cellID); after.Head != before.Head || after.Lease != before.Lease {
				t.Fatal("old fence changed the published chat")
			}
		})
	}
}

func TestIdleRelease_R9_RetiredCloseReportsOnlyPendingWork(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "none", true: "some"}[pending], func(t *testing.T) {
			r := newRig(t)
			r.publishFirst(map[string]string{"journal": "saved"})
			b := r.batcher()
			b.RetireFence(cellID, 1)
			if pending {
				note(b, r.seal(map[string]string{"journal": "pending"}))
			}
			before := r.head(cellID)
			err := b.Close(t.Context())
			if pending {
				if !errors.Is(err, cellstore.ErrReadOnly) || !syncwords.UploadPending(err) || b.Pending() != 1 {
					t.Fatalf("pending save must remain waiting: error=%v pending=%d", err, b.Pending())
				}
			} else if err != nil {
				t.Fatalf("empty retired close reported a failed save: %v", err)
			}
			if after := r.head(cellID); after.Head != before.Head || after.Lease != before.Lease {
				t.Fatal("retired close changed its old fence")
			}
		})
	}
}

func TestIdleRelease_R9_LateRetirementCannotCloseANewFence(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"journal": "saved"})
	b := r.batcher()
	b.RetireFence(cellID, 1)
	seen, err := r.dirA.Cell(t.Context(), cellID)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := b.Reacquire(t.Context(), seen)
	if err != nil || !ok || b.Fence() != 2 {
		t.Fatalf("conditional acquire=%v, %v; fence=%d", ok, err, b.Fence())
	}
	b.RetireFence(cellID, 1)
	if _, _, holding := b.RenewalStatus(); !holding {
		t.Fatal("a delayed old-fence reading closed the new hold")
	}
}
