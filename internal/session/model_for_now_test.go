package session

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// C1, C3, C4: a run-only model serves requests without replacing saved choices.
func TestRunOnlyModelSurvivesSavingAsTheConfiguredChoice(t *testing.T) {
	for _, paired := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "pair"}[paired], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			c := chained(nil)
			a, _ := newTestAgent(t, c, func(cfg *Config) {
				cfg.SessionFile, cfg.Place = path, Place{Dir: filepath.Dir(path)}
				cloud := modelsource.DefaultSource("http://127.0.0.1:1")
				local := modelsource.Source{ID: "local", Written: "available", Address: "http://127.0.0.1:2", KeyOptional: true}
				cfg.Sources = modelsource.NewSet(modelsource.Connected{Source: cloud, Key: "fixture", Address: cloud.Address}, modelsource.Connected{Source: local, Address: local.Address})
				if paired {
					cfg.ConversationModels = &ConversationModels{Main: cfg.Model, Fallback: "chosen/backup"}
				}
			})
			a.stampModel()
			a.SettleWrites()
			before, err := LoadMeta(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if err := a.UseModelForNow("available/model"); err != nil {
				t.Fatal(err)
			}
			if a.config.serviceFor(a.Model()).Source.ID == before.ModelProvider {
				t.Fatal("fixture must replace the configured provider as well as its model")
			}
			runModelForNowTurn(t, a)
			if c.model(0) != "model" || a.Model() != "available/model" || a.ConfiguredModel() != "test/model" {
				t.Fatalf("request=%q configured=%q", c.model(0), a.ConfiguredModel())
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			a.SettleWrites()
			meta, err := LoadMeta(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if meta.Model != before.Model || meta.ModelProvider != before.ModelProvider || meta.ModelIdentity == nil || meta.ModelIdentity.Selected != before.Model {
				t.Fatalf("saved choice changed: before=%+v after=%+v identity=%+v", before, meta, meta.ModelIdentity)
			}
			if paired && (meta.ModelIdentity.Models == nil || *meta.ModelIdentity.Models != *before.ModelIdentity.Models) {
				t.Fatalf("saved pair changed: %+v", meta.ModelIdentity.Models)
			}
			fresh := chained(nil)
			b, _ := newTestAgent(t, fresh, func(cfg *Config) {
				cfg.Model, cfg.Place = meta.Model, Place{Dir: filepath.Dir(path)}
			})
			if b.Model() != "test/model" || b.ModelIdentity().Selected != "test/model" {
				t.Fatalf("reopened on %q: %+v", b.Model(), b.ModelIdentity())
			}
			routes, _ := b.ErrandRoutes()
			found := false
			for _, route := range routes {
				if route.Role == "title" {
					found = true
					if route.Model != "test/model" {
						t.Fatalf("reopened title route: %+v", route)
					}
				}
			}
			if !found {
				t.Fatal("title route missing")
			}
			runModelForNowTurn(t, b)
			if fresh.model(0) != "test/model" {
				t.Fatalf("fresh request=%q", fresh.model(0))
			}
		})
	}
}

// C5: an explicit pair edit clears the temporary main, and promoting its
// current fallback through SetModel saves that model as the new main.
func TestExplicitPairSelectionClearsTheRunOnlyMain(t *testing.T) {
	a, _ := newTestAgent(t, chained(nil), func(cfg *Config) {
		cfg.Place = Place{Dir: t.TempDir()}
		cfg.ConversationModels = &ConversationModels{Main: cfg.Model, Fallback: "available/model"}
	})
	if err := a.UseModelForNow("available/model"); err != nil {
		t.Fatal(err)
	}
	pair := ConversationModels{Main: "test/model", Fallback: "available/model"}
	if _, err := a.SetConversationModels(pair); err != nil {
		t.Fatal(err)
	}
	if a.Model() != "test/model" || a.ConfiguredModel() != "test/model" {
		t.Fatal("explicit pair edit left a temporary main")
	}
	if err := a.UseModelForNow("available/model"); err != nil {
		t.Fatal(err)
	}
	a.SetModel("available/model")
	a.SettleWrites()
	meta, err := LoadMeta(a.config.Place.Dir)
	if err != nil || meta.Model != "available/model" || meta.ModelIdentity.Models.Main != "available/model" || meta.ModelIdentity.Models.Fallback != "" {
		t.Fatalf("explicit fallback promotion=%+v %v", meta, err)
	}
}

// C4, C5: restoration is transient; confirming the running fallback is durable.
func TestExplicitModelChoiceClearsRunOnlyModel(t *testing.T) {
	for _, paired := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "pair"}[paired], func(t *testing.T) {
			c := chained(nil)
			a, _ := newTestAgent(t, c, func(cfg *Config) {
				cfg.Place = Place{Dir: t.TempDir()}
				if paired {
					cfg.ConversationModels = &ConversationModels{Main: cfg.Model, Fallback: "chosen/backup"}
				}
			})
			for _, model := range []string{"available/model", "test/model", "available/model"} {
				if err := a.UseModelForNow(model); err != nil {
					t.Fatal(err)
				}
				runModelForNowTurn(t, a)
			}
			if c.model(1) != "test/model" {
				t.Fatalf("restored request=%q", c.model(1))
			}
			a.SetModel("available/model")
			a.SettleWrites()
			meta, err := LoadMeta(a.config.Place.Dir)
			if err != nil || meta.Model != "available/model" || a.ConfiguredModel() != "available/model" {
				t.Fatalf("explicit choice not saved: %+v %v", meta, err)
			}
			if paired && (meta.ModelIdentity.Models.Main != "available/model" || meta.ModelIdentity.Models.Fallback != "chosen/backup") {
				t.Fatalf("explicit pair=%+v", meta.ModelIdentity.Models)
			}
		})
	}
}

func runModelForNowTurn(t *testing.T, a *Agent) {
	t.Helper()
	events, err := a.Submit(t.Context(), "respond")
	if err != nil {
		t.Fatal(err)
	}
	if got := collect(t, events); len(textsOfKind(got, EventError)) != 0 {
		t.Fatalf("turn failed: %v", textsOfKind(got, EventError))
	}
}

// C1: removing a provider must preserve its identity beside the saved model.
func TestRunOnlyModelKeepsConfiguredProviderWhenDisconnected(t *testing.T) {
	cloud := modelsource.DefaultSource("http://127.0.0.1:1")
	local := modelsource.Source{ID: "local", Written: "available", Address: "http://127.0.0.1:2", KeyOptional: true}
	remaining := modelsource.NewSet(modelsource.Connected{Source: cloud, Key: "fixture", Address: cloud.Address})
	a, _ := newTestAgent(t, chained(nil), func(cfg *Config) {
		cfg.Model, cfg.Place = "available/configured", Place{Dir: t.TempDir()}
		cfg.Sources = modelsource.NewSet(modelsource.Connected{Source: cloud, Key: "fixture", Address: cloud.Address}, modelsource.Connected{Source: local, Address: local.Address})
	})
	a.stampModel()
	a.SettleWrites()
	a.SetSources(remaining)
	// Linked-local source refresh reapplies the live model before reconciliation.
	if err := a.UseModelForNow(a.Model()); err != nil {
		t.Fatal(err)
	}
	if err := a.UseModelForNow("vendor/replacement"); err != nil {
		t.Fatal(err)
	}
	a.SettleWrites()
	meta, err := LoadMeta(a.config.Place.Dir)
	if err != nil || meta.Model != "available/configured" || meta.ModelProvider != "local" {
		t.Fatalf("disconnected model identity=%+v, %v", meta, err)
	}
}
