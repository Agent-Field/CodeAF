package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// A cold start with a saved owned local row opens the restored crew's
// conversation FIRST, through the same assembly the app's opener uses, with no
// injected parent. That assembly must start nothing of the factory's: before
// the fix its worker resolved the factory, served one of its own with this
// process's pid and no bearer, and the app's resume then refused it.
func TestColdResumeServesAfterTheAppOpensAConversationFirst(t *testing.T) {
	s := newLocalSetup(t)
	first := s.start()
	s.backend.Close()
	t.Cleanup(closeFactoryServingHere)
	w, _ := reopened(t, s.backend.factoryHub())
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv(factoryFixtureEnv, "")
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Chdir(t.TempDir())
	freshEngineProcess(t)
	// This process-wide state belongs to the test that opened it.
	resetFactoryEngineState := func() {
		engineFactoryStore.once = sync.Once{}
		engineFactoryStore.store = nil
		setFactoryProcess(func() {
			factoryProcess.once = sync.Once{}
			factoryProcess.stop = nil
		})
	}
	resetFactoryEngineState()
	t.Cleanup(resetFactoryEngineState)
	// A machine with a factory: the floor watches a repository.
	seed, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.SetRepos([]string{"owner/repo"}); err != nil {
		t.Fatal(err)
	}
	if !config.AppMode() {
		t.Fatal("test is not the app process")
	}

	// The restored member's conversation, opened before any factory.* call.
	engine, err := assembleEngine(remote.Hello{Version: remote.Version, New: true, Launch: &remote.LaunchShape{Interactive: true, IntentNames: true}}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Agent.Close() })

	// NO WORKER LOOP WAS STARTED, so there is no tick that could serve later.
	var loop bool
	setFactoryProcess(func() { loop = factoryProcess.stop != nil })
	if loop {
		t.Fatal("opening a conversation started the factory worker loop")
	}
	if _, err := os.Stat(filepath.Join(v3FactoryRoot(), "serve.json")); err == nil {
		t.Fatal("opening a conversation served a factory (serve.json exists)")
	}
	factoryServingHere.mu.Lock()
	served := factoryServingHere.service != nil
	factoryServingHere.mu.Unlock()
	if served {
		t.Fatal("opening a conversation served a factory in the app process")
	}

	var status appwire.FactoryLocalStatus
	w.must("factory.local_status", map[string]any{"caller": "w1"}, &status)
	// THIS MAC'S FACTORY COMES BACK WITH ITS RUNNER: it knows whose GitHub it is,
	// and a runner starts nothing a person did not.
	if status.State != appwire.LocalRunning || status.Authority != appwire.LocalAuthorityApp || !status.Runner {
		t.Fatalf("cold resume did not serve as the app with its runner: %+v", status)
	}
	rows, _ := w.backend.factoryHub().snapshot()
	for _, r := range rows {
		if r.ID == appLocalID && (r.URL == first.URL || !strings.HasPrefix(r.URL, "http://127.0.0.1:")) {
			t.Fatalf("local row kept the old address or lost it: %+v (old %s)", r, first.URL)
		}
	}

	// Asking for the run answers the same runner.
	var run appwire.FactoryLocalStatus
	w.must("factory.local_run", map[string]any{"caller": "w1"}, &run)
	if !run.Runner {
		t.Fatalf("run lost the runner: %+v", run)
	}
}

// The terminal's windows and `codeaf engine` keep the background work.
func TestOnlyTheAppProcessSkipsTheFactoryBackground(t *testing.T) {
	defer config.UseAppCredentials(config.NewAppCredentials())()
	if factoryBackgroundHere() {
		t.Fatal("the app process starts factory background work")
	}
	restore := config.UseAppCredentials(nil)
	defer restore()
	if !factoryBackgroundHere() {
		t.Fatal("a terminal process lost the factory background work")
	}
}
