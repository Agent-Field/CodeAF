package tui3

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/session"
)

type modelForNowAgent struct {
	*fakeAgent
	configured string
	temporary  []string
	submitted  []string
}

func (a *modelForNowAgent) ConfiguredModel() string { return a.configured }
func (a *modelForNowAgent) UseModelForNow(model string) error {
	a.temporary = append(a.temporary, model)
	a.fakeAgent.SetModel(model)
	return nil
}
func (a *modelForNowAgent) SetModel(model string) {
	a.configured = model
	a.fakeAgent.SetModel(model)
}

func (a *modelForNowAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	a.submitted = append(a.submitted, a.Model())
	return a.fakeAgent.Submit(ctx, text)
}

func acknowledgeModelChanges(t *testing.T, a *app) {
	t.Helper()
	spend(t, a, a.takeModelCommands())
}

// C2, C4, C5: availability reconciles to the saved model without profile writes.
func TestListedModelReplacementKeepsProfileAndReturnsToConfiguredModel(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	dir := t.TempDir()
	profile := []byte(`{
  "model.talk": "vendor/configured",
  "models.tiers.reflex": "vendor/reflex",
  "models.tiers.low": "",
  "models.tiers.worker": "vendor/worker",
  "models.tiers.high": "vendor/high",
  "models.tiers.mastermind": "vendor/planner",
  "models.roles": "title:vendor/title",
  "task.model": "vendor/task"
}
`)
	if err := os.WriteFile(config.BudgetConfigPath(dir), profile, 0600); err != nil {
		t.Fatal(err)
	}
	rows := []Model{{ID: "vendor/replacement", ContextLength: 32768}}
	agent := &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/configured"}, configured: "vendor/configured"}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: dir, Sources: modelsource.NewSet(testDefaultService("key")), RequireListedModel: true,
		Models: func() []Model { return rows }, SaveModel: func(id string) error { return config.WriteChatModel(dir, id) },
	})
	acknowledgeModelChanges(t, a)
	if a.model != "vendor/replacement" || agent.Model() != a.model || agent.ConfiguredModel() != "vendor/configured" || agent.window != 32768 {
		t.Fatalf("replacement=%q/%q configured=%q window=%d", a.model, agent.Model(), agent.ConfiguredModel(), agent.window)
	}
	rows = append(rows, Model{ID: "vendor/configured", ContextLength: 65536})
	a.state = stateWorking
	a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
	if a.model != "vendor/replacement" {
		t.Fatal("catalog recovery changed an answering request")
	}
	a.settle()
	acknowledgeModelChanges(t, a)
	if a.model != "vendor/configured" || agent.Model() != a.model || agent.window != 65536 {
		t.Fatalf("recovered=%q/%q window=%d", a.model, agent.Model(), agent.window)
	}
	got, err := os.ReadFile(config.BudgetConfigPath(dir))
	if err != nil || !bytes.Equal(got, profile) {
		t.Fatalf("automatic selection changed profile: %s, %v", got, err)
	}
	rows = rows[:1]
	a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
	a.slash("/model vendor/replacement")
	acknowledgeModelChanges(t, a)
	if agent.ConfiguredModel() != "vendor/replacement" || config.ChatModelAt(dir) != "vendor/replacement" || a.modelWord() != "vendor/replacement" {
		t.Fatal("confirming the fallback did not save the explicit choice and HUD")
	}
	if note := noteSaying(t, a, "model ·"); note != "model · vendor/replacement" {
		t.Fatalf("explicit model note=%q", note)
	}
}

