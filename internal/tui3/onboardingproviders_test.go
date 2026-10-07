package tui3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	a.setup.providerAt = len(a.setupProviderRows()) - 1
	a.touch()
	screen := setupScreen(a)
	if strings.Contains(screen, "Skip for now") || strings.Contains(screen, "More providers") || !strings.Contains(screen, setupSkipKeysWord) {
		t.Fatal(screen)
	}
	hit := a.setup.providerHits[len(a.setup.providerHits)-1]
	a.setupProviderPress(hit.x, hit.y)
	if a.setup.provider != "custom" || a.connPanel.entry == nil || a.connPanel.entry.blank != "base URL" {
		t.Fatal("short-screen click did not select the provider")
	}
	a.backSetupProvider()
	a.setupProviderKey(key("esc"))
	if a.setup.open || config.SetupSeenAt(dir).IsZero() {
		t.Fatal("Esc did not skip setup")
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
	a.model = ""
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
	for _, entry := range a.entries {
		if strings.Contains(entry.text, "this conversation was on  ·") {
			t.Fatal("first connection invented an empty previous model")
		}
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
	if cmd := a.selectSetupProvider("codex"); cmd != nil || a.setup.providerBusy || a.setup.providerAttempt != nil {
		t.Fatal("choosing Codex opened sign-in before confirming its connection screen")
	}
	if screen := setupScreen(a); !strings.Contains(screen, setupBrowserConnectKeysWord) || strings.Contains(screen, signInLinkWord) {
		t.Fatal(screen)
	}
	cmd := a.setupServiceKey(key("enter"))
	if cmd == nil || !a.setup.providerBusy {
		t.Fatal("Enter did not start Codex browser sign-in")
	}
	flowMsg := cmd()
	_, wait := a.Update(flowMsg)
	if wait == nil || !strings.Contains(setupScreen(a), signInLinkWord) {
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

func TestProviderChooserShowsEverySupportedProviderInOneFlatList(t *testing.T) {
	a, _, _ := setupProviderApp(t, nil)
	rows := a.setupProviderRows()
	if len(rows) != len(modelsource.Vendored())+1 {
		t.Fatalf("registry providers=%v", rows)
	}
	for _, size := range []struct{ width, height int }{{120, 40}, {80, 24}, {40, 18}, {40, 12}, {40, 10}} {
		a.width, a.height = size.width, size.height
		for _, selected := range []int{0, len(rows) - 1} {
			a.setup.providerAt = selected
			a.touch()
			screen := setupScreen(a)
			if len(a.setup.providerHits) != len(rows) || strings.Contains(screen, "More providers") || strings.Contains(screen, "Skip for now") || strings.Contains(screen, "of 9") {
				t.Fatalf("flat list at %dx%d: %s", size.width, size.height, screen)
			}
			for at, row := range rows {
				if a.setup.providerHits[at].at != at || !strings.Contains(screen, row.name) {
					t.Fatalf("%q missing at %dx%d: %s", row.id, size.width, size.height, screen)
				}
			}
		}
	}
}

func TestBrowserSetupLinksStayOnOneRowAndCopyTheWholeAuthorizationURL(t *testing.T) {
	target := "https://auth.example/authorize?state=" + strings.Repeat("proof", 120) + "&redirect_uri=http%3A%2F%2Flocalhost%3A9999"
	for _, provider := range []string{"codex", "openrouter"} {
		t.Run(provider, func(t *testing.T) {
			a, _, _ := setupProviderApp(t, nil)
			a.setup.provider = provider
			if provider == "codex" {
				a.modelCatalog = modelsource.Vendored()
				a.setup.providerBusy = true
				a.setup.providerLink = target
			} else {
				a.setup.authFlow = &setupOpenRouterFlow{url: target}
				a.setup.authLink = target
			}
			for _, width := range []int{40, 80, 120} {
				a.width, a.height = width, 30
				a.touch()
				rendered, _, _ := a.frame()
				if strings.Count(rendered, linkOpen(target)) != 1 || strings.Count(plain(rendered), signInLinkWord) != 1 || strings.Contains(plain(rendered), "auth.example") {
					t.Fatalf("URL was split or exposed: %q", rendered)
				}
			}
			cmd, handled := a.setupKeyPress(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
			if !handled || cmd == nil || !reflect.DeepEqual(cmd(), tea.Raw(osc52(target, a.tmux))()) {
				t.Fatal("copy did not carry the complete authorization URL")
			}
		})
	}
}

func TestAllBrowserWaitingCardsUseOneShortLinkWithTheCompleteTarget(t *testing.T) {
	a := newTestApp(nil)
	target := "https://accounts.example/authorize?state=" + strings.Repeat("x", 700)
	e := &entry{kind: entryConnect, conn: &connectCard{name: "Google", state: connectWaiting, link: target}}
	for _, width := range []int{24, 40, 80} {
		rows := a.connectRows(e, width)
		if len(rows) != 2 || !strings.Contains(rows[1], linkOpen(target)) || !strings.Contains(plain(rows[1]), signInLinkWord) || strings.Contains(plain(rows[1]), "accounts.example") {
			t.Fatalf("waiting card: %q", rows)
		}
	}
}
