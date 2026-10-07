package tui3

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func providerAccessApp(t *testing.T) (*app, string) {
	t.Helper()
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	if err := config.WriteSources(dir, []config.PersistedSource{{ID: "ollama", Written: "ollama", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	ollama := modelsource.Connected{Source: testModelSource(t, "ollama"), Address: "http://localhost:11434/v1"}
	a := modelServiceTestApp(t, dir, "ollama/installed", modelsource.NewSet(testDefaultService(""), ollama), []Model{{ID: "vendor/cloud"}})
	a.sourceModels = map[string][]Model{"ollama": {{ID: "installed"}}}
	return a, dir
}

func selectAddProvider(t *testing.T, a *app, id string) {
	t.Helper()
	for at, item := range a.addPanel.items {
		if item.sourceID == id {
			a.addPanel.cursor = at
			a.addPanelKey(key("enter"))
			return
		}
	}
	t.Fatalf("provider %q is absent", id)
}

func TestLaterProviderMenusShowTheInitialCatalogAndActualConnectionStatus(t *testing.T) {
	a, _ := providerAccessApp(t)
	// A persisted row without credentials must not acquire a connected badge.
	services := append(a.sources.All(), modelsource.Connected{Source: testModelSource(t, "deepseek")})
	a.sources = modelsource.NewSet(services...)
	for _, routerKey := range []string{"", "router-key"} {
		a.sources = a.sources.WithDefaultKey(routerKey)
		a.openAddProvider(false)
		var ids, names []string
		for _, item := range a.addPanel.items {
			if item.heading || item.probe != nil {
				continue
			}
			ids, names = append(ids, item.sourceID), append(names, item.title)
			want := item.sourceID == "ollama" || item.sourceID == modelsource.DefaultID && routerKey != ""
			if item.connected != want || !strings.Contains(item.detail, map[bool]string{true: "connected ·", false: "not connected ·"}[want]) {
				t.Fatalf("%s status = %q, connected=%v, want %v", item.sourceID, item.detail, item.connected, want)
			}
		}
		var initialIDs, initialNames []string
		for _, row := range a.setupProviderRows() {
			initialIDs, initialNames = append(initialIDs, row.id), append(initialNames, row.name)
		}
		if len(ids) != 9 || !reflect.DeepEqual(ids, initialIDs) || !reflect.DeepEqual(names, initialNames) {
			t.Fatalf("later catalog = %v/%v, initial = %v/%v", ids, names, initialIDs, initialNames)
		}
		seen := map[string]bool{}
		for _, row := range a.modelConnectionRows() {
			id, _ := modelConnectionSource(row.ID)
			seen[id] = true
			if id == modelsource.DefaultID && row.Connected != (routerKey != "") || id == "ollama" && !row.Connected || id == "deepseek" && row.Connected {
				t.Fatalf("/connect row has incorrect status: %+v", row)
			}
		}
		for _, id := range initialIDs {
			if !seen[id] {
				t.Fatalf("/connect omits initial provider %s", id)
			}
		}
		a.addPanel.close()
	}
}

func TestAddingOpenRouterFromTheModelMenuPreservesOllamaAndHandsOverTheKey(t *testing.T) {
	a, dir := providerAccessApp(t)
	a.input.setText("keep this draft")
	a.openPicker()
	for at, row := range a.pick.list {
		if a.pick.all[row.hit].AddProvider {
			a.pick.cursor = at
			break
		}
	}
	a.pickerKey(key("enter"))
	selectAddProvider(t, a, modelsource.DefaultID)
	if !a.setup.connection || a.setup.returnAdd != true || a.pick.open || a.addPanel.open {
		t.Fatal("/model add row did not open the later OpenRouter connection form")
	}
	if strings.Contains(a.setupKeysWord(), "skips setup") || setupTitle(&a.setup) != "connect a provider" {
		t.Fatal("later connection still describes first-run setup")
	}
	var handed []string
	a.applyAPIKey = func(key string) error { handed = append(handed, key); return nil }
	const secret = "sk-or-v1-provider-menu-regression-key"
	a.setupPaste(secret)
	a.setupKeyPress(key("enter"))
	if config.APIKeyAt(dir) != secret || !reflect.DeepEqual(handed, []string{secret}) {
		t.Fatal("OpenRouter key did not reach the profile and live session")
	}
	if !config.SetupSeenAt(dir).IsZero() || a.setup.open || !a.addPanel.open || string(a.input.value) != "keep this draft" {
		t.Fatal("adding a provider revisited onboarding or changed the draft")
	}
	if rows := config.PersistedSources(dir); len(rows) != 1 || rows[0].ID != "ollama" {
		t.Fatalf("OpenRouter key was written as a direct provider or removed Ollama: %+v", rows)
	}
	var ids []string
	for _, model := range a.modelList() {
		ids = append(ids, model.ID)
	}
	if !reflect.DeepEqual(ids, []string{"vendor/cloud", "ollama/installed"}) {
		t.Fatalf("combined picker catalog = %v", ids)
	}
	selectAddProvider(t, a, modelsource.DefaultID)
	if !a.connPanel.open || a.setup.open {
		t.Fatal("a connected OpenRouter row did not open management")
	}
	row, ok := a.connPanel.choice()
	if !ok || row.ID != modelConnectionID(modelsource.DefaultID) || !row.Connected {
		t.Fatalf("management focused the wrong provider: %+v", row)
	}
	a.connectAct(a.connPanel.cursor)
	if config.APIKeyAt(dir) == "" {
		t.Fatal("first Enter disconnected without confirmation")
	}
	a.requireListedModel = true
	a.model = "vendor/cloud"
	a.connectAct(a.connPanel.cursor)
	if a.model != "ollama/installed" || a.agent.Model() != "ollama/installed" {
		t.Fatal("removing OpenRouter did not move the next request to available Ollama")
	}
	if config.APIKeyAt(dir) != "" || !reflect.DeepEqual(handed, []string{secret, ""}) {
		t.Fatal("disconnect did not revoke the profile and live key")
	}
	if models := a.modelList(); len(models) != 1 || models[0].ID != "ollama/installed" {
		t.Fatalf("disconnect lost Ollama or retained cloud models: %+v", models)
	}
	a.addPanel.close()
}

func TestLaterOpenRouterBrowserConnectionReturnsWithoutOnboarding(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "cancel"}[cancel], func(t *testing.T) {
			a, dir := providerAccessApp(t)
			flow := &setupOpenRouterFlow{url: "https://example.com/sign-in", key: "sk-or-v1-later-browser-regression-key"}
			a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return flow, nil }
			old := processOpener
			processOpener = func(string) error { return nil }
			t.Cleanup(func() { processOpener = old })
			a.openConnect()
			a.openModelConnection(modelsource.DefaultID)
			begin := a.connectAct(a.connPanel.cursor)
			if begin != nil || a.setup.authStarting || !strings.Contains(a.setupKeysWord(), setupBrowserConnectKeysWord) {
				t.Fatal("choosing OpenRouter bypassed browser confirmation")
			}
			begin, _ = a.setupKeyPress(key("enter"))
			_, wait := a.update(begin())
			if cancel {
				a.setupKeyPress(key("esc"))
				a.setupKeyPress(key("esc"))
			}
			a.update(wait())
			if a.setup.open || !a.connPanel.open || !config.SetupSeenAt(dir).IsZero() {
				t.Fatalf("browser return: setup=%v connection=%v panel=%v seen=%v refusal=%q", a.setup.open, a.setup.connection, a.connPanel.open, config.SetupSeenAt(dir), a.setup.refusal)
			}
			if got := config.APIKeyAt(dir); cancel && got != "" || !cancel && got != flow.key {
				t.Fatal("browser result did not honor completion/cancellation")
			}
			if cancel && !flow.cancelled {
				t.Fatal("browser flow was not cancelled")
			}
		})
	}
}

