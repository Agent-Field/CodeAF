package config

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/claudemodel"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// defaultsFixture is a profile's services and what each one lists, with the
// OpenRouter row present but keyless unless a key is given.
type defaultsFixture struct {
	openRouterKey string
	services      []modelsource.Connected
	models        map[string][]catalog.Model
}

func (f defaultsFixture) set() modelsource.Set {
	services := append([]modelsource.Connected{{Source: modelsource.DefaultSource(""), Key: f.openRouterKey}}, f.services...)
	return modelsource.NewSet(services...)
}

func (f defaultsFixture) pick(slot DefaultSlot, builtIn, chosenTalk string) string {
	sources := f.set()
	connected := connectedServices(sources)
	if len(connected) == 0 {
		return builtIn
	}
	if builtIn != "" && sendable(sources, builtIn) {
		return builtIn
	}
	return reachableDefault(sources, connected, slot, chosenTalk, func(service modelsource.Connected) []catalog.Model {
		return f.models[strings.ToLower(service.Source.ID)]
	})
}

// codexRows are the shapes ChatGPT's account listing returned on 2026-11-23:
// unfamiliar names, no prices, and thinking levels as the only strength signal.
func codexRows() []catalog.Model {
	all := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	return []catalog.Model{
		{ID: "gpt-6.1-terra", PriceUnknown: true, Reasoning: catalog.ReasoningProfile{Efforts: all}},
		{ID: "gpt-6.1-sol", PriceUnknown: true, Reasoning: catalog.ReasoningProfile{Efforts: all}},
		{ID: "gpt-6-astra", PriceUnknown: true, Reasoning: catalog.ReasoningProfile{Efforts: all}},
		{ID: "gpt-6-luna", PriceUnknown: true, Reasoning: catalog.ReasoningProfile{Efforts: all[:5]}},
	}
}

func TestDefaultsKeepTheBuiltInModelWhileOpenRouterCanSendIt(t *testing.T) {
	f := defaultsFixture{openRouterKey: "sk-or-1234567890", services: []modelsource.Connected{{Source: vendoredSource(t, "codex"), Key: "codex-sentinel"}}, models: map[string][]catalog.Model{"codex": codexRows()}}
	for _, slot := range []DefaultSlot{SlotTalk, SlotFallback, SlotLight} {
		if got := f.pick(slot, DefaultModel, ""); got != DefaultModel {
			t.Fatalf("%s with an OpenRouter key = %q, want the built-in %q", slot, got, DefaultModel)
		}
	}
}

func TestDefaultsWithNothingConnectedStayTheBuiltInModel(t *testing.T) {
	f := defaultsFixture{}
	if got := f.pick(SlotTalk, DefaultModel, ""); got != DefaultModel {
		t.Fatalf("talk with nothing connected = %q, want %q", got, DefaultModel)
	}
}

func TestChatGPTOnlyDefaultsAreAllChatGPTModels(t *testing.T) {
	f := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, "codex"), Key: "codex-sentinel"}}, models: map[string][]catalog.Model{"codex": codexRows()}}
	talk := f.pick(SlotTalk, DefaultModel, "")
	fallback := f.pick(SlotFallback, DefaultFallbackModel, "")
	light := f.pick(SlotLight, DefaultLowModel, "")
	for slot, got := range map[string]string{"talk": talk, "fallback": fallback, "light": light} {
		if !strings.HasPrefix(got, "codex/") {
			t.Fatalf("%s = %q, want a ChatGPT model", slot, got)
		}
	}
	if talk != "codex/gpt-6.1-terra" {
		t.Fatalf("talk = %q, want the account's first strong model", talk)
	}
	if fallback == talk {
		t.Fatalf("fallback = talk = %q, want a different model", fallback)
	}
	if light != "codex/gpt-6-luna" {
		t.Fatalf("light = %q, want the model with the fewest thinking levels", light)
	}
}

func TestChatGPTPreferredModelIsTheTalkModelWhenListed(t *testing.T) {
	rows := append(codexRows(), catalog.Model{ID: vendoredSource(t, "codex").Preferred, PriceUnknown: true})
	f := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, "codex"), Key: "codex-sentinel"}}, models: map[string][]catalog.Model{"codex": rows}}
	if got, want := f.pick(SlotTalk, DefaultModel, ""), "codex/"+vendoredSource(t, "codex").Preferred; got != want {
		t.Fatalf("talk = %q, want the provider's preferred %q", got, want)
	}
}

