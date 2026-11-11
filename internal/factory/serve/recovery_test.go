package serve_test

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecoveryHTTPExposesTryAgainAfterCompletedPublication(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Autonomy: factory.AutonomyManual, State: factory.StateNeedsYou, Assignee: "sam", Cap: 10, PullRequest: &factory.PullRequest{Number: 69}, PublishStatus: &factory.Publishing{State: "opened", Attempt: 1}, Failure: &factory.Failure{Step: factory.FollowPRStep, Code: factory.CodeMachineLost, Detail: "Runner went offline mid-step", Next: factory.FailureStopped}, Stages: []factory.Stage{{Name: "plan", On: true}, {Name: factory.FollowPRStep, On: true}}, Stream: &factory.Stream{Cur: 1, Spent: 1.25, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: factory.FollowPRStep, State: factory.PhaseRunning}}}})
	j := f.read(t, it)
	if !j.Controls.RerunStep {
		t.Fatal("old opened publication hid Try again")
	}
	f.post(t, it, "steps/followpr/rerun", fmt.Sprintf(`{"expect":{"version":%d,"state":"needs_you","step":"followpr"}}`, j.Version), 200)
	cur, e := f.st.Get(it.ID)
	if e != nil {
		t.Fatal(e)
	}
	if cur.State != factory.StateQueued || cur.PullRequest.Number != 69 || cur.Stream.Spent != 1.25 || cur.Stream.Phases[0].State != factory.PhaseDone {
		t.Fatal("retry restarted completed work")
	}
}
func TestRecoveryHTTPPlansAndAdvancesWithoutApproval(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Autonomy: factory.AutonomyAuto, State: factory.StateNeedsYou, Assignee: "sam", Cap: 10, Failure: &factory.Failure{Step: "write", Code: factory.CodeTransportError, Detail: "connection reset by peer", Next: factory.FailureStopped, At: time.Unix(0, f.clock.Load())}, Stages: []factory.Stage{{Name: "write", On: true}}, Stream: &factory.Stream{Spent: 1, Phases: []factory.Phase{{Name: "write", State: factory.PhaseFailed}}}})
	j := f.read(t, it)
	if j.Recovery == nil || j.Recovery.State != "waiting" {
		t.Fatalf("no countdown: %+v", j.Recovery)
	}
	f.clock.Store(j.Recovery.NextAt.UnixNano())
	j = f.read(t, it)
	if j.State != "queued" || j.Recovery.Attempts != 1 || j.Recovery.State != "queued" {
		t.Fatalf("not automatically queued: %+v", j.Recovery)
	}
}

