package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// A RUN THAT ENDED SECONDS BEFORE A QUIT IS READ AFTER THE RESTART, NEVER RUN
// AGAIN (live e2e round 9, job 306, in Manual): the run ended, the engine
// closed 3 s later, the clean quit handed the hold back, and the restart
// started a second run by itself — which asked for plan approval again — while
// the first run's outcome was never shown. The step's start had moved past
// the run's (a round entered again after an earlier restart), so the round
// that took the job up disowned the run by its clock. The record now says
// the end is owed to the step, and the runner that takes the job reads it,
// whatever the clocks and whichever door handed the job back. Both endings:
// a run that opened its pull request, and one that failed with none.
func TestARunThatEndedJustBeforeAQuitIsReadAfterTheRestartNotRunAgain(t *testing.T) {
	endings := []struct {
		name   string
		result string
		notes  []agentfield.Note
		check  func(t *testing.T, res factory.StageResult, err error, end factory.Item)
	}{
		{
			name:   "opened a pull request",
			result: `{"success":true,"pr_url":"https://github.com/santoshkumarradha/factory-demo/pull/322","pr_number":322}`,
			check: func(t *testing.T, res factory.StageResult, err error, end factory.Item) {
				if err != nil || !res.Done || !strings.Contains(res.Output, "pull/322") {
					t.Fatalf("the ended run did not end its step with its pull request: %+v, %v", res, err)
				}
				if end.PullRequest == nil || end.PullRequest.Number != 322 {
					t.Fatalf("the job does not hold the run's pull request: %+v", end.PullRequest)
				}
			},
		},
		{
			name:   "failed with no pull request",
			result: `{"success":false,"summary":"Partial: 1/1 issues completed"}`,
			notes: []agentfield.Note{
				{Message: "Project build check failed: `go build ./...` exited 1", Timestamp: afTime("2026-11-08T15:24:08.454Z")},
				{Message: "No pull request: the build did not pass verification (Partial: 1/1 issues completed, verification: OVERALL: FAIL. The project does not build or its tests fail: `go build ./...` exited 1. 3 of 9 pass.)", Timestamp: afTime("2026-11-08T15:24:27.947Z")},
			},
			check: func(t *testing.T, res factory.StageResult, err error, end factory.Item) {
				if err == nil || res.Done {
					t.Fatalf("a failed run's end read as done: %+v, %v", res, err)
				}
				if !strings.Contains(res.Summary, "no pull request was opened") || !strings.Contains(res.Summary, "(go build ./... failed)") {
					t.Fatalf("the run's own outcome was not read: %q", res.Summary)
				}
				if end.PullRequest != nil {
					t.Fatalf("a pull request was invented: %+v", end.PullRequest)
				}
			},
		},
	}
	for _, ending := range endings {
		t.Run(ending.name, func(t *testing.T) {
			appDayProfile(t)
			prev := afRemoteEvery
			afRemoteEvery = 10 * time.Millisecond
			t.Cleanup(func() { afRemoteEvery = prev })
			st, it, stage := afStepJob(t)
			if err := st.Update(it.ID, func(it *factory.Item) error {
				it.Autonomy, it.Assignee, it.State = factory.AutonomyManual, "sam", factory.StateQueued
				it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "build", Kind: factory.StageAgentField, State: factory.PhaseRunning, Round: 1, Started: time.Now().Add(-time.Minute)}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			desk, err := lease.Open(st, lease.Options{})
			if err != nil {
				t.Fatal(err)
			}
			runner := lease.Worker{ID: "runner-1", Handle: "sam"}
			held, err := desk.Pull(runner)
			if err != nil {
				t.Fatal(err)
			}
			it, _ = st.Get(it.ID)
			// A step of the run whose record never closes holds the reading
			// after the run's end (its drain), as a node that has gone quiet
			// does, so the quit lands before the step takes that end.
			code := afNode("e-code", "coder", "succeeded", "2026-11-08T15:00:01Z", "2026-11-08T15:10:00Z")
			cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "running", "2026-11-08T15:00:00Z", "", code)}
			cp.relayLines("e-code", [][]byte{
				[]byte(`{"type":"session","version":1,"id":"nodecoder","cwd":"/node","model":"m","timestamp":"2026-11-08T15:00:01Z"}`),
				[]byte(`{"type":"message","role":"user","content":"add HasPositive","timestamp":"2026-11-08T15:00:01Z"}`),
			}, false)
			r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}

			// The first session starts the run and follows it.
			ctx, quit := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				_, err := r.Step(ctx, factoryrun.Job{Item: it, Stage: stage, Round: 1})
				done <- err
			}()
			afUntil(t, st, it.ID, "starting the run", func(i factory.Item) bool {
				run := i.AgentFieldSteps["build"]
				return run != nil && run.ExecutionID != ""
			})
			// The run ends on the control plane, and its end reaches the job.
			cp.mu.Lock()
			cp.tree = afNode("exec-root", "build_issue", "succeeded", "2026-11-08T15:00:00Z", "2026-11-08T15:11:12Z", code)
			cp.result = json.RawMessage(ending.result)
			cp.notes = map[string][]agentfield.Note{"exec-root": ending.notes}
			cp.mu.Unlock()
			afUntil(t, st, it.ID, "recording the run's end", func(i factory.Item) bool {
				run := i.AgentFieldSteps["build"]
				return run != nil && !run.Ended.IsZero()
			})
			// ⌘Q: the engine closes before the step takes that end.
			quit()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("the step did not let go when the engine closed")
			}
			if run := afRunOf(t, st, it.ID); !run.EndOwed {
				t.Fatalf("a run whose end the step never read is not owed to it: %+v", run)
			}
			// The clean quit hands the hold back: queued, with no failure.
			if err := desk.HandBack(held.ID, runner.ID); err != nil {
				t.Fatal(err)
			}
			back, _ := st.Get(it.ID)
			if back.State != factory.StateQueued || back.Failure != nil {
				t.Fatalf("the handed-back job is %s with failure %+v", back.State, back.Failure)
			}
			// The step's start has moved past the run's, as job 306's had.
			if err := st.Update(it.ID, func(it *factory.Item) error {
				it.Stream.Phases[0].Started = it.AgentFieldSteps["build"].Began.Add(15 * time.Minute)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			// By the restart the quiet step's record has closed.
			cp.mu.Lock()
			kept := cp.held["e-code"]
			cp.held["e-code"] = append(kept, agentfield.RelayEntry{Seq: 1000, Kind: "ended", Data: json.RawMessage(`{}`)})
			cp.mu.Unlock()
			// The restart's runner takes the job and runs its step's round.
			if _, err := desk.Pull(runner); err != nil {
				t.Fatalf("the restart could not take the job: %v", err)
			}
			again, _ := st.Get(it.ID)
			res, err := r.Step(context.Background(), factoryrun.Job{Item: again, Stage: stage, Round: 1})
			if n := len(cp.dispatched); n != 1 {
				t.Fatalf("the restart started %d more run(s) for a run that had ended", n-1)
			}
			end, _ := st.Get(it.ID)
			ending.check(t, res, err, end)
			if run := afRunOf(t, st, it.ID); run.EndOwed {
				t.Fatalf("the step read its run's end but it is still owed: %+v", run)
			}
			// Try again after the step read the end is a new start.
			if _, err := r.Step(context.Background(), factoryrun.Job{Item: end, Stage: stage, Round: 2}); err != nil && !strings.Contains(err.Error(), "did not succeed") {
				t.Fatalf("fresh round: %v", err)
			}
			if n := len(cp.dispatched); n != 2 {
				t.Fatalf("a fresh round after the end was read started %d runs in all, want 2", n)
			}
		})
	}
}