// C1, C4: catalog recovery changes the next real, scripted provider request.
func TestCatalogRecoveryUsesConfiguredModelOnTheNextRequest(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	server := sourcestub.New("configured", "replacement")
	defer server.Close()
	service := testDirectService(server.URL())
	sources := modelsource.NewSet(testDefaultService(""), service)
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "deepseek-direct/configured", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	rows := []Model{{ID: "replacement"}}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), Sources: sources, RequireListedModel: true,
		ModelsForService: func(modelsource.Connected) []Model { return rows },
	})
	acknowledgeModelChanges(t, a)
	drainModelServiceTurn(t, agent, "use the available model")
	rows = append(rows, Model{ID: "configured"})
	a.serviceModelsLanded(service.Source.ID, service.Address)
	acknowledgeModelChanges(t, a)
	drainModelServiceTurn(t, agent, "use the recovered model")
	requests := completionRequests(server)
	if len(requests) != 2 || completionModel(t, requests[0]) != "replacement" || completionModel(t, requests[1]) != "configured" {
		t.Fatalf("provider requests=%+v", requests)
	}
}

// C2, C5: setup only saves an actual pick, including the current run-only model.
func TestSetupCanExplicitlyChooseTheRunOnlyModel(t *testing.T) {
	dir := t.TempDir()
	agent := &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/configured"}, configured: "vendor/configured"}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: dir, SaveModel: func(id string) error { return config.WriteChatModel(dir, id) }})
	a.useModelForNow("vendor/replacement", 0)
	acknowledgeModelChanges(t, a)
	if config.ChatModelAt(dir) != "" {
		t.Fatal("an automatic setup model was saved")
	}
	a.takeSetupModel("vendor/replacement")
	if agent.ConfiguredModel() != "vendor/replacement" || config.ChatModelAt(dir) != "vendor/replacement" {
		t.Fatal("setup ignored an explicit pick of the running fallback")
	}
}

// C6: an untouched implicit credit default follows the ordinary saved model
// road, and availability reconciliation preserves it at the submission gate.
func TestCreditDefaultSurvivesListedModelSubmission(t *testing.T) {
	for _, low := range []bool{true, false} {
		t.Run(map[bool]string{true: "low", false: "recovered"}[low], func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			initial, want := config.DefaultModel, config.FreeChatModel
			if !low {
				initial, want = want, initial
			}
			agent := &modelForNowAgent{fakeAgent: &fakeAgent{model: initial}, configured: initial}
			a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), ImplicitTalk: true, RequireListedModel: true,
				Sources: modelsource.NewSet(testDefaultService("credit-test-key")),
				Models:  func() []Model { return []Model{{ID: config.DefaultModel}, {ID: config.FreeChatModel}} },
			})
			if err := config.SetCrewFreeRoutes(a.profileDir, true); err != nil {
				t.Fatal(err)
			}
			seedLowCredits(t, a)
			reading := credits.Reading{Known: true, Low: low}
			if err := config.WriteCreditsReading(a.profileDir, "credit-test-key", reading); err != nil {
				t.Fatal(err)
			}
			a.readCredits = func(context.Context) (credits.Reading, error) { return reading, nil }
			a.refreshCreditWarnings()
			a.input.insert("answer on the credit default")
			spend(t, a, a.enterLine())
			if len(agent.submitted) != 1 || agent.submitted[0] != want || agent.ConfiguredModel() != want || config.ChatModelAt(a.profileDir) != "" || len(agent.temporary) != 0 {
				t.Fatalf("submitted=%v configured=%q temporary=%v", agent.submitted, agent.ConfiguredModel(), agent.temporary)
			}
		})
	}
}

// C6: remote doors leave the engine's model alone even when it is not listed.
func TestRemoteCatalogDoesNotSelectARunOnlyModel(t *testing.T) {
	agent := &modelForNowAgent{fakeAgent: &fakeAgent{model: "engine/model"}, configured: "engine/model"}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), Models: func() []Model { return []Model{{ID: "other/model"}} }})
	a.ensureAvailableModel()
	if agent.Model() != "engine/model" || len(agent.temporary) != 0 {
		t.Fatal("a remote catalog changed the engine selection")
	}
}