func TestOpenRouterDisconnectPreservesShellKeysAndAnAnsweringTurn(t *testing.T) {
	a, dir := providerAccessApp(t)
	t.Setenv(config.APIKeyEnv, "sk-or-v1-shell-key")
	a.handAPIKey()
	a.disconnectModelService(modelsource.DefaultID)
	if !a.sources.Default().HasCredentials() || config.APIKeyAt(dir) == "" {
		t.Fatal("disconnect pretended to remove a shell credential")
	}
	t.Setenv(config.APIKeyEnv, "")
	if err := config.WriteAPIKey(dir, "sk-or-v1-profile-key"); err != nil {
		t.Fatal(err)
	}
	a.handAPIKey()
	a.model, a.state = "vendor/cloud", stateWorking
	a.disconnectModelService(modelsource.DefaultID)
	if config.APIKeyAt(dir) == "" {
		t.Fatal("disconnect revoked a provider answering the current turn")
	}
}

func TestLaterOpenRouterConnectionRefreshesItsCatalog(t *testing.T) {
	a, _ := providerAccessApp(t)
	refreshed := false
	a.refreshModels = func(context.Context) ([]Model, time.Time, error) {
		refreshed = true
		return []Model{{ID: "vendor/new"}}, time.Now(), nil
	}
	a.openModelConnection(modelsource.DefaultID)
	a.connectAct(a.connPanel.cursor)
	a.setupPaste("sk-or-v1-refresh-catalog-regression-key")
	cmd, _ := a.setupKeyPress(key("enter"))
	if cmd == nil {
		t.Fatal("connection did not schedule catalog discovery")
	}
	// The connection returned a catalog command without starting another sign-in.
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, command := range batch {
			if command != nil {
				a.Update(command())
			}
		}
	} else {
		a.Update(msg)
	}
	if !refreshed || !a.sources.Default().HasCredentials() {
		t.Fatal("connected OpenRouter did not discover its own models")
	}
}
