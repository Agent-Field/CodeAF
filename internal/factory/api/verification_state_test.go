package api

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestVerificationRuntimeSurvivesWorkerAndReaderCodecs(t *testing.T) {
	status := &factory.StepVerification{State: "running", Checks: []factory.RunningCheck{{ID: "receipt-1", Purpose: "session suite", StartedAt: time.Now().UTC()}}}
	r := factory.RunReport{Stages: []factory.StageReport{{Name: "test", State: factory.PhaseRunning, Round: 4, RoundLimit: 6, Verification: status}}}
	back := ToWorkerReport(FromRunReport(r)).Stages[0]
	if back.RoundLimit != 6 || back.Verification.Checks[0].ID != "receipt-1" {
		t.Fatal("worker codec lost running work")
	}
	s := runStep(Step{StepDef: StepDef{Name: "test"}}, factory.Phase{Name: "test", State: factory.PhaseRunning, RoundLimit: 6, Verification: status})
	j := Job{State: StateRunning, Steps: []Step{s}}
	PresentJob(&j)
	if j.StatePresentation.Kind != "verifying" || j.StatePresentation.Title != "Waiting for checks" {
		t.Fatal("running check looks idle or failed")
	}
	ph := toPhase(s)
	if ph.RoundLimit != 6 || ph.Verification.Checks[0].Purpose != "session suite" {
		t.Fatal("reader codec lost runtime metadata")
	}
}