// A STOPPED RUN IS NEVER A LATER ROUND'S OUTCOME: a person's Reject or Stop
// ends the run on the control plane while the step's end is still owed, and
// Reopen begins the step afresh with a run of its own.
func TestAStoppedRunsOwedEndIsNotReadByAReopenedRound(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	began := afTime("2026-11-08T15:13:15Z")
	if err := st.Update(it.ID, func(it *factory.Item) error {
		it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "build", Kind: factory.StageAgentField, State: factory.PhaseRunning, Round: 1}}}
		it.AgentFieldSteps = map[string]*factory.AgentFieldRun{"build": {Target: "swe-planner.build_issue", ExecutionID: "exec-old", RunID: "run-old", Began: began, Ended: began.Add(18 * time.Minute), Status: "cancelled", EndOwed: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it, _ = st.Get(it.ID)
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "succeeded", "2026-11-08T16:00:00Z", "2026-11-08T16:01:00Z"), result: json.RawMessage(`{"success":true}`)}
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	if _, err := r.Step(context.Background(), factoryrun.Job{Item: it, Stage: stage, Round: 1}); err != nil {
		t.Fatal(err)
	}
	if len(cp.dispatched) != 1 {
		t.Fatalf("a reopened step read a stopped run's end instead of starting its own: %d dispatched", len(cp.dispatched))
	}
}

func afRunOf(t *testing.T, st interface {
	Get(int) (factory.Item, error)
}, id int) factory.AgentFieldRun {
	t.Helper()
	it, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	run := it.AgentFieldSteps["build"]
	if run == nil {
		t.Fatal("the step has no run on its record")
	}
	return *run
}
