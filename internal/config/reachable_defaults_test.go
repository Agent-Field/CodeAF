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
	return chooseDefault(f.set(), slot, builtIn, chosenTalk, func(service modelsource.Connected) []catalog.Model {
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
	if got := one.pick(SlotFallback, DefaultFallbackModel, "ollama/qwen3:8b"); got != "ollama/qwen3:8b" {
		t.Fatalf("fallback with one local model = %q, want that model, never an unreachable one", got)
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
	if got := ModelFallbacksAt(dir); got != "ollama/qwen3:8b" {
		t.Fatalf("ModelFallbacksAt with one local model = %q, want that model", got)
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

func ids(names ...string) []catalog.Model {
	rows := make([]catalog.Model, 0, len(names))
	for _, name := range names {
		rows = append(rows, catalog.Model{ID: name})
	}
	return rows
}

// TestEveryShippedProviderGetsReachableDefaultsOfItsOwn connects each shipped
// provider alone, with the models it lists (or, for one that publishes no list,
// the models it names), and checks the conversation, fallback and light seats.
func TestEveryShippedProviderGetsReachableDefaultsOfItsOwn(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	zai := vendoredSource(t, "z-ai")
	var codingPlan modelsource.Door
	for _, door := range zai.Doors {
		if door.ID == "coding-plan" {
			codingPlan = door
		}
	}
	custom := modelsource.Source{ID: modelsource.CustomID + "-lab", Written: "lab", Name: "lab", Listing: modelsource.ListingModels}
	cases := []struct {
		name                  string
		service               modelsource.Connected
		models                []catalog.Model
		talk, fallback, light string
	}{
		{
			name:    "DeepSeek",
			service: modelsource.Connected{Source: vendoredSource(t, "deepseek"), Key: "sk-ds"},
			models:  ids("deepseek-v4-flash", "deepseek-v4-pro"),
			talk:    "deepseek/deepseek-v4-pro", fallback: "deepseek/deepseek-v4-flash", light: "deepseek/deepseek-v4-flash",
		},
		{
			name:    "Z.ai coding plan",
			service: modelsource.Connected{Source: zai, Key: "zai", Door: codingPlan},
			models:  namedModels(modelsource.Connected{Source: zai, Door: codingPlan}),
			talk:    "z-ai/" + zai.Preferred, fallback: "z-ai/glm-5.3-flash", light: "z-ai/glm-5.3-flash",
		},
		{
			name:    "Moonshot",
			service: modelsource.Connected{Source: vendoredSource(t, "moonshot"), Key: "sk-moon"},
			models:  namedModels(modelsource.Connected{Source: vendoredSource(t, "moonshot")}),
			talk:    "moonshot/" + vendoredSource(t, "moonshot").Preferred, fallback: "moonshot/" + vendoredSource(t, "moonshot").Preferred, light: "moonshot/" + vendoredSource(t, "moonshot").Preferred,
		},
		{
			name:    "MiniMax",
			service: modelsource.Connected{Source: vendoredSource(t, "minimax"), Key: "sk-mm"},
			models:  namedModels(modelsource.Connected{Source: vendoredSource(t, "minimax")}),
			talk:    "minimax/" + vendoredSource(t, "minimax").Preferred, fallback: "minimax/" + vendoredSource(t, "minimax").Preferred, light: "minimax/" + vendoredSource(t, "minimax").Preferred,
		},
		{
			name:    "Qwen",
			service: modelsource.Connected{Source: vendoredSource(t, "qwen"), Key: "sk-qw"},
			models:  namedModels(modelsource.Connected{Source: vendoredSource(t, "qwen")}),
			talk:    "qwen/qwen3.7-plus", fallback: "qwen/qwen3.8-flash", light: "qwen/qwen3.8-flash",
		},
		{
			name:    "ai&",
			service: modelsource.Connected{Source: vendoredSource(t, "aiand"), Key: "sk-aiand"},
			models:  ids("zai-org/glm-5.3-flash", "deepseek-ai/deepseek-v4-flash", "zai-org/glm-5.3", "moonshotai/kimi-k3"),
			talk:    "aiand/zai-org/glm-5.3-flash", fallback: "aiand/zai-org/glm-5.3", light: "aiand/zai-org/glm-5.3-flash",
		},
		{
			name:    "OpenCode Go",
			service: modelsource.Connected{Source: vendoredSource(t, "opencode-go"), Key: "sk-oc"},
			models:  ids("kimi-k3", vendoredSource(t, "opencode-go").Preferred, "qwen3.8-flash"),
			talk:    "opencode-go/" + vendoredSource(t, "opencode-go").Preferred, fallback: "opencode-go/kimi-k3", light: "opencode-go/qwen3.8-flash",
		},
		{
			name:    "Claude Code",
			service: modelsource.Connected{Source: vendoredSource(t, claudemodel.ID), Key: claudemodel.Sentinel},
			models:  claudemodel.Models(),
			talk:    claudemodel.ID + "/sonnet", fallback: claudemodel.ID + "/opus", light: claudemodel.ID + "/haiku",
		},
		{
			name:    "a custom endpoint",
			service: modelsource.Connected{Source: custom, Key: "sk-lab", Address: "https://lab.example/v1"},
			models:  ids("lab-model-8b", "lab-model-70b"),
			talk:    "lab/lab-model-70b", fallback: "lab/lab-model-8b", light: "lab/lab-model-8b",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := strings.ToLower(c.service.Source.ID)
			f := defaultsFixture{services: []modelsource.Connected{c.service}, models: map[string][]catalog.Model{id: c.models}}
			talk := f.pick(SlotTalk, DefaultModel, "")
			if talk != c.talk {
				t.Fatalf("talk = %q, want %q", talk, c.talk)
			}
			if got := f.pick(SlotFallback, DefaultFallbackModel, talk); got != c.fallback {
				t.Fatalf("fallback = %q, want %q", got, c.fallback)
			}
			if got := f.pick(SlotLight, DefaultLowModel, talk); got != c.light {
				t.Fatalf("light = %q, want %q", got, c.light)
			}
			if got := f.pick(SlotLight, DefaultReflexModel, talk); got != c.light {
				t.Fatalf("reflex = %q, want %q", got, c.light)
			}
		})
	}
}

// A BUILT-IN ID THAT ROUTES TO A CONNECTED ACCOUNT BY ITS PREFIX IS KEPT ONLY
// WHEN THAT ACCOUNT LISTS IT: `deepseek/deepseek-v4.1-flash` routes to a direct
// DeepSeek connection, whose own list need not carry that id.
func TestABuiltInIDThatRoutesByPrefixIsKeptOnlyWhenListed(t *testing.T) {
	deepseek := modelsource.Connected{Source: vendoredSource(t, "deepseek"), Key: "sk-ds"}
	without := defaultsFixture{services: []modelsource.Connected{deepseek}, models: map[string][]catalog.Model{"deepseek": ids("deepseek-v4-pro", "deepseek-v4-flash")}}
	if got := without.pick(SlotFallback, DefaultFallbackModel, ""); got == DefaultFallbackModel {
		t.Fatalf("fallback kept the unlisted built-in %q", got)
	}
	with := defaultsFixture{services: []modelsource.Connected{deepseek}, models: map[string][]catalog.Model{"deepseek": ids("deepseek-v4-pro", "deepseek-v4.1-flash")}}
	if got := with.pick(SlotFallback, DefaultFallbackModel, ""); got != DefaultFallbackModel {
		t.Fatalf("fallback = %q, want the listed built-in %q", got, DefaultFallbackModel)
	}
	unknown := defaultsFixture{services: []modelsource.Connected{deepseek}, models: map[string][]catalog.Model{}}
	if got := unknown.pick(SlotFallback, DefaultFallbackModel, ""); got != DefaultFallbackModel {
		t.Fatalf("with no known list, fallback = %q, want the built-in on routing alone", got)
	}
}

// AN OPENROUTER KEY KEEPS EVERY BUILT-IN AS IT WAS, even when another connected
// account captures a built-in id by its prefix and does not list it: choosing
// among OpenRouter's whole catalog would send a fallback to its most expensive
// model and a helper to its cheapest.
func TestAnOpenRouterKeyKeepsEveryBuiltInEvenBesideDeepSeekDirect(t *testing.T) {
	f := defaultsFixture{
		openRouterKey: "sk-or-1234567890",
		services:      []modelsource.Connected{{Source: vendoredSource(t, "deepseek"), Key: "sk-ds"}},
		models: map[string][]catalog.Model{
			"openrouter": {{ID: "anthropic/claude-opus-4.5", PromptPrice: 1e-4}, {ID: "some/model:free"}},
			"deepseek":   ids("deepseek-v4-pro", "deepseek-v4-flash"),
		},
	}
	for _, c := range []struct {
		slot    DefaultSlot
		builtIn string
	}{
		{SlotTalk, DefaultModel}, {SlotFallback, DefaultFallbackModel}, {SlotLight, DefaultLowModel},
		{SlotLight, DefaultReflexModel}, {SlotLight, DefaultKnowledgeModel},
	} {
		for _, chosen := range []string{"", "deepseek/deepseek-v4-pro"} {
			if got := f.pick(c.slot, c.builtIn, chosen); got != c.builtIn {
				t.Fatalf("%s (talk %q) = %q, want the built-in %q unchanged", c.slot, chosen, got, c.builtIn)
			}
		}
	}
}

func TestTheTalkDefaultIsNeverEmptyWhenNoModelsAreKnown(t *testing.T) {
	f := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, "ollama")}}, models: map[string][]catalog.Model{}}
	for _, slot := range []DefaultSlot{SlotTalk, SlotFallback, SlotLight} {
		if got := f.pick(slot, DefaultModel, ""); got != DefaultModel {
			t.Fatalf("%s with a cold local catalog = %q, want the built-in rather than nothing", slot, got)
		}
	}
}

