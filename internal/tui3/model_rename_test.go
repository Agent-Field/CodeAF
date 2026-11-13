package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

type pairedModelForNowAgent struct {
	*modelForNowAgent
	pair session.ConversationModels
}

func (a *pairedModelForNowAgent) ModelIdentity() session.ModelIdentity {
	pair := a.pair
	return session.ModelIdentity{Selected: a.configured, Models: &pair}
}

func (a *pairedModelForNowAgent) SetConversationModels(pair session.ConversationModels) ([]session.ModelSwitch, error) {
	a.pair = pair
	a.SetModel(pair.Main)
	return nil, nil
}

// C1, C2, C4: idle and deferred service renames carry the configured and
// temporary models separately. The configured model returns when listed.
func TestServiceRenamePreservesConfiguredAndRunOnlySelections(t *testing.T) {
	for _, working := range []bool{false, true} {
		for _, configuredService := range []bool{false, true} {
			for _, paired := range []bool{false, true} {
				name := map[bool]string{false: "idle", true: "deferred"}[working] + "/" + map[bool]string{false: "replacement service", true: "configured service"}[configuredService] + "/" + map[bool]string{false: "single", true: "pair"}[paired]
				t.Run(name, func(t *testing.T) {
					dir := t.TempDir()
					customs := connectionWriteSources(t, dir, "mybox")
					configured, live := "vendor/configured", "mybox/replacement"
					wantConfigured, wantLive := configured, "lab/replacement"
					var cloud []Model
					if configuredService {
						configured, live = "mybox/chosen", "vendor/replacement"
						wantConfigured, wantLive = "lab/chosen", live
						cloud = []Model{{ID: live}}
					}
					base := &modelForNowAgent{fakeAgent: &fakeAgent{model: configured}, configured: configured}
					var agent Agent = base
					var pairAgent *pairedModelForNowAgent
					if paired {
						pairAgent = &pairedModelForNowAgent{modelForNowAgent: base, pair: session.ConversationModels{Main: configured, Fallback: "mybox/backup"}}
						agent = pairAgent
					}
					a := modelServiceTestAppWithAgent(t, dir, configured, modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...), cloud, agent)
					a.modelsForService = func(modelsource.Connected) []Model { return []Model{{ID: "replacement"}} }
					if err := config.WriteChatModel(dir, configured); err != nil {
						t.Fatal(err)
					}
					a.useModelForNow(live, 0)
					acknowledgeModelChanges(t, a)
					a.requireListedModel = true
					if working {
						a.state = stateWorking
					}
					rows := config.PersistedSources(dir)
					rows[0].Written = "lab"
					if err := config.WriteSources(dir, rows); err != nil {
						t.Fatal(err)
					}
					a.adoptModelConnectResult(modelConnectResultMsg{service: customs[0].Source.ID, written: "lab", renamedFrom: "mybox", outcome: modelsource.Outcome{Kind: modelsource.OutcomeConnected}, models: []Model{{ID: "replacement"}}})
					if working {
						if a.model != live || base.ConfiguredModel() != configured {
							t.Fatal("rename changed the answering turn")
						}
						a.settle()
					}
					acknowledgeModelChanges(t, a)
					if a.model != wantLive || base.Model() != wantLive || base.ConfiguredModel() != wantConfigured || config.ChatModelAt(dir) != wantConfigured {
						t.Fatalf("live=%q/%q configured=%q saved=%q", a.model, base.Model(), base.ConfiguredModel(), config.ChatModelAt(dir))
					}
					if paired && (pairAgent.pair.Main != wantConfigured || pairAgent.pair.Fallback != "lab/backup") {
						t.Fatalf("renamed saved pair=%+v", pairAgent.pair)
					}
					if configuredService {
						a.modelsForService = func(modelsource.Connected) []Model { return []Model{{ID: "replacement"}, {ID: "chosen"}} }
						delete(a.sourceModels, customs[0].Source.ID)
						a.serviceModelsLanded(customs[0].Source.ID, customs[0].Address)
						acknowledgeModelChanges(t, a)
						if a.model != wantConfigured || base.Model() != wantConfigured || config.ChatModelAt(dir) != wantConfigured {
							t.Fatal("the renamed configured model did not return when listed")
						}
					}
				})
			}
		}
	}
}
