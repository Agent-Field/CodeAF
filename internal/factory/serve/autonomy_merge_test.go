package serve

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func TestExplicitAutonomyModesDoNotGrantAutomaticMerge(t *testing.T) {
	h := &Handler{seam: factory.Seam{RecipeAt: func(string) (factory.Recipe, []factory.Problem, string, error) {
		return factory.Recipe{Merge: factory.MergePolicy{Auto: true, Method: factory.MergeMethodRebase}}, nil, "", nil
	}}}
	r := httptest.NewRequest("POST", "/v1/jobs/1/approve", nil)
	for _, mode := range []string{"auto", "manual"} {
		if policy := h.pinnedMerge(r, factory.Item{Autonomy: mode}); policy != nil {
			t.Fatalf("%s granted merge authority", mode)
		}
	}
}

// THE MERGE A JOB PINS IS ITS OWN FILE'S: a job decided on main's file pins
// main's merge settings although the factory now keeps a copy that merges by
// itself, and a job that carries the copy pins the copy's.
func TestPinnedMergeIsTheJobsOwnFile(t *testing.T) {
	copied := &factory.PipelineCopy{Repo: "acme/api", Text: "merge:\n  auto: true\n  method: squash\n", By: "test-owner"}
	read := func(withCopy bool) (pipeline.File, error) {
		if withCopy {
			return pipeline.CopyFile(*copied)
		}
		return pipeline.Parse("merge:\n  auto: false\n")
	}
	h := &Handler{seam: factory.Seam{RecipeAt: func(string) (factory.Recipe, []factory.Problem, string, error) {
		return factory.DefaultRecipe(), nil, "", nil
	}},
		config: Config{JobPipeline: func(_ context.Context, it factory.Item, _ string) (pipeline.File, error) {
			return pipeline.JobFile(it, "", read)
		}}}
	r := httptest.NewRequest("POST", "/v1/jobs/1/approve", nil)
	if policy := h.pinnedMerge(r, factory.Item{Repo: "acme/api", PipelineDecided: true}); policy != nil {
		t.Fatalf("a job decided on main pinned %+v", policy)
	}
	if policy := h.pinnedMerge(r, factory.Item{Repo: "acme/api", PipelineCopy: copied, PipelineDecided: true}); policy == nil || !policy.Auto {
		t.Fatalf("a job carrying the copy pinned %+v", policy)
	}
}
