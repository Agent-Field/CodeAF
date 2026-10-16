package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
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