func TestTheSavedTalkModelIsTheTalkDefaultWhenTheBuiltInIsUnreachable(t *testing.T) {
	f := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, "codex"), Key: "codex-sentinel"}}, models: map[string][]catalog.Model{"codex": codexRows()}}
	if got := f.pick(SlotTalk, DefaultModel, "codex/gpt-6-astra"); got != "codex/gpt-6-astra" {
		t.Fatalf("talk = %q, want the saved conversation model", got)
	}
}

func TestAPausedPlanIsChosenOnlyWhenNothingElseIsConnected(t *testing.T) {
	paused := modelsource.Connected{Source: vendoredSource(t, "codex"), Key: "codex-sentinel", PlanPaused: "limit reached"}
	alone := defaultsFixture{services: []modelsource.Connected{paused}, models: map[string][]catalog.Model{"codex": codexRows()}}
	if got := alone.pick(SlotTalk, DefaultModel, ""); got != "codex/gpt-6.1-terra" {
		t.Fatalf("talk with only a paused plan = %q, want that plan", got)
	}
	beside := defaultsFixture{services: []modelsource.Connected{paused, {Source: vendoredSource(t, "ollama")}}, models: map[string][]catalog.Model{"codex": codexRows(), "ollama": ids("qwen3:8b")}}
	if got := beside.pick(SlotTalk, DefaultModel, ""); got != "ollama/qwen3:8b" {
		t.Fatalf("talk with a paused plan beside Ollama = %q, want Ollama", got)
	}
}

