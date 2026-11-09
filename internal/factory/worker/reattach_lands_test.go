package worker_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

// A RUN'S OWN STEPS ARE NEVER THE PIPELINE'S. Jobs 72 and 74 of the fourth
// live run: each was mid-way through its one AgentField step when the engine
// restarted, and the runner that took it back followed the same run to its
// end. But the job it pulled carried the run's own calls folded under the
// step (plan, …, execute, …), and the runner read them back as steps of its
// own pipeline. When the run ended the runner went on to the run's next call,
// reported that as a step of the job, and the factory refused the whole
// report ("report names an unknown step"): the run had opened its pull
// request, and the job never landed on it.
//
// A job re-taken mid-run reports only the step that owns the run, and lands on
// the pull request its run opened.
func TestAJobReTakenMidRunLandsOnItsRunsPullRequest(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := serve.New(factory.LocalSeam(st, time.Now()), serve.Config{Mode: api.FactoryTeam, Store: st, PullWait: time.Millisecond})
	s := httptest.NewServer(h)
	t.Cleanup(func() { s.Close(); h.Close() })
	// The run's own calls, as the runner that followed it before the restart
	// wrote them under the step: a parent call and its children.
	remote := []factory.Stage{
		{Name: "outline", Kind: factory.StageCheck, On: true},
		{Name: "draft", Parent: "outline", Kind: factory.StageChat, On: true},
		{Name: "carryout", Kind: factory.StageCheck, On: true},
		{Name: "coder", Parent: "carryout", Kind: factory.StageChat, On: true},
	}
	remotePhases := []factory.Phase{
		{Name: "outline", Kind: factory.StageCheck, State: factory.PhaseDone},
		{Name: "draft", Kind: factory.StageChat, State: factory.PhaseDone},
		{Name: "carryout", Kind: factory.StageCheck, State: factory.PhaseRunning},
		{Name: "coder", Kind: factory.StageChat, State: factory.PhaseRunning},
	}
	it, err := st.Create(factory.Item{Title: "Add Debits", State: factory.StateRunning, Assignee: "sam", WorktreeMachine: "studio", Kind: factory.KindIssue,
		Stages: []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "planner.build", On: true}},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {Target: "planner.build", ExecutionID: "exec-root", RunID: "run-1", Status: "running",
			Stages: remote, Phases: remotePhases}},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "build", Kind: factory.StageAgentField, State: factory.PhaseRunning, Round: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	followed := make(chan []string, 4)
	// The step's executor follows the same run to its end, as the
	// AgentField step does: the run's end and the pull request it opened
	// are written on the runner's copy of the job.
	follow := run.ExecutorFunc(func(_ context.Context, job run.Job) (factory.StageResult, error) {
		cur, _ := mirror.Get(job.Item.ID)
		names := []string{}
		for _, ph := range cur.Stream.Phases {
			names = append(names, ph.Name)
		}
		followed <- names
		if err := mirror.Update(job.Item.ID, func(it *factory.Item) error {
			r := it.AgentFieldSteps["build"]
			r.Status, r.Ended = "succeeded", time.Now()
			it.PullRequest = &factory.PullRequest{Number: 230, URL: "https://github.com/o/r/pull/230"}
			return nil
		}); err != nil {
			return factory.StageResult{}, err
		}
		return factory.StageResult{Done: true, Output: "the AgentField run planner.build finished and opened https://github.com/o/r/pull/230"}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w := client.Open(s.URL, "").Worker("")
	opts := worker.Options{Slots: 1,
		Dial:   func(int) *client.WorkerClient { return w },
		Attach: api.WorkerAttach{Name: "studio", Machine: "studio", Handle: "sam"},
		Mirror: mirror, Runner: run.New(run.Options{Store: mirror, RepoDir: func(string) string { return mirror.Root() },
			Exec: map[factory.StageKind]run.Executor{factory.StageAgentField: follow}}),
		ReportEvery: time.Millisecond, BeatEvery: 5 * time.Millisecond,
	}
	go func() { defer close(done); worker.Run(ctx, opts) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case names := <-followed:
		if strings.Join(names, ",") != "build" {
			t.Fatalf("the runner took the run's own calls for its pipeline's steps: %v", names)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the re-taken job's run was never followed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := st.Get(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == factory.StateShipped {
			if got.PullRequest == nil || got.PullRequest.Number != 230 {
				t.Fatalf("the job did not land on its run's pull request: %+v", got.PullRequest)
			}
			if got.Stages[0].Name != "build" || len(got.Stages) != 1 || len(got.Stream.Phases) != 1 {
				t.Fatalf("the run's calls became the job's steps: %+v", got.Stream.Phases)
			}
			return
		}
		if got.QKind == "results_not_saved" || strings.Contains(got.Question, "unknown step") {
			t.Fatalf("the factory refused the re-taken runner's report: %q", got.Question)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job never landed on its run's pull request: %s %q %+v", got.State, got.Question, got.Stream.Phases)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
