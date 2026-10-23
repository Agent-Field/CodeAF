package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"net/http/httptest"
	"testing"
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
