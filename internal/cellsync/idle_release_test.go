package cellsync

import (
	"errors"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestIdleRelease_C1_C3_ReleaseStopsRenewalAndReacquires(t *testing.T) {
	r := newRig(t)
	head := r.publishFirst(map[string]string{"journal": "saved answer"})
	b := r.batcher()
	released, err := b.ReleaseIdle(t.Context(), head, func() error { return nil })
	if err != nil || !released {
		t.Fatalf("C1 release: %v, %v", released, err)
	}
	b.beat(t.Context())
	v, err := r.dir.For(devA).Cell(t.Context(), cellID)
	if err != nil || v.Cell.Lease.Expires != 0 || r.dirA.beats.Load() != 0 {
		t.Fatalf("C1 released reader renewed: %+v, %v", v, err)
	}
	if _, _, held := b.RenewalStatus(); held {
		t.Fatal("C1 released reader still reports a hold")
	}
	ok, err := b.Reacquire(t.Context(), v)
	if err != nil || !ok {
		t.Fatalf("C3 take-back: %v, %v", ok, err)
	}
	note(b, r.seal(map[string]string{"journal": "new work"}))
	b.flush(t.Context())
	got := r.head(cellID)
	if got.Lease.Fence != v.Cell.Lease.Fence+1 || got.Head != r.drv.Head || b.Pending() != 0 {
		t.Fatalf("C3 work did not publish under the new fence: %+v", got)
	}
}

func TestIdleRelease_C2_C5_UnsentTurnsOrFailedVaultKeepHold(t *testing.T) {
	for _, failure := range []string{"unsent turns", "older head", "vault upload"} {
		t.Run(failure, func(t *testing.T) {
			r := newRig(t)
			head := r.publishFirst(map[string]string{"journal": "saved"})
			b := r.batcher()
			before := r.head(cellID)
			if failure == "unsent turns" {
				note(b, r.seal(map[string]string{"journal": "not uploaded"}))
			}
			if failure == "older head" {
				head = "older"
			}
			released, _ := b.ReleaseIdle(t.Context(), head, func() error {
				if failure == "vault upload" {
					return errors.New("vault upload failed")
				}
				return nil
			})
			if released || r.head(cellID).Lease != before.Lease {
				t.Fatal("C2/C5 incomplete transfer released the hold")
			}
			b.beat(t.Context())
			if r.dirA.beats.Load() != 1 {
				t.Fatal("C2/C5 hold no longer renews")
			}
		})
	}
}

func TestIdleRelease_C12_SendAndOpenHaveOneFencedWinner(t *testing.T) {
	r := newRig(t)
	head := r.publishFirst(map[string]string{"journal": "last answer"})
	b := r.batcher()
	if released, err := b.ReleaseIdle(t.Context(), head, func() error { return nil }); err != nil || !released {
		t.Fatalf("release: %v, %v", released, err)
	}
	seen, err := r.dir.For(devB).Cell(t.Context(), cellID)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var sent, opened bool
	go func() {
		defer wg.Done()
		<-start
		sent, _ = b.Reacquire(t.Context(), seen)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := r.dir.For(devB).Acquire(t.Context(), cellID, directory.AcquireOpts{
			RequireConditional: true,
			Expect:             &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence},
		})
		opened = err == nil
	}()
	close(start)
	wg.Wait()
	if sent == opened {
		t.Fatalf("C12 send won=%v, open won=%v", sent, opened)
	}
	got := r.head(cellID)
	if got.Head != head || got.Lease.Fence != seen.Cell.Lease.Fence+1 {
		t.Fatalf("C12 race changed turns or split the fence: %+v", got)
	}
}
