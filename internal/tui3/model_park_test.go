package tui3

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

type parkedModelAgent struct{ *heldModelAgent }

func (a *parkedModelAgent) SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	a.atSubmit <- a.Model()
	a.submitted = append(a.submitted, a.Model())
	return a.fakeAgent.SubmitImage(ctx, text, images)
}

// C1, C2, C4: parked text, paste bodies and attachments submit exactly once after
// the model door, without UI acknowledgment or loss of a newer draft tray.
func TestParkedMessageFollowsItsOrderedModelAtStreamClose(t *testing.T) {
	for _, attachment := range []string{"text", "file", "picture"} {
		t.Run(attachment, func(t *testing.T) {
			a, base, unblock := modelOrderingApp(t, false)
			agent := &parkedModelAgent{base}
			a.agent = agent
			a.state = stateWorking
			a.pastes = []pasteChip{{n: 1, text: "the complete pasted document"}}
			if attachment != "text" {
				path := filepath.Join(t.TempDir(), "attachment")
				if err := os.WriteFile(path, []byte("attachment bytes"), 0600); err != nil {
					t.Fatal(err)
				}
				a.chips = []chip{{path: path, file: attachment == "file"}}
			}
			a.park("waiting "+pasteToken(1, 1), []segment{{from: 0, to: 7}})
			waiting := a.parks[0]
			a.chips = []chip{{path: "draft attachment", file: true}}
			draft := append([]chip(nil), a.chips...)
			_, cmd := a.Update(streamClosedMsg{gen: a.gen})
			done := beginOrderedSend(t, base, cmd)
			if len(a.parks) != 0 || !reflect.DeepEqual(a.chips, draft) {
				t.Fatal("surface held the parked message or spent the newer tray")
			}
			unblock()
			finishOrderedSend(t, a, done)
			if len(agent.sent) != 1 || !strings.Contains(agent.sent[0], waiting.spoken()) || len(agent.submitted) != 1 || agent.submitted[0] != "vendor/replacement" {
				t.Fatalf("sent=%q models=%q", agent.sent, agent.submitted)
			}
			if attachment == "picture" && (len(agent.images) != 1 || len(agent.images[0]) != 1 || string(agent.images[0][0].Bytes) != "attachment bytes") {
				t.Fatalf("picture=%+v", agent.images)
			}
			if attachment == "file" && !strings.Contains(agent.sent[0], waiting.chips[0].path) {
				t.Fatal("file path was lost")
			}
			acknowledgeModelChanges(t, a)
			if len(agent.sent) != 1 || agent.ConfiguredModel() != "vendor/configured" {
				t.Fatal("acknowledgment repeated the send or saved the replacement")
			}
		})
	}
}

type replayModelAgent struct{ *heldModelAgent }

func (a *replayModelAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	return nil, nil, func() {}
}

// C1, C4: a replay-deferred draft survives a catalog change before the replay
// fold and sends once after the switch, including a refused switch.
func TestReplayDeferredSendFollowsItsOrderedModel(t *testing.T) {
	for _, refused := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "refused"}[refused], func(t *testing.T) {
			a, base, unblock := modelOrderingApp(t, refused)
			a.agent = &replayModelAgent{base}
			base.fakeAgent.model, a.model = "vendor/replacement", "vendor/replacement"
			answer := a.refreshHostedReplay()()
			a.input.insert("keep the replay-deferred words")
			if cmd := a.enterLine(); cmd != nil || len(a.hostDeferred) != 1 || a.input.String() != "" {
				t.Fatal("send was not deferred at the replay boundary")
			}
			a.models = func() []Model { return []Model{{ID: "vendor/configured"}} }
			a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
			_, cmd := a.Update(answer)
			done := beginOrderedSend(t, base, cmd)
			if len(a.hostDeferred) != 0 {
				t.Fatal("replay kept a second copy of its send")
			}
			unblock()
			finishOrderedSend(t, a, done)
			want := "vendor/configured"
			if refused {
				want = "vendor/replacement"
			}
			if len(base.sent) != 1 || base.sent[0] != "keep the replay-deferred words" || base.submitted[0] != want {
				t.Fatalf("sent=%q models=%q", base.sent, base.submitted)
			}
			acknowledgeModelChanges(t, a)
			if len(base.sent) != 1 {
				t.Fatal("replay acknowledgment repeated the send")
			}
		})
	}
}

type parkedSwitchModelAgent struct{ *heldModelAgent }

func (a *parkedSwitchModelAgent) Attach() (<-chan session.Event, bool, func()) {
	return nil, false, func() {}
}

