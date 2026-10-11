package serve_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// dryRunYAML is a checkout whose start rules name one label, a size, a cost
// and a path that always asks. The endpoint test reads it through RecipeAt's
// directory, the same door the pipeline read uses.
const dryRunYAML = `
start:
  labels: [agent-ready]
  max_size: M
  max_cost: 2
  never: [auth/**]
`

func dryRunFloor() []factory.Item {
	return []factory.Item{
		{ID: 2088, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready"}, Cap: 5,
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 1, Repo: "acme/web", State: factory.StateRunning, Kind: factory.KindIssue, Labels: []string{"agent-ready"},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 2090, Repo: "Acme/Web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"Agent-Ready"}, Cap: 5,
			Triage: factory.Triage{Size: "M", Est: 1, TriagedAt: now}},
		{ID: 50, Repo: "acme/api", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready"},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 2077, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindPR, Tier: factory.TierOwner, Labels: []string{"agent-ready"},
			Files:  []factory.FileChange{{Path: "auth/keys.go", Added: 4}},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 60, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"bug"},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 70, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready"}, Cap: 1,
			Triage: factory.Triage{Size: "S", Est: 1.5, TriagedAt: now}},
		{ID: 80, Repo: "acme/web", State: factory.StateQueued, Kind: factory.KindIssue, Labels: []string{"agent-ready"},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 2104, Repo: "Web", Product: "Acme", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready"}, Cap: 5,
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 2105, Repo: "web", Product: "other", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready"},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
	}
}

func TestDryRunAppliesStartToTheBacklog(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte(dryRunYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	seam := factory.FixtureSeam(now)
	seam.RecipeAt = func(repo string) (factory.Recipe, []factory.Problem, string, error) {
		if repo != "acme/web" {
			t.Fatalf("RecipeAt got %q", repo)
		}
		return factory.DefaultRecipe(), nil, dir, nil
	}
	seam.Load = func() (factory.Snapshot, error) {
		return factory.Snapshot{Items: dryRunFloor()}, nil
	}
	body := getDryRun(t, seam, "/v1/repos/acme/web/dryrun")
	want := api.DryRun{
		WouldStart: []int{2088, 2090, 2104},
		WouldAsk: []api.Ask{
			{Job: 2077, Reason: "touches auth/keys.go (auth/**)"},
			{Job: 70, Reason: "over your cap of $1"},
		},
		WouldWait: []api.Ask{{Job: 60, Reason: "no agent-ready label"}},
	}
	if !reflect.DeepEqual(body.DryRun, want) {
		t.Fatalf("dry run = %+v", body.DryRun)
	}
}

func TestDryRunWithNoCheckoutUsesDefaults(t *testing.T) {
	seam := factory.FixtureSeam(now)
	seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) {
		return factory.DefaultRecipe(), nil, "", nil
	}
	seam.Load = func() (factory.Snapshot, error) {
		return factory.Snapshot{Items: []factory.Item{
			{ID: 4, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready"},
				Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		}}, nil
	}
	body := getDryRun(t, seam, "/v1/repos/acme/web/dryrun")
	if len(body.WouldStart) != 0 || len(body.WouldAsk) != 0 {
		t.Fatalf("defaults named a label: %+v", body.DryRun)
	}
	if len(body.WouldWait) != 1 || body.WouldWait[0].Job != 4 || body.WouldWait[0].Reason != "nothing starts on its own" {
		t.Fatalf("wait = %+v", body.WouldWait)
	}
}

func TestDryRunReportsAFileThatCannotBeRead(t *testing.T) {
	seam := factory.FixtureSeam(now)
	seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) {
		return factory.Recipe{}, nil, "", errors.New("pipeline unavailable")
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/repos/acme/web/dryrun", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	serve.New(seam, config).ServeHTTP(w, r)
	checkJSON(t, w, http.StatusInternalServerError)
}

func getDryRun(t *testing.T, seam factory.Seam, path string) struct {
	Factory api.Factory `json:"factory"`
	api.DryRun
} {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	serve.New(seam, config).ServeHTTP(w, r)
	checkJSON(t, w, http.StatusOK)
	var body struct {
		Factory api.Factory `json:"factory"`
		api.DryRun
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Factory.Address != config.Address || body.WouldStart == nil || body.WouldAsk == nil {
		t.Fatalf("envelope = %s", w.Body.Bytes())
	}
	return body
}
