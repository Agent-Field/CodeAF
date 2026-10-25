package lease_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestUnchangedIdlePullDoesNotDecodeFinishedItems(t *testing.T) {
	f := setup(t)
	for range 200 {
		f.add(t, factory.Item{Title: "finished", State: factory.StateShipped})
	}
	allocs := testing.AllocsPerRun(3, func() {
		if _, err := f.desk.Pull(worker("idle", "sam")); !errors.Is(err, lease.ErrNoJob) {
			t.Fatalf("empty queue: %v", err)
		}
	})
	// The budget allows filesystem metadata reads, but not decoding two
	// hundred unchanged job documents on every empty pull.
	if allocs > 2000 {
		t.Fatalf("unchanged idle pull allocated %.0f objects; want at most 2000", allocs)
	}
	t.Logf("unchanged idle pull: %.0f allocations", allocs)
}

func TestIdlePullSeesAnotherStoresEnqueue(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Title: "finished", State: factory.StateShipped, Assignee: "sam"})
	if _, err := f.desk.Pull(worker("idle", "sam")); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("empty queue: %v", err)
	}
	other, err := store.Open(f.store.Root())
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Update(it.ID, func(it *factory.Item) error {
		it.State = factory.StateQueued
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	held, err := f.desk.Pull(worker("idle", "sam"))
	if err != nil || held.Job != it.ID {
		t.Fatalf("pull after external enqueue: job %d, %v; want %d", held.Job, err, it.ID)
	}
}

func TestIdlePullRefreshesRepositoryIdentityAfterMetadataChanges(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Title: "queued", Repo: "api"})
	w := worker("idle", "sam")
	w.TakesUnassigned = map[string]bool{"acme/api": true}
	if _, err := f.desk.Pull(w); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("unresolved repository: %v", err)
	}
	if err := f.store.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	held, err := f.desk.Pull(w)
	if err != nil || held.Job != it.ID {
		t.Fatalf("pull after repository metadata changed: job %d, %v; want %d", held.Job, err, it.ID)
	}
}

func TestConcurrentIdlePullsShareADesk(t *testing.T) {
	f := setup(t)
	f.add(t, factory.Item{Title: "finished", State: factory.StateShipped})
	start := make(chan struct{})
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			<-start
			if _, err := f.desk.Pull(worker("idle", "sam")); !errors.Is(err, lease.ErrNoJob) {
				t.Errorf("empty queue: %v", err)
			}
		})
	}
	close(start)
	group.Wait()
}
