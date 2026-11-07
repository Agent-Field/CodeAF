package run

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE PAUSE IS THE CONTROL PLANE'S AND THE JOB ONLY SHADOWS IT: a run that
// waits puts the job in Needs you with the reasoner's question; the same wait
// read again changes nothing; a run no longer waiting (answered here, in hax,
// on the control plane) takes the question down by itself.
func TestAJobShadowsItsAgentFieldRunsPause(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 3, State: factory.StateRunning, AgentFieldSteps: map[string]*factory.AgentFieldRun{
		"build": {ExecutionID: "exec-root", Waiting: &factory.AgentFieldWait{Execution: "exec-root", Step: "build", RequestID: "swe-plan-exec-root-r1",
			Question: "Approve the plan for #12: Add Len?", Body: "# Plan\n\n1. add Len", Kind: "plan_review", At: now}},
	}}
	if !SyncAgentFieldWait(&it, now) {
		t.Fatal("a waiting run did not reach Needs you")
	}
	if it.State != factory.StateNeedsYou || it.QKind != factory.QKindAgentField || it.Question != "Approve the plan for #12: Add Len?" {
		t.Fatalf("job = %s %q %q", it.State, it.QKind, it.Question)
	}
	if SyncAgentFieldWait(&it, now) {
		t.Fatal("the same wait changed the job twice")
	}
	// Answered elsewhere: the run's next reading has no wait.
	it.AgentFieldSteps["build"].Waiting = nil
	if !SyncAgentFieldWait(&it, now.Add(time.Second)) || it.State != factory.StateRunning || it.Question != "" || it.QKind != "" {
		t.Fatalf("an answered pause stayed: %s %q %q", it.State, it.QKind, it.Question)
	}
	// THE ANSWER IS THE RUN'S RECORDED DECISION, NEVER A LOG LINE: who
	// answered, how and with what words are in agentfield.decisions[], and a
	// reader draws them from there. Taking the question down writes nothing
	// to the job's log that a reader would have to match.
	for _, l := range it.Stream.Log {
		if l.Tone != "ask" {
			t.Fatalf("taking an answered pause down wrote a log line: %+v", l)
		}
	}
	// A wait never takes a job that has ended back to Needs you.
	it.State = factory.StateShipped
	it.AgentFieldSteps["build"].Waiting = &factory.AgentFieldWait{Execution: "exec-root", Question: "again?"}
	if SyncAgentFieldWait(&it, now) || it.State != factory.StateShipped {
		t.Fatalf("an ended job went back to %s", it.State)
	}
	// A question of the job's own is never cleared by a run with no wait.
	own := factory.Item{State: factory.StateNeedsYou, Question: "which branch?", QKind: "step"}
	if SyncAgentFieldWait(&own, now) || own.Question != "which branch?" {
		t.Fatal("a step's question was taken down by an AgentField run")
	}
}
