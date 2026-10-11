package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// A CONVERSATION BORN UNDER --one-model STAYS ON THE PIN WHEN A HOST REOPENS IT.
//
// The live factory run that found this: the window's launch pinned the model,
// the manager's first turns rode it, and then a team wake reopened the manager
// headless through [resumeTeamConversation], whose hello names only the folder
// and the transcript. The host built that conversation from the profile's
// defaults, so its turns rode the default model and its mark reader, reflex and
// judge rode the profile's crew. This goes through the host's own door
// ([bootEngine]) twice: once with the window's pinned hello, once with the
// team wake's bare one, against a profile whose every other row names another
// model, and asserts every request the endpoint saw named the pin.
func TestATeamWakeReopensAPinnedConversationOnThePin(t *testing.T) {
	const pinned = "deepseek/deepseek-v4.1-flash"
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	profile := v3Profile(t, map[string]any{
		config.KeyChatModel:       "default/talk",
		"models.roles":            "markreader:pinned/markreader\nreflex:pinned/reflex\nplanner:pinned/planner",
		"models.tiers.low":        "tier/low",
		"models.tiers.high":       "tier/high",
		"models.tiers.reflex":     "tier/reflex",
		"models.tiers.mastermind": "tier/mastermind",
		"task.model":              "task/model",
		"models.fallbacks":        "fallback/one,fallback/two",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv(factoryNoWorkerEnv, "1")
	model := newStageModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	turn := func(hello remote.Hello) string {
		t.Helper()
		engine, err := bootEngine(hello, "", "")
		if err != nil {
			t.Fatalf("the engine door did not open: %v", err)
		}
		if got := engine.Agent.Model(); got != pinned {
			t.Errorf("conversation opened on %q; want %q", got, pinned)
		}
		events, err := engine.Agent.Submit(ctx, "where does the work stand?")
		if err != nil {
			t.Fatal(err)
		}
		for range events {
		}
		if err := engine.Agent.Close(); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(engine.SessionFile)
	}

	// The window: a pinned launch.
	transcript := turn(remote.Hello{Version: remote.Version, Workspace: workspace,
		Model: pinned, Launch: &remote.LaunchShape{OneModel: true}})
	if transcript == "" {
		t.Fatal("the pinned launch named no transcript")
	}
	// The team wake: the hello [resumeTeamConversation] sends, and nothing else.
	turn(remote.Hello{Version: remote.Version, Workspace: workspace, Session: transcript})

	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.models) < 2 {
		t.Fatalf("the endpoint saw %d requests; want both turns", len(model.models))
	}
	for _, got := range model.models {
		if got != pinned {
			t.Errorf("a request named %q; want every request on %q", got, pinned)
		}
	}
}