// C1, C4, C5: a held chat's pending switch precedes its parked send and never
// changes the unrelated front chat; the keeper consumes the message once.
func TestHeldSendFollowsItsOwnOrderedModel(t *testing.T) {
	a, base, unblock := modelOrderingApp(t, false)
	agent := &parkedSwitchModelAgent{base}
	a.agent = agent
	a.file = filepath.Join(t.TempDir(), "held.jsonl")
	a.parks = []parked{{text: "the held conversation's message"}}
	a.ensureAvailableModel()
	conv, side := a.front(), a.detachConversation()
	stowed := a.stow(conv, side)
	key := a.convKey(conv.SessionFile)
	held := a.behind[key]
	t.Cleanup(held.watch.stop)
	front := &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/chosen"}, configured: "vendor/chosen"}
	attached := a.attachConversation(Conversation{Agent: front, SessionFile: filepath.Join(t.TempDir(), "front.jsonl")}, nil)
	done := beginOrderedSend(t, base, tea.Batch(a.sendBehindParked(key, held), stowed, attached))
	unblock()
	finishOrderedSend(t, a, done)
	acknowledgeModelChanges(t, a)
	if len(base.sent) != 1 || base.sent[0] != "the held conversation's message" || base.submitted[0] != "vendor/replacement" || len(side.parks) != 0 {
		t.Fatalf("held send=%q models=%q parks=%+v", base.sent, base.submitted, side.parks)
	}
	if len(front.sent) != 0 || front.Model() != "vendor/chosen" || a.model != "vendor/chosen" {
		t.Fatal("held selection affected the front chat")
	}
}

// C1, C4, C5: returning before the model ask completes restores its immediate
// display and sends once through the same agent token, across front generations.
func TestReturningBeforeModelSelectionKeepsTheOrderedSend(t *testing.T) {
	a, base, unblock := modelOrderingApp(t, false)
	a.ensureAvailableModel()
	conv, side := a.front(), a.detachConversation()
	away := a.attachConversation(Conversation{Agent: &fakeAgent{model: "vendor/chosen"}}, nil)
	a.detachConversation()
	back := a.attachConversation(conv, side)
	if a.model != "vendor/replacement" {
		t.Fatal("returning lost the pending model display")
	}
	done := beginOrderedSend(t, base, tea.Batch(away, back, a.submit("send after returning")))
	unblock()
	finishOrderedSend(t, a, done)
	acknowledgeModelChanges(t, a)
	if len(base.sent) != 1 || base.submitted[0] != "vendor/replacement" {
		t.Fatal("returning repeated or bypassed the ordered send")
	}
}

// C4, C6: catalog recovery does not reconcile a hidden chat; its next front
// attach restores the configured model before that chat's subsequent request.
func TestHiddenChatReconcilesAvailabilityOnlyWhenItReturns(t *testing.T) {
	a, base, unblock := modelOrderingApp(t, false)
	agent := &parkedSwitchModelAgent{base}
	a.agent = agent
	a.file = filepath.Join(t.TempDir(), "held.jsonl")
	a.ensureAvailableModel()
	unblock()
	acknowledgeModelChanges(t, a)
	conv, side := a.front(), a.detachConversation()
	stowed := a.stow(conv, side)
	key := a.convKey(conv.SessionFile)
	held := a.behind[key]
	t.Cleanup(held.watch.stop)
	front := &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/chosen"}, configured: "vendor/chosen"}
	spend(t, a, tea.Batch(stowed, a.attachConversation(Conversation{Agent: front}, nil)))
	a.models = func() []Model { return []Model{{ID: "vendor/configured"}, {ID: "vendor/chosen"}} }
	a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
	held.side.parks = []parked{{text: "send while hidden"}}
	spend(t, a, a.checkBehindParked(key, agent))
	if len(base.sent) != 1 || base.submitted[0] != "vendor/replacement" || len(base.temporary) != 1 {
		t.Fatal("hidden catalog arrival reconciled the held conversation")
	}
	base.finish()
	held.watch.stop()
	delete(a.behind, key)
	a.detachConversation()
	spend(t, a, a.attachConversation(conv, side))
	acknowledgeModelChanges(t, a)
	spend(t, a, a.submit("send after returning"))
	if len(base.sent) != 2 || base.submitted[1] != "vendor/configured" || base.ConfiguredModel() != "vendor/configured" {
		t.Fatalf("returning send=%q models=%q", base.sent, base.submitted)
	}
}
