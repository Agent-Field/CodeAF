package tui3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestProviderChooserBackSkipPasteAndShortPointerRows(t *testing.T) {
	a, dir, _ := setupProviderApp(t, nil)
	a.setupPaste("must not become a hidden key")
	if a.setup.text != "" {
		t.Fatal("chooser collected a secret")
	}
	a.selectSetupProvider("openrouter")
	a.setupPaste("sk-or-v1-a-secret-that-will-be-discarded")
	a.setupKeyPress(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	if a.setup.provider != "" || a.setup.text != "" || config.APIKeyAt(dir) != "" {
		t.Fatal("back saved or retained a secret")
	}
	a.width, a.height = 40, 12
	a.selectSetupProvider("!more")
	a.setup.providerAt = len(a.setupProviderRows()) - 1
	a.touch()
	screen := setupScreen(a)
	if !strings.Contains(screen, "Skip for now") || !strings.Contains(screen, setupSkipKeysWord) {
		t.Fatal(screen)
	}
	hit := a.setup.providerHits[len(a.setup.providerHits)-1]
	a.setupProviderPress(hit.x, hit.y)
	if a.setup.open || config.SetupSeenAt(dir).IsZero() {
		t.Fatal("short-screen click did not skip")
	}
}

func TestProviderChooserReusesRegionsAndMasksKeys(t *testing.T) {
	for _, id := range []string{"z-ai", "moonshot", "qwen", "deepseek", "minimax"} {
		t.Run(id, func(t *testing.T) {
			a, _, _ := setupProviderApp(t, nil)
			a.selectSetupProvider(id)
			entry := a.connPanel.entry
			if entry == nil {
				t.Fatal("provider offered no input")
			}
			if entry.choosing() {
				a.setupServiceKey(key("down"))
				expected := entry.value()
				a.setupServiceKey(key("enter"))
				if a.modelDraft.row.Region != expected {
					t.Fatal("region was not applied")
				}
			}
			a.setupPaste("a-secret-key-that-must-be-masked")
			screen := setupScreen(a)
			if strings.Contains(screen, "a-secret-key") || !strings.Contains(screen, "your key") {
				t.Fatal(screen)
			}
			a.backSetupProvider()
			if a.modelDraft != nil || a.connPanel.entry != nil {
				t.Fatal("back left the entry alive")
			}
		})
	}
}

func TestOnboardingAnonymousCustomConnectsAndUsesItsListedModel(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected connection request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("anonymous server received a credential")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"id": "installed-one"}, map[string]string{"id": "installed-two"}}})
	}))
	defer backend.Close()
	a, dir, _ := setupProviderApp(t, nil)
	a.sources = modelsource.NewSet(testDefaultService(""))
	a.serviceModelRefresh = func(_ context.Context, _ modelsource.Connected, seed []Model) ([]Model, error) { return seed, nil }
	a.selectSetupProvider("custom")
	a.setupPaste(backend.URL + "/v1")
	cmd := a.setupServiceKey(key("enter"))
	if cmd == nil {
		t.Fatal("address check did not start")
	}
	a.Update(cmd())
	if entry := a.connPanel.entry; entry == nil || entry.blank != "name" {
		t.Fatal("anonymous address did not advance to name")
	}
	a.connPanel.entry.box.setText("local-test")
	cmd = a.setupServiceKey(key("enter"))
	if cmd == nil || !a.setup.providerBusy {
		t.Fatal("anonymous endpoint asked for a key")
	}
	a.Update(cmd())
	if a.setup.step() != setupControls {
		t.Fatalf("connection did not reach controls: %s", setupScreen(a))
	}
	if a.model != "local-test/installed-one" || a.agent.(*fakeAgent).model != a.model {
		t.Fatalf("surface %q, engine %q", a.model, a.agent.(*fakeAgent).model)
	}
	source, ok := config.ResolveSources(dir, "", config.DefaultBaseURL).ByID("custom")
	if !ok || !source.HasCredentials() || !source.Source.KeyOptional {
		t.Fatal("anonymous connection was not saved")
	}
	if got := a.modelList(); len(got) != 2 {
		t.Fatalf("listed models: %#v", got)
	}
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return nil, nil }
	a.model = ""
	if a.defaultProviderNeeded() {
		t.Fatal("saved anonymous connection opened chooser while catalog was cold")
	}
}

func TestOnboardingOllamaNeedsNoKeyAndCancelledResultsCannotSwitchModel(t *testing.T) {
	a, _, _ := setupProviderApp(t, nil)
	cmd := a.selectSetupProvider("ollama")
	attempt := a.setup.providerAttempt
	if cmd == nil || !a.setup.providerBusy || a.connPanel.entry != nil || !a.modelDraft.source.KeyOptional {
		t.Fatal("Ollama did not start without a key")
	}
	a.backSetupProvider()
	if attempt.ctx.Err() == nil {
		t.Fatal("back did not cancel connection")
	}
	original := a.model
	a.Update(modelConnectResultMsg{setupAttempt: attempt, service: "ollama", models: []Model{{ID: "installed"}}, outcome: modelsource.Outcome{Kind: modelsource.OutcomeConnected}})
	if a.model != original || a.setup.provider != "" || a.setup.step() != setupKey {
		t.Fatal("late result moved the model or advanced setup")
	}
}

func TestOnboardingCodexSignInIsVisibleAndLateFlowsAreCancelled(t *testing.T) {
	a, dir, _ := setupProviderApp(t, nil)
	flow := &panelCodexFlow{url: "https://auth.example/onboarding"}
	a.codexConnect = func(context.Context) (CodexFlow, error) { return flow, nil }
	old := processOpener
	processOpener = func(string) error { return nil }
	t.Cleanup(func() { processOpener = old })
	cmd := a.selectSetupProvider("codex")
	flowMsg := cmd()
	_, wait := a.Update(flowMsg)
	if wait == nil || !strings.Contains(setupScreen(a), flow.URL()) {
		t.Fatal("browser link is hidden behind setup")
	}
	a.backSetupProvider()
	if !flow.cancelled {
		t.Fatal("back did not cancel browser flow")
	}
	a.Update(wait())
	if _, ok := config.ResolveSources(dir, "", config.DefaultBaseURL).ByID("codex"); ok {
		t.Fatal("cancelled browser answer saved a connection")
	}
	a.Update(flowMsg)
	if !flow.cancelled || a.codexFlow != nil {
		t.Fatal("late flow was installed after back")
	}
}
