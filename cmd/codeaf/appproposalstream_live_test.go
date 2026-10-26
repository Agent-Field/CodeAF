//go:build e2e

package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestLiveCrewProposalStreamsBesideARealConversation(t *testing.T) {
	if os.Getenv("CODEAF_LIVE_CREW_PROPOSAL") != "1" {
		t.Skip("set CODEAF_LIVE_CREW_PROPOSAL=1 to use OpenRouter")
	}
	if liveFactoryKey == "" {
		t.Fatal("no OpenRouter credential configured")
	}
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	profile := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	freshEngineProcess(t)
	rec := newAppRecorder()
	var mu sync.Mutex
	var started, first time.Time
	emit := appProposalLiveEmitter{rec: rec, observe: func(event string) {
		if event == appwire.EventCrewProposalDelta {
			mu.Lock()
			if first.IsZero() {
				first = time.Now()
			}
			mu.Unlock()
		}
	}}
	backend := newAppBackend(emit, appOpenInProcess)
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, Model: liveFactoryModel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "We are preparing a community workshop. We need an attendee plan, an accessible venue checklist, and a clear invitation. Give me a short first plan in prose. Do not use tools or start tasks.", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 2*time.Minute, appTurnDone)
	mu.Lock()
	started = time.Now()
	mu.Unlock()
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 3*time.Minute, appAskEvent(appwire.EventAskOpened))
	elapsed := time.Since(started)
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	mu.Lock()
	firstDelta := first.Sub(started)
	missing := first.IsZero()
	mu.Unlock()
	if missing || ask.Proposal == nil || len(ask.Proposal.Helpers) == 0 {
		t.Fatalf("no streamed proposal; ask subkind=%s", ask.Subkind)
	}
	t.Logf("first_delta=%s ask=%s helpers=%d model=%s", firstDelta.Round(time.Millisecond), elapsed.Round(time.Millisecond), len(ask.Proposal.Helpers), liveFactoryModel)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for model := range recorder.requests {
		if model != liveFactoryModel {
			t.Fatalf("unexpected model %q", model)
		}
	}
	for status, count := range recorder.statuses {
		if status != 200 && count > 0 {
			t.Fatalf("provider returned status %d for %d calls", status, count)
		}
	}
	if len(recorder.problems) > 0 {
		t.Fatalf("provider validation = %v", recorder.problems)
	}
}

type appProposalLiveEmitter struct {
	rec     *appRecorder
	observe func(string)
}

func (e appProposalLiveEmitter) Emit(event string, data any) {
	e.observe(event)
	e.rec.Emit(event, data)
}
