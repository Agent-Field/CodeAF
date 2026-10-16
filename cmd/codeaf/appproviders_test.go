package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// appProviderTest is a backend on a fresh profile with the app's keys
// installed, so nothing a test does can reach a real key or a real profile.
func appProviderTest(t *testing.T) (*appBackend, *appRecorder, string) {
	t.Helper()
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	for _, name := range []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY", "ZHIPU_API_KEY", "MOONSHOT_API_KEY", "MINIMAX_API_KEY", "DASHSCOPE_API_KEY", "AIAND_API_KEY"} {
		t.Setenv(name, "")
	}
	restore := config.UseAppCredentials(config.NewAppCredentials())
	t.Cleanup(restore)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, nil)
	t.Cleanup(func() { _ = backend.Close() })
	return backend, rec, profile
}

// appStubbed is the vendored table with one service moved onto stub
// addresses by move, so a proof reaches the stub and nothing else.
func appStubbed(id string, move func(*modelsource.Source)) func() []modelsource.Source {
	return func() []modelsource.Source {
		sources := modelsource.Vendored()
		for i := range sources {
			if sources[i].ID == id {
				move(&sources[i])
			}
		}
		return sources
	}
}

func appProvidersByID(t *testing.T, backend *appBackend) map[string]appwire.Provider {
	t.Helper()
	providers, err := backend.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]appwire.Provider{}
	for _, p := range providers {
		byID[p.ID] = p
	}
	return byID
}

// assertNoKeyOnDisk fails when any of the keys is anywhere in the profile.
func assertNoKeyOnDisk(t *testing.T, profile string, keys ...string) {
	t.Helper()
	raw, _ := os.ReadFile(config.BudgetConfigPath(profile))
	for _, key := range keys {
		if strings.Contains(string(raw), key) {
			t.Fatalf("a key reached the profile: %s", redact.Secrets(string(raw)))
		}
	}
}

