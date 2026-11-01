package main

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// runnerHome is a joined runner's own codeaf home: a checkout of owner/repo
// and, when provider is not empty, a profile whose saved key and base URL
// name that endpoint, exactly what `codeaf chat` on the same home would use.
func runnerHome(t *testing.T, lab *runnerLab, provider string) {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	rows := map[string]any{}
	if provider != "" {
		rows["api_key"] = "sk-test-not-real"
		t.Setenv("CODEAF_BASE_URL", provider)
	}
	t.Setenv(config.ProfileDirEnv, v3Profile(t, rows))
	floor, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	checkout := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "first"}} {
		if out, err := exec.Command("git", append([]string{"-C", checkout}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := floor.SetCheckout("owner/repo", checkout); err != nil {
		t.Fatal(err)
	}
	if _, err := lab.store.Create(factory.Item{Repo: "owner/repo", Kind: factory.KindIssue, Title: "look it over", Cap: 10, Assignee: "priya",
		State: factory.StateQueued, Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))}); err != nil {
		t.Fatal(err)
	}
}

// joinRunner joins the lab as Priya and runs that runner until the test ends.
func joinRunner(t *testing.T, lab *runnerLab) factoryTarget {
	t.Helper()
	got, err := joinFactory(context.Background(), lab.link, "", "ci-box", "", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = runFactoryRunner(ctx, got, io.Discard) }()
	t.Cleanup(func() { cancel(); <-done })
	return factoryTarget{lab.server.URL, got.Token}
}

// A JOINED RUNNER WORKS WITH ITS OWN HOME'S PROVIDER. Only `--model` is passed
// on the command line: the first step it leases reaches the endpoint the home
// names, on the pinned model, instead of failing on "provider base URL is
// required".
func TestRunnerJoinRunsItsFirstStepOnItsOwnHomesProvider(t *testing.T) {
	lab := newRunnerLabAt(t, "Priya", time.Now().Add(time.Hour), time.Now)
	model := newStageModel(t)
	runnerHome(t, lab, model.server.URL)
	stageSettle = time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })
	resetFactoryParent(t, session.Config{})
	setFactoryModelPin(savedOneModel, true)
	joinRunner(t, lab)
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		model.mu.Lock()
		reached := len(model.models)
		model.mu.Unlock()
		if reached > 0 {
			model.mu.Lock()
			defer model.mu.Unlock()
			for _, got := range model.models {
				if got != savedOneModel {
					t.Fatalf("a step asked for %q; want the --model pin %q", got, savedOneModel)
				}
			}
			return
		}
		if why := jobLogMentions(t, lab.store, "provider base URL is required"); why != "" {
			t.Fatalf("the first step failed on the runner's provider: %s", why)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no step reached the runner's provider; job log: %s", jobLogMentions(t, lab.store, ""))
}

// A RUNNER WITH NO PROVIDER IS NOT HANDED STEPS IT CANNOT RUN. The factory's
// runner list says what it needs and the queued job stays queued.
func TestRunnerJoinWithoutAProviderShowsWhyAndLeasesNothing(t *testing.T) {
	lab := newRunnerLabAt(t, "Priya", time.Now().Add(time.Hour), time.Now)
	runnerHome(t, lab, "")
	target := joinRunner(t, lab)
	var runners api.Runners
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		runners = api.Runners{}
		if appFactoryRequest(context.Background(), target, "GET", "/v1/runners", nil, &runners) == nil && len(runners.Runners) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(runners.Runners) != 1 {
		t.Fatalf("runners after joining: %+v", runners)
	}
	if got := runners.Runners[0]; got.State != api.RunnerNeedsProvider || got.Reason != "needs a model provider" {
		t.Fatalf("runner = state %q reason %q; want %q, %q", got.State, got.Reason, api.RunnerNeedsProvider, "needs a model provider")
	}
	// Two pull waits: a runner that was going to lease the job has done so.
	time.Sleep(2 * time.Second)
	items, err := lab.store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Title == "look it over" && it.State != factory.StateQueued {
			t.Fatalf("the job went to a runner with no provider: state %q, log %s", it.State, jobLogMentions(t, lab.store, ""))
		}
	}
}

// jobLogMentions is the lab's jobs as the factory recorded them when that
// record contains words (always when words is empty), and "" otherwise. The
// whole record is read because a failed step can land on its log, its rounds
// or its question.
func jobLogMentions(t *testing.T, st *store.Store, words string) string {
	t.Helper()
	items, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), words) {
		return ""
	}
	return string(raw)
}
