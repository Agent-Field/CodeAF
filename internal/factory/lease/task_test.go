package lease_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// taskFixture is a job a worker holds in the middle of its write step.
func taskFixture(t *testing.T) (*fixture, *lease.Doors, factory.Item, lease.Lease) {
	t.Helper()
	f := setup(t)
	doors := lease.NewDoors(f.desk)
	it := f.add(t, factory.Item{State: factory.StateNew, Kind: factory.KindIssue, Cap: 10, Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))})
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	held, err := f.desk.Pull(lease.Worker{ID: "w1", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Update(it.ID, func(it *factory.Item) error {
		it.State = factory.StateRunning
		for i := range it.Stream.Phases {
			switch it.Stream.Phases[i].Name {
			case "plan", "approve":
				it.Stream.Phases[i].State, it.Stream.Phases[i].Round = factory.PhaseDone, 1
			case "write":
				it.Stream.Phases[i].State, it.Stream.Phases[i].Round = factory.PhaseRunning, 1
				it.Stream.Cur = i
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return f, doors, it, held
}

// A TASK THAT WAITS FOR THE ROUND RUNNING NOW RIDES THE LEASE. The runner
// keeps its hold and its round, and hears of the task as a signal that names
// the step, the instructions and who asked.
func TestATaskForAfterTheRoundSignalsTheRunner(t *testing.T) {
	f, doors, it, held := taskFixture(t)
	added, err := doors.Task(it.ID, factory.TaskByMainAgent, "Add the rounding note.", false)
	if err != nil || added.Step != "task" || added.Road != factory.TaskRunsAfter {
		t.Fatalf("added %+v (%v)", added, err)
	}
	saved, _ := f.store.Get(it.ID)
	if phaseNames(saved) != "plan approve write task test review proof" || saved.State != factory.StateRunning {
		t.Fatalf("the run is %q, state %s", phaseNames(saved), saved.State)
	}
	if !factory.Offered(saved, factory.OfferByMainAgent, factory.DoTask) || saved.Wait.What != "write" {
		t.Fatalf("wait %+v, offers %v", saved.Wait, saved.Offers)
	}
	if _, err := f.desk.Renew(held.ID, "w1"); err != nil {
		t.Fatalf("the runner lost its hold to a task that waits for its round: %v", err)
	}
	signals, err := f.desk.DrainLive(held.ID, "w1")
	if err != nil {
		t.Fatal(err)
	}
	at := slices.IndexFunc(signals, func(s lease.Signal) bool { return s.Verb == lease.SignalTask })
	if at < 0 || signals[at].ID != "task" || signals[at].Words != "Add the rounding note." || signals[at].By != factory.TaskByMainAgent {
		t.Fatalf("signals %+v", signals)
	}
	// A second task goes behind the first.
	if added, err = doors.Task(it.ID, factory.TaskByYou, "And the changelog.", false); err != nil || added.Step != "task2" {
		t.Fatalf("second task %+v (%v)", added, err)
	}
	if saved, _ = f.store.Get(it.ID); phaseNames(saved) != "plan approve write task task2 test review proof" {
		t.Fatalf("the run is %q", phaseNames(saved))
	}
}

// A TASK ASKED FOR NOW LETS THE RUNNER GO, AS A STEP EDIT DOES. The lease is
// withdrawn, never merely released, so the runner ends its round without a
// stop being recorded, and the job is queued with the task before the step
// that was cut, which keeps its round.
func TestATaskNowWithdrawsTheRunner(t *testing.T) {
	f, doors, it, held := taskFixture(t)
	added, err := doors.Task(it.ID, factory.TaskByYou, "Add the rounding note.", true)
	if err != nil || added.Road != factory.TaskRunsBefore {
		t.Fatalf("added %+v (%v)", added, err)
	}
	saved, _ := f.store.Get(it.ID)
	if phaseNames(saved) != "plan approve task write test review proof" || saved.State != factory.StateQueued || saved.Stream.Phases[saved.Stream.Cur].Name != "task" {
		t.Fatalf("the run is %q, state %s, on %s", phaseNames(saved), saved.State, saved.Stream.Phases[saved.Stream.Cur].Name)
	}
	if write := saved.Stream.Phases[3]; write.State != factory.PhaseRunning || write.Round != 1 {
		t.Fatalf("the cut step is %s at round %d, want still running its round 1", write.State, write.Round)
	}
	if saved.Wait.Who != factory.WaitRunner || saved.Wait.What != "task" {
		t.Fatalf("wait %+v, want a runner for the task", saved.Wait)
	}
	if _, err := f.desk.Renew(held.ID, "w1"); !errors.Is(err, lease.ErrWithdrawn) {
		t.Fatalf("the old runner's heartbeat = %v, want it told its execution was withdrawn", err)
	}
	if factory.StoppedByPerson(saved) {
		t.Fatal("a task recorded a person's stop")
	}
	again, err := f.desk.Pull(lease.Worker{ID: "w1", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil || again.Job != it.ID {
		t.Fatalf("the job was not offered again: %+v (%v)", again, err)
	}
}

// A refused task changes neither the job nor the runner's hold.
func TestARefusedTaskLeavesTheJobAndTheHold(t *testing.T) {
	f, doors, it, held := taskFixture(t)
	if err := f.store.Update(it.ID, func(it *factory.Item) error { it.Manual = true; return nil }); err != nil {
		t.Fatal(err)
	}
	before, _ := f.store.Revision(it.ID)
	_, err := doors.Task(it.ID, factory.TaskByMainAgent, "Add the rounding note.", true)
	var refused factory.TaskRefused
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want a task refusal", err)
	}
	if after, _ := f.store.Revision(it.ID); after != before {
		t.Fatalf("the refused task wrote the job: revision %d then %d", before, after)
	}
	if _, err := f.desk.Renew(held.ID, "w1"); err != nil {
		t.Fatalf("the refused task took the runner's hold: %v", err)
	}
}

// A lease that only lends the manager to a reply runs no step: a task on a
// landed job it holds is queued as on a job nobody holds, and the lease is
// left alone.
func TestATaskLeavesAThreadOnlyLeaseAlone(t *testing.T) {
	f := setup(t)
	doors := lease.NewDoors(f.desk)
	it := f.add(t, factory.Item{State: factory.StateLanded, Kind: factory.KindIssue, Cap: 10, Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}},
		Proof: []factory.Claim{{Text: "works", OK: true, Evidence: "ran it"}},
		Talk:  "talk-1", Thread: []factory.Message{{ID: "m1", From: "you", Text: "what changed?", State: "pending"}},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1}}}})
	held, err := f.desk.Pull(lease.Worker{ID: "w1", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil || !held.ThreadOnly {
		t.Fatalf("the fixture's pending reply did not lend a thread-only lease: %+v (%v)", held, err)
	}
	added, err := doors.Task(it.ID, factory.TaskByYou, "Add the rounding note.", false)
	if err != nil || added.Road != factory.TaskRunsLanded {
		t.Fatalf("added %+v (%v)", added, err)
	}
	if saved, _ := f.store.Get(it.ID); saved.State != factory.StateQueued || phaseNames(saved) != "write task" {
		t.Fatalf("state %s, run %q", saved.State, phaseNames(saved))
	}
	if _, err := f.desk.Renew(held.ID, "w1"); err != nil {
		t.Fatalf("the reply's lease was taken: %v", err)
	}
}
