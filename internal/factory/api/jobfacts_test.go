package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A JOB SAYS WHERE IT CAME FROM, WHY IT STARTED AND HOW FAR IT IS, computed in
// internal/factory and carried here; a client seam reads the key and the rule
// back.
func TestAJobCarriesItsSourceRouteAndStepCount(t *testing.T) {
	it := factory.Item{
		ID: 7, Origin: "jira", SourceRef: "jira:ENG-142", URL: "https://acme.atlassian.net/browse/ENG-142", Repo: "acme/api", Kind: factory.KindIssue,
		Route:  "small bugs",
		Judged: map[string]factory.RouteAnswer{"small bugs": {Yes: true, Why: "a one-line fix", Model: "v/cheap", Key: "k"}, "other": {Why: "x"}},
		Stages: []factory.Stage{{Name: "plan", On: true}, {Name: "write", On: true}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseRunning}}},
	}
	j := FromItem(it)
	if j.Source == nil || *j.Source != (factory.JobSourceFacts{Name: "jira", Ref: "ENG-142", URL: it.URL}) {
		t.Fatalf("source = %+v", j.Source)
	}
	if j.RepoURL != "https://github.com/acme/api" {
		t.Fatalf("repo_url = %q", j.RepoURL)
	}
	if j.Route == nil || j.Route.Rule != "small bugs" || j.Route.Answer == nil || *j.Route.Answer != (RouteAnswer{Yes: true, Why: "a one-line fix", Model: "v/cheap"}) {
		t.Fatalf("route = %+v", j.Route)
	}
	if j.StepsDone != 1 || j.StepsTotal != 2 {
		t.Fatalf("steps = %d of %d", j.StepsDone, j.StepsTotal)
	}
	back := ToItem(j)
	if back.SourceRef != it.SourceRef || back.Route != it.Route || back.Judged["small bugs"].Why != "a one-line fix" {
		t.Fatalf("read back: ref %q route %q judged %+v", back.SourceRef, back.Route, back.Judged)
	}
	raw, _ := json.Marshal(FromItem(factory.Item{ID: 1}))
	if !strings.Contains(string(raw), `"steps_done":0`) || !strings.Contains(string(raw), `"steps_total":0`) || strings.Contains(string(raw), `"route"`) || strings.Contains(string(raw), `"runs_on"`) {
		t.Fatalf("an empty job's wire: %s", raw)
	}
}

// RUNS_ON IS THE RESOLUTION THE RUNNER OPENS THE STEP ON, said on reads only:
// a job's own (its current or next conversation) and each unfinished
// conversation step's, never on a check step or a finished one, and never by
// FromItem, whose steps a worker's report carries to a factory that may not
// know the field.
func TestRunsOnSaysTheOneResolutionOnReadsOnly(t *testing.T) {
	it := factory.Item{
		ID: 3, FactoryModel: "v/house",
		Stages: []factory.Stage{{Name: "plan", On: true}, {Name: "test", Kind: factory.StageCheck, On: true}, {Name: "review", On: true, Effort: "cheap"}, {Name: "proof", On: true, Model: "v/pinned"}},
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "test", Kind: factory.StageCheck, State: factory.PhasePending}, {Name: "review", State: factory.PhasePending}, {Name: "proof", State: factory.PhasePending}}},
	}
	j := FromItem(it)
	if j.RunsOn != nil {
		t.Fatal("FromItem said runs_on")
	}
	for _, s := range j.Steps {
		if s.RunsOn != nil {
			t.Fatalf("FromItem said %s's runs_on", s.Name)
		}
	}
	ModelsIn(&j, it, factory.ModelDefaults{Runner: factory.RunnerModels{Effort: map[string]string{"cheap": "v/small"}, Worker: "v/worker"}})
	if j.RunsOn == nil || *j.RunsOn != (factory.ModelChoice{Model: "v/small", From: factory.ModelFromEffort}) {
		t.Fatalf("job runs_on = %+v, want review's", j.RunsOn)
	}
	want := map[string]*factory.ModelChoice{
		"plan":   nil,
		"test":   nil,
		"review": {Model: "v/small", From: factory.ModelFromEffort},
		"proof":  {Model: "v/pinned", From: factory.ModelFromStep},
	}
	for _, s := range j.Steps {
		w := want[s.Name]
		if (w == nil) != (s.RunsOn == nil) || w != nil && *w != *s.RunsOn {
			t.Errorf("%s runs_on = %+v, want %+v", s.Name, s.RunsOn, w)
		}
	}
}
