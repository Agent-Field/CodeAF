package run

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"sync"
	"testing"
)

func TestTaskDeleteWorkerLifetimeReservesBeforeFactoryAndKeepsCancelledLaunchAbsent(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.db", "project", "root", "root", "brief")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entered, release, ended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	active := false
	s := NewSupervisor(store, t.TempDir(), 1, Limits{}, func(plandb.Task) Worker {
		close(entered)
		<-release
		return workerFunc(func(context.Context, plandb.Task) (Report, error) { return Report{}, nil })
	})
	s.onWorker = func(_ string, on bool) { mu.Lock(); active = on; mu.Unlock() }
	go func() { defer close(ended); s.launch(context.Background(), *store.Task("root"), ""); s.drain() }()
	<-entered
	mu.Lock()
	reserved := active
	mu.Unlock()
	if !reserved {
		t.Fatal("factory ran without a deletion lifetime reservation")
	}
	if err = store.StopRoot("deleted"); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-ended
	mu.Lock()
	reserved = active
	mu.Unlock()
	if reserved {
		t.Fatal("worker lifetime did not settle")
	}
	// A later stale launch cannot create records or call its factory at all.
	called := false
	s.factory = func(plandb.Task) Worker { called = true; return nil }
	s.launch(context.Background(), plandb.Task{TaskSpec: plandb.TaskSpec{ID: "root"}}, "")
	if called {
		t.Fatal("cancelled task reached factory after deletion")
	}
}