func TestOllamaDefaultsUseTheLocalModels(t *testing.T) {
	ollama := modelsource.Connected{Source: vendoredSource(t, "ollama")}
	one := defaultsFixture{services: []modelsource.Connected{ollama}, models: map[string][]catalog.Model{"ollama": {{ID: "qwen3:8b"}}}}
	if got := one.pick(SlotTalk, DefaultModel, ""); got != "ollama/qwen3:8b" {
		t.Fatalf("talk = %q, want ollama/qwen3:8b", got)
	}
	if got := one.pick(SlotLight, DefaultReflexModel, ""); got != "ollama/qwen3:8b" {
		t.Fatalf("light = %q, want the one local model", got)
	}
	if got := one.pick(SlotFallback, DefaultFallbackModel, "ollama/qwen3:8b"); got != "" {
		t.Fatalf("fallback with one local model = %q, want none rather than an unreachable model", got)
	}

	two := defaultsFixture{services: []modelsource.Connected{ollama}, models: map[string][]catalog.Model{"ollama": {{ID: "qwen3:8b"}, {ID: "llama3.2:3b"}, {ID: "nomic-embed-text"}}}}
	if got := two.pick(SlotTalk, DefaultModel, ""); got != "ollama/qwen3:8b" {
		t.Fatalf("talk = %q, want the larger local model", got)
	}
	if got := two.pick(SlotLight, DefaultLowModel, ""); got != "ollama/llama3.2:3b" {
		t.Fatalf("light = %q, want the smaller local model, never the embedding model", got)
	}
	if got := two.pick(SlotFallback, DefaultFallbackModel, "ollama/qwen3:8b"); got != "ollama/llama3.2:3b" {
		t.Fatalf("fallback = %q, want the other local model", got)
	}
}

func TestClaudeCodeDefaultsSpanItsThreeModels(t *testing.T) {
	f := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, claudemodel.ID), Key: claudemodel.Sentinel}}, models: map[string][]catalog.Model{claudemodel.ID: claudemodel.Models()}}
	talk := f.pick(SlotTalk, DefaultModel, "")
	if want := claudemodel.ID + "/" + vendoredSource(t, claudemodel.ID).Preferred; talk != want {
		t.Fatalf("talk = %q, want the preferred %q", talk, want)
	}
	if got := f.pick(SlotLight, DefaultLowModel, ""); !strings.Contains(got, "haiku") {
		t.Fatalf("light = %q, want haiku", got)
	}
	if got := f.pick(SlotFallback, DefaultFallbackModel, talk); !strings.Contains(got, "opus") {
		t.Fatalf("fallback = %q, want the strongest model other than the talk model", got)
	}
}

func TestDefaultsFollowTheSavedTalkModelsProvider(t *testing.T) {
	f := defaultsFixture{
		services: []modelsource.Connected{{Source: vendoredSource(t, "codex"), Key: "codex-sentinel"}, {Source: vendoredSource(t, "ollama")}},
		models:   map[string][]catalog.Model{"codex": codexRows(), "ollama": {{ID: "qwen3:8b"}, {ID: "llama3.2:3b"}}},
	}
	if got := f.pick(SlotLight, DefaultLowModel, "ollama/qwen3:8b"); got != "ollama/llama3.2:3b" {
		t.Fatalf("light with an Ollama talk model = %q, want Ollama's light model", got)
	}
	if got := f.pick(SlotLight, DefaultLowModel, "codex/gpt-6.1-terra"); got != "codex/gpt-6-luna" {
		t.Fatalf("light with a ChatGPT talk model = %q, want ChatGPT's light model", got)
	}
}

func TestAHostedProviderAnswersBeforeALocalOneWhenNothingIsChosen(t *testing.T) {
	f := defaultsFixture{
		services: []modelsource.Connected{{Source: vendoredSource(t, "ollama")}, {Source: vendoredSource(t, "codex"), Key: "codex-sentinel"}},
		models:   map[string][]catalog.Model{"codex": codexRows(), "ollama": {{ID: "qwen3:8b"}, {ID: "llama3.2:3b"}}},
	}
	if got := f.pick(SlotTalk, DefaultModel, ""); got != "codex/gpt-6.1-terra" {
		t.Fatalf("talk = %q, want ChatGPT before the local server", got)
	}
	if got := f.pick(SlotLight, DefaultLowModel, ""); got != "codex/gpt-6-luna" {
		t.Fatalf("light = %q, want the talk model's provider", got)
	}
	if got := f.pick(SlotFallback, DefaultFallbackModel, ""); got != "codex/gpt-6.1-sol" {
		t.Fatalf("fallback = %q, want the talk model's provider", got)
	}
}

