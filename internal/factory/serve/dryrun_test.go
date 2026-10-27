package serve_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
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
		{ID: 2088, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready", "size:S"}, Cap: 2,
			Triage: factory.Triage{Size: "XL", Est: 99, TriagedAt: now}},
		{ID: 1, Repo: "acme/web", State: factory.StateRunning, Kind: factory.KindIssue, Labels: []string{"agent-ready"},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 2090, Repo: "Acme/Web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"Agent-Ready", "Size/M"}, Cap: 2,
			Triage: factory.Triage{Size: "M", Est: 1, TriagedAt: now}},
		{ID: 50, Repo: "acme/api", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready"},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 2077, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindPR, Tier: factory.TierOwner, Labels: []string{"agent-ready", "size:s"}, Cap: 2,
			Files:  []factory.FileChange{{Path: "auth/keys.go", Added: 4}},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 60, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"bug"},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 70, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready", "size:S"}, Cap: 3,
			Triage: factory.Triage{Size: "S", Est: 1.5, TriagedAt: now}},
		{ID: 80, Repo: "acme/web", State: factory.StateQueued, Kind: factory.KindIssue, Labels: []string{"agent-ready"},
			Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: now}},
		{ID: 2104, Repo: "Web", Product: "Acme", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready", "size:S"}, Cap: 2,
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
			{Job: 70, Reason: "job budget $3 is over $2"},
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

// A dry run is a preview of intake, so it must say what DecideJob says for
// the same items: explicit size labels and the job budget decide, and the
// triage guess (size or estimate) never grants a start.
func TestDryRunMatchesDecideJobForTheSameItems(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte(dryRunYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	guess := factory.Triage{Size: "S", Est: 0.1, TriagedAt: now}
	ready := func(id int, cap float64, labels ...string) factory.Item {
		return factory.Item{ID: id, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue,
			Labels: append([]string{"agent-ready"}, labels...), Cap: cap, Triage: guess}
	}
	items := []factory.Item{
		ready(1, 1, "size:S"),           // starts
		ready(2, 1),                     // no label: M, whatever the S guess says
		ready(3, 3, "size:S"),           // over the $2 ceiling despite a $0.10 estimate
		ready(4, 1, "size:XL"),          // explicit oversize
		ready(5, 1, "size:S", "size/L"), // several: the largest wins
		ready(6, 1, "Size/M"),           // slash spelling, any case
		ready(7, 0, "size:s"),           // no cap: the default budget applies
		{ID: 8, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"bug", "size:S"}, Triage: guess},
	}
	seam := factory.FixtureSeam(now)
	seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) {
		return factory.DefaultRecipe(), nil, dir, nil
	}
	seam.Load = func() (factory.Snapshot, error) { return factory.Snapshot{Items: items}, nil }
	body := getDryRun(t, seam, "/v1/repos/acme/web/dryrun")

	file, err := pipeline.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var start []int
	ask := map[int]string{}
	var wait []int
	for _, it := range items {
		switch d := file.DecideJob(it, intake.Job(it).Paths); {
		case d.Start:
			start = append(start, it.ID)
		case d.Ask:
			ask[it.ID] = strings.Join(d.Reasons, " · ")
		default:
			wait = append(wait, it.ID)
		}
	}
	if !reflect.DeepEqual(body.WouldStart, start) {
		t.Fatalf("would start %v, DecideJob starts %v", body.WouldStart, start)
	}
	for _, a := range body.WouldAsk {
		if ask[a.Job] != a.Reason {
			t.Fatalf("job %d asks %q, DecideJob says %q", a.Job, a.Reason, ask[a.Job])
		}
		delete(ask, a.Job)
	}
	if len(ask) != 0 {
		t.Fatalf("DecideJob asks for jobs the dry run did not: %v", ask)
	}
	if len(wait) != 1 || len(body.WouldWait) != 1 || body.WouldWait[0].Job != 8 {
		t.Fatalf("wait = %+v, want %v", body.WouldWait, wait)
	}
	// The anchors, so parity cannot pass by both sides being wrong together.
	if !reflect.DeepEqual(start, []int{1, 2, 6}) {
		t.Fatalf("DecideJob starts %v", start)
	}
	for id, want := range map[int]string{3: "job budget $3 is over $2", 4: "this one is XL", 5: "this one is L", 7: "job budget"} {
		found := false
		for _, a := range body.WouldAsk {
			found = found || (a.Job == id && strings.Contains(a.Reason, want))
		}
		if !found {
			t.Fatalf("job %d: no ask containing %q in %+v", id, want, body.WouldAsk)
		}
	}
}
