package run

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A RUNNER'S RESHAPED STEPS KEEP THE PERSON'S MODEL PICKS. The pick is the
// person's and only the factory takes it; a runner older than picks reshapes
// the steps without one, which must not put a picked step back on the job's
// model.
func TestARunnersReshapeKeepsAPersonsModelPick(t *testing.T) {
	it := &factory.Item{
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}, {Name: "review", Kind: factory.StageChat, On: true, Model: "vendor/picked"}},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", State: factory.PhaseRunning}, {Name: "review", State: factory.PhasePending}}},
	}
	change := &factory.PipelineChange{
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}, {Name: "docs", Kind: factory.StageChat, On: true}, {Name: "review", Kind: factory.StageChat, On: true}},
		Phases: []factory.Phase{{Name: "write"}, {Name: "docs"}, {Name: "review"}},
	}
	if err := applyPipeline(it, change); err != nil {
		t.Fatal(err)
	}
	if it.Stages[2].Name != "review" || it.Stages[2].Model != "vendor/picked" {
		t.Fatalf("the reshaped steps are %+v", it.Stages)
	}
}
