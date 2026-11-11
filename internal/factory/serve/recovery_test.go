package serve_test

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
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
