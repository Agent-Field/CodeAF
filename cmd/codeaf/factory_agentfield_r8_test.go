package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// A FAILED BUILD'S HEADLINE IS WHY IT FAILED, NEVER THE TIDY-UP AFTER IT
// (live e2e round 8, job 84): the reasoner wrote "Repo finalized: …" 8 ms
// after "No pull request: …", and the newest note used to become the job's
// headline. The note is chosen by what it says, and nothing here names the
// pipeline or its steps.
func TestAFailedBuildsHeadlineIsTheNoPullRequestNoteNotTheNewerTidyUp(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	report := "Partial: 1/1 issues completed, verification: OVERALL: FAIL. The project does not build or its tests fail: `go build ./...` exited 1."
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "succeeded", "2026-11-08T12:00:00Z", "2026-11-08T12:21:38Z"),
		result: json.RawMessage(`{"success":false,"summary":"the run did not finish its work"}`),
		notes: map[string][]agentfield.Note{"exec-root": {
			{Message: "Project build check failed: `go build ./...` exited 1", Tags: []string{"build", "verify", "project_check", "failed"}, Timestamp: afTime("2026-11-08T12:21:18.446Z")},
			{Message: "Phase 3b: Repo finalization", Tags: []string{"build", "finalize"}, Timestamp: afTime("2026-11-08T12:21:18.453Z")},
			{Message: "No pull request: the build did not pass verification (" + report + ")", Tags: []string{"build", "github_pr", "skipped"}, Timestamp: afTime("2026-11-08T12:21:37.464Z")},
			{Message: "Repo finalized: Surveyed the repository: it is a small Go project (go.mod, ledger.go…). Removed the two untracked directories this run created.", Tags: []string{"build", "finalize", "complete"}, Timestamp: afTime("2026-11-08T12:21:37.472Z")},
		}}}
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	res, err := r.Step(context.Background(), factoryrun.Job{Item: it, Stage: stage})
	// The failing command is named, never what it printed (live e2e round 9).
	const why = "The build did not pass its checks (go build ./... failed), so no pull request was opened"
	if err == nil || err.Error() != "Build with SWE-AF did not succeed: "+why {
		t.Fatalf("err = %v", err)
	}
	if res.Summary != why {
		t.Fatalf("headline = %q, want %q", res.Summary, why)
	}
	if strings.Contains(res.Summary, "finalized") || strings.Contains(err.Error(), "finalized") {
		t.Fatalf("the tidy-up note became the outcome: %q / %v", res.Summary, err)
	}
}

// A RUN THAT CHANGED NOTHING IS NOT SHIPPED (live e2e round 8, job 79): the
// work was already on main, the run ended cleanly, and its notes said the
// forge had "No commits between main and …". The step is done — nothing
// failed — and its output is the plain outcome, which the job's read carries
// as its state's detail (api.PresentJob), never a bare done with no word why.
func TestARunThatChangedNothingEndsWithNothingToChangeNotShipped(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "succeeded", "2026-11-08T12:02:34Z", "2026-11-08T12:10:54Z"),
		result: json.RawMessage(`{"success":true,"summary":"Build succeeded: 1/1 issues"}`),
		notes: map[string][]agentfield.Note{"exec-root": {
			{Message: "Build succeeded: 1/1 issues, verification=passed", Timestamp: afTime("2026-11-08T12:09:18.248Z")},
			{Message: "Project build check did not run: the run changed nothing", Timestamp: afTime("2026-11-08T12:09:18.249Z")},
			{Message: "Repo finalized: Surveyed the repository: a small Go ledger module.", Timestamp: afTime("2026-11-08T12:09:36.985Z")},
			{Message: "PR creation failed: No pull request could be created: the integration branch 'feature/x' has no commits ahead of the base branch 'main'", Timestamp: afTime("2026-11-08T12:10:31.780Z")},
			{Message: "Opening the PR with gh failed: pull request create failed: GraphQL: No commits between main and feature/x (createPullRequest)", Timestamp: afTime("2026-11-08T12:10:31.781Z")},
		}}}
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	res, err := r.Step(context.Background(), factoryrun.Job{Item: it, Stage: stage})
	if err != nil || !res.Done {
		t.Fatalf("a run that changed nothing is not a failure: %+v, %v", res, err)
	}
	if res.Output != factory.AgentFieldNothingToChange {
		t.Fatalf("output = %q, want %q", res.Output, factory.AgentFieldNothingToChange)
	}
	if end, _ := st.Get(it.ID); end.PullRequest != nil {
		t.Fatalf("a pull request was invented: %+v", end.PullRequest)
	}
}

