package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// taskModelsBackend is settingsBackend over a profile with one OpenRouter key
// and a small catalog. UNIT DATA: the rows and the key are this test's own,
// and nothing reaches a network or a model.
func taskModelsBackend(t *testing.T) (*appBackend, *appRecorder) {
	t.Helper()
	for _, name := range []string{config.APIKeyEnv, "OPENAI_API_KEY", "DEEPSEEK_API_KEY", "ZHIPU_API_KEY", "MOONSHOT_API_KEY",
		"MINIMAX_API_KEY", "DASHSCOPE_API_KEY", "AIAND_API_KEY"} {
		t.Setenv(name, "")
	}
	backend := settingsBackend(t)
	rec := newAppRecorder()
	backend.emit = rec
	if err := config.WriteAPIKey(config.ProfileDir(), "sk-or-v1-taskmodels-wire-6789"); err != nil {
		t.Fatal(err)
	}
	previous := config.CrewCatalog
	t.Cleanup(func() { config.CrewCatalog = previous })
	rows := []catalog.Model{
		{ID: "z-ai/glm-5.3-flash", OpenWeights: true, PromptPrice: 1.5e-7, CompletionPrice: 5e-7, IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}},
		{ID: "moonshotai/kimi-k3", OpenWeights: true, PromptPrice: 3e-6, CompletionPrice: 1.5e-5, IntelligenceIndex: 43.6, CodingIndex: 76.2, AgenticIndex: 50, ContextLength: 1048576, Parameters: []string{"tools"}},
		{ID: "deepseek/deepseek-v4-flash", OpenWeights: true, PromptPrice: 8e-8, CompletionPrice: 1.6e-7, IntelligenceIndex: 24.2, CodingIndex: 56.2, AgenticIndex: 22.2, ContextLength: 1048576, Parameters: []string{"tools"}},
		{ID: "anthropic/claude-opus-5", PromptPrice: 5e-6, CompletionPrice: 2.5e-5, IntelligenceIndex: 50.8, CodingIndex: 78, AgenticIndex: 56.5, ContextLength: 1000000, Parameters: []string{"tools"}},
		{ID: "z-ai/glm-next", PriceUnknown: true, OpenWeights: true, IntelligenceIndex: 60, CodingIndex: 90, AgenticIndex: 70, ContextLength: 1048576, Parameters: []string{"tools"}},
		{ID: "vendor/image-only", PromptPrice: 1e-9, CompletionPrice: 1e-9, Parameters: []string{"temperature"}},
	}
	config.CrewCatalog = func() []catalog.Model { return rows }
	return backend, rec
}

// GET IS SMALL AND HONEST: provider headers with counts and a masked key, no
// model list; a provider with no key says the one fix this engine has.
func TestAppTaskModelsGetIsHeadersOnly(t *testing.T) {
	backend, _ := taskModelsBackend(t)
	got, err := backend.TaskModels()
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != appwire.TaskModelsAuto || got.OneModel != nil || got.ReadAt.IsZero() || got.Revision == "" {
		t.Fatalf("got %+v", got)
	}
	or := got.Providers[0]
	if or.ID != "openrouter" || or.State != appwire.ProviderConnected || or.KeyHint != "…6789" || or.ModelCount != 5 || or.AllowedCount != 5 {
		t.Errorf("openrouter header %+v", or)
	}
	for _, p := range got.Providers[1:] {
		if p.ID == "moonshot" && (p.State != appwire.ProviderNoKey || p.Fix != appwire.FixAddKey || p.ModelCount != 0 || p.KeyHint != "") {
			t.Errorf("moonshot %+v", p)
		}
	}
	if got.Roles.Checker.Pin != nil || got.Roles.Checker.Warning != nil || got.Roles.Checker.EligibleCount == 0 {
		t.Errorf("checker %+v", got.Roles.Checker)
	}
}