// A restart arms the saved deadline without a desktop poll or approval click.
func TestRecoveryDurableTimerAdvancesWithoutUI(t *testing.T) {
	f := newV2Jobs(t)
	now := time.Unix(0, f.clock.Load())
	it := f.create(t, factory.Item{Autonomy: factory.AutonomyAuto, Assignee: "sam", State: factory.StateNeedsYou, Cap: 10, Failure: &factory.Failure{Step: "write", Code: factory.CodeTransportError, Next: factory.FailureStopped, At: now}, Recovery: &factory.StepRecovery{Step: "write", State: "waiting", Limit: factory.RecoveryLimit, LastAt: now, NextAt: now.Add(20 * time.Millisecond), Deadline: now.Add(time.Minute)}, Stages: []factory.Stage{{Name: "write", On: true}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", State: factory.PhaseFailed}}}})
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	defer f.h.Close()
	f.clock.Store(now.Add(time.Second).UnixNano())
	deadline := time.After(time.Second)
	for {
		cur, e := f.st.Get(it.ID)
		if e != nil {
			t.Fatal(e)
		}
		if cur.State == factory.StateQueued && cur.Recovery.Attempts == 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("saved timer did not recover without a UI read")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRecoveryRetriesAfterTemporaryItemLockFailure(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Autonomy: factory.AutonomyAuto, State: factory.StateNeedsYou, Assignee: "sam", Cap: 10, Failure: &factory.Failure{Step: "write", Code: factory.CodeTransportError, Next: factory.FailureStopped, At: time.Unix(0, f.clock.Load())}, Stages: []factory.Stage{{Name: "write", On: true}}, Stream: &factory.Stream{Spent: 1, Phases: []factory.Phase{{Name: "write", State: factory.PhaseFailed}}}})
	lock := filepath.Join(f.st.Root(), fmt.Sprintf("%d.lock", it.ID))
	if err := os.Remove(lock); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	j := f.read(t, it)
	if j.Recovery != nil {
		t.Fatal("fixture did not block recovery")
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	j = f.read(t, it)
	if j.Recovery == nil || j.Recovery.State != "waiting" {
		t.Fatalf("temporary lock failure froze recovery: %+v", j.Recovery)
	}
}

func staleWorkerJob(at time.Time) factory.Item {
	return factory.Item{
		State: factory.StateRunning, Assignee: "sam", Autonomy: factory.AutonomyAuto,
		Created: at, Changed: at, Cap: 5, WorktreeMachine: "worker-computer", HeadCommit: "kept-head",
		Stages: []factory.Stage{{Name: "write", On: true}, {Name: "test", On: true}},
		Stream: &factory.Stream{Started: at, Cur: 1, Spent: 2, Phases: []factory.Phase{
			{Name: "write", State: factory.PhaseDone},
			{Name: "test", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 4, RoundLimit: 6, Started: at, Chat: "interrupted-test",
				Verification: &factory.StepVerification{Checks: []factory.RunningCheck{{Purpose: "go test"}}}},
		}},
	}
}

func TestRecoveryHTTPRestoresAnInterruptedWorkerJobWithoutALease(t *testing.T) {
	f := newV2Jobs(t)
	clock := time.Unix(0, f.clock.Load())
	it := f.create(t, staleWorkerJob(clock.Add(-time.Hour)))
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)

	job := f.read(t, it)
	if job.State == api.StateRunning || job.Recovery == nil || job.Recovery.Step != "test" || job.Recovery.State != "waiting" {
		t.Fatalf("lease-less test still appears active: state=%s recovery=%+v", job.State, job.Recovery)
	}
	if len(job.Steps) != 2 || job.Steps[1].State != api.StepWaiting || job.Steps[1].Verification != nil {
		t.Fatalf("interrupted test still shows live work: %+v", job.Steps)
	}
	f.clock.Store(job.Recovery.NextAt.UnixNano())
	job = f.read(t, it)
	if job.State != api.StateQueued || job.Recovery == nil || job.Recovery.State != "queued" || job.Recovery.Attempts != 1 {
		t.Fatalf("interrupted test did not queue for the runner: state=%s recovery=%+v", job.State, job.Recovery)
	}
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	test := saved.Stream.Phases[1]
	if saved.HeadCommit != "kept-head" || saved.Stream.Spent != 2 || saved.Stream.Phases[0].State != factory.PhaseDone || test.Round != 5 || len(test.Rounds) != 1 || test.Rounds[0].Chat != "interrupted-test" {
		t.Fatalf("retry lost the previous work or attempt: head=%s spend=%v write=%s test=%+v", saved.HeadCommit, saved.Stream.Spent, saved.Stream.Phases[0].State, test)
	}
}

func TestRecoveryHTTPLeavesIntentionallyHeldAndLocalJobsAlone(t *testing.T) {
	for name, change := range map[string]func(*factory.Item){
		"paused":               func(it *factory.Item) { it.Stream.Paused = true },
		"manual takeover":      func(it *factory.Item) { it.Manual = true },
		"person stopped":       factory.MarkPersonStopped,
		"local without worker": func(it *factory.Item) { it.WorktreeMachine = "" },
		"publication":          func(it *factory.Item) { it.Publishing = true },
		"merge":                func(it *factory.Item) { it.Merging = &factory.MergeRun{State: factory.MergeMerging} },
		"pending question":     func(it *factory.Item) { it.Question = "Which test should run?" },
		"just changed":         func(it *factory.Item) { it.Changed = it.Created.Add(time.Hour) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newV2Jobs(t)
			clock := time.Unix(0, f.clock.Load())
			it := staleWorkerJob(clock.Add(-time.Hour))
			change(&it)
			it = f.create(t, it)
			f.read(t, it)
			saved, err := f.st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.State != factory.StateRunning || saved.Recovery != nil || saved.Failure != nil {
				t.Fatalf("unexpected recovery for %s: state=%s recovery=%+v failure=%+v", name, saved.State, saved.Recovery, saved.Failure)
			}
		})
	}
}

func TestRecoveryHTTPLiveLeaseKeepsAnOldWorkerStepRunning(t *testing.T) {
	f := newV2Jobs(t)
	clock := time.Now().Add(time.Hour)
	f.clock.Store(clock.UnixNano())
	it := staleWorkerJob(clock.Add(-2 * time.Hour))
	it.State = factory.StateQueued
	it = f.create(t, it)
	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := desk.Pull(lease.Worker{ID: "worker", Machine: it.WorktreeMachine, Handle: it.Assignee}); err != nil {
		t.Fatal(err)
	}
	job := f.read(t, it)
	if job.State != api.StateRunning || job.Recovery != nil || job.HeldBy == nil {
		t.Fatalf("active worker was interrupted: state=%s recovery=%+v held=%v", job.State, job.Recovery, job.HeldBy)
	}
}

func TestRecoveryHTTPManualWorkerJobWaitsForAnExplicitRetry(t *testing.T) {
	f := newV2Jobs(t)
	clock := time.Unix(0, f.clock.Load())
	it := staleWorkerJob(clock.Add(-time.Hour))
	it.Autonomy = factory.AutonomyManual
	it = f.create(t, it)
	job := f.read(t, it)
	if job.State != api.StateNeedsYou || job.NeedsYou == nil || job.NeedsYou.Kind != factory.NeedsRunnerOffline ||
		job.Recovery == nil || job.Recovery.State != "exhausted" || !job.Controls.RerunStep {
		t.Fatalf("manual job lacks an explicit retry: state=%s needs=%+v recovery=%+v", job.State, job.NeedsYou, job.Recovery)
	}
}

func TestRecoveryHTTPChecksForMissingRunnerAfterGracePeriod(t *testing.T) {
	f := newV2Jobs(t)
	clock := time.Unix(0, f.clock.Load())
	it := f.create(t, staleWorkerJob(clock))
	if job := f.read(t, it); job.State != api.StateRunning || job.Recovery != nil {
		t.Fatalf("job changed during the grace period: state=%s recovery=%+v", job.State, job.Recovery)
	}
	f.clock.Store(clock.Add(lease.DefaultTTL).UnixNano())
	if job := f.read(t, it); job.State == api.StateRunning || job.Recovery == nil {
		t.Fatalf("job still stuck after the grace period: state=%s recovery=%+v", job.State, job.Recovery)
	}
}
