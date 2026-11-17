package lease_test

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// A STEP EDIT TO A RUNNING JOB KEEPS IT RUNNING. The live audit's manager added
// a security step during Write: the edit landed, but the worker's lease was
// released rather than withdrawn, so the worker read it as a person's Stop, the
// job fell back to new with a Start button, and the next Start ran Plan again.
// The worker must be told its execution was WITHDRAWN (a handover, never a
// stop), and the job must be queued again at the step it was on.
func TestEditStepsOnARunningJobWithdrawsAndRequeuesAtTheCurrentStep(t *testing.T) {
	f := setup(t)
	doors := lease.NewDoors(f.desk)
	it := f.add(t, factory.Item{State: factory.StateNew, Kind: factory.KindIssue, Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))})
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
				it.Stream.Phases[i].State = factory.PhaseDone
			case "write":
				it.Stream.Phases[i].State = factory.PhaseRunning
				it.Stream.Cur = i
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	security := factory.Stage{Name: "security", Kind: factory.StageChat, Ask: "audit the diff", Until: "done", On: true}
	got, lines, err := doors.EditSteps(it.ID, factory.RunEdit{By: factory.ByManager, Add: []factory.Added{{Stage: security, After: "review"}}}, factory.DefaultRecipe())
	if err != nil || len(lines) == 0 {
		t.Fatalf("the edit = %v (%v)", lines, err)
	}
	if got.State != factory.StateQueued {
		t.Fatalf("the edited running job is %q, want queued", got.State)
	}
	if _, err := f.desk.Renew(held.ID, "w1"); !errors.Is(err, lease.ErrWithdrawn) {
		t.Fatalf("the old worker's heartbeat = %v, want it told its execution was withdrawn (not a stop)", err)
	}
	again, err := f.desk.Pull(lease.Worker{ID: "w1", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil || again.Job != it.ID {
		t.Fatalf("the edited job was not offered again: %+v (%v)", again, err)
	}
	saved, _ := f.store.Get(it.ID)
	if cur := saved.Stream.Phases[saved.Stream.Cur].Name; cur != "write" || saved.Stream.Phases[0].State != factory.PhaseDone {
		t.Fatalf("the re-offered job is on %q with plan %q; it must carry on at write", cur, saved.Stream.Phases[0].State)
	}
	if phaseNames(saved) != "plan approve write test review security proof" {
		t.Fatalf("the run is %q", phaseNames(saved))
	}
	if factory.StoppedByPerson(saved) {
		t.Fatal("a step edit recorded a person's stop")
	}
}
