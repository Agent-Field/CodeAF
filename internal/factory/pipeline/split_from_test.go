package pipeline_test

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// S6: a split job takes the repository's steps; run-as-new still keeps its own.
func TestSplitFromDoesNotPreventPipelineStamping(t *testing.T) {
	f, err := pipeline.Parse("pipeline:\n  issue: [write, test, proof]\n")
	if err != nil {
		t.Fatal(err)
	}
	arrival := factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))
	split := factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: arrival, SplitFrom: 80}
	if !pipeline.StampSteps(&split, f) {
		t.Fatal("split lineage prevented pipeline stamping")
	}
	var names []string
	for _, stage := range split.Stages {
		names = append(names, stage.Name)
	}
	if !reflect.DeepEqual(names, []string{"write", "test", "proof"}) || split.SplitFrom != 80 || split.FromJob != 0 {
		t.Fatalf("split steps=%v split_from=%d from_job=%d", names, split.SplitFrom, split.FromJob)
	}
	copy := factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: arrival, FromJob: 80}
	if pipeline.StampSteps(&copy, f) || !reflect.DeepEqual(copy.Stages, arrival) {
		t.Fatal("run-as-new no longer keeps its source steps")
	}
}
