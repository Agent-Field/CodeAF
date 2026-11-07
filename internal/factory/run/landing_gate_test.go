package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE LIVE DESK WALK'S #176, replayed on the runner. A job on a library
// pipeline (planfix, writefix, runvetand, then the approve step proof2) was
// switched to Manual, held before runvetand, and switched to Auto during that
// hold. It ran on and published while proof2 still read "to come": the run
// landed past its approve step as though landing were the pass. The switch
// may pass the standing hold (that is Auto's word for Manual's ask), but the
// approve step after it is still a wait until the proof approval answers it.
func TestASwitchToAutoDuringAManualHoldLandsWithTheApproveStepWaiting(t *testing.T) {
	g := newRig(t, nil, func(o *Options) {
		o.Exec = map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			return done(job.Stage.Name + " done"), nil
		})}
	})
	gate := factory.Stage{Name: "proof2", Kind: factory.StageGate}
	id := g.add("quick fix", chat("planfix"), chat("writefix"), chat("runvetand"), gate)
	if err := g.st.Update(id, func(it *factory.Item) error { it.Autonomy = factory.AutonomyManual; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.asked(id, factory.NextStepQuestion("writefix"))
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	g.asked(id, factory.NextStepQuestion("runvetand"))
	if err := g.st.Update(id, func(it *factory.Item) error { it.Autonomy = factory.AutonomyAuto; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	ph := it.Stream.Phases
	if ph[2].State != factory.PhaseDone || ph[3].Name != "proof2" || ph[3].State != factory.PhaseWaiting || it.Stream.Cur != 3 {
		t.Fatalf("landed past its approve step: phases %+v cur %d", ph, it.Stream.Cur)
	}
	if factory.NeedsKind(it) != factory.NeedsApproveProof {
		t.Fatalf("the landing waits as %q", factory.NeedsKind(it))
	}
	// The proof approval is the answer, and only then is the step passed.
	if _, err := g.r.SignOff(id, false); err != nil {
		t.Fatal(err)
	}
	it, _ = g.st.Get(id)
	if it.Stream.Phases[3].State != factory.PhaseDone {
		t.Fatalf("the approval did not pass proof2: %+v", it.Stream.Phases[3])
	}
}

// A WORKER NEVER REPORTS AN APPROVE STEP, so the service's copy learns the
// landing's wait when the run lands there, and the approval that publishes is
// what passes it.
func TestALandedReportLeavesTheApproveStepWaitingUntilPublication(t *testing.T) {
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 176, Autonomy: factory.AutonomyAuto, State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "runvetand", Kind: factory.StageCheck, On: true}, {Name: "proof2", Kind: factory.StageGate, On: true}},
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{
			{Name: "runvetand", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1},
			{Name: "proof2", Kind: factory.StageGate, State: factory.PhasePending},
		}}}
	if err := FinishReport(&it, factory.StateLanded, now); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Phases[1].State != factory.PhaseWaiting || it.Stream.Cur != 1 {
		t.Fatalf("landed with %+v cur %d", it.Stream.Phases[1], it.Stream.Cur)
	}
	if err := PublishItem(&it, false, now); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Phases[1].State != factory.PhaseDone {
		t.Fatalf("publication did not pass the step it answers: %+v", it.Stream.Phases[1])
	}
}

// NOTHING PUBLISHES PAST AN APPROVE STEP NOBODY PASSED: a landed run with one
// still pending before its last step is refused, in Auto and Manual alike,
// and a step switched off is not a wait.
func TestApprovalRefusesARunThatLandedPastAnApproveStep(t *testing.T) {
	landed := func(mode string) factory.Item {
		return factory.Item{ID: 9, Autonomy: mode, State: factory.StateLanded,
			Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "approve", Kind: factory.StageGate, On: true}, {Name: "write", Kind: factory.StageChat, On: true}},
			Stream: &factory.Stream{Cur: 2, Phases: []factory.Phase{
				{Name: "plan", State: factory.PhaseDone},
				{Name: "approve", Kind: factory.StageGate, State: factory.PhasePending},
				{Name: "write", State: factory.PhaseDone},
			}}}
	}
	for _, mode := range []string{factory.AutonomyAuto, factory.AutonomyManual} {
		it := landed(mode)
		err := ApprovalRefused(&it, false)
		if err == nil || !strings.Contains(err.Error(), "an approve step nobody has passed yet: approve") {
			t.Fatalf("%s: %v", mode, err)
		}
		if err := PublishItem(&it, false, time.Now()); err == nil {
			t.Fatalf("%s: published past the approve step", mode)
		}
	}
	off := landed(factory.AutonomyAuto)
	off.Stages[1].On = false
	if err := ApprovalRefused(&off, false); err != nil {
		t.Fatalf("a step switched off held the approval: %v", err)
	}
}
