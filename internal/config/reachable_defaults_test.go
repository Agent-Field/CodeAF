package config

import (
	"strconv"
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
	for _, slot := range []DefaultSlot{SlotTalk, SlotFallback} {
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
	for slot, got := range map[string]string{"talk": talk, "fallback": fallback} {
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
	if got := one.pick(SlotFallback, DefaultFallbackModel, "ollama/qwen3:8b"); got != "ollama/qwen3:8b" {
		t.Fatalf("fallback with one local model = %q, want that model, never an unreachable one", got)
	}

	two := defaultsFixture{services: []modelsource.Connected{ollama}, models: map[string][]catalog.Model{"ollama": {{ID: "qwen3:8b"}, {ID: "llama3.2:3b"}, {ID: "nomic-embed-text"}}}}
	if got := two.pick(SlotTalk, DefaultModel, ""); got != "ollama/qwen3:8b" {
		t.Fatalf("talk = %q, want the larger local model", got)
	}
	if got := two.pick(SlotFallback, DefaultFallbackModel, "ollama/qwen3:8b"); got != "ollama/llama3.2:3b" {
		t.Fatalf("fallback = %q, want the other local model, never the embedding model", got)
	}
}

func TestClaudeCodeDefaultsAreItsPreferredAndStrongestModels(t *testing.T) {
	f := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, claudemodel.ID), Key: claudemodel.Sentinel}}, models: map[string][]catalog.Model{claudemodel.ID: claudemodel.Models()}}
	talk := f.pick(SlotTalk, DefaultModel, "")
	if want := claudemodel.ID + "/" + vendoredSource(t, claudemodel.ID).Preferred; talk != want {
		t.Fatalf("talk = %q, want the preferred %q", talk, want)
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
	if got := f.pick(SlotFallback, DefaultFallbackModel, "ollama/qwen3:8b"); got != "ollama/llama3.2:3b" {
		t.Fatalf("fallback with an Ollama talk model = %q, want Ollama's other model", got)
	}
	if got := f.pick(SlotFallback, DefaultFallbackModel, "codex/gpt-6.1-terra"); !strings.HasPrefix(got, "codex/") {
		t.Fatalf("fallback with a ChatGPT talk model = %q, want a ChatGPT model", got)
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
	if got := KnowledgeModelAt(dir, ""); got != "ollama/qwen3:8b" {
		t.Fatalf("KnowledgeModelAt with only Ollama = %q, want the conversation's default model", got)
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
// the models it names), and checks the conversation and fallback seats.
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
		name           string
		service        modelsource.Connected
		models         []catalog.Model
		talk, fallback string
	}{
		{
			name:    "DeepSeek",
			service: modelsource.Connected{Source: vendoredSource(t, "deepseek"), Key: "sk-ds"},
			models:  ids("deepseek-v4-flash", "deepseek-v4-pro"),
			talk:    "deepseek/deepseek-v4-pro", fallback: "deepseek/deepseek-v4-flash",
		},
		{
			name:    "Z.ai coding plan",
			service: modelsource.Connected{Source: zai, Key: "zai", Door: codingPlan},
			models:  namedModels(modelsource.Connected{Source: zai, Door: codingPlan}),
			talk:    "z-ai/" + zai.Preferred, fallback: "z-ai/glm-5.3-flash",
		},
		{
			name:    "Moonshot",
			service: modelsource.Connected{Source: vendoredSource(t, "moonshot"), Key: "sk-moon"},
			models:  namedModels(modelsource.Connected{Source: vendoredSource(t, "moonshot")}),
			talk:    "moonshot/" + vendoredSource(t, "moonshot").Preferred, fallback: "moonshot/" + vendoredSource(t, "moonshot").Preferred,
		},
		{
			name:    "MiniMax",
			service: modelsource.Connected{Source: vendoredSource(t, "minimax"), Key: "sk-mm"},
			models:  namedModels(modelsource.Connected{Source: vendoredSource(t, "minimax")}),
			talk:    "minimax/" + vendoredSource(t, "minimax").Preferred, fallback: "minimax/" + vendoredSource(t, "minimax").Preferred,
		},
		{
			name:    "Qwen",
			service: modelsource.Connected{Source: vendoredSource(t, "qwen"), Key: "sk-qw"},
			models:  namedModels(modelsource.Connected{Source: vendoredSource(t, "qwen")}),
			talk:    "qwen/qwen3.7-plus", fallback: "qwen/qwen3.8-flash",
		},
		{
			name:    "ai&",
			service: modelsource.Connected{Source: vendoredSource(t, "aiand"), Key: "sk-aiand"},
			models:  ids("zai-org/glm-5.3-flash", "deepseek-ai/deepseek-v4-flash", "zai-org/glm-5.3", "moonshotai/kimi-k3"),
			talk:    "aiand/zai-org/glm-5.3-flash", fallback: "aiand/zai-org/glm-5.3",
		},
		{
			name:    "OpenCode Go",
			service: modelsource.Connected{Source: vendoredSource(t, "opencode-go"), Key: "sk-oc"},
			models:  ids("kimi-k3", vendoredSource(t, "opencode-go").Preferred, "qwen3.8-flash"),
			talk:    "opencode-go/" + vendoredSource(t, "opencode-go").Preferred, fallback: "opencode-go/kimi-k3",
		},
		{
			name:    "Claude Code",
			service: modelsource.Connected{Source: vendoredSource(t, claudemodel.ID), Key: claudemodel.Sentinel},
			models:  claudemodel.Models(),
			talk:    claudemodel.ID + "/sonnet", fallback: claudemodel.ID + "/opus",
		},
		{
			name:    "a custom endpoint",
			service: modelsource.Connected{Source: custom, Key: "sk-lab", Address: "https://lab.example/v1"},
			models:  ids("lab-model-8b", "lab-model-70b"),
			talk:    "lab/lab-model-70b", fallback: "lab/lab-model-8b",
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
		})
	}
}