func TestAFallbackSkipsAVariantOfTheTalkModel(t *testing.T) {
	zai := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, "z-ai"), Key: "zai"}}, models: map[string][]catalog.Model{"z-ai": ids("glm-5.3", "glm-5.3[1m]")}}
	if got := zai.pick(SlotFallback, DefaultFallbackModel, "z-ai/glm-5.3"); got != "z-ai/glm-5.3[1m]" {
		t.Fatalf("fallback with only a variant = %q, want the variant", got)
	}
}

func TestChatGPTThinkingLevelsSurviveTheCatalog(t *testing.T) {
	dir := t.TempDir()
	service := modelsource.Connected{Source: vendoredSource(t, "codex"), Key: "codex-sentinel", Address: "https://chatgpt.example/backend"}
	row := codexCatalogRow(codexauth.Model{ID: "gpt-6-luna", ContextLength: 272000, ReasoningLevels: []string{"low", "high"}})
	if err := catalog.Remember(CatalogOptionsFor(service, dir), []catalog.Model{row}); err != nil {
		t.Fatal(err)
	}
	rows := catalog.Recall(CatalogOptionsFor(service, dir)).ModelsNow()
	if len(rows) != 1 || len(rows[0].Reasoning.Efforts) != 2 || rows[0].Reasoning.Known {
		t.Fatalf("remembered = %+v, want the two levels kept for ranking and no published profile", rows)
	}
}

func TestTheSettingsRowsShowWhatAProfileWithoutOpenRouterUses(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	listed := true
	if err := WriteSources(dir, []PersistedSource{{ID: "ollama", Written: "ollama", Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	ollama, _ := ResolveSources(dir, "", DefaultBaseURL).ByID("ollama")
	if err := catalog.Remember(CatalogOptionsFor(ollama, dir), ids("qwen3:8b")); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{KeyKnowledgeModel, KeyModelFallbacks} {
		var row *Setting
		for _, s := range registry(t, dir).Rows() {
			if s.Key == key {
				s := s
				row = &s
			}
		}
		if row == nil {
			t.Fatalf("no %s row", key)
		}
		if got := row.Value(); got != "ollama/qwen3:8b" {
			t.Fatalf("%s row = %q, want the reachable model", key, got)
		}
	}
}
