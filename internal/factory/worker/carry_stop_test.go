package worker_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

// A STOP THAT LANDED WITH NO RUNNER HOLDING THE JOB REACHES ITS RUN through
// the runner that next takes it: the job is held to talk only, the worker
// hands it to CarryStop, writes no step, lets go, and the job stays stopped.
func TestTheWorkerCarriesAStopToARunThatGoesOnElsewhere(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := serve.New(factory.LocalSeam(st, time.Now()), serve.Config{Mode: api.FactoryTeam, Store: st, PullWait: time.Millisecond})
	s := httptest.NewServer(h)
	t.Cleanup(func() { s.Close(); h.Close() })
	it, err := st.Create(factory.Item{Title: "Add Debits", State: factory.StateRunning, Assignee: "sam", WorktreeMachine: "studio",
		Stages:          []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "planner.build", On: true}},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {Target: "planner.build", ExecutionID: "exec-root", RunID: "run-1", Status: "running"}},
		Stream:          &factory.Stream{Phases: []factory.Phase{{Name: "build", Kind: factory.StageAgentField, State: factory.PhaseRunning, Round: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(desk).Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	carried := make(chan factory.Item, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w := client.Open(s.URL, "").Worker("")
	opts := worker.Options{Slots: 1,
		Dial:   func(int) *client.WorkerClient { return w },
		Attach: api.WorkerAttach{Name: "studio", Machine: "studio", Handle: "sam"},
		Mirror: mirror, Runner: run.New(run.Options{Store: mirror}),
		ReportEvery: time.Millisecond, BeatEvery: 5 * time.Millisecond,
		CarryStop: func(_ context.Context, got factory.Item) error {
			carried <- got
			// The carry writes the run's end on the factory's job.
			return st.Update(got.ID, func(it *factory.Item) error {
				r := it.AgentFieldSteps["build"]
				r.Status, r.Ended = "cancelled", time.Now()
				return nil
			})
		},
	}
	go func() { defer close(done); worker.Run(ctx, opts) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case got := <-carried:
		if got.ID != it.ID {
			t.Fatalf("carried job %d", got.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stop never reached the run: no runner carried it")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		live, err := desk.Live()
		if err != nil {
			t.Fatal(err)
		}
		if len(live) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the runner kept the stopped job after carrying its stop: %+v", live)
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _ := st.Get(it.ID)
	if !factory.StoppedByPerson(got) {
		t.Fatalf("carrying the stop undid it: %s", got.State)
	}
	select {
	case <-carried:
		t.Fatal("a stop already carried was carried again")
	case <-time.After(50 * time.Millisecond):
	}
}