// EVERY PROVIDER THE TERMINAL OFFERS, IN ITS ORDER. Setup listed four of the
// terminal's ten: the ChatGPT sign-in, Ollama and a custom endpoint were never
// offered at all. Each now says what connecting it asks for, which is what the
// terminal's own connect flow asks.
func TestAppListsEveryProviderTheTerminalOffers(t *testing.T) {
	backend, _, _ := appProviderTest(t)
	providers, err := backend.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	var want, got []string
	for _, source := range tui3.ProviderCatalog(modelsource.Vendored()) {
		want = append(want, source.ID)
	}
	for _, p := range providers {
		got = append(got, p.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") || len(got) != 10 {
		t.Fatalf("providers = %v, want the terminal's %v", got, want)
	}
	byID := appProvidersByID(t, backend)
	for id, kind := range map[string]string{
		"openrouter": appwire.ProviderKey, "deepseek": appwire.ProviderKey, "z-ai": appwire.ProviderKey,
		"moonshot": appwire.ProviderKey, "minimax": appwire.ProviderKey, "qwen": appwire.ProviderKey,
		"aiand": appwire.ProviderKey, "codex": appwire.ProviderBrowser,
		"ollama": appwire.ProviderAddress, "custom": appwire.ProviderAddress,
	} {
		if p := byID[id]; p.Kind != kind || p.SignedIn || p.Name == "" {
			t.Errorf("%s = %+v, want an unconnected %s provider", id, p, kind)
		}
	}
	if p := byID["openrouter"]; !p.SignIn || p.KeyOptional {
		t.Errorf("openrouter = %+v", p)
	}
	if p := byID["codex"]; !p.SignIn {
		t.Errorf("codex = %+v", p)
	}
	if p := byID["ollama"]; !p.KeyOptional || p.Address != "http://localhost:11434/v1" {
		t.Errorf("ollama = %+v", p)
	}
	if p := byID["custom"]; !p.KeyOptional || p.Address != "" {
		t.Errorf("custom = %+v", p)
	}
	for _, id := range []string{"z-ai", "moonshot", "qwen"} {
		if p := byID[id]; len(p.Regions) != 2 || p.Regions[0].ID != "intl" || p.Regions[1].ID != "cn" {
			t.Errorf("%s regions = %+v", id, p.Regions)
		}
	}
	for _, id := range []string{"deepseek", "minimax", "aiand", "openrouter"} {
		if p := byID[id]; len(p.Regions) != 0 {
			t.Errorf("%s has regions %+v", id, p.Regions)
		}
	}
}

// A REGION PROVIDER IS CONNECTED ON THE ROAD ITS KEY OPENS. The check walks
// the billing roads the terminal walks, plan first, and answers the plan's
// models spelled as models.list spells them; the set keeps the region and the
// plan in the profile and the key nowhere but memory.
func TestAppConnectsARegionProviderOnThePlanItsKeyOpens(t *testing.T) {
	backend, _, profile := appProviderTest(t)
	plan, metered := sourcestub.New("glm-5.3"), sourcestub.New("glm-5.3", "glm-5.3-flash")
	defer plan.Close()
	defer metered.Close()
	backend.vendored = appStubbed("z-ai", func(source *modelsource.Source) {
		source.Doors[0].Address, source.Doors[1].Address = plan.URL(), metered.URL()
		source.Regions[0].Address, source.Regions[1].Address = metered.URL(), metered.URL()
	})
	const key = "zai-dummy-not-a-real-key-0001"

	if _, err := backend.CheckProvider(appwire.KeyParams{Provider: "z-ai", Key: key, Region: "moon"}); err == nil {
		t.Fatal("a region the provider does not have was checked")
	}
	check, err := backend.CheckProvider(appwire.KeyParams{Provider: "z-ai", Key: key, Region: "intl"})
	if err != nil || !check.OK || check.Connection == nil {
		t.Fatalf("check = %+v, %v", check, err)
	}
	if c := check.Connection; c.Region != "intl" || c.Door != "coding-plan" || c.Plan != "coding plan" {
		t.Fatalf("connection = %+v", c)
	}
	if len(check.Models) == 0 || check.Models[0].ID != "z-ai/glm-5.3" || check.Models[0].Name != "glm-5.3" || check.Models[0].Provider != "z-ai" {
		t.Fatalf("models = %+v", check.Models)
	}
	if len(metered.Requests()) != 0 {
		t.Fatal("a key the plan answered was also sent down the metered road")
	}
	if _, ok := backend.keys.Key("z-ai"); ok || len(config.PersistedSources(profile)) != 0 {
		t.Fatal("a check kept something")
	}

	if err := backend.SetProvider(appwire.KeyParams{Provider: "z-ai", Key: key, Connection: check.Connection}); err != nil {
		t.Fatal(err)
	}
	rows := config.PersistedSources(profile)
	if len(rows) != 1 || rows[0].ID != "z-ai" || rows[0].Region != "intl" || rows[0].Door != "coding-plan" || rows[0].Key != "" {
		t.Fatalf("kept rows = %+v", rows)
	}
	assertNoKeyOnDisk(t, profile, key)
	if p := appProvidersByID(t, backend)["z-ai"]; !p.SignedIn || p.Region != "intl" || p.Plan != "coding plan" || p.KeyHint != "…0001" {
		t.Fatalf("z-ai = %+v", p)
	}
	// A launch's hand-over carries the key alone, and the kept road stays.
	if err := backend.SetKey("z-ai", key); err != nil {
		t.Fatal(err)
	}
	if rows := config.PersistedSources(profile); len(rows) != 1 || rows[0].Door != "coding-plan" {
		t.Fatalf("a launch's hand-over moved the row: %+v", rows)
	}
}

// A PROVIDER WITH NO LISTING IS PROVED BY ITS ONE-TOKEN CALL. A key that
// lists nothing used to read as a key that did not work; it is checked the way
// the terminal checks it, and offers the one model the terminal would move to.
func TestAppConnectsAListinglessProviderByItsOneTokenCall(t *testing.T) {
	backend, _, profile := appProviderTest(t)
	stub := sourcestub.New()
	defer stub.Close()
	stub.Listingless()
	backend.vendored = appStubbed("minimax", func(source *modelsource.Source) { source.Address = stub.URL() })
	const key = "sk-minimax-dummy-not-a-real-key"

	if check, err := backend.CheckProvider(appwire.KeyParams{Provider: "minimax", Key: "not-a-key"}); err != nil || check.OK || check.Status != appCheckShape {
		t.Fatalf("a key of the wrong shape = %+v, %v", check, err)
	}
	check, err := backend.CheckProvider(appwire.KeyParams{Provider: "minimax", Key: key})
	if err != nil || !check.OK || check.Connection == nil || check.Connection.Listed == nil || *check.Connection.Listed {
		t.Fatalf("check = %+v, %v", check, err)
	}
	if len(check.Models) != 1 || check.Models[0].ID != "minimax/MiniMax-M3" {
		t.Fatalf("models = %+v", check.Models)
	}
	paths := []string{}
	for _, request := range stub.Requests() {
		paths = append(paths, request.Method+" "+request.Path)
	}
	if strings.Join(paths, ",") != "GET /v1/models,POST /v1"+modelsource.ChatCompletionsPath {
		t.Fatalf("requests = %v", paths)
	}
	if err := backend.SetProvider(appwire.KeyParams{Provider: "minimax", Key: key, Connection: check.Connection}); err != nil {
		t.Fatal(err)
	}
	if rows := config.PersistedSources(profile); len(rows) != 1 || rows[0].Listed == nil || *rows[0].Listed {
		t.Fatalf("kept rows = %+v", rows)
	}
	assertNoKeyOnDisk(t, profile, key)

	// A refusal is the provider's own words and its own status code.
	stub.Refuse(http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`)
	refused, err := backend.CheckProvider(appwire.KeyParams{Provider: "minimax", Key: key})
	if err != nil || refused.OK || refused.Status != http.StatusUnauthorized || refused.Message != "invalid api key" {
		t.Fatalf("refused = %+v, %v", refused, err)
	}
}

// AN ADDRESS IS CONNECTED WITH A KEY OR WITHOUT ONE. A custom endpoint that
// answers with no key is connected with none and named for its host; Ollama is
// connected at the address given rather than only at its usual one; neither
// writes a key, and removing one is the terminal's disconnect.
func TestAppConnectsAnAddressWithAndWithoutAKey(t *testing.T) {
	backend, rec, profile := appProviderTest(t)
	endpoint := sourcestub.New("local-model")
	defer endpoint.Close()

	if _, err := backend.CheckProvider(appwire.KeyParams{Provider: "custom"}); err == nil {
		t.Fatal("a custom endpoint with no address was checked")
	}
	if _, err := backend.CheckProvider(appwire.KeyParams{Provider: "custom", Address: "ftp://example.invalid/v1"}); err == nil {
		t.Fatal("an address that is not http was checked")
	}
	check, err := backend.CheckProvider(appwire.KeyParams{Provider: "custom", Address: endpoint.URL()})
	if err != nil || !check.OK || check.Connection == nil || check.Connection.Address != endpoint.URL() {
		t.Fatalf("check = %+v, %v", check, err)
	}
	if len(check.Models) != 1 || check.Models[0].ID != "127-0-0-1/local-model" || check.Models[0].Provider != "127-0-0-1" {
		t.Fatalf("models = %+v", check.Models)
	}
	if bearer := endpoint.Requests()[0].Bearer; bearer != "" {
		t.Fatalf("a keyless check sent %q", bearer)
	}
	if err := backend.SetProvider(appwire.KeyParams{Provider: "custom", Connection: check.Connection}); err != nil {
		t.Fatal(err)
	}
	rows := config.PersistedSources(profile)
	if len(rows) != 1 || rows[0].ID != "custom" || rows[0].Address != endpoint.URL() || !rows[0].KeyOptional || rows[0].Written != "127-0-0-1" {
		t.Fatalf("kept rows = %+v", rows)
	}
	if p := appProvidersByID(t, backend)["custom"]; !p.SignedIn || p.Address != endpoint.URL() || p.Name != "127-0-0-1" || p.KeyHint != "" {
		t.Fatalf("custom = %+v", p)
	}

	// Ollama somewhere other than its usual place, with a key it does not need.
	ollama := sourcestub.New("llama-local")
	defer ollama.Close()
	const key = "ollama-dummy-not-a-real-key-7c2e"
	check, err = backend.CheckProvider(appwire.KeyParams{Provider: "ollama", Key: key, Address: ollama.URL()})
	if err != nil || !check.OK || check.Models[0].ID != "ollama/llama-local" {
		t.Fatalf("ollama check = %+v, %v", check, err)
	}
	if err := backend.SetProvider(appwire.KeyParams{Provider: "ollama", Key: key, Connection: check.Connection}); err != nil {
		t.Fatal(err)
	}
	if service, ok := config.ResolveSources(profile, "", config.DefaultBaseURL).ByID("ollama"); !ok || service.Address != ollama.URL() || service.Key != key {
		t.Fatalf("ollama resolves to %q", service.Address)
	}
	if p := appProvidersByID(t, backend)["ollama"]; !p.SignedIn || p.Address != ollama.URL() || p.KeyHint != "…7c2e" {
		t.Fatalf("ollama = %+v", p)
	}
	assertNoKeyOnDisk(t, profile, key)

	// A launch's hand-over of a key writes no row, so it never brings back a
	// connection the terminal removed; with no key, Ollama is kept at its usual
	// address, because the row is all that connects it.
	if err := config.DisconnectService(profile, "ollama"); err != nil {
		t.Fatal(err)
	}
	if err := backend.SetKey("ollama", key); err != nil {
		t.Fatal(err)
	}
	if _, kept := appPersistedRows()["ollama"]; kept {
		t.Fatal("a launch's hand-over wrote a row")
	}
	if err := backend.SetProvider(appwire.KeyParams{Provider: "ollama"}); err != nil {
		t.Fatal(err)
	}
	if row, kept := appPersistedRows()["ollama"]; !kept || row.Address != "http://localhost:11434/v1" {
		t.Fatalf("a keyless Ollama was kept as %+v", row)
	}

	if err := backend.RemoveKey("custom"); err != nil {
		t.Fatal(err)
	}
	if p := appProvidersByID(t, backend)["custom"]; p.SignedIn || p.Address != "" {
		t.Fatalf("a removed endpoint = %+v", p)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.ProviderChanged)
		return ok && changed.Provider.ID == "custom" && !changed.Provider.SignedIn
	})
}

// appFakeCodexFlow is a browser sign-in that finishes as soon as it is waited
// for, with tokens nobody issued.
type appFakeCodexFlow struct{ tokens codexauth.Tokens }

func (f *appFakeCodexFlow) URL() string { return "http://localhost:1455/auth?fake" }
func (f *appFakeCodexFlow) Wait(context.Context) (codexauth.Tokens, error) {
	return f.tokens, nil
}
func (f *appFakeCodexFlow) Cancel() {}

// THE CHATGPT SIGN-IN KEEPS ITS TOKENS WHERE THE TERMINAL KEEPS THEM. The
// app holds no key for it: the sign-in finishes, the tokens and the row land
// in the profile, provider.changed says signed in with no key in it, and a
// removal takes the tokens away again.
func TestAppSignsInToChatGPTAndKeepsItsTokensWhereTheTerminalDoes(t *testing.T) {
	backend, rec, profile := appProviderTest(t)
	listing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"slug": "gpt-5.5", "visibility": "list", "context_window": 272000}}})
	}))
	defer listing.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", listing.URL)
	backend.codexBegin = func(context.Context) (codexConnectFlow, error) {
		return &appFakeCodexFlow{tokens: codexauth.Tokens{AccessToken: "codex-dummy-access", RefreshToken: "codex-dummy-refresh", ExpiresAt: time.Now().Add(time.Hour)}}, nil
	}
	if _, err := backend.CheckProvider(appwire.KeyParams{Provider: "codex", Key: "sk-anything-0000000000"}); err == nil {
		t.Fatal("a key was checked for the browser sign-in")
	}
	address, err := backend.SignIn("codex")
	if err != nil || address == "" {
		t.Fatalf("sign in = %q, %v", address, err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.ProviderChanged)
		return ok && changed.Provider.ID == "codex" && changed.Provider.SignedIn && changed.Key == ""
	})
	if !codexauth.Connected(profile) {
		t.Fatal("the sign-in's tokens are not in the profile")
	}
	if p := appProvidersByID(t, backend)["codex"]; !p.SignedIn || p.KeyHint != "" {
		t.Fatalf("codex = %+v", p)
	}
	if service, ok := config.ResolveSources(profile, "", config.DefaultBaseURL).ByID("codex"); !ok || service.Key == "" {
		t.Fatal("the signed-in service does not resolve")
	}
	if err := backend.RemoveKey("codex"); err != nil {
		t.Fatal(err)
	}
	if codexauth.Connected(profile) || appProvidersByID(t, backend)["codex"].SignedIn {
		t.Fatal("a removed sign-in is still connected")
	}
}

// KEYS.REMOVE SIGNS THE CHATGPT ACCOUNT OUT AND SETTLES ITS SIGN-IN FIRST. A
// sign-in still waiting for the browser is cancelled, so an answer arriving
// after the sign-out keeps nothing and the row says signed out and nothing
// older; one already being saved answers busy, because the tokens it is writing
// would outlive the sign-out.
func TestAppRemovingCodexSignsOutAndSettlesItsSignIn(t *testing.T) {
	backend, _, _ := appProviderTest(t)
	saved := make(chan struct{}, 1)
	pending := &accountTestFlow{make(chan error, 1), make(chan struct{})}
	if _, err := backend.beginAccountSignIn(func(context.Context) (appAccountFlow, error) { return pending, nil }, func(context.Context, codexauth.Tokens) error {
		saved <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.RemoveKey("codex"); err != nil {
		t.Fatal(err)
	}
	pending.result <- nil
	<-pending.ended
	select {
	case <-saved:
		t.Fatal("a sign-in the sign-out cancelled kept its tokens")
	case <-time.After(20 * time.Millisecond):
	}
	if p := appProvidersByID(t, backend)["codex"]; p.SignedIn || p.SignInState != "" || p.SignInAttempt != "" {
		t.Fatalf("codex after the sign-out = %+v", p)
	}

	saving := &accountTestFlow{make(chan error, 1), make(chan struct{})}
	entered, release := make(chan struct{}), make(chan struct{})
	if _, err := backend.beginAccountSignIn(func(context.Context) (appAccountFlow, error) { return saving, nil }, func(context.Context, codexauth.Tokens) error {
		close(entered)
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	saving.result <- nil
	<-entered
	if err := backend.RemoveKey("codex"); wireCode(err) != appwire.CodeBusy {
		t.Fatalf("a sign-out during the save was answered %v", err)
	}
	close(release)
	awaitAccountState(t, backend, "signed_in")
	if len(saved) != 0 {
		t.Fatal("the cancelled sign-in saved after all")
	}
}

// MODELS.LIST OFFERS WHAT A NEWLY CONNECTED PROVIDER SERVES. Connected
// through keys.set, a custom endpoint's listing is fetched in the background
// and reaches models.list under the name it was connected as, and a provider
// that lists nothing offers the model the terminal would move onto.
func TestAppModelsListOffersANewlyConnectedProvidersModels(t *testing.T) {
	root := resolvedTempDir(t)
	_, _ = wakeEnvironment(t, root)
	restore := config.UseAppCredentials(config.NewAppCredentials())
	defer restore()
	endpoint := sourcestub.New("local-model")
	defer endpoint.Close()
	backend := newAppBackend(newAppRecorder(), nil)
	defer backend.Close()
	if _, err := backend.ListModels(); err != nil {
		t.Fatal(err)
	}
	if err := backend.SetProvider(appwire.KeyParams{Provider: "custom", Connection: &appwire.Connection{Address: endpoint.URL()}}); err != nil {
		t.Fatal(err)
	}
	if err := backend.SetProvider(appwire.KeyParams{Provider: "moonshot", Key: "sk-moonshot-dummy-not-a-real-key"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		models, err := backend.ListModels()
		if err != nil {
			t.Fatal(err)
		}
		byID := map[string]appwire.Model{}
		for _, m := range models {
			byID[m.ID] = m
		}
		local, moon := byID["127-0-0-1/local-model"], byID["moonshot/kimi-k2.7-code"]
		if local.Provider == "127-0-0-1" && moon.Provider == "moonshot" && moon.ProviderName == "Moonshot" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("models = %+v", models)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// keys.check and keys.set say what they need in a person's words.
func TestAppProviderRefusalsSayWhatIsMissing(t *testing.T) {
	backend, _, _ := appProviderTest(t)
	for _, params := range []appwire.KeyParams{
		{Provider: "deepseek"},
		{Provider: "codex", Key: "sk-anything-0000000000"},
		{Provider: "nowhere", Key: "sk-anything-0000000000"},
		{Provider: "z-ai", Key: "zai-dummy", Connection: &appwire.Connection{Door: "moonlight"}},
	} {
		err := backend.SetProvider(params)
		var wire *appwire.Error
		if !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
			t.Errorf("%+v was answered %v", params, err)
		}
	}
	if _, err := backend.SignIn("deepseek"); err == nil {
		t.Fatal("a sign-in started for a provider with none")
	}
}

// THE PROCESS OPENS ONCE THE FIRST PROVIDER ARRIVES. A first launch starts
// with nothing connected, and the hello's warm fails to open the process half
// for want of a key; that refusal used to be remembered for the whole launch,
// so a provider connected in setup offered nothing until the app was quit and
// opened again. The app's own first look at its models is an empty list that
// opens nothing. Here the first provider is an endpoint that takes no key, and
// its models reach models.list in the same launch.
func TestAppModelsListOpensOnceTheFirstProviderIsConnected(t *testing.T) {
	root := resolvedTempDir(t)
	_, _ = wakeEnvironment(t, root)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	restore := config.UseAppCredentials(config.NewAppCredentials())
	defer restore()
	endpoint := sourcestub.New("local-model")
	defer endpoint.Close()
	backend := newAppBackend(newAppRecorder(), nil)
	defer backend.Close()
	backend.Warm()
	if models, err := backend.ListModels(); err != nil || len(models) != 0 || openedEngineProcess() != nil {
		t.Fatalf("a launch with nothing connected answered %+v, %v, or opened a process", models, err)
	}
	if err := backend.SetProvider(appwire.KeyParams{Provider: "custom", Connection: &appwire.Connection{Address: endpoint.URL()}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		models, err := backend.ListModels()
		if err == nil {
			for _, m := range models {
				if m.ID == "127-0-0-1/local-model" {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("models = %+v, %v", models, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