// C5, C6; fix round 1 items 1 and 4: connecting or reconnecting under the same
// name is a persistent choice, idle or deferred, even on the running fallback.
func TestServiceConnectionPersistsIdleAndDeferredMoves(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		working bool
		running bool
	}{
		{name: "idle"},
		{name: "deferred", working: true},
		{name: "idle on the fallback", running: true},
		{name: "deferred on the fallback", working: true, running: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			for _, connection := range []string{"connect", "reconnect"} {
				t.Run(connection, func(t *testing.T) {
					a, dir := newZAIConnectTestApp(t, "")
					configured := a.model
					want := "z-ai-direct/glm-5.3"
					if connection == "reconnect" {
						connectZAIFromPanel(t, a, "plan-test-key")
						a.switchModel(configured, 0)
					}
					agent := &modelForNowAgent{fakeAgent: &fakeAgent{model: configured}, configured: configured}
					a.agent = agent
					a.requireListedModel = true
					if err := config.WriteChatModel(dir, configured); err != nil {
						t.Fatal(err)
					}
					if testCase.running {
						a.requireListedModel = false
						a.useModelForNow(want, 0)
						acknowledgeModelChanges(t, a)
						a.requireListedModel = true
					}
					temporary := len(agent.temporary)
					opening := a.model
					if testCase.working {
						a.state = stateWorking
					}
					if connection == "reconnect" {
						a.adoptModelConnectResult(modelConnectResultMsg{
							service: "z-ai", written: "z-ai-direct", renamedFrom: "z-ai-direct",
							outcome: modelsource.Outcome{Kind: modelsource.OutcomeConnected},
							models:  a.sourceModels["z-ai"],
						})
					} else {
						connectZAIFromPanel(t, a, "plan-test-key")
					}
					if testCase.working {
						if a.model != opening || agent.ConfiguredModel() != configured || config.ChatModelAt(dir) != configured || a.deferredModelServiceModel != want {
							t.Fatal("connection changed the answering turn or lost its deferred choice")
						}
						a.settle()
					}
					if a.model != want || agent.Model() != want || agent.ConfiguredModel() != want || config.ChatModelAt(dir) != want || a.deferredModelServiceModel != "" || len(agent.temporary) != temporary {
						t.Fatal("connection did not persist its preferred model")
					}
				})
			}
		})
	}
}

// C1, C2, C5; fix round 1 items 3 and 4: disconnecting keeps the configured
// choice until the person explicitly confirms the running fallback, idle or deferred.
func TestDisconnectMoveStaysTemporaryUntilExplicitlyChosen(t *testing.T) {
	for _, working := range []bool{false, true} {
		name := "idle"
		if working {
			name = "deferred"
		}
		t.Run(name, func(t *testing.T) {
			a, dir := newZAIConnectTestApp(t, "sk-default-1234567890")
			fallback := a.model
			connectZAIFromPanel(t, a, "plan-test-key")
			configured := a.model
			agent := &modelForNowAgent{fakeAgent: &fakeAgent{model: configured}, configured: configured}
			a.agent = agent
			a.requireListedModel = true
			a.disconnectModelService("z-ai")
			acknowledgeModelChanges(t, a)
			if a.model != fallback || agent.Model() != fallback || agent.ConfiguredModel() != configured || config.ChatModelAt(dir) != configured || len(agent.temporary) == 0 {
				t.Fatal("disconnect did not keep its replacement temporary")
			}
			if working {
				a.state = stateWorking
			}
			a.moveConversationOrDefer(fallback, "openrouter")
			if working {
				if agent.ConfiguredModel() != configured || config.ChatModelAt(dir) != configured || a.deferredModelServiceModel != fallback {
					t.Fatal("explicit fallback choice did not wait for the turn boundary")
				}
				a.settle()
			}
			if a.model != fallback || agent.ConfiguredModel() != fallback || config.ChatModelAt(dir) != fallback || a.deferredModelServiceModel != "" {
				t.Fatal("explicit confirmation of the running fallback did not persist")
			}
		})
	}
}
