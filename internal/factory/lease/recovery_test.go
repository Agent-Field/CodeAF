package lease_test

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"testing"
	"time"
)

func recoveryJob() factory.Item {
	return factory.Item{Autonomy: factory.AutonomyAuto, Assignee: "sam", Cap: 10, State: factory.StateQueued, HeadCommit: "kept-head", PullRequest: &factory.PullRequest{Number: 69},
		Stages: []factory.Stage{{Name: "plan", On: true}, {Name: factory.FollowPRStep, On: true}},
		Stream: &factory.Stream{Cur: 1, Spent: 1.25, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: factory.FollowPRStep, State: factory.PhaseRunning, Round: 2, Chat: "interrupted-chat"}}}}
}
func recoveryDesk(t *testing.T, f *fixture) *lease.Desk {
	t.Helper()
	d, e := lease.Open(f.store, lease.Options{TTL: time.Second, Now: func() time.Time { return f.now }})
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func readRecovery(t *testing.T, f *fixture, id int) factory.Item {
	t.Helper()
	it, e := f.store.Get(id)
	if e != nil {
		t.Fatal(e)
	}
	return it
}

func TestRecoveryRunnerLossIsBoundedDurableAndKeepsWork(t *testing.T) {
	f := setup(t)
	d := recoveryDesk(t, f)
	it := f.add(t, recoveryJob())
	w := worker("runner-1", "sam")
	held, e := d.Pull(w)
	if e != nil {
		t.Fatal(e)
	}
	for attempt := 1; attempt <= factory.RecoveryLimit; attempt++ {
		f.now = held.ExpiresAt
		if _, e = d.Expire(); e != nil {
			t.Fatal(e)
		}
		doors := lease.NewDoors(recoveryDesk(t, f))
		if _, e = doors.RecoverAutomatic(it.ID); e != nil {
			t.Fatal(e)
		}
		waiting := readRecovery(t, f, it.ID)
		if waiting.State != factory.StateQueued || waiting.Recovery == nil || waiting.Recovery.State != "waiting" || waiting.Stream.Phases[1].Failure == nil {
			t.Fatalf("not waiting: %+v", waiting.Recovery)
		}
		revision, _ := f.store.Revision(it.ID)
		if changed, e := doors.RecoverAutomatic(it.ID); e != nil || changed {
			t.Fatalf("repeated poll: %v %v", changed, e)
		}
		after, _ := f.store.Revision(it.ID)
		if after != revision {
			t.Fatal("idle recovery advanced the document revision")
		}
		if _, e := d.Pull(worker("early", "sam")); !errors.Is(e, lease.ErrNoJob) {
			t.Fatalf("backoff admitted a worker early: %v", e)
		}
		f.now = waiting.Recovery.NextAt
		if _, e = doors.RecoverAutomatic(it.ID); e != nil {
			t.Fatal(e)
		}
		queued := readRecovery(t, f, it.ID)
		if queued.State != factory.StateQueued || queued.Recovery.Attempts != attempt || queued.PullRequest.Number != 69 || queued.HeadCommit != "kept-head" || queued.Stream.Spent != 1.25 || queued.Stream.Phases[0].State != factory.PhaseDone {
			t.Fatalf("lost work: recovery=%+v spend=%f", queued.Recovery, queued.Stream.Spent)
		}
		w = worker("returning-runner", "sam")
		held, e = d.Pull(w)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = d.Heartbeat(it.ID, "runner-1"); !errors.Is(e, lease.ErrWorker) {
			t.Fatalf("old worker revived: %v", e)
		}
	}
	f.now = held.ExpiresAt
	_, _ = d.Expire()
	doors := lease.NewDoors(d)
	_, e = doors.RecoverAutomatic(it.ID)
	if e != nil {
		t.Fatal(e)
	}
	ended := readRecovery(t, f, it.ID)
	if ended.State != factory.StateNeedsYou || ended.Recovery.State != "exhausted" || ended.Recovery.Attempts != factory.RecoveryLimit {
		t.Fatalf("unbounded: %+v", ended.Recovery)
	}
	// A person's retry remains available after automatic retries end.
	if e = doors.StepControl(it.ID, factory.FollowPRStep, true, ""); e != nil {
		t.Fatal(e)
	}
	manual := readRecovery(t, f, it.ID)
	if manual.State != factory.StateQueued || manual.Recovery != nil || manual.Stream.Spent != 1.25 || manual.PullRequest.Number != 69 {
		t.Fatal("manual retry lost state")
	}
}

func TestRecoveryWaitingForRunnerExpiresAndModeBudgetChangesStopAdmission(t *testing.T) {
	for _, change := range []string{"deadline", "manual", "budget"} {
		t.Run(change, func(t *testing.T) {
			f := setup(t)
			it := recoveryJob()
			it.State = factory.StateNeedsYou
			fail := factory.NewFailure(factory.FollowPRStep, factory.CodeMachineLost, "Runner went offline mid-step", "", factory.FailureStopped, 1, f.now)
			factory.StampFailure(&it, fail)
			it = f.add(t, it)
			doors := lease.NewDoors(f.desk)
			_, _ = doors.RecoverAutomatic(it.ID)
			waiting := readRecovery(t, f, it.ID)
			f.now = waiting.Recovery.NextAt
			_, _ = doors.RecoverAutomatic(it.ID)
			if change == "deadline" {
				f.now = f.now.Add(factory.RunnerReturnWindow)
			} else {
				if e := f.store.Update(it.ID, func(cur *factory.Item) error {
					if change == "manual" {
						cur.Autonomy = factory.AutonomyManual
					} else {
						cur.Cap = 1
					}
					return nil
				}); e != nil {
					t.Fatal(e)
				}
			}
			if change != "deadline" {
				if _, e := f.desk.Pull(worker("new", "sam")); !errors.Is(e, lease.ErrNoJob) {
					t.Fatalf("admitted after %s: %v", change, e)
				}
			}
			if _, e := doors.RecoverAutomatic(it.ID); e != nil {
				t.Fatal(e)
			}
			ended := readRecovery(t, f, it.ID)
			if ended.Recovery.State != "exhausted" || ended.State != factory.StateNeedsYou {
				t.Fatalf("not recoverable: %+v", ended.Recovery)
			}
		})
	}
}

func TestRecoveryManualRetryRejectsAStaleDecisionAtomically(t *testing.T) {
	f := setup(t)
	it := f.add(t, recoveryJob())
	revision, _ := f.store.Revision(it.ID)
	if e := f.store.Update(it.ID, func(cur *factory.Item) error { cur.Cap = 20; return nil }); e != nil {
		t.Fatal(e)
	}
	before, _ := f.store.Revision(it.ID)
	err := lease.NewDoors(f.desk).StepControlExpected(it.ID, factory.FollowPRStep, true, "", &revision)
	var stale *store.StaleError
	if !errors.As(err, &stale) {
		t.Fatalf("stale retry sent: %v", err)
	}
	after, _ := f.store.Revision(it.ID)
	if after != before {
		t.Fatal("refused retry changed the job")
	}
}
func TestRecoveryDoesNotReplaceAHeldWorkerAndStillEndsItsWait(t *testing.T) {
	f := setup(t)
	it := f.add(t, recoveryJob())
	w := worker("held-worker", "sam")
	held, e := f.desk.Pull(w)
	if e != nil {
		t.Fatal(e)
	}
	if e := f.store.Update(it.ID, func(cur *factory.Item) error {
		factory.StampFailure(cur, factory.NewFailure(factory.FollowPRStep, factory.CodeTransportError, "connection reset", "", factory.FailureStopped, 1, f.now))
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	doors := lease.NewDoors(f.desk)
	_, e = doors.RecoverAutomatic(it.ID)
	if e != nil {
		t.Fatal(e)
	}
	// The wait for a held worker is bounded by the runner-return window, not by
	// attempts it never made.
	for i := 0; i <= int(factory.RunnerReturnWindow/(30*time.Second)); i++ {
		f.now = f.now.Add(30 * time.Second)
		if _, e = f.desk.Heartbeat(it.ID, w.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = doors.RecoverAutomatic(it.ID); e != nil {
			t.Fatal(e)
		}
	}
	cur := readRecovery(t, f, it.ID)
	if cur.Recovery == nil || cur.Recovery.State != "exhausted" || cur.Recovery.Attempts != 0 {
		t.Fatalf("held work superseded or waited forever: %+v", cur.Recovery)
	}
	if _, e = f.desk.Heartbeat(held.Job, w.ID); e != nil {
		t.Fatalf("live holder was revoked: %v", e)
	}
	if e = doors.StepControl(it.ID, factory.FollowPRStep, true, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = f.desk.Heartbeat(held.Job, w.ID); !errors.Is(e, lease.ErrNoLease) {
		t.Fatalf("manual retry did not fence old holder: %v", e)
	}
}

func TestRecoveryRepositoryOffLeavesAManualRecoveryPath(t *testing.T) {
	f := setup(t)
	it := recoveryJob()
	it.State = factory.StateNeedsYou
	factory.StampFailure(&it, factory.NewFailure(factory.FollowPRStep, factory.CodeMachineLost, "offline", "", factory.FailureStopped, 1, f.now))
	it = f.add(t, it)
	doors := lease.NewDoors(f.desk)
	_, _ = doors.RecoverAutomatic(it.ID)
	cur := readRecovery(t, f, it.ID)
	f.now = cur.Recovery.NextAt
	_, _ = doors.RecoverAutomatic(it.ID)
	if e := f.store.SetRepos([]string{}); e != nil {
		t.Fatal(e)
	}
	if _, e := doors.RecoverAutomatic(it.ID); e != nil {
		t.Fatal(e)
	}
	cur = readRecovery(t, f, it.ID)
	if cur.State != factory.StateNeedsYou || cur.Failure == nil || cur.Recovery.State != "exhausted" {
		t.Fatalf("off job has no recovery: %+v", cur.Recovery)
	}
	if e := f.store.SetRepos([]string{"acme/api"}); e != nil {
		t.Fatal(e)
	}
	if e := doors.StepControl(it.ID, factory.FollowPRStep, true, ""); e != nil {
		t.Fatal(e)
	}
}
