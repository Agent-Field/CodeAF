package serve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// A BACKLOG JOB SAYS WHICH PROTECTED PATHS IT NAMES: the ask-first path lines
// and never paths of the rules it would start under, matched against the
// paths its issue, comments and files name, as a start matches them. An empty
// list is a job that names none; absent is a job nobody judged (not in the
// backlog, or rules nobody could read).
func TestTheQueueSaysWhichProtectedPathsABacklogJobNames(t *testing.T) {
	seam := factory.FixtureSeam(now)
	seam.RecipeAt = func(repo string) (factory.Recipe, []factory.Problem, string, error) {
		return factory.DefaultRecipe(), nil, "", nil
	}
	seam.Load = func() (factory.Snapshot, error) {
		return factory.Snapshot{Items: []factory.Item{
			{ID: 1, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Title: "Rotate keys", Body: "The bug is in infra/terraform/main.tf and docs/a.md"},
			{ID: 2, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Title: "Typo", Body: "README.md says teh"},
			{ID: 3, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindPR, Body: "infra/x.tf"},
			{ID: 6, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Title: "CI", Comments: []factory.Comment{{Body: "see .github/workflows/ci.yml"}}},
			{ID: 4, Repo: "acme/web", State: factory.StateRunning, Kind: factory.KindIssue, Body: "infra/x.tf"},
			{ID: 5, Repo: "acme/api", State: factory.StateNew, Kind: factory.KindIssue, Body: "infra/x.tf"},
		}}, nil
	}
	cfg := config
	cfg.Pipeline = func(_ context.Context, repo, _ string) (pipeline.File, error) {
		if repo == "acme/api" {
			return pipeline.File{}, context.DeadlineExceeded
		}
		f := pipeline.Defaults()
		f.Start.AskFirst = []string{"touches infra/", "path: billing/", "Ask before deleting files"}
		f.Start.Never = []string{".github/workflows/"}
		return f, nil
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/queue", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	serve.New(seam, cfg).ServeHTTP(w, r)
	checkJSON(t, w, http.StatusOK)
	var q api.Queue
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	if len(q.Jobs) != 5 {
		t.Fatalf("jobs = %d", len(q.Jobs))
	}
	got := map[string]*api.JobProtected{}
	for _, j := range q.Jobs {
		got[j.JobID] = j.Protected
	}
	want := map[string][]string{"web#1": {"infra/"}, "web#2": {}, "web#6": {".github/workflows/"}}
	for id, paths := range want {
		p := got[id]
		if p == nil || len(p.Paths) != len(paths) || (len(paths) > 0 && p.Paths[0] != paths[0]) {
			t.Errorf("job %s protected = %+v, want %v", id, p, paths)
		}
	}
	for _, id := range []string{"web#4", "api#5"} {
		if got[id] != nil {
			t.Errorf("job %s was judged: %+v", id, got[id])
		}
	}
}
