package cellsync

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestReacquireReleasedLeaseS5(t *testing.T) {
	for _, scenario := range []string{"released", "new winner", "new head", "pending work"} {
		t.Run(scenario, func(t *testing.T) {
			r := newRig(t)
			r.publishFirst(map[string]string{"file": "saved"})
			b := r.batcher()
			r.takeOver()
			other := r.dir.For(devB)
			v, err := other.Cell(t.Context(), cellID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "new head" {
				r.bPublishes(v.Cell.Head)
			}
			if err = other.Release(t.Context(), cellID, v.Cell.Lease.Fence); err != nil {
				t.Fatal(err)
			}
			seen, err := other.Cell(t.Context(), cellID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "new winner" {
				if _, err = other.Acquire(t.Context(), cellID, directory.AcquireOpts{}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "pending work" {
				note(b, r.seal(map[string]string{"file": "not yet published"}))
			}
			resumed, err := b.Reacquire(t.Context(), seen)
			if scenario != "released" {
				if resumed {
					t.Fatal("S5 resumed stale, raced or unpublished work")
				}
				return
			}
			if err != nil || !resumed {
				t.Fatalf("S5 resume: %v, %v", resumed, err)
			}
			got := r.head(cellID)
			if got.Lease.Device != devA || got.Lease.Fence != seen.Cell.Lease.Fence+1 {
				t.Fatalf("S5 lease: %+v", got.Lease)
			}
			note(b, r.seal(map[string]string{"file": "after failed move"}))
			b.flush(t.Context())
			if got := r.head(cellID); got.Head != r.drv.Head || b.Pending() != 0 {
				t.Fatalf("S5 resumed publication: %+v", got)
			}
		})
	}
}
