package worker_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// planGateWrite is the shape every real pipeline has: the approve step is not
// the first step. The existing approve test puts the gate first, where the
// runner's current step happens to be the gate and the defect cannot show.
func planGateWrite() []factory.Stage {
	plan := check("true")
	plan.Name = "plan"
	write := check("true")
	write.Name = "write"
	return []factory.Stage{plan, {Name: factory.ApproveName, Kind: factory.StageGate, On: true}, write}
}

func phase(it factory.Item, name string) factory.Phase {
	for _, ph := range it.Stream.Phases {
		if ph.Name == name {
			return ph
		}
	}
	return factory.Phase{}
}

// T1: the question reaches the server on the gate, with the plan done.
func TestAGateThatIsNotFirstAsksOnTheGate(t *testing.T) {
	g := newRig(t)
	id := g.add(t, planGateWrite()...)
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	held := g.waitFor(t, id, factory.StateNeedsYou)
	if held.QKind != factory.QKindApprove || held.Asking == nil || held.Asking.Stage != factory.ApproveName {
		t.Fatalf("question qkind %q asking %+v", held.QKind, held.Asking)
	}
	if phase(held, "plan").State != factory.PhaseDone || phase(held, factory.ApproveName).State != factory.PhaseWaiting || phase(held, "write").State != factory.PhasePending {
		t.Fatalf("phases %+v", held.Stream.Phases)
	}
	if held.Stream.Phases[held.Stream.Cur].Name != factory.ApproveName {
		t.Fatalf("cur %d", held.Stream.Cur)
	}
}

// T2: a yes while no worker holds the job resumes at write, never at plan.
func TestAColdYesResumesAfterTheGateWithoutRerunningThePlan(t *testing.T) {
	g := newRig(t)
	id := g.add(t, planGateWrite()...)
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	held := g.waitFor(t, id, factory.StateNeedsYou)

	// The same stored document on a factory whose worker has never held it.
	g2 := newRig(t)
	cp := held
	cp.ID = 0
	created, err := g2.st.Create(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err := g2.doors.Answer(created.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	landed := g2.waitFor(t, created.ID, factory.StateLanded)
	for _, ph := range landed.Stream.Phases {
		if ph.State != factory.PhaseDone {
			t.Fatalf("landed with %s=%s", ph.Name, ph.State)
		}
	}
	if p := phase(landed, "plan"); p.Round != phase(held, "plan").Round || len(p.Rounds) != len(phase(held, "plan").Rounds) {
		t.Fatalf("plan ran again: before %+v after %+v", phase(held, "plan"), p)
	}
	// One check for the plan, one for write, and the gate asked exactly once.
	if n := logHas(landed, "check: true"); n != 2 {
		t.Fatalf("want plan and write to run once each, saw %d checks: %+v", n, landed.Stream.Log)
	}
	if n := logHas(landed, "plan is ready"); n != 1 {
		t.Fatalf("the gate asked %d times: %+v", n, landed.Stream.Log)
	}
}

// T3: a yes while the worker still holds the lease lands with every server phase done.
func TestAWarmYesLandsWithEveryServerPhaseDone(t *testing.T) {
	g := newRig(t)
	id := g.add(t, planGateWrite()...)
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if err := g.doors.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	landed := g.waitFor(t, id, factory.StateLanded)
	for _, ph := range landed.Stream.Phases {
		if ph.State != factory.PhaseDone {
			t.Fatalf("landed with %s=%s", ph.Name, ph.State)
		}
	}
}

// T4: the shape an older worker stored, a settled approval older than the ask
// and the question filed against the finished plan, reads as waiting, and a
// yes lands without running the plan again.
func TestTheStoredMisfiledShapeLandsWithoutRerunningThePlan(t *testing.T) {
	g := newRig(t)
	now := time.Now()
	q := factory.ApproveQuestion("plan")
	it, err := g.st.Create(factory.Item{
		Repo: "api", Title: "legacy", Tier: factory.TierOwner, State: factory.StateNeedsYou, Stages: planGateWrite(),
		Question: q, QKind: factory.QKindApprove,
		Asking:   &factory.Asked{Stage: "plan", Question: q, At: now, With: factory.AskedYou},
		Approval: &factory.Approval{Kind: "plan", State: "approved", DecidedBy: "me", At: now.Add(-time.Minute), UndoUntil: now.Add(-55 * time.Second)},
		Stream: &factory.Stream{Cur: 0, Started: now, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageCheck, State: factory.PhaseWaiting, Round: 1, Result: &factory.StageResult{Done: true}},
			{Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhasePending},
			{Name: "write", Kind: factory.StageCheck, State: factory.PhasePending},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a := factory.JobApproval(it); a == nil || a.State != "waiting" {
		t.Fatalf("the settled approval still covers the new ask: %+v", a)
	}
	if err := g.doors.Answer(it.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	landed := g.waitFor(t, it.ID, factory.StateLanded)
	if p := phase(landed, "plan"); p.Round != 1 || p.Result == nil {
		t.Fatalf("plan ran again: %+v", p)
	}
	if n := logHas(landed, "check: true"); n != 1 {
		t.Fatalf("only write should run after legacy repair; saw %d checks: %+v", n, landed.Stream.Log)
	}
}