// EVERY ROW IS REACHABLE BY PAGES, A CURSOR IS ONLY GOOD FOR ITS OWN LIST, and
// a missing price is null, never zero.
func TestAppTaskModelsListPagesEveryRowAndRefusesAStaleCursor(t *testing.T) {
	backend, _ := taskModelsBackend(t)
	seen := map[string]bool{}
	p := appwire.TaskModelsListParams{Limit: 2}
	total := -1
	for pages := 0; ; pages++ {
		page, err := backend.TaskModelsList(p)
		if err != nil {
			t.Fatal(err)
		}
		total = page.Total
		for _, row := range page.Models {
			if seen[row.ID+"@"+row.Provider] {
				t.Errorf("%s listed twice", row.ID)
			}
			seen[row.ID+"@"+row.Provider] = true
			if row.ID == "z-ai/glm-next" && (row.Price != nil || row.AutoEligible || row.Reason != appwire.ModelNoPrice || !row.Allowed) {
				t.Errorf("unpriced row %+v", row)
			}
			if row.ID == "z-ai/glm-5.3-flash" && (row.Price == nil || row.Price.InPerM < 0.149 || row.Price.InPerM > 0.151) {
				t.Errorf("openrouter's own price %+v", row.Price)
			}
		}
		if page.NextCursor == "" {
			break
		}
		if pages > 10 {
			t.Fatal("the pages never end")
		}
		p.Cursor = page.NextCursor
	}
	if total != 5 || len(seen) != total {
		t.Fatalf("saw %d of %d rows", len(seen), total)
	}
	first, _ := backend.TaskModelsList(appwire.TaskModelsListParams{Limit: 2})
	if _, err := backend.TaskModelsList(appwire.TaskModelsListParams{Limit: 2, Q: "glm", Cursor: first.NextCursor}); wireCode(err) != appwire.CodeBadParams {
		t.Errorf("a cursor from another filter: %v", err)
	}
	if _, err := backend.SetTaskModels(appwire.TaskModelsPatch{Allowed: []appwire.TaskModelAllowed{{Model: appwire.ModelRef{ID: "moonshotai/kimi-k3", Provider: "openrouter"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.TaskModelsList(appwire.TaskModelsListParams{Limit: 2, Cursor: first.NextCursor}); wireCode(err) != appwire.CodeBadParams {
		t.Errorf("a cursor from before a change: %v", err)
	}
	found, err := backend.TaskModelsList(appwire.TaskModelsListParams{Q: "KIMI"})
	if err != nil || found.Total != 1 || found.Models[0].Allowed {
		t.Errorf("search: %+v %v", found, err)
	}
}

// A SET IS ONE CHANGE, ANSWERED WITH THE WHOLE PAGE AND SAID ONCE; the Checks
// row of settings is the checker pin, route and all; a stale revision is a conflict and a pin
// to nothing listed is bad params.
func TestAppTaskModelsSetWritesWholeAndSaysIt(t *testing.T) {
	backend, rec := taskModelsBackend(t)
	before, _ := backend.TaskModels()
	checker := &appwire.ModelRef{ID: "deepseek/deepseek-v4-flash", Provider: "openrouter"}
	got, err := backend.SetTaskModels(appwire.TaskModelsPatch{
		Revision: before.Revision,
		Pins:     map[string]*appwire.ModelRef{appwire.RoleChecker: checker},
		Allowed:  []appwire.TaskModelAllowed{{Model: appwire.ModelRef{ID: "anthropic/claude-opus-5", Provider: "openrouter"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pin := got.Roles.Checker.Pin; pin == nil || *pin != *checker || got.Revision == before.Revision {
		t.Fatalf("after the set %+v", got.Roles.Checker)
	}
	if !got.ReadAt.After(before.ReadAt) {
		t.Errorf("the answer to a set reads at %v, not after the earlier read at %v", got.ReadAt, before.ReadAt)
	}
	rec.waitFor(t, 0, func(event string, data any) bool {
		changed, ok := data.(appwire.SettingsChanged)
		return event == appwire.EventSettingsChanged && ok && changed.TaskModels != nil && changed.TaskModels.Revision == got.Revision &&
			changed.Settings.ChecksModel == "deepseek/deepseek-v4-flash@openrouter"
	})
	if _, err := backend.SetTaskModels(appwire.TaskModelsPatch{Revision: before.Revision, Pins: map[string]*appwire.ModelRef{appwire.RoleChecker: nil}}); wireCode(err) != appwire.CodeConflict {
		t.Errorf("a stale revision: %v", err)
	}
	if _, err := backend.SetTaskModels(appwire.TaskModelsPatch{Pins: map[string]*appwire.ModelRef{appwire.RoleWorker: {ID: "nobody/listed"}}}); wireCode(err) != appwire.CodeBadParams {
		t.Errorf("a pin to nothing listed: %v", err)
	}
	if _, err := backend.SetTaskModels(appwire.TaskModelsPatch{Pins: map[string]*appwire.ModelRef{appwire.RoleWorker: {ID: "anthropic/claude-opus-5", Provider: "openrouter"}}}); wireCode(err) != appwire.CodeBadParams {
		t.Errorf("a pin to a model unticked a moment ago: %v", err)
	}
}

// A BUDGET WRITE IS NEVER HELD UP BY THE TASK MODELS: its settings.changed
// carries none; a Checks change carries them, because it is the checker pin.
func TestAppSettingsChangedCarriesTaskModelsOnlyForTheirRows(t *testing.T) {
	backend, rec := taskModelsBackend(t)
	usd := 7.0
	if _, err := backend.SetSettings(appwire.SettingsPatch{TaskCapUSD: &appwire.Figure{USD: &usd}}); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 0, func(event string, _ any) bool { return event == appwire.EventSettingsChanged })
	rec.mu.Lock()
	if changed := rec.data[at].(appwire.SettingsChanged); changed.TaskModels != nil || changed.Settings.TaskCapUSD == nil || *changed.Settings.TaskCapUSD != 7 {
		t.Errorf("a cap change said %+v", changed)
	}
	rec.mu.Unlock()
	checks := "z-ai/glm-5.3-flash"
	if _, err := backend.SetSettings(appwire.SettingsPatch{ChecksModel: &checks}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 0, func(event string, data any) bool {
		changed, ok := data.(appwire.SettingsChanged)
		return ok && changed.TaskModels != nil && changed.TaskModels.Roles.Checker.Pin != nil && changed.TaskModels.Roles.Checker.Pin.ID == checks
	})
	if got := config.CrewTaskCapAt(config.ProfileDir()); got != 7 {
		t.Errorf("the task cap is %v, want the one row Spending writes", got)
	}
}

// ONE LIST DECIDES WHAT IS SERVED. A model models.list offers on a provider is
// a model settings.models.set accepts there, even when the crew's own rows were
// read from an older list that lacks it.
func TestAppTaskModelsSetAcceptsAModelTheModelsListServes(t *testing.T) {
	backend, _ := taskModelsBackend(t)
	freshEngineProcess(t)
	const listed = "deepseek/deepseek-v4.1-flash"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"id":"`+listed+`","name":"Flash","context_length":1048576,"supported_parameters":["tools"],"pricing":{"prompt":"0.00000005","completion":"0.0000002"}}]}`)
	}))
	t.Cleanup(server.Close)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	options := catalog.Options{BaseURL: server.URL, APIKey: settings.APIKey, Dir: settings.ProfileDir, HTTPClient: server.Client()}
	models := catalog.LoadLazy(t.Context(), options)
	t.Cleanup(models.Close)
	proc := &v3Process{Settings: settings, ProfileDir: settings.ProfileDir, Models: models, Shelf: newV3ModelShelf(models, options)}
	proc.Shelf.setSources(settings.Sources)
	engineProcess.mu.Lock()
	engineProcess.proc, engineProcess.tried = proc, true
	engineProcess.mu.Unlock()

	served, err := backend.ListModels()
	if err != nil || !slices.ContainsFunc(served, func(m appwire.Model) bool { return m.ID == listed && m.Provider == "openrouter" }) {
		t.Fatalf("models.list = %+v, %v; the test needs it to serve %s", served, err, listed)
	}
	mode := appwire.TaskModelsOne
	got, err := backend.SetTaskModels(appwire.TaskModelsPatch{Mode: &mode, OneModel: &appwire.ModelRef{ID: listed, Provider: "openrouter"}})
	if err != nil {
		t.Fatalf("a model models.list serves was refused: %v", err)
	}
	if got.Mode != appwire.TaskModelsOne || got.OneModel == nil || got.OneModel.ID != listed {
		t.Fatalf("after the set %+v", got)
	}
	// And a task is then routed onto it, every role.
	d, err := config.RouteCrew(config.ProfileDir(), config.CrewAsk{})
	if err != nil {
		t.Fatalf("a task under the one model: %v", err)
	}
	for _, seat := range crewroute.Seats {
		if pick := d.Seat(seat); pick.Send != listed {
			t.Errorf("the %s role runs %q, want %s", seat, pick.Send, listed)
		}
	}
}

// THE SMALL-JOBS MODEL IS A ROLE PIN LIKE THE OTHER THREE. settings.models.get
// says it as roles.auxiliary, settings.models.set takes pins.auxiliary with the
// same checks, the profile's small-work tier then answers it, settings.changed
// says it, and null gives the build's own default back.
func TestAppTaskModelsAuxiliaryPinReadsSetsAndClears(t *testing.T) {
	backend, rec := taskModelsBackend(t)
	before, err := backend.TaskModels()
	if err != nil {
		t.Fatal(err)
	}
	if before.Roles.Auxiliary == nil || before.Roles.Auxiliary.Pin != nil {
		t.Fatalf("an unset small-jobs model reads %+v, want the role with no pin", before.Roles.Auxiliary)
	}
	small := &appwire.ModelRef{ID: "deepseek/deepseek-v4-flash", Provider: "openrouter"}
	got, err := backend.SetTaskModels(appwire.TaskModelsPatch{Pins: map[string]*appwire.ModelRef{appwire.RoleAuxiliary: small}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Roles.Auxiliary == nil || got.Roles.Auxiliary.Pin == nil || *got.Roles.Auxiliary.Pin != *small || got.Revision == before.Revision {
		t.Fatalf("after the set %+v", got.Roles.Auxiliary)
	}
	if model := config.TierModelAt(config.ProfileDir(), config.ModelTierLow); model != small.ID {
		t.Fatalf("the small-work tier answers %q, want %q", model, small.ID)
	}
	rec.waitFor(t, 0, func(event string, data any) bool {
		changed, ok := data.(appwire.SettingsChanged)
		return event == appwire.EventSettingsChanged && ok && changed.TaskModels != nil && changed.TaskModels.Roles.Auxiliary != nil &&
			changed.TaskModels.Roles.Auxiliary.Pin != nil && *changed.TaskModels.Roles.Auxiliary.Pin == *small
	})
	if _, err := backend.SetTaskModels(appwire.TaskModelsPatch{Pins: map[string]*appwire.ModelRef{appwire.RoleAuxiliary: {ID: "nobody/listed"}}}); wireCode(err) != appwire.CodeBadParams {
		t.Errorf("a small-jobs pin to nothing listed: %v", err)
	}
	cleared, err := backend.SetTaskModels(appwire.TaskModelsPatch{Pins: map[string]*appwire.ModelRef{appwire.RoleAuxiliary: nil}})
	if err != nil || cleared.Roles.Auxiliary == nil || cleared.Roles.Auxiliary.Pin != nil {
		t.Fatalf("after clearing %+v, %v", cleared.Roles.Auxiliary, err)
	}
	if model := config.TierModelAt(config.ProfileDir(), config.ModelTierLow); model != config.DefaultLowModel {
		t.Fatalf("a cleared small-jobs model answers %q, want the build's default %q", model, config.DefaultLowModel)
	}
}

// insightModelListed puts the page AI's default model in the test catalog, as
// OpenRouter's own listing carries it.
func insightModelListed(t *testing.T) {
	t.Helper()
	rows := config.CrewCatalog()
	rows = append(rows, catalog.Model{ID: "deepseek/deepseek-v4.1-flash", OpenWeights: true, PromptPrice: 1e-7, CompletionPrice: 2e-7, ContextLength: 1048576, Parameters: []string{"tools"}})
	config.CrewCatalog = func() []catalog.Model { return rows }
}

// insightResolves is the model a page-AI errand of role would run on, read
// through the same live source an agent's calls read.
func insightResolves(t *testing.T, role roles.Role) string {
	t.Helper()
	source, err := v3RolesSource(t.TempDir(), config.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	model, err := roles.Resolve(roles.Source(source), role, "floor/model")
	if err != nil {
		t.Fatal(err)
	}
	return model
}

// THE PAGE AI's DEFAULT IS THE ENGINE'S, NEVER A WRITTEN PIN. With OpenRouter
// connected and the model allowed, insight reads no pin and a default of
// deepseek/deepseek-v4.1-flash on openrouter, the overrides follow insight, and
// an errand of any of them resolves to that default. Nothing is written.
func TestAppTaskModelsInsightDefaultIsSeededNotWritten(t *testing.T) {
	backend, _ := taskModelsBackend(t)
	insightModelListed(t)
	got, err := backend.TaskModels()
	if err != nil {
		t.Fatal(err)
	}
	insight := got.Roles.Insight
	want := appwire.ModelRef{ID: "deepseek/deepseek-v4.1-flash", Provider: "openrouter"}
	if insight == nil || insight.Pin != nil || insight.Default == nil || *insight.Default != want || insight.Follows != "" || insight.EligibleCount == 0 {
		t.Fatalf("roles.insight = %+v", insight)
	}
	for name, row := range map[string]*appwire.TaskModelRole{"notes": got.Roles.InsightNotes, "ask": got.Roles.InsightAsk, "themes": got.Roles.InsightThemes} {
		if row == nil || row.Pin != nil || row.Default != nil || row.Follows != appwire.RoleInsight {
			t.Fatalf("roles.insight_%s = %+v", name, row)
		}
	}
	if config.ModelRolesAt(config.ProfileDir()) != "" {
		t.Fatalf("the default was written as a pin: %q", config.ModelRolesAt(config.ProfileDir()))
	}
	for _, role := range []roles.Role{roles.RoleInsight, roles.RoleInsightNotes, roles.RoleInsightAsk, roles.RoleInsightThemes} {
		if model := insightResolves(t, role); model != want.ID {
			t.Errorf("%s resolves to %q, want the default %q", role, model, want.ID)
		}
	}
	// The default never reaches a role outside the family.
	if model := insightResolves(t, roles.RoleTitle); model == want.ID {
		t.Errorf("title took the page AI's default")
	}
}

// A DEFAULT THE ALLOWED MODELS DO NOT ADMIT IS NO DEFAULT: insight follows its
// tier, and the row says so by carrying none.
func TestAppTaskModelsInsightFollowsItsTierWhenTheDefaultIsNotAllowed(t *testing.T) {
	backend, _ := taskModelsBackend(t)
	insightModelListed(t)
	got, err := backend.SetTaskModels(appwire.TaskModelsPatch{Allowed: []appwire.TaskModelAllowed{{Model: appwire.ModelRef{ID: "deepseek/deepseek-v4.1-flash", Provider: "openrouter"}, Allowed: false}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Roles.Insight == nil || got.Roles.Insight.Default != nil || got.Roles.Insight.Pin != nil {
		t.Fatalf("roles.insight = %+v", got.Roles.Insight)
	}
	if model := insightResolves(t, roles.RoleInsightNotes); model != config.TierModelAt(config.ProfileDir(), config.ModelTierLow) {
		t.Fatalf("insight_notes resolves to %q, want the small-work tier", model)
	}
}

// With no OpenRouter key there is no default either.
func TestAppTaskModelsInsightHasNoDefaultWithoutOpenRouter(t *testing.T) {
	backend, _ := taskModelsBackend(t)
	insightModelListed(t)
	if err := config.WriteAPIKey(config.ProfileDir(), ""); err != nil {
		t.Fatal(err)
	}
	got, err := backend.TaskModels()
	if err != nil {
		t.Fatal(err)
	}
	if got.Roles.Insight == nil || got.Roles.Insight.Default != nil {
		t.Fatalf("roles.insight = %+v", got.Roles.Insight)
	}
}

// A PAGE-AI PIN IS A ROLE PIN: settings.models.set writes it to the role pins
// row beside the person's other pins, an override pin wins over the family's,
// an insight pin outranks the default, and null gives each back.
func TestAppTaskModelsInsightPinsReadSetAndClear(t *testing.T) {
	backend, rec := taskModelsBackend(t)
	insightModelListed(t)
	pinsRow, _ := config.NewSettings(config.SettingsOptions{ProfileDir: config.ProfileDir()}).Row(config.KeyModelRoles)
	if err := pinsRow.Apply("title:z-ai/glm-5.3-flash"); err != nil {
		t.Fatal(err)
	}
	before, err := backend.TaskModels()
	if err != nil {
		t.Fatal(err)
	}
	kimi := &appwire.ModelRef{ID: "moonshotai/kimi-k3", Provider: "openrouter"}
	glm := &appwire.ModelRef{ID: "z-ai/glm-5.3-flash", Provider: "openrouter"}
	got, err := backend.SetTaskModels(appwire.TaskModelsPatch{Pins: map[string]*appwire.ModelRef{appwire.RoleInsight: glm, appwire.RoleInsightNotes: kimi}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Roles.Insight.Pin == nil || *got.Roles.Insight.Pin != *glm || got.Roles.InsightNotes.Pin == nil || *got.Roles.InsightNotes.Pin != *kimi || got.Revision == before.Revision {
		t.Fatalf("after the set insight=%+v notes=%+v", got.Roles.Insight, got.Roles.InsightNotes)
	}
	if model := insightResolves(t, roles.RoleInsightNotes); model != kimi.ID {
		t.Errorf("insight_notes resolves to %q", model)
	}
	if model := insightResolves(t, roles.RoleInsightAsk); model != glm.ID {
		t.Errorf("insight_ask resolves to %q, want the family pin", model)
	}
	if model := insightResolves(t, roles.RoleTitle); model != "z-ai/glm-5.3-flash" {
		t.Errorf("the person's title pin was lost: %q", model)
	}
	rec.waitFor(t, 0, func(event string, data any) bool {
		changed, ok := data.(appwire.SettingsChanged)
		return event == appwire.EventSettingsChanged && ok && changed.TaskModels != nil && changed.TaskModels.Roles.InsightNotes != nil &&
			changed.TaskModels.Roles.InsightNotes.Pin != nil
	})
	if _, err := backend.SetTaskModels(appwire.TaskModelsPatch{Pins: map[string]*appwire.ModelRef{appwire.RoleInsightAsk: {ID: "nobody/listed"}}}); wireCode(err) != appwire.CodeBadParams {
		t.Errorf("an insight pin to nothing listed: %v", err)
	}
	cleared, err := backend.SetTaskModels(appwire.TaskModelsPatch{Pins: map[string]*appwire.ModelRef{appwire.RoleInsight: nil, appwire.RoleInsightNotes: nil}})
	if err != nil || cleared.Roles.Insight.Pin != nil || cleared.Roles.InsightNotes.Pin != nil || cleared.Roles.Insight.Default == nil {
		t.Fatalf("after clearing insight=%+v notes=%+v %v", cleared.Roles.Insight, cleared.Roles.InsightNotes, err)
	}
	if row := config.ModelRolesAt(config.ProfileDir()); row != "title:z-ai/glm-5.3-flash" {
		t.Fatalf("the pins row after clearing = %q", row)
	}
}
