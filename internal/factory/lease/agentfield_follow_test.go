package lease_test

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// pausedRemoteJob is liveRemoteJob waiting in Needs you on its run's
// approval, as a runner that follows the run writes it.
func pausedRemoteJob() factory.Item {
	it := liveRemoteJob(false)
	it.State = factory.StateNeedsYou
	it.QKind = factory.QKindAgentField
	it.AgentFieldSteps["build"].Waiting = &factory.AgentFieldWait{Execution: "exec-plan", RequestID: "req-1", Question: "Approve the plan?"}
	it.Question = factory.AgentFieldQuestion(*it.AgentFieldSteps["build"].Waiting)
	return it
}

// A LIVE REMOTE RUN WITH NOBODY FOLLOWING IT IS OWED A RUNNER. Jobs 71 and 73
// of the third live run: each waited on its run's approval, held by this
// machine's runner, when the engine restarted. The hold expired into nothing
// (a job in Needs you is not running, so it was not queued), the person then
// approved with no runner holding the job, the job turned running with no
// lease, and no runner ever took it again: it read "running" while its run
// went on at the control plane and opened its pull request.
func TestARunPausedAtApprovalIsFollowedAgainAfterItsRunnerWentAway(t *testing.T) {
	f := setup(t)
	d := recoveryDesk(t, f)
	it := f.add(t, pausedRemoteJob())
	// The job is followed: its runner holds it while it waits.
	held, err := d.Pull(worker("before-restart", "sam"))
	if err != nil {
		t.Fatalf("the paused run was not offered to its runner: %v", err)
	}
	if held.ThreadOnly {
		t.Fatal("following a paused run is not a thread-only hold")
	}
	paused := readRecovery(t, f, it.ID)
	if paused.State != factory.StateNeedsYou || paused.QKind != factory.QKindAgentField {
		t.Fatalf("taking the paused job took down its question: %s %q", paused.State, paused.QKind)
	}
	// The engine restarts: the hold runs out with nobody renewing it.
	f.now = held.ExpiresAt
	if _, err := d.Expire(); err != nil {
		t.Fatal(err)
	}
	// The person approves while no runner holds the job.
	if err := lease.NewDoors(d).AnswerAgentField(it.ID, "req-1", factory.AgentFieldDecision{Decision: "approved", By: "sam"}); err != nil {
		t.Fatal(err)
	}
	// The runner of the restarted engine takes it again, to follow the same
	// run and carry the approval it still owes.
	again, err := d.Pull(worker("after-restart", "sam"))
	if err != nil {
		t.Fatalf("after the restart no runner took the job back: %v", err)
	}
	if again.ThreadOnly {
		t.Fatal("the job was taken back only to talk, not to follow its run")
	}
	got := readRecovery(t, f, it.ID)
	if got.Failure != nil || got.Recovery != nil || got.AgentFieldSteps["build"].ExecutionID != "exec-root" || len(got.AgentFieldSteps["build"].Decisions) != 1 {
		t.Fatalf("taking the run back changed it: failure %+v recovery %+v run %+v", got.Failure, got.Recovery, got.AgentFieldSteps["build"])
	}
	// The run ends; its end lands on the job.
	if err := d.Apply(again.ID, "after-restart", false, false, func(it *factory.Item) error {
		r := it.AgentFieldSteps["build"]
		r.Status, r.Ended = "succeeded", f.now
		it.Stream.Phases[0].State = factory.PhaseDone
		it.State = factory.StateShipped
		return nil
	}); err != nil {
		t.Fatalf("the run's end did not land on the job: %v", err)
	}
	if got := readRecovery(t, f, it.ID); got.AgentFieldSteps["build"].Ended.IsZero() || got.State != factory.StateShipped {
		t.Fatalf("the run's end is not on the job: %+v", got.AgentFieldSteps["build"])
	}
}