func TestOneLocalModelFallsBackToAnotherConnectedProvider(t *testing.T) {
	f := defaultsFixture{
		services: []modelsource.Connected{{Source: vendoredSource(t, "ollama")}, {Source: vendoredSource(t, "codex"), Key: "codex-sentinel"}},
		models:   map[string][]catalog.Model{"codex": codexRows(), "ollama": {{ID: "qwen3:8b"}}},
	}
	if got := f.pick(SlotFallback, DefaultFallbackModel, "ollama/qwen3:8b"); got != "codex/gpt-6.1-terra" {
		t.Fatalf("fallback = %q, want ChatGPT's talk model", got)
	}
}

func TestRankingReadsPublishedSignalsBeforeNames(t *testing.T) {
	models := []catalog.Model{
		{ID: "vendor/pro-model", PromptPrice: 1e-7, CompletionPrice: 1e-7},
		{ID: "vendor/mini-model", PromptPrice: 5e-6, CompletionPrice: 5e-6},
	}
	ranked := rankByStrength(models)
	if ranked[0].ID != "vendor/pro-model" {
		t.Fatalf("lightest = %q, want the cheaper model whatever its name says", ranked[0].ID)
	}
}

func TestThinkingLevelsAreNotPartOfTheRoute(t *testing.T) {
	if got := stripLevel("codex/gpt-6.1-terra:high"); got != "codex/gpt-6.1-terra" {
		t.Fatalf("stripLevel = %q", got)
	}
	if got := stripLevel("qwen/qwen3.8-27b:free"); got != "qwen/qwen3.8-27b:free" {
		t.Fatalf("stripLevel kept a free route = %q", got)
	}
}

func TestReachableDefaultAtReadsTheProfile(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	if got := ReachableDefaultAt(dir, SlotTalk, DefaultModel); got != DefaultModel {
		t.Fatalf("an empty profile = %q, want the built-in %q", got, DefaultModel)
	}
	listed := true
	if err := WriteSources(dir, []PersistedSource{{ID: "ollama", Written: "ollama", Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	ollama, ok := ResolveSources(dir, "", DefaultBaseURL).ByID("ollama")
	if !ok {
		t.Fatal("the Ollama row did not resolve")
	}
	if err := catalog.Remember(CatalogOptionsFor(ollama, dir), []catalog.Model{{ID: "qwen3:8b"}}); err != nil {
		t.Fatal(err)
	}
	if got := ChatDefaultAt(dir); got != "ollama/qwen3:8b" {
		t.Fatalf("ChatDefaultAt with only Ollama = %q, want ollama/qwen3:8b", got)
	}
	if got := KnowledgeModelAt(dir); got != "ollama/qwen3:8b" {
		t.Fatalf("KnowledgeModelAt with only Ollama = %q", got)
	}
	if got := ModelFallbacksAt(dir); got != "" {
		t.Fatalf("ModelFallbacksAt with one local model = %q, want none", got)
	}
	if err := WriteAPIKey(dir, "sk-or-1234567890"); err != nil {
		t.Fatal(err)
	}
	if got := ChatDefaultAt(dir); got != DefaultModel {
		t.Fatalf("ChatDefaultAt with an OpenRouter key = %q, want the built-in %q", got, DefaultModel)
	}
}

func TestChatGPTCrewRoutesServeTheAccountsListedModels(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	if err := codexauth.Save(dir, codexauth.Tokens{AccessToken: "placeholder", RefreshToken: "placeholder", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := WriteSources(dir, []PersistedSource{{ID: "codex", Written: "codex", Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	find := func() CrewProvider {
		for _, p := range CrewProvidersAt(dir) {
			if p.ID == "codex" {
				return p
			}
		}
		t.Fatal("ChatGPT is not a crew provider")
		return CrewProvider{}
	}
	if got := find().Serves; len(got) != 1 || got[0] != vendoredSource(t, "codex").Preferred {
		t.Fatalf("with nothing remembered, serves = %v, want the preferred model", got)
	}
	service, _ := ResolveSources(dir, "", DefaultBaseURL).ByID("codex")
	if err := catalog.Remember(CatalogOptionsFor(service, dir), codexRows()); err != nil {
		t.Fatal(err)
	}
	got := find().Serves
	if len(got) != len(codexRows()) || got[0] != "gpt-6.1-terra" {
		t.Fatalf("serves = %v, want the account's listed models", got)
	}
	for _, id := range got {
		if id == vendoredSource(t, "codex").Preferred {
			t.Fatalf("serves the preferred %q the account did not list", id)
		}
	}
}
