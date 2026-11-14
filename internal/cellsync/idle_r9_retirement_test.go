package cellsync

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

func TestIdleRelease_R9_RetiredFenceDrainsSealsWithoutRenewalBeats(t *testing.T) {
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
			sealed := r.seal(map[string]string{"journal": "finished after retirement"})
			note(b, sealed)
			err := b.drain(t.Context())
			if err != nil || b.Pending() != 0 {
				t.Fatalf("retired writer stranded a sealed turn: error=%v pending=%d", err, b.Pending())
			}
			after := r.head(cellID)
			if takeover {
				if b.Driving.ID() == cellID || r.head(b.Driving.ID()).Head != sealed || after.Head != before.Head || after.Lease != before.Lease {
					t.Fatal("retired takeover lost its branch or changed the winner")
				}
			} else if after.Head != sealed || after.Lease.Fence != before.Lease.Fence || after.Lease.Expires <= before.Lease.Expires {
				t.Fatal("late publish did not renew the unchanged expired fence")
			}
			b.beat(t.Context())
			if r.dirA.beats.Load() != beats {
				t.Fatal("draining a late seal restored renewal beats")
			}
			if _, _, holding := b.RenewalStatus(); holding {
				t.Fatal("draining a late seal restored the retired hold")
			}
		})
	}
}

func TestIdleRelease_R9_RetiredCloseDrainsSealedWork(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "none", true: "some"}[pending], func(t *testing.T) {
			r := newRig(t)
			r.publishFirst(map[string]string{"journal": "saved"})
			b := r.batcher()
			b.RetireFence(cellID, 1)
			before := r.head(cellID)
			sealed := before.Head
			if pending {
				sealed = r.seal(map[string]string{"journal": "pending"})
				note(b, sealed)
			}
			err := b.Close(t.Context())
			if err != nil || b.Pending() != 0 {
				t.Fatalf("retired close did not drain its sealed work: error=%v pending=%d", err, b.Pending())
			}
			if after := r.head(cellID); after.Head != sealed || after.Lease.Fence != before.Lease.Fence {
				t.Fatal("retired close lost its head or changed its fence")
			}
		})
	}
}

func TestIdleRelease_R9_RetiredCloseRetainsSealsDuringOutage(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"journal": "saved"})
	b := r.batcher()
	b.RetireFence(cellID, 1)
	note(b, r.seal(map[string]string{"journal": "pending"}))
	r.store.set(directory.ErrUnreachable, nil)
	if err := b.Close(t.Context()); !errors.Is(err, directory.ErrUnreachable) || !syncwords.UploadPending(err) || b.Pending() != 1 {
		t.Fatalf("unreachable close must retain the pending seal: error=%v pending=%d", err, b.Pending())
	}
	r.store.set(nil, nil)
	if err := b.Close(t.Context()); err != nil || b.Pending() != 0 {
		t.Fatalf("recovered close did not drain the pending seal: error=%v pending=%d", err, b.Pending())
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