// The same for a run that was running, not paused, when its runner went away
// and the job was left running with no hold (job 72): it is taken again.
func TestARunningRemoteRunWithNoHoldIsTakenAgain(t *testing.T) {
	f := setup(t)
	d := recoveryDesk(t, f)
	job := liveRemoteJob(false)
	job.State = factory.StateRunning
	it := f.add(t, job)
	again, err := d.Pull(worker("after-restart", "sam"))
	if err != nil {
		t.Fatalf("a running remote run with no hold was not taken again: %v", err)
	}
	if again.ThreadOnly {
		t.Fatal("it was taken only to talk")
	}
	if got := readRecovery(t, f, it.ID); got.State != factory.StateRunning || got.Failure != nil {
		t.Fatalf("taken back as %s with %+v", got.State, got.Failure)
	}
}

// A run that ended, or a job a person paused, is not followed: it is not owed.
func TestAnEndedOrPausedRemoteRunIsNotTakenAgain(t *testing.T) {
	for name, job := range map[string]func() factory.Item{
		"ended": func() factory.Item { it := liveRemoteJob(true); it.State = factory.StateRunning; return it },
		"paused": func() factory.Item {
			it := liveRemoteJob(false)
			it.State = factory.StateRunning
			it.Stream.Paused = true
			return it
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			f.add(t, job())
			if _, err := recoveryDesk(t, f).Pull(worker("w", "sam")); !errors.Is(err, lease.ErrNoJob) {
				t.Fatalf("a run that is not owed a runner was taken: %v", err)
			}
		})
	}
}

// A STOP OF A JOB NOBODY HOLDS STILL REACHES ITS RUN. Job 71: Stop landed while
// no runner held the job, so nothing carried it to the control plane, and the
// run opened its pull request four seconds later. The stopped job is offered
// to its runner once more, held to talk only, so that runner cancels the run;
// once the run's end is on the job, nothing is owed and it is not offered again.
func TestAStopOfAnUnheldRemoteRunIsCarriedByTheNextRunner(t *testing.T) {
	f := setup(t)
	d := recoveryDesk(t, f)
	job := liveRemoteJob(false)
	job.State = factory.StateRunning
	it := f.add(t, job)
	if err := lease.NewDoors(d).Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	carry, err := d.Pull(worker("after-restart", "sam"))
	if err != nil {
		t.Fatalf("the stopped job's live run was never carried to a runner: %v", err)
	}
	if !carry.ThreadOnly {
		t.Fatal("a stopped job was taken to run its steps")
	}
	if got := readRecovery(t, f, it.ID); !factory.StoppedByPerson(got) {
		t.Fatalf("carrying the stop undid it: %s", got.State)
	}
	if err := d.Requeue(it.ID, "after-restart"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Update(it.ID, func(it *factory.Item) error {
		r := it.AgentFieldSteps["build"]
		r.Status, r.Ended = "cancelled", f.now
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Pull(worker("after-restart", "sam")); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("a stopped job whose run ended was offered again: %v", err)
	}
}

// A report from a runner that stamped the person's no on its step as a
// tool_error still ends the job Rejected with no failure on the step: the
// desk's rail reads "rejected here", as after a native Reject.
func TestARejectedRunsStepKeepsNoFailureFromAnOlderRunner(t *testing.T) {
	f := setup(t)
	d := recoveryDesk(t, f)
	job := liveRemoteJob(false)
	it := f.add(t, job)
	held, err := d.Pull(worker("runner", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Apply(held.ID, "runner", false, false, func(it *factory.Item) error {
		r := it.AgentFieldSteps["build"]
		r.Status, r.Error, r.Ended = "cancelled", factory.AgentFieldRejectCancel+": out of scope", f.now
		failed := factory.NewFailure("build", factory.CodeToolError, "the AgentField run cancelled: approval_rejected: out of scope", "", factory.FailureStopped, 1, f.now)
		factory.StampFailure(it, failed)
		it.Stream.Phases[0].State = factory.PhaseFailed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got := readRecovery(t, f, it.ID)
	if got.Rejection == nil || got.Failure != nil || got.Stream.Phases[0].Failure != nil {
		t.Fatalf("rejection %+v, job failure %+v, step failure %+v", got.Rejection, got.Failure, got.Stream.Phases[0].Failure)
	}
}
