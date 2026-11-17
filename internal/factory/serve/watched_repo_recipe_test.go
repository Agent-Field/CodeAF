package serve

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A REPOSITORY JUST TURNED ON HAS NO JOBS, AND ITS FIRST JOB MUST STILL BE
// STARTABLE: the queue says what its pipeline says, default budget included, so
// the app can say what Start now may cost. A read that fails says nothing,
// rather than a figure nobody chose, and the repository stays listed.
func TestAWatchedRepositoryWithNoJobsSaysItsPipeline(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya"}
	h, now, _ := identityHandler(t, g)
	h.seam.Load = func() (factory.Snapshot, error) { return factory.Snapshot{Now: *now}, nil }
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/new", "acme/broken"}); err != nil {
		t.Fatal(err)
	}
	h.config.Store = st
	h.seam.RecipeAt = func(repo string) (factory.Recipe, []factory.Problem, string, error) {
		if repo == "acme/broken" {
			return factory.Recipe{}, nil, "", errors.New("the file cannot be read")
		}
		return factory.Recipe{DefaultBudget: 7, Stages: []factory.Stage{{Name: "write"}}}, nil, "", nil
	}
	q := queueAs(t, h, "floor-secret", false)
	if len(q.Repos) != 2 {
		t.Fatalf("repos = %+v", q.Repos)
	}
	for _, repo := range q.Repos {
		switch repo.Name {
		case "acme/new":
			if repo.Pipeline == nil || repo.Pipeline.DefaultBudget != 7 || len(repo.Pipeline.Steps) != 1 {
				t.Fatalf("a watched repository with no jobs said no pipeline: %+v", repo.Pipeline)
			}
		case "acme/broken":
			if repo.Pipeline != nil && repo.Pipeline.DefaultBudget != 0 {
				t.Fatalf("a failed read invented a budget: %+v", repo.Pipeline)
			}
		}
	}
}