// A RUN THAT FINISHED JUST BEFORE A QUIT IS RECORDED FINISHED (live e2e round
// 8, job 77): the run succeeded and opened its pull request 5 s before ⌘Q, so
// its end was on the job's record but the step was still running. The round
// a restart picks up is that same round (Continuing), and its run's end is
// read as the step's outcome: done, with the pull request, and no second run
// dispatched. A fresh round of the same step does start a run of its own.
func TestTheRoundARestartPicksUpReadsItsRunsEndInsteadOfStartingAnother(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	began := afTime("2026-11-08T12:01:27Z")
	if err := st.Update(it.ID, func(it *factory.Item) error {
		it.Stream = &factory.Stream{Started: began.Add(-time.Second), Phases: []factory.Phase{{Name: "build", Kind: factory.StageAgentField, State: factory.PhaseRunning, Round: 1, Started: began.Add(-time.Second)}}}
		it.AgentFieldSteps = map[string]*factory.AgentFieldRun{"build": {Target: "swe-planner.build_issue", ExecutionID: "exec-root", RunID: "run-1", Began: began, Ended: afTime("2026-11-08T12:18:25Z"), Status: "succeeded"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it, _ = st.Get(it.ID)
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "succeeded", "2026-11-08T12:01:27Z", "2026-11-08T12:18:25Z"),
		result: json.RawMessage(`{"success":true,"pr_url":"https://github.com/santoshkumarradha/factory-demo/pull/298","pr_number":298}`)}
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	res, err := r.Step(context.Background(), factoryrun.Job{Item: it, Stage: stage, Round: 1, Continuing: true})
	if err != nil || !res.Done || !strings.Contains(res.Output, "pull/298") {
		t.Fatalf("the finished run did not end its step: %+v, %v", res, err)
	}
	if len(cp.dispatched) != 0 {
		t.Fatalf("a run that had finished was started again: %v", cp.dispatched)
	}
	if end, _ := st.Get(it.ID); end.PullRequest == nil || end.PullRequest.Number != 298 {
		t.Fatalf("the job does not hold the run's pull request: %+v", end.PullRequest)
	}
	// A round begun afresh is a new start and runs again.
	if _, err := r.Step(context.Background(), factoryrun.Job{Item: it, Stage: stage, Round: 1}); err != nil {
		t.Fatalf("fresh round: %v", err)
	}
	if len(cp.dispatched) != 1 {
		t.Fatalf("a fresh round of an ended step did not start its own run: %d dispatched", len(cp.dispatched))
	}
}

// THE DRAIN AFTER A RUN'S END NEVER HOLDS A QUIT. A run that ended still has
// its steps' last lines read for up to afStopDrain, and that wait used to run
// on a context nothing could cancel, so a quit seconds after a run finished
// held the local factory's shutdown for the whole wait. The reading's own end
// (an engine closing, a lease let go) ends the drain at once.
func TestTheDrainAfterARunsEndStopsWhenTheEngineCloses(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "succeeded", "2026-11-08T12:00:00Z", "2026-11-08T12:00:05Z",
		afNode("e-code", "coder", "succeeded", "2026-11-08T12:00:01Z", "2026-11-08T12:00:04Z")),
		result: json.RawMessage(`{"success":true}`)}
	// The step's record never closes: the node that relays it is gone.
	cp.relayLines("e-code", [][]byte{
		[]byte(`{"type":"session","version":1,"id":"nodecoder","cwd":"/node","model":"m","timestamp":"2026-11-08T12:00:01Z"}`),
		[]byte(`{"type":"message","role":"user","content":"add Len","timestamp":"2026-11-08T12:00:01Z"}`),
	}, false)
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Step(ctx, factoryrun.Job{Item: it, Stage: stage})
		done <- err
	}()
	afUntil(t, st, it.ID, "recording the run's end", func(i factory.Item) bool {
		run := i.AgentFieldSteps["build"]
		return run != nil && !run.Ended.IsZero()
	})
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("the drain held the closing engine past its end (afStopDrain is %s)", afStopDrain)
	}
}
