package lease_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// A HOLD THAT RAN OUT ON A LIVE REMOTE RUN RECORDS NOTHING. Jobs 72, 74 and
// 75 of the fourth live run: every job a restarted runner took back got an
// event "step_failed" reading " ·  · " at the moment it was taken, because
// the hold's expiry wrote one for every lost hold, including the one it had
// just decided was no failure at all (the run goes on at the control plane
// and the next runner follows it).
func TestAnExpiredHoldOnALiveRemoteRunRecordsNoFailedStep(t *testing.T) {
	f := setup(t)
	rec, err := record.Open(f.store, record.Options{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	d, err := lease.Open(f.store, lease.Options{TTL: time.Second, Now: func() time.Time { return f.now }, Record: rec})
	if err != nil {
		t.Fatal(err)
	}
	job := liveRemoteJob(false)
	job.State = factory.StateRunning
	f.add(t, job)
	held, err := d.Pull(worker("before-restart", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = held.ExpiresAt
	if _, err := d.Pull(worker("after-restart", "sam")); err != nil {
		t.Fatalf("the live run was not taken again: %v", err)
	}
	events, err := rec.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind == record.StepFailed {
			t.Fatalf("taking a live run back recorded a failed step: %+v", e)
		}
	}
}

// A NATIVE JOB WHOSE RUNNER QUIT MID-STEP IS AN INTERRUPTED STEP, whichever
// door let its hold go. Task A of the fourth live run: the engine quit while
// its proof step ran; the quitting runner settled the step's last spending
// through the receipt door, which released the hold and left the job
// "running" with nobody holding it, so expiry never saw it and no runner was
// ever offered it again. It read "running 35m" until a person paused and
// resumed it. It is requeued exactly as a hold that expired is: the step is
// interrupted, and the automatic retry, within its limit, offers it again.
func TestARunnerSettlingSpendAsItQuitsRequeuesTheInterruptedStep(t *testing.T) {
	f := setup(t)
	d := recoveryDesk(t, f)
	it := f.add(t, recoveryJob())
	w := worker("runner-1", "sam")
	w.Machine = "laptop"
	held, err := d.Pull(w)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Update(it.ID, func(it *factory.Item) error {
		it.WorktreeMachine = "laptop"
		it.ReportReceipts = append(it.ReportReceipts, factory.WorkerReportReceipt{Digest: "anchor", Lease: held.ID, Machine: "laptop", Handle: "sam", At: f.now})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The quitting runner settles the step's spending on its anchor receipt.
	if _, _, err := d.ReconcileReport(it.ID, w, held.ID, "anchor", 0.25, func(it *factory.Item, receipt *factory.WorkerReportReceipt) error {
		receipt.AdditionalCost = 0.25
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got := readRecovery(t, f, it.ID)
	if got.State == factory.StateRunning {
		t.Fatal("the job was left running with no hold")
	}
	if got.Failure == nil || got.Failure.Code != factory.CodeMachineLost {
		t.Fatalf("the interrupted step was not recorded as one: %s %+v", got.State, got.Failure)
	}
	doors := lease.NewDoors(d)
	if _, err := doors.RecoverAutomatic(it.ID); err != nil {
		t.Fatal(err)
	}
	waiting := readRecovery(t, f, it.ID)
	if waiting.Recovery == nil || waiting.Recovery.State != "waiting" {
		t.Fatalf("no automatic retry was planned: %+v", waiting.Recovery)
	}
	f.now = waiting.Recovery.NextAt
	if _, err := doors.RecoverAutomatic(it.ID); err != nil {
		t.Fatal(err)
	}
	again := worker("runner-2", "sam")
	again.Machine = "laptop"
	if _, err := d.Pull(again); err != nil {
		t.Fatalf("the interrupted step was never offered again: %v", err)
	}
	if got := readRecovery(t, f, it.ID); got.Recovery == nil || got.Recovery.Attempts != 1 {
		t.Fatalf("the retry did not spend one of its attempts: %+v", got.Recovery)
	}
}
