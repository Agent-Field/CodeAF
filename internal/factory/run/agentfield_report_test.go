package run

import (
	"strings"
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

// AN ANSWER ON ITS WAY IS NOT ASKED AGAIN, AND A REFUSED ONE IS. A person
// answered the run's pause on the factory; the runner's next report still
// reads the pause from before it carried the answer, and that reading must not
// put the question back in Needs you. When the control plane refuses the
// answer, the runner says so and the question stands again, with the words.
func TestAnAnswerOnItsWayIsNotAskedAgainAndARefusedOneIs(t *testing.T) {
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	wait := &factory.AgentFieldWait{Execution: "exec-plan", Step: "build", RequestID: "plan-r1", Question: "Approve the plan?", Body: "# Plan"}
	server := factory.Item{ID: 9, State: factory.StateRunning, Stream: &factory.Stream{},
		Stages:          []factory.Stage{{Name: "build", Kind: factory.StageAgentField, On: true}},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {ExecutionID: "exec-root", Decisions: []factory.AgentFieldDecision{{RequestID: "plan-r1", Execution: "exec-plan", Decision: "approved", By: "sam", At: at}}}}}
	stale := factory.RunReport{AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {ExecutionID: "exec-root", Status: "waiting", Waiting: wait}}}
	if err := ApplyReport(&server, stale, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if server.State == factory.StateNeedsYou || server.QKind == factory.QKindAgentField {
		t.Fatalf("a pause the person already answered was asked again: state %s qkind %q", server.State, server.QKind)
	}
	if got := server.AgentFieldSteps["build"].Decisions; len(got) != 1 || got[0].By != "sam" {
		t.Fatalf("the factory's decision was not kept: %+v", got)
	}
	refused := factory.RunReport{AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {ExecutionID: "exec-root", Status: "waiting", Waiting: wait,
		Refused: &factory.AgentFieldRefusal{RequestID: "plan-r1", Execution: "exec-plan", Words: "this node does not own the run", At: at.Add(2 * time.Second)}}}}
	if err := ApplyReport(&server, refused, at.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if server.State != factory.StateNeedsYou || server.QKind != factory.QKindAgentField {
		t.Fatalf("a refused answer left the question down: state %s qkind %q", server.State, server.QKind)
	}
	said := false
	for _, l := range server.Stream.Log {
		said = said || strings.Contains(l.Text, "this node does not own the run")
	}
	if !said {
		t.Fatalf("the refusal was not said on the job: %+v", server.Stream.Log)
	}
}
