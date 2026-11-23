package run

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A refused newer attempt withdraws an earlier success, never an earlier
// incomplete report: a blocker or a "needs you" is not a success to withdraw.
func TestRejectReportWithdrawsOnlyADoneReport(t *testing.T) {
	done := newStageDoor()
	done.report = &factory.StageResult{Done: true, Output: "fixed"}
	done.RejectReport()
	if !done.report.Checkpoint {
		t.Fatal("a stale success still stands")
	}
	blocked := newStageDoor()
	blocked.report = &factory.StageResult{Done: false, Output: "the test still fails"}
	blocked.RejectReport()
	if blocked.report.Checkpoint {
		t.Fatal("an incomplete report was withdrawn as if it were a success")
	}
	newStageDoor().RejectReport() // no report: nothing to withdraw
}
