package api

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// AN AGENTFIELD STEP'S DETAILS READ AS ITS LABEL, NEVER ITS REASONER'S ID
// (live e2e round 9: a step's details showed `swe-planner.build_issue`). Any
// other note, and every other step's, is the step's own words.
func TestAnAgentFieldStepsNoteReadsAsItsLabelNotItsReasoner(t *testing.T) {
	j := &Job{Steps: []Step{
		{StepDef: StepDef{Name: "build", Label: "Build on AgentField", Kind: string(factory.StageAgentField), Target: "swe-planner.build_issue", Ask: "swe-planner.build_issue"}, Note: "swe-planner.build_issue"},
		{StepDef: StepDef{Name: "coder", Kind: string(factory.StageChat)}, RunKind: string(factory.StageAgentField), Note: "the node went away"},
		{StepDef: StepDef{Name: "write", Label: "Write", Kind: string(factory.StageChat)}, Note: "a.b"},
	}}
	PlainStepNotes(j)
	for i, want := range []string{"Build on AgentField", "the node went away", "a.b"} {
		if got := j.Steps[i].Note; got != want {
			t.Errorf("step %s note = %q, want %q", j.Steps[i].Name, got, want)
		}
	}
}
