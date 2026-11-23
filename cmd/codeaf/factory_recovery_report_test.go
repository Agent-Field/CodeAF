package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestVerificationRefusalKeepsItsNewRecoveryAssessment(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("CHECK_EXIT=7 cargo test --test cli regression")
	parent := &withdrawingReportDoor{}
	door := &receiptStageDoor{StageDoor: parent, file: file, dir: root, repair: true}
	t.Cleanup(door.closeVerification)
	report := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "checked"}}
	for range stageVerificationRejectLimit {
		if err := door.Report(t.Context(), report); err == nil {
			t.Fatal("a failed check was accepted as repaired")
		}
	}
	if parent.last.Checkpoint || parent.last.Repair == nil || parent.last.Repair.Decision != "needs_you" {
		t.Fatalf("report rejection withdrew its own newer recovery assessment: %+v", parent.last)
	}
	// The tool also withdraws candidates after a refused submission. This
	// must not withdraw the recovery owner's newer terminal assessment.
	door.RejectReport()
	if parent.last.Checkpoint || parent.last.Done {
		t.Fatal("the tool rejection withdrew the terminal recovery assessment")
	}
}
