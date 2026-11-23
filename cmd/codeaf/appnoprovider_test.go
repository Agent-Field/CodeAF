package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
)

// AN APP WITH NO PROVIDER CONNECTED IS AN ORDINARY APP, NOT A BROKEN ONE. The
// setup can finish with no provider connected, so the app can be in use with
// nothing a turn could run on. models.list then answers an empty list rather than a failure,
// and a first message that is sent anyway ends its turn saying no provider is
// connected, never the terminal's advice to export a variable and run it again.
func TestAppWithNoProviderListsNoModelsAndATurnSaysWhy(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	defer config.UseAppCredentials(config.NewAppCredentials())()
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	defer backend.Close()

	models, err := backend.ListModels()
	if err != nil || models == nil || len(models) != 0 {
		t.Fatalf("models.list with no provider = %#v, %v; want an empty list and no error", models, err)
	}
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: workspace, Text: "say done"})
	if err != nil {
		t.Fatalf("agents.create with no provider = %v", err)
	}
	i := rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && event == appwire.EventTurnDone && done.Agent == agent.ID
	})
	rec.mu.Lock()
	done := rec.data[i].(appwire.TurnDone)
	rec.mu.Unlock()
	if done.OK || done.Error != appNoProvider {
		t.Fatalf("the first message with no provider ended %+v; want it refused as %q", done, appNoProvider)
	}
}

// THE LAST PROVIDER TAKEN AWAY TAKES ITS MODELS WITH IT. A process half that
// opened while a provider was connected stays open, and models.list used to
// offer the default model alone once that provider went, although nothing
// could run it.
func TestAppModelsListEmptiesWhenTheLastProviderGoes(t *testing.T) {
	root := resolvedTempDir(t)
	wakeEnvironment(t, root)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	defer config.UseAppCredentials(config.NewAppCredentials())()
	backend := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer backend.Close()

	if err := backend.SetKey("openrouter", "sk-or-v1-dummy-not-a-real-key"); err != nil {
		t.Fatalf("keys.set = %v", err)
	}
	if models, err := backend.ListModels(); err != nil || len(models) == 0 {
		t.Fatalf("models.list with a provider connected = %+v, %v", models, err)
	}
	if err := backend.RemoveKey("openrouter"); err != nil {
		t.Fatalf("keys.remove = %v", err)
	}
	if models, err := backend.ListModels(); err != nil || len(models) != 0 {
		t.Fatalf("models.list after the last provider went = %+v, %v; want an empty list", models, err)
	}
}
