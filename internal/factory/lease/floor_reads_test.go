package lease_test

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestIdleFloorReadsOnlyChangedJobs(t *testing.T) {
	for _, route := range []string{"Pull", "OverLimit"} {
		t.Run(route, func(t *testing.T) {
			f := setup(t)
			for range 300 {
				f.add(t, factory.Item{State: factory.StateNew})
			}
			look := func() {
				t.Helper()
				if route == "Pull" {
					if _, err := f.desk.Pull(worker("idle", "sam")); !errors.Is(err, lease.ErrNoJob) {
						t.Fatalf("Pull: %v", err)
					}
				} else if n, err := f.desk.OverLimit(worker("idle", "sam")); err != nil || n != 0 {
					t.Fatalf("OverLimit: %d, %v", n, err)
				}
			}
			look()
			before := f.store.DocumentReads()
			for range 3 {
				look()
			}
			if n := f.store.DocumentReads() - before; n != 0 {
				t.Fatalf("unchanged 300-job floor: read %d documents, want 0", n)
			}
			other, err := store.Open(f.store.Root())
			if err != nil {
				t.Fatal(err)
			}
			if err := other.Update(150, func(it *factory.Item) error { it.Title = "changed elsewhere"; return nil }); err != nil {
				t.Fatal(err)
			}
			before = f.store.DocumentReads()
			look()
			if n := f.store.DocumentReads() - before; n != 1 {
				t.Fatalf("one changed job: read %d documents, want 1", n)
			}
		})
	}
}
