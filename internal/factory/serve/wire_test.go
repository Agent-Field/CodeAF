package serve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestPipelineRouteReturnsTheFile(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	repoText := "" +
		"start:\n" +
		"  labels: [agent-ready]\n" +
		"  max_size: M\n" +
		"  max_cost: 2\n" +
		"size:\n" +
		"  pr_lines: [40, 200]\n" +
		"  issue: label\n" +
		"steps:\n" +
		"  plan:\n" +
		"    prompt: name the files\n" +
		"    until: the plan names its files\n"
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte(repoText), 0o644); err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(home, ".codeaf", "factory.yaml")
	if err := os.MkdirAll(filepath.Dir(personal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(personal, []byte("start:\n  max_cost: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seam := factory.FixtureSeam(now)
	seam.RecipeAt = func(repo string) (factory.Recipe, []factory.Problem, string, error) {
		if repo != "acme/web" {
			t.Fatalf("RecipeAt got %q", repo)
		}
		return factory.DefaultRecipe(), nil, dir, nil
	}
	cfg := config
	cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) {
		return pipeline.LoadMerged(dir, personal)
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/repos/acme/web/pipeline", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	serve.New(seam, cfg).ServeHTTP(w, r)
	checkJSON(t, w, http.StatusOK)
	var body struct {
		api.Pipeline
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Origin != "working tree" || !body.Personal || !strings.Contains(body.Text, "the plan names its files") || !strings.Contains(body.PersonalText, "max_cost: 1") {
		t.Fatalf("file = origin %q personal %v text %q personal %q", body.Origin, body.Personal, body.Text, body.PersonalText)
	}
	if body.Start == nil || len(body.Start.Labels) != 1 || body.Start.Labels[0] != "agent-ready" || body.Start.MaxCost != 1 || body.Start.MaxSize != "M" {
		t.Fatalf("start = %+v", body.Start)
	}
	if body.Size == nil || body.Size.PRLines != [2]int{40, 200} || body.Size.Issue != "label" {
		t.Fatalf("size = %+v", body.Size)
	}
	var plan api.StepDef
	for _, step := range body.Steps {
		if step.Name == "plan" {
			plan = step
		}
	}
	if plan.Until != "done" || plan.UntilSentence != "the plan names its files" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestAboutIsTheJoinPreview(t *testing.T) {
	remote := func(cfg serve.Config, path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		serve.New(factory.FixtureSeam(now), cfg).ServeHTTP(w, r)
		return w
	}
	w := remote(config, "/v1/about")
	checkJSON(t, w, http.StatusOK)
	type aboutBody struct {
		Name          string `json:"name"`
		CodeafVersion string `json:"codeaf_version"`
		Contract      int    `json:"contract"`
		Needs         string `json:"needs"`
		Access        string `json:"access"`
		PollingEvery  string `json:"polling_every"`
		ServedBy      string `json:"served_by"`
	}
	var about aboutBody
	if err := json.Unmarshal(w.Body.Bytes(), &about); err != nil {
		t.Fatal(err)
	}
	if about.Name != "agentfield" || about.CodeafVersion != "test" || about.Contract != 1 || about.Needs != "token" || about.Access != "token" || about.PollingEvery != "1m0s" || about.ServedBy != "factory" {
		t.Fatalf("about = %+v", about)
	}
	if strings.Contains(w.Body.String(), "jobs") || strings.Contains(w.Body.String(), "acme") {
		t.Fatalf("about named the floor: %s", w.Body.String())
	}
	denied := remote(config, "/v1/queue")
	checkJSON(t, denied, http.StatusUnauthorized)

	open := config
	open.AccessToken = ""
	open.PollingEvery = 0
	w = remote(open, "/v1/about")
	checkJSON(t, w, http.StatusOK)
	about = aboutBody{}
	if err := json.Unmarshal(w.Body.Bytes(), &about); err != nil {
		t.Fatal(err)
	}
	if about.Needs != "none" || about.Access != "loopback" || about.ServedBy != "tui" || about.PollingEvery != "" {
		t.Fatalf("window = %+v", about)
	}

	device := config
	device.Needs = "device"
	w = remote(device, "/v1/about")
	checkJSON(t, w, http.StatusOK)
	about = aboutBody{}
	if err := json.Unmarshal(w.Body.Bytes(), &about); err != nil {
		t.Fatal(err)
	}
	if about.Needs != "device" || about.Access != "token" {
		t.Fatalf("device = %+v", about)
	}

	tail := config
	tail.AccessToken = ""
	tail.PollingEvery = 0
	tail.Address = "http://192.0.2.10:7420"
	w = remote(tail, "/v1/about")
	if w.Code != http.StatusOK {
		t.Fatalf("tailnet status %d body %s", w.Code, w.Body.Bytes())
	}
	about = aboutBody{}
	if err := json.Unmarshal(w.Body.Bytes(), &about); err != nil {
		t.Fatal(err)
	}
	if about.Name != "agentfield" || about.Needs != "none" || about.Access != "tailnet" || about.ServedBy != "factory" {
		t.Fatalf("tailnet = %+v", about)
	}
}
