package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/session"
)

// providerFactory drives the app's real wire and local runner. Its only step
// waits for approval, and any accidental model request fails the test.
type providerFactory struct {
	*localSetup
	status appwire.FactoryLocalStatus
}

func newProviderFactory(t *testing.T) *providerFactory {
	t.Helper()
	s := newLocalSetup(t)
	t.Setenv(factoryBenchesEnv, "1")
	t.Setenv("CODEAF_MODEL_POOL", "off")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Provider discovery reads metadata without generating a response.
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		t.Errorf("approval-only pipeline called a model: %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	rules, err := pipeline.Parse("default_budget: 1\npipeline: {issue: [approve]}\n")
	if err != nil {
		t.Fatal(err)
	}
	s.backend.local.rules = func(context.Context, string, string) (pipeline.File, error) { return rules, nil }
	base := s.backend.local.seamHook
	s.backend.local.seamHook = func(seam factory.Seam) factory.Seam {
		seam = base(seam)
		seam.RecipeAt = func(repo string) (factory.Recipe, []factory.Problem, string, error) {
			return rules.Recipe(), nil, s.backend.local.host.floor.CheckoutDir(repo), nil
		}
		return seam
	}
	// offlineLocal already supplies the signed-in GitHub owner. Keep its
	// credential absent so ancillary recipe reads cannot reach the real forge.
	return &providerFactory{localSetup: s}
}

func (f *providerFactory) run() {
	f.t.Helper()
	f.status = f.start()
	f.must("factory.local_run", map[string]any{"caller": "w1"}, &f.status)
	g := newRunRig(f.t)
	if err := f.backend.local.host.floor.SetCheckout("acme/api", g.workspace); err != nil {
		f.t.Fatal(err)
	}
	host, runnerDone := f.backend.local.host, f.backend.local.runnerDone
	f.t.Cleanup(func() {
		f.backend.Close()
		select {
		case <-runnerDone:
		default:
			f.t.Error("Close returned before the factory worker joined")
		}
		release, ok := host.floor.TryWorker()
		if !ok {
			f.t.Error("Close kept the factory worker lock")
			return
		}
		defer release()
	})
}

func (f *providerFactory) key(key string) {
	f.t.Helper()
	f.must("keys.set", map[string]any{"provider": "openrouter", "key": key}, nil)
	var providers appwire.ProvidersResult
	f.must("providers.list", struct{}{}, &providers)
	for _, p := range providers.Providers {
		if p.ID == "openrouter" && p.SignedIn {
			return
		}
	}
	f.t.Fatalf("keys.set did not sign in openrouter: %+v", providers)
}

func (f *providerFactory) runner() api.Runner {
	f.t.Helper()
	var runners api.Runners
	f.must("factory.runners", map[string]any{"factory": f.status.Factory}, &runners)
	if len(runners.Runners) == 0 {
		return api.Runner{}
	}
	if len(runners.Runners) != 1 {
		f.t.Fatalf("wanted one runner, got %+v", runners)
	}
	return runners.Runners[0]
}

func (f *providerFactory) waitRunner(state string) api.Runner {
	f.t.Helper()
	var runner api.Runner
	eventually(f.t, "runner state "+state, func() bool {
		runner = f.runner()
		return runner.ID != "" && runner.State == state
	})
	return runner
}

func (f *providerFactory) create() int {
	f.t.Helper()
	var made appwire.FactoryJobActed
	f.must("factory.job_create", map[string]any{"factory": f.status.Factory, "repo": "acme/api", "title": "Wait for my approval", "pipeline": "issue", "assignee": "owner", "start": "now"}, &made)
	return made.Job
}

func (f *providerFactory) job(id int) api.Job {
	f.t.Helper()
	var job api.Job
	f.must("factory.job", map[string]any{"factory": f.status.Factory, "job": id}, &job)
	return job
}

func TestAppFactoryRunnerSeesLateProvider(t *testing.T) {
	f := newProviderFactory(t)
	f.run()
	f.waitRunner(api.RunnerNeedsProvider)
	id := f.create()
	if job := f.job(id); job.State != api.StateQueued {
		t.Fatalf("keyless job state = %q, want queued", job.State)
	}
	f.key("late-test-key")
	var runner api.Runner
	var job api.Job
	defer func() {
		if t.Failed() {
			t.Logf("after keys.set: runner state=%q jobs=%v; job state=%q runner=%q", runner.State, runner.Jobs, job.State, job.Runner)
		}
	}()
	eventually(t, "late provider clears needs_provider and leases the queued approval job without restart", func() bool {
		runner, job = f.runner(), f.job(id)
		return runner.ID != "" && runner.State != api.RunnerNeedsProvider && job.Runner == runner.ID && job.State != api.StateQueued
	})
}

func TestAppFactoryRunnerSeesProviderRemoval(t *testing.T) {
	f := newProviderFactory(t)
	f.key("initial-test-key")
	f.run()
	f.waitRunner(api.RunnerIdle)
	f.must("keys.remove", map[string]any{"provider": "openrouter"}, nil)
	// An idle pull may already be open. The real factory bounds it at thirty
	// seconds; the following poll must publish the removal before taking work.
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for runner := f.runner(); runner.State != api.RunnerNeedsProvider; runner = f.runner() {
		select {
		case <-ctx.Done():
			t.Fatalf("one lease poll after keys.remove: runner state=%q, want needs_provider", runner.State)
		case <-tick.C:
		}
	}
	id := f.create()
	if job := f.job(id); job.State != api.StateQueued || job.Runner != "" {
		t.Fatalf("runner with no provider leased job: state=%q runner=%q", job.State, job.Runner)
	}
	f.key("restored-test-key")
	eventually(t, "the approval job leases after the provider is restored", func() bool {
		return f.job(id).Runner != ""
	})
}

func TestAppFactoryRunnerProviderReplacementKeepsLease(t *testing.T) {
	f := newProviderFactory(t)
	f.key("first-test-key")
	f.run()
	runner := f.waitRunner(api.RunnerIdle)
	f.key("idle-replacement-test-key")
	if after := f.runner(); after.ID != runner.ID || len(after.Jobs) != 0 || after.State != api.RunnerIdle {
		t.Fatalf("idle replacement changed the runner: before=%+v after=%+v", runner, after)
	}
	id := f.create()
	var running api.Job
	eventually(t, "approval-only job is running under its first lease", func() bool {
		running = f.job(id)
		return running.State == api.StateRunning && running.Runner == runner.ID && running.Desk != nil && running.Desk.Run != nil
	})
	started := running.Desk.Run.Started
	// Replacement while this lease is running must not withdraw it, restart
	// its run, or consume another slot.
	f.key("running-replacement-test-key")
	var finished api.Job
	defer func() {
		if t.Failed() {
			t.Logf("after replacement: state=%q runner=%q reason=%q steps=%+v", finished.State, finished.Runner, finished.Reason, finished.Steps)
		}
	}()
	eventually(t, "the same run reaches its approval gate after key replacement", func() bool {
		finished = f.job(id)
		return finished.State == api.StateNeedsYou
	})
	if finished.Runner != runner.ID || finished.Desk == nil || finished.Desk.Run == nil || !finished.Desk.Run.Started.Equal(started) {
		t.Fatalf("key replacement interrupted or replaced the run: started=%v, after=%+v", started, finished.Desk)
	}
	if len(finished.Steps) != 1 || finished.Steps[0].State != "waiting" || finished.Steps[0].Round != 0 {
		t.Fatalf("approval step was duplicated or lost: %+v", finished.Steps)
	}
	for _, line := range finished.Desk.Run.Log {
		if strings.Contains(strings.ToLower(line.Text), "interrupted") {
			t.Fatalf("key replacement interrupted the running job: %s", line.Text)
		}
	}
}

func TestAppFactoryRunnerInjectedProviderNeverNeedsProfileKey(t *testing.T) {
	f := newProviderFactory(t)
	f.backend.local.parent = func() session.Config {
		return session.Config{Model: "fixture/model", OneModel: true, BaseURL: "http://127.0.0.1:1", APIKey: "injectors-test-key"}
	}
	f.run()
	runner := f.waitRunner(api.RunnerIdle)
	f.key("profile-test-key")
	f.must("keys.remove", map[string]any{"provider": "openrouter"}, nil)
	id := f.create()
	eventually(t, "injected provider leases an approval job with no profile key", func() bool {
		now, job := f.runner(), f.job(id)
		if now.State == api.RunnerNeedsProvider {
			t.Fatal("injected provider incorrectly needs the profile's provider key")
		}
		return job.Runner == runner.ID && job.State != api.StateQueued
	})
}
