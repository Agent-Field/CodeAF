package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/session"
)

// C1, C2, C3: the app reopens the saved choice and title route, leaving settings
// and setup state byte-identical across an automatic run-only selection.
func TestAppReopensConfiguredModelAfterRunOnlyReplacement(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	dir, workspace := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	provider := sourcestub.New("vendor/configured", "available/model", "vendor/backup")
	defer provider.Close()
	settings := []byte(`{
  "model.talk": "vendor/configured",
  "models.tiers.low": "",
  "models.tiers.reflex": "vendor/reflex",
  "models.tiers.worker": "vendor/worker",
  "models.tiers.high": "vendor/checker",
  "models.tiers.mastermind": "vendor/planner",
  "models.roles": "planner:vendor/planner",
  "task.model": "vendor/task",
  "models.tasks.mode": "auto",
  "models.tasks.one": "vendor/one"
}
`)
	if err := os.WriteFile(config.BudgetConfigPath(profile), settings, 0600); err != nil {
		t.Fatal(err)
	}
	rolesSource, err := v3RolesSource(workspace, profile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := session.Config{Workspace: workspace, Place: session.Place{Dir: dir, Workspace: workspace}, SessionFile: path,
		Model: "vendor/configured", APIKey: "fixture", BaseURL: provider.URL(), ProfileDir: profile, System: "SYSTEM",
		RolesSource:        rolesSource,
		ConversationModels: &session.ConversationModels{Main: "vendor/configured", Fallback: "vendor/backup"},
	}
	a, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var reopened *session.Agent
	b := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		meta, err := session.LoadMeta(dir)
		if err != nil {
			return nil, err
		}
		fresh := cfg
		fresh.Model = meta.Model
		reopened, err = session.New(fresh)
		if err != nil {
			return nil, err
		}
		return &appConn{Conv: reopened, Workspace: workspace, Transcript: path, Release: func() { _ = reopened.Close() }}, nil
	})
	defer b.Close()
	b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	if err := b.state.write(func(f *appStateFile) error {
		f.Setup = &appwire.Setup{DefaultModel: cfg.Model}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(b.state.where())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.UseModelForNow("available/model"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	events, err := a.Submit(ctx, "respond on the available model")
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatalf("scripted turn failed: %v", event.Err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.SettleWrites()
	profileAfter, err := os.ReadFile(config.BudgetConfigPath(profile))
	if err != nil || !bytes.Equal(settings, profileAfter) {
		t.Fatalf("fallback wrote profile: %s, %v", profileAfter, err)
	}
	stateAfter, err := os.ReadFile(b.state.where())
	if err != nil || !bytes.Equal(stateBefore, stateAfter) {
		t.Fatalf("fallback wrote app setup: %s, %v", stateAfter, err)
	}
	row, ok := session.ReadRows([]string{path})[filepath.Clean(path)]
	if !ok {
		t.Fatal("saved conversation is missing from app inventory")
	}
	listed := appRowView(row)
	if listed.Model != cfg.Model || listed.ModelIdentity == nil || listed.ModelIdentity.Selected != cfg.Model {
		t.Fatalf("agents.list row=%+v", listed)
	}
	b.remember(row)
	opened, err := b.OpenAgent(listed.ID)
	if err != nil || opened.Model != cfg.Model {
		t.Fatalf("agents.open=%+v, %v", opened, err)
	}
	routes, _, err := b.errandRoutes(listed.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		if route.Role == "title" {
			if route.Model != cfg.Model {
				t.Fatalf("agents.errand.models title=%+v", route)
			}
			return
		}
	}
	t.Fatal("agents.errand.models omitted title")
}