// A built-in id that routes to a connected account by its prefix is kept only
// when that account lists it: `deepseek/deepseek-v4.1-flash` routes to a direct
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

// An OpenRouter key keeps every built-in as it was, even when another connected
// account captures a built-in id by its prefix and does not list it: choosing
// among OpenRouter's whole catalog would send a fallback to its most expensive
// model.
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
		{SlotTalk, DefaultModel}, {SlotFallback, DefaultFallbackModel},
	} {
		for _, chosen := range []string{"", "deepseek/deepseek-v4-pro"} {
			if got := f.pick(c.slot, c.builtIn, chosen); got != c.builtIn {
				t.Fatalf("%s (talk %q) = %q, want the built-in %q unchanged", c.slot, chosen, got, c.builtIn)
			}
		}
	}
}

func TestADefaultIsNeverEmptyWhenNoModelsAreKnown(t *testing.T) {
	f := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, "ollama")}}, models: map[string][]catalog.Model{}}
	for slot, builtIn := range map[DefaultSlot]string{SlotTalk: DefaultModel, SlotFallback: DefaultFallbackModel} {
		if got := f.pick(slot, builtIn, ""); got != builtIn {
			t.Fatalf("%s with a cold local catalog = %q, want the built-in rather than nothing", slot, got)
		}
	}
}

// A default fallback is for when the conversation model fails, so a listed
// built-in that is the conversation model itself gives way to another model.
func TestTheDefaultFallbackIsNotTheConversationModel(t *testing.T) {
	f := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, "deepseek"), Key: "sk-ds"}}, models: map[string][]catalog.Model{"deepseek": ids("deepseek-v4-pro", "deepseek-v4.1-flash")}}
	if got := f.pick(SlotFallback, DefaultFallbackModel, DefaultFallbackModel); got != "deepseek/deepseek-v4-pro" {
		t.Fatalf("fallback while talking on %q = %q, want the other model", DefaultFallbackModel, got)
	}
}

// The lists that do not come from a remembered listing are built once and
// shared, so the filter and rank memos, keyed by a list's backing array, see
// one list rather than a new one per lookup.
func TestListsThatAreNotRememberedAreBuiltOnce(t *testing.T) {
	dir := t.TempDir()
	for _, service := range []modelsource.Connected{
		{Source: vendoredSource(t, claudemodel.ID), Key: claudemodel.Sentinel},
		{Source: vendoredSource(t, "codex"), Key: "codex-sentinel"},
		{Source: vendoredSource(t, "qwen"), Key: "sk-qw"},
	} {
		first := serviceModelsAt(dir, service)
		if len(first) == 0 {
			t.Fatalf("%s offers no models", service.Source.ID)
		}
		_ = rankByStrength(usableModels(first))
		ranks := listRanks.Load()
		for i := 0; i < 10; i++ {
			again := serviceModelsAt(dir, service)
			if &again[0] != &first[0] {
				t.Fatalf("%s built its list afresh", service.Source.ID)
			}
			_ = rankByStrength(usableModels(again))
		}
		if got := listRanks.Load() - ranks; got != 0 {
			t.Fatalf("%s ranked its unchanged list %d more times, want 0", service.Source.ID, got)
		}
	}
}

