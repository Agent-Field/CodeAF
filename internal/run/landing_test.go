package run

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func landingStore(t *testing.T) *plandb.Store {
	t.Helper()
	s, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "landing", "root", "root", "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func finishedLeaf(t *testing.T, store *plandb.Store) plandb.Task {
	t.Helper()
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "leaf", Title: "leaf", ParentID: "root"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim("leaf", "leaf", "owner"); err != nil {
		t.Fatal(err)
	}
	leaf := *store.Task("leaf")
	if _, err := store.Done("leaf", "leaf", "finished", nil, nil); err != nil {
		t.Fatal(err)
	}
	return leaf
}

func TestCompleteTreeWaitsUntilAStoreFinishedLeafReturnIsAbsorbed(t *testing.T) {
	store := landingStore(t)
	leaf := finishedLeaf(t, store)
	s := NewSupervisor(store, t.TempDir(), 1, Limits{ReviewRound: true}, func(plandb.Task) Worker {
		return roadGWorker(func(context.Context, plandb.Task) (Report, error) { return Report{Result: "holds: reviewed"}, nil })
	})
	s.cancels[leaf.ID] = func() {}
	s.completeTree()
	if got := store.Task("root").Status; got == plandb.StatusDone {
		t.Fatal("root completed from a store row before the return landed")
	}
	delete(s.cancels, leaf.ID)
	s.absorb(workerReturn{task: leaf, report: Report{Result: "finished"}})
	if len(s.checkOf) != 1 {
		t.Fatalf("checks seated = %d, want 1", len(s.checkOf))
	}
}

func TestLaunchWakesWaitsUntilAStoreFinishedChildReturnIsAbsorbed(t *testing.T) {
	store := landingStore(t)
	leaf := finishedLeaf(t, store)
	s := NewSupervisor(store, t.TempDir(), 2, Limits{}, func(plandb.Task) Worker {
		return roadGWorker(func(context.Context, plandb.Task) (Report, error) { return Report{}, nil })
	})
	s.dispatchedRoot = true
	s.wakes = make(map[string]int)
	s.reported = make(map[string]map[string]bool)
	s.lastReport = make(map[string]string)
	s.cancels[leaf.ID] = func() {}
	s.launchWakes(context.Background(), "root")
	if s.inFlight != 0 {
		t.Fatalf("wake workers = %d, want 0 before landing", s.inFlight)
	}
	delete(s.cancels, leaf.ID)
	s.launchWakes(context.Background(), "root")
	if s.inFlight != 1 {
		t.Fatalf("wake workers = %d, want 1 after landing", s.inFlight)
	}
	ret := <-s.finished
	s.inFlight--
	s.absorb(ret)
	s.drain()
}

func TestRunWaitsForLateChildLandingBeforeAcceptingAStoredRootEnding(t *testing.T) {
	store := landingStore(t)
	leaf := finishedLeaf(t, store)
	const answer = "integrated answer"
	if _, err := store.Done("root", "root", answer, nil, nil); err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor(store, t.TempDir(), 1, Limits{ReviewRound: true}, func(plandb.Task) Worker {
		return roadGWorker(func(context.Context, plandb.Task) (Report, error) { return Report{Result: "holds: reviewed"}, nil })
	})
	s.dispatchedRoot = true
	s.wakes = make(map[string]int)
	s.reported = map[string]map[string]bool{"root": {"leaf": true}}
	s.lastReport = make(map[string]string)
	s.inFlight = 1
	s.cancels[leaf.ID] = func() {}
	if got := s.pass(context.Background(), "root"); got != "" {
		t.Fatalf("pass = %q before landing, want empty", got)
	}
	s.inFlight--
	s.absorb(workerReturn{task: leaf, report: Report{Result: "finished"}})
	if len(s.checkOf) != 1 {
		t.Fatalf("checks seated = %d, want 1", len(s.checkOf))
	}
	check := *store.Task(func() string {
		for id := range s.checkOf {
			return id
		}
		return ""
	}())
	if _, err := store.Claim(check.ID, check.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	s.absorb(workerReturn{task: check, report: Report{Result: "holds: reviewed"}})
	if got := store.Task("root"); got.Status != plandb.StatusDone || got.Result != answer {
		t.Fatalf("root = %s %q", got.Status, got.Result)
	}
}

func TestDrainStillEndsAnUnfinishedRemnant(t *testing.T) {
	store := landingStore(t)
	started := make(chan struct{})
	returned := make(chan struct{})
	s := NewSupervisor(store, t.TempDir(), 1, Limits{}, func(plandb.Task) Worker {
		return roadGWorker(func(ctx context.Context, _ plandb.Task) (Report, error) {
			close(started)
			<-ctx.Done()
			close(returned)
			return Report{}, ctx.Err()
		})
	})
	s.launch(context.Background(), *store.Task("root"), "")
	<-started
	s.drain()
	<-returned
}
