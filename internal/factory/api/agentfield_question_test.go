package api

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A run's pause for a person reads as the job's question with the WHOLE
// plan, its round, the plan before it and the words that asked for this one;
// it comes from the agentfield step, which is itself on the job's steps, and
// its person can approve, ask for changes or reject.
func TestAnAgentFieldPauseReadsAsTheJobsQuestionWithTheWholePlan(t *testing.T) {
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	run := &factory.AgentFieldRun{
		ExecutionID: "exec-root",
		Stages:      []factory.Stage{{Name: "plan", Kind: factory.StageCheck, Label: "Plan"}},
		Phases:      []factory.Phase{{Name: "plan"}},
		Waiting: &factory.AgentFieldWait{Execution: "exec-root", Step: "build", Node: "swe-planner", RequestID: "swe-plan-exec-root-r2",
			Question: "Approve the plan for #12: Add Len?", Body: "# Plan v2\n\nuse sqlite", Kind: "plan_review", At: at},
		Asks:      []factory.AgentFieldAsk{{RequestID: "swe-plan-exec-root-r1", Body: "# Plan v1"}, {RequestID: "swe-plan-exec-root-r2", Body: "# Plan v2\n\nuse sqlite"}},
		Decisions: []factory.AgentFieldDecision{{RequestID: "swe-plan-exec-root-r1", Decision: "request_changes", Words: "use sqlite"}},
	}
	it := factory.Item{ID: 4, State: factory.StateNeedsYou, QKind: factory.QKindAgentField, Question: "Approve the plan for #12: Add Len?",
		Stages:          []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "swe-planner.build_issue", On: true}},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": run}}
	job := FromItem(it)
	if job.NeedsYou == nil || job.NeedsYou.Question == nil {
		t.Fatalf("no question: %+v", job.NeedsYou)
	}
	q := job.NeedsYou.Question
	if q.Kind != factory.QKindAgentField || q.ID != "q4:swe-plan-exec-root-r2" || q.Body != "# Plan v2\n\nuse sqlite" || q.Round != 2 ||
		q.PreviousBody != "# Plan v1" || q.Note != "use sqlite" || q.ApprovalKind != "plan_review" || q.Node != "swe-planner" || q.FromStep != "build" {
		t.Fatalf("question = %+v", q)
	}
	if job.NeedsYou.WhoCanAnswer != factory.WhoJobPerson {
		t.Fatalf("who = %q; only the job's person answers a run's approval", job.NeedsYou.WhoCanAnswer)
	}
	names := map[string]string{}
	for _, s := range job.Steps {
		names[s.Name] = s.Parent
	}
	if _, ok := names[q.FromStep]; !ok {
		t.Fatalf("from_step %q is not one of the job's steps %v", q.FromStep, names)
	}
	if names["plan"] != "build" {
		t.Fatalf("the run's step is not under its agentfield step: %v", names)
	}
	can := CanFor(it, "")
	if !can.Approve || !can.RequestChanges || !can.RejectPlan {
		t.Fatalf("can = %+v", can)
	}
	// Answered: no wait, no verbs.
	run.Waiting = nil
	it.State, it.QKind, it.Question = factory.StateRunning, "", ""
	if c := CanFor(it, ""); c.RequestChanges || c.RejectPlan {
		t.Fatalf("an answered pause still offers %+v", c)
	}
}
