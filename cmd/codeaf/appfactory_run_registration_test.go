package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// A successful start must reach a real worker attach, with no queued jobs and
// no model request. The production settings/governance path is used: no
// injected local.parent conversation config that could hide startup errors.
func TestLocalRunnerRegistersWithProductionSettingsAndNoJobs(t *testing.T) {
	s := newLocalSetup(t)
	// Reproduce the exact role settings used by the native review profile.
	model := "deepseek/deepseek-v4.1-flash"
	settings := map[string]string{"model_pool": "off", "model.talk": model, "models.fallbacks": model, "models.crew.allowed": model}
	for _, role := range []string{"reflex", "low", "worker", "high", "mastermind"} {
		settings["models.tiers."+role] = model
	}
	for _, role := range []string{"worker", "planner", "checker"} {
		settings["models.crew.route."+role] = model + "@openrouter"
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	profile := s.backend.localProfile()
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	st := s.start()
	s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret})
	var run appwire.FactoryLocalStatus
	s.must("factory.local_run", map[string]any{"caller": "w1"}, &run)
	if !run.Runner {
		t.Fatal("runner did not start")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := s.backend.FactoryRunners(context.Background(), st.Factory)
		if err == nil {
			for _, row := range rows.Runners {
				if row.Owner == "owner" && row.LiveCapacity && row.State != "offline" {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("start returned on but no live-capacity worker registered through production settings")
}

// A PERSONAL FACTORY RUNS WITHOUT GITHUB (2026-11-17): with GitHub never
// connected, factory.local_run is no longer refused, and the runner registers
// live under the local runner name, so a folder job made before GitHub runs.
// Before, the start answered "connect GitHub before turning the runner on".
func TestLocalRunnerRegistersWithoutGitHub(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	var run appwire.FactoryLocalStatus
	s.must("factory.local_run", map[string]any{"caller": "w1"}, &run)
	if !run.Runner {
		t.Fatal("runner did not start without GitHub")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := s.backend.FactoryRunners(context.Background(), st.Factory)
		if err == nil {
			for _, row := range rows.Runners {
				if row.Owner == factory.LocalRunnerHandle && row.LiveCapacity && row.State != "offline" {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no live runner registered under %q with GitHub never connected", factory.LocalRunnerHandle)
}
