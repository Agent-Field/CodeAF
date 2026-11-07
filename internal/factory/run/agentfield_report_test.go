package run

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A RUN_ON JOB'S RUN IS THE FACTORY'S TO KEEP: what the worker's runner
// recorded of the AgentField run travels in its report and lands on the
// factory's copy, so a runner that takes the job again follows that run
// instead of dispatching a second one.
func TestARunOnJobsRunTravelsInTheReport(t *testing.T) {
	prev := factory.Item{ID: 7, RunOn: "support.handle_ticket", State: factory.StateRunning, Stream: &factory.Stream{}}
	cur := prev
	cur.AgentField = &factory.AgentFieldRun{Target: "support.handle_ticket", ExecutionID: "exec-root", RunID: "run-1", Status: "running"}
	report, ok := Diff(prev, cur)
	if !ok || report.AgentField == nil || report.AgentField.ExecutionID != "exec-root" {
		t.Fatalf("the report does not carry the run: %+v", report.AgentField)
	}
	server := prev
	if err := ApplyReport(&server, report, time.Now()); err != nil {
		t.Fatal(err)
	}
	if server.AgentField == nil || server.AgentField.RunID != "run-1" {
		t.Fatalf("the factory's copy did not keep the run: %+v", server.AgentField)
	}
	if again, _ := Diff(cur, cur); again.AgentField != nil {
		t.Fatal("an unchanged run was reported again")
	}
}
