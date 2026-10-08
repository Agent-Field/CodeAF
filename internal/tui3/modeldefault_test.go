package tui3

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestTheOpeningModelAlwaysBelongsToItsAvailablePicker(t *testing.T) {
	ollama := testModelSource(t, "ollama")
	local := modelsource.Connected{Source: ollama, Address: ollama.Address}
	for _, test := range []struct {
		name, key, preferred, want string
		local                      bool
		cloud, installed           []Model
	}{
		{name: "Ollama only", preferred: config.DefaultModel, local: true, installed: []Model{{ID: "qwen3:0.6b"}, {ID: "llama3.2:1b"}}, want: "ollama/qwen3:0.6b"},
		{name: "OpenRouter only without shipped default", key: "key", preferred: config.DefaultModel, cloud: []Model{{ID: "vendor/cloud"}}, want: "vendor/cloud"},
		{name: "listed shipped default", key: "key", preferred: config.DefaultModel, cloud: []Model{{ID: "vendor/cloud"}, {ID: config.DefaultModel}}, want: config.DefaultModel},
		{name: "saved installed choice", preferred: "ollama/qwen3:0.6b", local: true, installed: []Model{{ID: "llama3.2:1b"}, {ID: "qwen3:0.6b"}}, want: "ollama/qwen3:0.6b"},
		{name: "saved removed choice", preferred: "ollama/removed", local: true, installed: []Model{{ID: "qwen3:0.6b"}}, want: "ollama/qwen3:0.6b"},
		{name: "both keep saved local choice", key: "key", preferred: "ollama/qwen3:0.6b", local: true, cloud: []Model{{ID: "vendor/cloud"}}, installed: []Model{{ID: "qwen3:0.6b"}}, want: "ollama/qwen3:0.6b"},
		{name: "both replace unlisted choice", key: "key", preferred: "removed", local: true, cloud: []Model{{ID: "vendor/cloud"}}, installed: []Model{{ID: "qwen3:0.6b"}}, want: "vendor/cloud"},
		{name: "no connections", preferred: config.DefaultModel, cloud: []Model{{ID: config.DefaultModel}}},
		{name: "empty connected catalog", key: "key", preferred: config.DefaultModel, cloud: []Model{}},
		{name: "nonchat rows cannot become defaults", key: "key", preferred: config.DefaultModel, cloud: []Model{{ID: "vendor/speech", Input: []string{"text"}, Output: []string{"audio"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			services := []modelsource.Connected{testDefaultService(test.key)}
			if test.local {
				services = append(services, local)
			}
			agent := &fakeAgent{model: test.preferred}
			saved := ""
			a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), Sources: modelsource.NewSet(services...), RequireListedModel: true,
				Models:           func() []Model { return test.cloud },
				ModelsForService: func(modelsource.Connected) []Model { return test.installed },
				SaveModel:        func(id string) error { saved = id; return nil },
			})
			if a.model != test.want {
				t.Fatalf("opening model = %q, want %q", a.model, test.want)
			}
			if test.want != "" && (!a.isAvailableModel(a.model) || agent.Model() != test.want) {
				t.Fatalf("default is not on both the engine and picker: %q/%q", a.model, agent.Model())
			}
			if saved != "" {
				t.Fatalf("automatic default overwrote the preference with %q", saved)
			}
		})
	}
}

func TestAColdCatalogHoldsTheDraftThenSuppliesTheEngineDefault(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	var rows []Model
	agent := &fakeAgent{model: config.DefaultModel}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), Sources: modelsource.NewSet(testDefaultService("key")), RequireListedModel: true, Models: func() []Model { return rows }})
	a.input.insert("keep this draft")
	if cmd := a.enterLine(); cmd != nil || len(agent.sent) != 0 || a.input.String() != "keep this draft" || a.model != "" {
		t.Fatalf("cold send: cmd=%v sent=%v draft=%q model=%q", cmd != nil, agent.sent, a.input.String(), a.model)
	}
	rows = []Model{{ID: "vendor/real", ContextLength: 32768}}
	a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
	if a.model != "vendor/real" || agent.Model() != a.model || agent.window != 32768 || a.input.String() != "keep this draft" {
		t.Fatalf("landed default = %q/%q window=%d draft=%q", a.model, agent.Model(), agent.window, a.input.String())
	}
	a.pick.close()
	cmd := a.enterLine()
	if cmd == nil {
		t.Fatal("the available default did not release the draft")
	}
	spend(t, a, cmd)
	if len(agent.sent) != 1 || agent.sent[0] != "keep this draft" {
		t.Fatalf("send = %v", agent.sent)
	}
}

