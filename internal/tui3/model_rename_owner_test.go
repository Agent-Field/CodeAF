package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

func deferredRenameApp(t *testing.T, paired bool) (*app, *pairedModelForNowAgent) {
	t.Helper()
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "mybox")
	agent := &pairedModelForNowAgent{
		modelForNowAgent: &modelForNowAgent{fakeAgent: &fakeAgent{model: "mybox/chosen"}, configured: "mybox/chosen"},
	}
	var door Agent = agent.modelForNowAgent
	if paired {
		agent.pair = session.ConversationModels{Main: "mybox/chosen", Fallback: "mybox/backup"}
		door = agent
	}
	a := modelServiceTestAppWithAgent(t, dir, agent.configured,
		modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...),
		[]Model{{ID: "vendor/replacement"}, {ID: "vendor/other"}, {ID: "vendor/explicit"}}, door)
	a.modelsForService = func(modelsource.Connected) []Model { return []Model{{ID: "replacement"}} }
	if err := config.WriteChatModel(dir, agent.configured); err != nil {
		t.Fatal(err)
	}
	a.useModelForNow("vendor/replacement", 0)
	acknowledgeModelChanges(t, a)
	a.requireListedModel = true
	a.state = stateWorking
	rows := config.PersistedSources(dir)
	rows[0].Written = "lab"
	if err := config.WriteSources(dir, rows); err != nil {
		t.Fatal(err)
	}
	a.adoptModelConnectResult(modelConnectResultMsg{service: customs[0].Source.ID, written: "lab", renamedFrom: "mybox",
		outcome: modelsource.Outcome{Kind: modelsource.OutcomeConnected}, models: []Model{{ID: "replacement"}},
	})
	if a.deferredModelRename == nil || agent.ConfiguredModel() != "mybox/chosen" {
		t.Fatal("rename did not wait for its answering conversation")
	}
	return a, agent
}

// C1, C2, C4, C5: switching chats before settlement keeps the rename with its
// owner. The unrelated chat settles without adopting either captured model.
func TestDeferredRenameStaysWithItsConversationAcrossASwitch(t *testing.T) {
	for _, paired := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "pair"}[paired], func(t *testing.T) {
			a, original := deferredRenameApp(t, paired)
			a.park("waiting for the renamed conversation", nil)
			conv, side := a.front(), a.detachConversation()
			if a.sendBehindParked("owner", &kept{conv: conv, side: side}) != nil || len(side.parks) != 1 {
				t.Fatal("the hidden parked send bypassed its deferred rename")
			}
			other := &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/other"}, configured: "vendor/other"}
			spend(t, a, a.attachConversation(Conversation{Agent: other}, nil))
			a.switchModel("vendor/other", 0)
			a.state = stateWorking
			drive(t, a, streamClosedMsg{gen: a.gen})
			if other.ConfiguredModel() != "vendor/other" || other.Model() != "vendor/other" || config.ChatModelAt(a.profileDir) != "vendor/other" || len(other.temporary) != 0 {
				t.Fatal("another conversation's settlement adopted the deferred rename")
			}
			if original.ConfiguredModel() != "mybox/chosen" {
				t.Fatal("the original conversation changed before returning to it")
			}
			a.detachConversation()
			spend(t, a, a.attachConversation(conv, side))
			acknowledgeModelChanges(t, a)
			if original.ConfiguredModel() != "lab/chosen" || original.Model() != "vendor/replacement" || other.ConfiguredModel() != "vendor/other" || config.ChatModelAt(a.profileDir) == "vendor/replacement" {
				t.Fatal("the owning conversation lost its rename or saved its replacement")
			}
			if paired && original.pair != (session.ConversationModels{Main: "lab/chosen", Fallback: "lab/backup"}) {
				t.Fatalf("the owner's renamed pair=%+v", original.pair)
			}
			if len(original.sent) != 1 || original.sent[0] != "waiting for the renamed conversation" || len(a.parks) != 0 {
				t.Fatal("the owner's parked send did not resume after the rename acknowledgment")
			}
			original.finish()
		})
	}
}

// C2, C5: a later explicit single or pair choice retires the captured rename,
// including a pair whose main model is unchanged and only its fallback changes.
func TestLaterExplicitChoiceRetiresTheDeferredRename(t *testing.T) {
	for _, pick := range []string{"single", "pair", "pair fallback", "same pair"} {
		t.Run(pick, func(t *testing.T) {
			a, agent := deferredRenameApp(t, pick != "single")
			want := "vendor/explicit"
			var pair session.ConversationModels
			if pick == "single" {
				a.slash("/model " + want)
			} else {
				pair = session.ConversationModels{Main: want, Fallback: "vendor/backup"}
				if pick == "pair fallback" {
					want, pair.Main = "mybox/chosen", "mybox/chosen"
				}
				if pick == "same pair" {
					pair = agent.pair
					want = pair.Main
				}
				if _, err := agent.SetConversationModels(pair); err != nil {
					t.Fatal(err)
				}
				a.modelSwitched(want, 0, true)
			}
			acknowledgeModelChanges(t, a)
			drive(t, a, streamClosedMsg{gen: a.gen})
			if agent.ConfiguredModel() != want || config.ChatModelAt(a.profileDir) != want || a.deferredModelRename != nil {
				t.Fatalf("settlement replaced the explicit choice: configured=%q saved=%q", agent.ConfiguredModel(), config.ChatModelAt(a.profileDir))
			}
			if pick != "single" && agent.pair != pair {
				t.Fatalf("settlement replaced the explicit pair: %+v", agent.pair)
			}
		})
	}
}
