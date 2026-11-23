package factory

import "testing"

// ONE STEP COUNT: the run's phases with a skipped one counted for neither
// side, then a switched-on step the run has not reached. The Brief reads the
// same count, so the two cannot disagree.
func TestStepCountIsTheOneCountTheBriefReadsToo(t *testing.T) {
	it := Item{
		Stages: []Stage{{Name: "plan", On: true}, {Name: "write", On: true}, {Name: "test", On: true}, {Name: "review", On: false}, {Name: "proof", On: true}, {Name: "late", On: true}},
		Stream: &Stream{Cur: 2, Phases: []Phase{
			{Name: "plan", State: PhaseDone}, {Name: "write", State: PhaseDone}, {Name: "test", State: PhaseRunning},
			{Name: "review", State: PhaseSkipped}, {Name: "proof", State: PhasePending},
		}},
	}
	done, total := StepCount(it)
	if done != 2 || total != 5 {
		t.Fatalf("StepCount = %d of %d, want 2 of 5", done, total)
	}
	stub := BriefStub(it)
	if stub.StepsDone != done || stub.StepsTotal != total {
		t.Fatalf("the Brief says %d of %d, the job %d of %d", stub.StepsDone, stub.StepsTotal, done, total)
	}
	if d, n := StepCount(Item{Stages: []Stage{{Name: "plan", On: true}, {Name: "x", On: false}}}); d != 0 || n != 1 {
		t.Fatalf("a job not yet run: %d of %d, want 0 of 1", d, n)
	}
}

// THE MODEL CHAIN, rung by rung: a person's pick, one model, effort, the
// factory's default (the queued one over the current one), the worker model,
// the launch. Each answer names the rung.
func TestStepModelTriesEachRungInOrder(t *testing.T) {
	runner := RunnerModels{Effort: map[string]string{"cheap": "v/small"}, Worker: "v/worker", Launch: "v/launch"}
	for name, c := range map[string]struct {
		st   Stage
		it   Item
		d    ModelDefaults
		want ModelChoice
	}{
		"step pick":      {Stage{Model: "v/step", Effort: "cheap"}, Item{Model: "v/job"}, ModelDefaults{Runner: runner}, ModelChoice{"v/step", ModelFromStep}},
		"job pick":       {Stage{Effort: "cheap"}, Item{Model: "v/job"}, ModelDefaults{Runner: runner}, ModelChoice{"v/job", ModelFromJob}},
		"one model":      {Stage{Effort: "cheap"}, Item{FactoryModel: "v/f"}, ModelDefaults{Runner: RunnerModels{OneModel: true, Launch: "v/one", Worker: "v/worker"}}, ModelChoice{"v/one", ModelFromOneModel}},
		"effort":         {Stage{Effort: "cheap"}, Item{FactoryModel: "v/f"}, ModelDefaults{Runner: runner}, ModelChoice{"v/small", ModelFromEffort}},
		"queued default": {Stage{}, Item{FactoryModel: "v/queued"}, ModelDefaults{Factory: "v/now", Runner: runner}, ModelChoice{"v/queued", ModelFromFactory}},
		"current":        {Stage{}, Item{}, ModelDefaults{Factory: "v/now", Runner: runner}, ModelChoice{"v/now", ModelFromFactory}},
		"worker":         {Stage{Effort: "strong"}, Item{}, ModelDefaults{Runner: runner}, ModelChoice{"v/worker", ModelFromWorker}},
		"launch":         {Stage{}, Item{}, ModelDefaults{Runner: RunnerModels{Launch: "v/launch"}}, ModelChoice{"v/launch", ModelFromRunner}},
		"unknown here":   {Stage{}, Item{}, ModelDefaults{}, ModelChoice{}},
	} {
		if got := StepModel(c.st, c.it, c.d); got != c.want {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
}

// The job's model is its current conversation step's, else its next one's: a
// check step runs on no model and is passed over.
func TestNextModelStageIsTheCurrentOrNextConversation(t *testing.T) {
	stages := []Stage{{Name: "plan", On: true, Model: "v/plan"}, {Name: "test", Kind: StageCheck, On: true}, {Name: "review", On: true, Model: "v/review"}}
	it := Item{Stages: stages, Stream: &Stream{Cur: 1, Phases: []Phase{{Name: "plan", State: PhaseDone}, {Name: "test", Kind: StageCheck, State: PhaseRunning}, {Name: "review", State: PhasePending}}}}
	if st, ok := NextModelStage(it); !ok || st.Name != "review" {
		t.Fatalf("next = %q %v, want review", st.Name, ok)
	}
	if st, ok := NextModelStage(Item{Stages: stages}); !ok || st.Name != "plan" {
		t.Fatalf("not run: %q %v, want plan", st.Name, ok)
	}
}

// Every job says where it came from, and its repository's address is read
// off the forge's own address, keeping the forge's host.
func TestJobSourceAndRepoURL(t *testing.T) {
	gh := Item{Origin: OriginForge, Product: "acme", Repo: "api", Num: 59, URL: "https://ghe.acme.dev/acme/api/issues/59"}
	if s, ok := JobSource(gh); !ok || s != (JobSourceFacts{Name: "github", Ref: "acme/api#59", URL: gh.URL}) {
		t.Fatalf("github source = %+v %v", s, ok)
	}
	if u := RepoURL(gh); u != "https://ghe.acme.dev/acme/api" {
		t.Fatalf("repo url = %q", u)
	}
	jira := Item{Origin: "jira", SourceRef: "jira:ENG-142", URL: "https://acme.atlassian.net/browse/ENG-142", Repo: "acme/api"}
	if s, ok := JobSource(jira); !ok || s != (JobSourceFacts{Name: "jira", Ref: "ENG-142", URL: jira.URL}) {
		t.Fatalf("jira source = %+v %v", s, ok)
	}
	if u := RepoURL(jira); u != "https://github.com/acme/api" {
		t.Fatalf("a jira job's repo url = %q, want github's (the ticket is not the forge)", u)
	}
	trig := Item{Origin: "inbox:ops", SourceRef: "inbox:ops:A1"}
	if s, ok := JobSource(trig); !ok || s.Name != "inbox:ops" || s.Ref != "A1" {
		t.Fatalf("trigger source = %+v", s)
	}
	if s, ok := JobSource(Item{Origin: OriginTerminal}); !ok || s.Name != "terminal" || s.Ref != "" {
		t.Fatalf("typed job source = %+v", s)
	}
	if _, ok := JobSource(Item{}); ok {
		t.Fatal("a job with no origin claimed a source")
	}
	if u := RepoURL(Item{Repo: "api"}); u != "" {
		t.Fatalf("a bare name guessed %q", u)
	}
}