func TestTheSavedTalkModelIsTheTalkDefaultWhenTheBuiltInIsUnreachable(t *testing.T) {
	f := defaultsFixture{services: []modelsource.Connected{{Source: vendoredSource(t, "codex"), Key: "codex-sentinel"}}, models: map[string][]catalog.Model{"codex": codexRows()}}
	if got := f.pick(SlotTalk, DefaultModel, "codex/gpt-6-astra"); got != "codex/gpt-6-astra" {
		t.Fatalf("talk = %q, want the saved conversation model", got)
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

func TestTheFallbackRowShowsWhatAProfileWithoutOpenRouterUses(t *testing.T) {
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
	var row *Setting
	for _, s := range registry(t, dir).Rows() {
		if s.Key == KeyModelFallbacks {
			s := s
			row = &s
		}
	}
	if row == nil {
		t.Fatal("no fallback models row")
	}
	if got := row.Value(); got != "ollama/qwen3:8b" {
		t.Fatalf("fallback models row = %q, want the reachable model", got)
	}
}

// The ordering rules read services the way a real profile resolves them: every
// persisted row carries a plan-pause policy, and the person's order may put a
// local server first.
func TestAHostedProviderIsChosenBeforeALocalOneOnARealProfile(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	dir := t.TempDir()
	listed := true
	if err := WriteSources(dir, []PersistedSource{
		{ID: "ollama", Written: "ollama", Listed: &listed, Order: 0},
		{ID: "deepseek", Written: "deepseek", Key: "sk-ds-1234567890", Listed: &listed, Order: 1},
	}); err != nil {
		t.Fatal(err)
	}
	set := ResolveSources(dir, "", DefaultBaseURL)
	for id, rows := range map[string][]catalog.Model{"ollama": ids("qwen3:8b", "llama3.2:3b"), "deepseek": ids("deepseek-v4-pro", "deepseek-v4-flash")} {
		service, ok := set.ByID(id)
		if !ok {
			t.Fatalf("no %s row", id)
		}
		if err := catalog.Remember(CatalogOptionsFor(service, dir), rows); err != nil {
			t.Fatal(err)
		}
	}
	if got := ChatDefaultAt(dir); got != "deepseek/deepseek-v4-pro" {
		t.Fatalf("talk = %q, want DeepSeek before the local server", got)
	}
	if got := ModelFallbacksAt(dir); got != "deepseek/deepseek-v4-flash" {
		t.Fatalf("fallback = %q, want the talk provider's other model", got)
	}
	if got := KnowledgeModelAt(dir, ""); got != "deepseek/deepseek-v4-pro" {
		t.Fatalf("knowledge = %q, want the conversation's default model", got)
	}
}

func TestAModelWithAnUnknownPriceIsNotTakenForTheCheapest(t *testing.T) {
	models := []catalog.Model{
		{ID: "vendor/big-flagship-pro", PriceUnknown: true},
		{ID: "vendor/small-mini", PromptPrice: 1e-8, CompletionPrice: 1e-8},
		{ID: "vendor/mid", IntelligenceIndex: 30, PromptPrice: 1e-6, CompletionPrice: 1e-6},
	}
	if got := rankByStrength(models)[0].ID; got != "vendor/small-mini" {
		t.Fatalf("lightest = %q, want the model whose name says it is small", got)
	}
	if got := rankByStrength(models)[len(models)-1].ID; got != "vendor/big-flagship-pro" {
		t.Fatalf("strongest = %q, want the flagship by its name", got)
	}
}

func TestAModelVariableSkipsTheDefaultInLoad(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-1234567890")
	t.Setenv(ModelEnv, "vendor/chosen-model")
	t.Setenv("CODEAF_HOME", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "vendor/chosen-model" {
		t.Fatalf("Load().Model = %q, want the model variable", cfg.Model)
	}
}

func TestChatGPTCrewRoutesStayAsTheyWereWithOpenRouter(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-1234567890")
	dir := t.TempDir()
	if err := codexauth.Save(dir, codexauth.Tokens{AccessToken: "placeholder", RefreshToken: "placeholder", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := WriteSources(dir, []PersistedSource{{ID: "codex", Written: "codex", Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	service, _ := ResolveSources(dir, "", DefaultBaseURL).ByID("codex")
	if err := catalog.Remember(CatalogOptionsFor(service, dir), codexRows()); err != nil {
		t.Fatal(err)
	}
	for _, p := range CrewProvidersAt(dir) {
		if p.ID == "codex" {
			if len(p.Serves) != 1 || p.Serves[0] != vendoredSource(t, "codex").Preferred {
				t.Fatalf("with OpenRouter, ChatGPT serves %v, want only the preferred model as before", p.Serves)
			}
			return
		}
	}
	t.Fatal("ChatGPT is not a crew provider")
}

func TestARememberedListIsReadAgainWhenItChanges(t *testing.T) {
	dir := t.TempDir()
	options := catalog.Options{Source: "ollama", BaseURL: "http://localhost:11434/v1", Dir: dir}
	if err := catalog.Remember(options, ids("qwen3:8b")); err != nil {
		t.Fatal(err)
	}
	if got := recallCached(options); len(got) != 1 || got[0].ID != "qwen3:8b" {
		t.Fatalf("first read = %+v", got)
	}
	if err := catalog.Remember(options, ids("qwen3:8b", "llama3.2:3b")); err != nil {
		t.Fatal(err)
	}
	if got := recallCached(options); len(got) != 2 {
		t.Fatalf("after the list changed = %+v, want both models", got)
	}
}

func TestOnlyTheFallbackRowFollowsTheProviders(t *testing.T) {
	dir := t.TempDir()
	if !FollowsProviders(KeyModelFallbacks) {
		t.Fatal("the fallback models row does not follow the providers")
	}
	if WrittenAt(dir, KeyModelFallbacks) {
		t.Fatal("the fallback models row reads as written on an empty profile")
	}
	if err := writeText(dir, KeyModelFallbacks, "vendor/model"); err != nil {
		t.Fatal(err)
	}
	if !WrittenAt(dir, KeyModelFallbacks) {
		t.Fatal("a written fallback models row does not read as written")
	}
	// These rows read empty until written and follow the chosen model, so an
	// untouched profile's reading is already the right comparison.
	for _, key := range []string{KeyKnowledgeModel, KeyTierReflexModel, KeyTierLowModel, KeyChatModelRotation} {
		if FollowsProviders(key) {
			t.Fatalf("%s follows the providers", key)
		}
	}
}

// CODEAF_MODEL names the conversation model when nothing is saved, so the
// default fallback follows its provider, as the conversation itself does.
func TestTheDefaultFallbackFollowsTheModelVariable(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	dir := t.TempDir()
	listed := true
	if err := WriteSources(dir, []PersistedSource{
		{ID: "deepseek", Written: "deepseek", Key: "sk-ds-1234567890", Listed: &listed},
		{ID: "ollama", Written: "ollama", Listed: &listed},
	}); err != nil {
		t.Fatal(err)
	}
	set := ResolveSources(dir, "", DefaultBaseURL)
	for id, rows := range map[string][]catalog.Model{"ollama": ids("qwen3:8b", "llama3.2:3b"), "deepseek": ids("deepseek-v4-pro", "deepseek-v4-flash")} {
		service, _ := set.ByID(id)
		if err := catalog.Remember(CatalogOptionsFor(service, dir), rows); err != nil {
			t.Fatal(err)
		}
	}
	if got := DefaultFallbackAt(dir); got != "deepseek/deepseek-v4-flash" {
		t.Fatalf("with nothing chosen, fallback = %q, want the hosted provider's", got)
	}
	t.Setenv(ModelEnv, "ollama/qwen3:8b")
	if got := DefaultFallbackAt(dir); got != "ollama/llama3.2:3b" {
		t.Fatalf("with %s=ollama/qwen3:8b, fallback = %q, want Ollama's other model", ModelEnv, got)
	}
}

func TestTheDefaultFallbackIgnoresTheWrittenRow(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-1234567890")
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	if err := writeText(dir, KeyModelFallbacks, "vendor/mine"); err != nil {
		t.Fatal(err)
	}
	if got := DefaultFallbackAt(dir); got != DefaultFallbackModel {
		t.Fatalf("DefaultFallbackAt = %q, want codeaf's default %q whatever the row says", got, DefaultFallbackModel)
	}
}

// An unchanged provider list is decoded and ranked once, however often the
// defaults are asked for: the settings sheet asks on every paint.
func TestUnchangedProviderListsAreReadAndRankedOnce(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	dir := t.TempDir()
	listed := true
	if err := WriteSources(dir, []PersistedSource{{ID: "deepseek", Written: "deepseek", Key: "sk-ds-1234567890", Listed: &listed}, {ID: "ollama", Written: "ollama", Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	set := ResolveSources(dir, "", DefaultBaseURL)
	deepseek, _ := set.ByID("deepseek")
	ollama, _ := set.ByID("ollama")
	var rows []catalog.Model
	for i := 0; i < 400; i++ {
		rows = append(rows, catalog.Model{ID: "vendor-model-" + strconv.Itoa(i), PromptPrice: 1e-6, CompletionPrice: 1e-6})
	}
	if err := catalog.Remember(CatalogOptionsFor(deepseek, dir), rows); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Remember(CatalogOptionsFor(ollama, dir), ids("qwen3:8b", "llama3.2:3b")); err != nil {
		t.Fatal(err)
	}
	lookups := func() {
		_ = ChatDefaultAt(dir)
		_ = ModelFallbacksAt(dir)
	}
	lookups()
	reads, ranks := listReads.Load(), listRanks.Load()
	for i := 0; i < 100; i++ {
		lookups()
	}
	if got := listReads.Load() - reads; got != 0 {
		t.Fatalf("200 lookups decoded unchanged lists %d times, want 0", got)
	}
	if got := listRanks.Load() - ranks; got != 0 {
		t.Fatalf("200 lookups ranked unchanged lists %d times, want 0", got)
	}
}