func TestRemovingTheDefaultModelChoosesAnotherListedModel(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	rows := []Model{{ID: "vendor/old"}, {ID: "vendor/replacement"}}
	agent := &fakeAgent{model: "vendor/old"}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), Sources: modelsource.NewSet(testDefaultService("key")), RequireListedModel: true, Models: func() []Model { return rows }})
	rows = []Model{{ID: "vendor/replacement"}}
	a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
	if a.model != "vendor/replacement" || agent.Model() != a.model {
		t.Fatalf("replacement = %q/%q", a.model, agent.Model())
	}
	rows = []Model{}
	a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
	if a.model != "" {
		t.Fatalf("empty catalog left %q as the displayed default", a.model)
	}
	called := false
	if cmd := a.submitting("blocked", func() (<-chan session.Event, error) { called = true; return nil, nil }); cmd != nil || called {
		t.Fatal("an alternate send bypassed the empty catalog")
	}
}

func TestListedDefaultsKeepEffortAndNeverSelectPickerControls(t *testing.T) {
	rows := []Model{{Unavailable: true, Notice: "warming"}, {AddProvider: true}, {ID: "ollama/qwen3:0.6b"}}
	if model, ok := availableConversationModel("ollama/qwen3:0.6b:high", rows); !ok || model.ID != "ollama/qwen3:0.6b:high" {
		t.Fatalf("listed effort = %+v/%t", model, ok)
	}
	if model, ok := availableConversationModel("removed:high", rows); !ok || model.ID != "ollama/qwen3:0.6b" {
		t.Fatalf("replacement retained removed effort: %+v/%t", model, ok)
	}
}

func TestCreditDefaultsCannotSelectAnUnlistedModel(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	dir := t.TempDir()
	agent := &fakeAgent{model: config.FreeChatModel}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: dir, Sources: modelsource.NewSet(testDefaultService("key")), RequireListedModel: true, ImplicitTalk: true,
		Models: func() []Model { return []Model{{ID: config.FreeChatModel}} },
	})
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, nil }
	a.refreshCreditWarnings()
	if a.model != config.FreeChatModel || agent.Model() != a.model {
		t.Fatalf("credit reading chose an unlisted default: %q/%q", a.model, agent.Model())
	}
}

func TestAnAvailableOpeningModelIsTheModelSentToItsRealProvider(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	server := sourcestub.New("actually-listed")
	defer server.Close()
	service := testDirectService(server.URL())
	sources := modelsource.NewSet(testDefaultService(""), service)
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: config.DefaultModel, Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), Sources: sources, RequireListedModel: true,
		Models:           func() []Model { return []Model{{ID: config.DefaultModel}} },
		ModelsForService: func(modelsource.Connected) []Model { return []Model{{ID: "actually-listed"}} },
	})
	if a.model != "deepseek-direct/actually-listed" {
		t.Fatalf("default = %q", a.model)
	}
	drainModelServiceTurn(t, agent, "answer on the available default")
	requests := completionRequests(server)
	if len(requests) != 1 || completionModel(t, requests[0]) != "actually-listed" {
		t.Fatalf("provider requests = %+v", requests)
	}
}

func TestARefreshDefersDefaultReplacementUntilTheAnswerSettles(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	rows := []Model{{ID: "vendor/old"}, {ID: "vendor/replacement"}}
	agent := &fakeAgent{model: "vendor/old"}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), Sources: modelsource.NewSet(testDefaultService("key")), RequireListedModel: true, Models: func() []Model { return rows }})
	a.state = stateWorking
	rows = []Model{{ID: "vendor/replacement"}}
	a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
	if a.model != "vendor/old" || agent.Model() != a.model {
		t.Fatal("the catalog change interrupted an answering request")
	}
	a.settle()
	if a.model != "vendor/replacement" || agent.Model() != a.model {
		t.Fatalf("settled default = %q/%q", a.model, agent.Model())
	}
}
