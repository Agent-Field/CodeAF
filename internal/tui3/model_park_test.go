package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

type parkedModelAgent struct{ *heldModelAgent }

func (a *parkedModelAgent) SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	a.submitted = append(a.submitted, a.Model())
	return a.fakeAgent.SubmitImage(ctx, text, images)
}

// C1, C2, C4: stream closure holds parked words, paste bodies and attachments
// through catalog recovery/removal and a pending/refused model acknowledgment.
func TestParkedMessageWaitsForTheAcknowledgedModelAtStreamClose(t *testing.T) {
	for _, recovery := range []bool{true, false} {
		for _, attachment := range []string{"text", "file", "picture"} {
			for _, refused := range []bool{false, true} {
				name := map[bool]string{true: "recovery", false: "removal"}[recovery] + "/" + attachment + "/" + map[bool]string{false: "accepted", true: "refused"}[refused]
				t.Run(name, func(t *testing.T) {
					live, want := "vendor/configured", "vendor/replacement"
					if recovery {
						live, want = want, live
					}
					agent := &parkedModelAgent{&heldModelAgent{
						modelForNowAgent: &modelForNowAgent{fakeAgent: &fakeAgent{model: live}, configured: "vendor/configured"},
						entered:          make(chan struct{}), release: make(chan struct{}),
					}}
					defer agent.finish()
					if refused {
						agent.err = errors.New("connection unavailable")
					}
					rows := []Model{{ID: live}}
					dir := t.TempDir()
					if err := config.WriteChatModel(dir, "vendor/configured"); err != nil {
						t.Fatal(err)
					}
					a := newApp(t.Context(), Options{Agent: agent, ProfileDir: dir, RequireListedModel: true,
						Sources: modelsource.NewSet(testDefaultService("key")), Models: func() []Model { return rows },
						SaveModel: func(id string) error { return config.WriteChatModel(dir, id) },
					})
					spend(t, a, a.submit("first message"))
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
					// A newer draft's tray must survive the older parked send too.
					a.chips = []chip{{path: "draft attachment", file: true}}
					draftChips := append([]chip(nil), a.chips...)
					rows = []Model{{ID: want}}
					a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
					agent.finish()
					_, cmd := a.Update(streamClosedMsg{gen: a.gen})
					select {
					case <-agent.entered:
					case <-time.After(time.Second):
						close(agent.release)
						t.Fatal("the turn-end model door never started")
					}
					// The direct attachment door must preserve the newer tray at
					// the same gate, even when called outside the parked road.
					if attachment != "text" {
						cmd = tea.Batch(cmd, a.submitImagesShown("draft", "draft", nil))
					}
					if len(agent.sent) != 1 || len(agent.imageText) != 0 || !reflect.DeepEqual(a.parks, []parked{waiting}) || !reflect.DeepEqual(a.chips, draftChips) {
						close(agent.release)
						t.Fatalf("pending selection spent a parked message: sent=%q parks=%+v tray=%+v", agent.sent, a.parks, a.chips)
					}
					close(agent.release)
					spend(t, a, cmd)
					if refused {
						if len(agent.sent) != 1 || len(agent.imageText) != 0 || !reflect.DeepEqual(a.parks, []parked{waiting}) || !a.modelChangePending() {
							t.Fatal("refused selection spent the parked words or attachments")
						}
						if got := lastNote(t, a); got != "could not change model · connection unavailable" {
							t.Fatalf("refusal=%q", got)
						}
						agent.err = nil
						if a.ensureAvailableModel() {
							t.Fatal("retry opened the gate before acknowledgment")
						}
						acknowledgeModelChanges(t, a)
					}
					if len(agent.sent) != 2 || !strings.Contains(agent.sent[1], waiting.spoken()) || len(agent.submitted) != 2 || agent.submitted[1] != want || len(a.parks) != 0 {
						t.Fatalf("acknowledged send=%q models=%q parks=%+v", agent.sent, agent.submitted, a.parks)
					}
					if attachment == "picture" && (len(agent.images) != 1 || len(agent.images[0]) != 1 || string(agent.images[0][0].Bytes) != "attachment bytes") {
						t.Fatalf("parked picture=%+v", agent.images)
					}
					if attachment == "file" && !strings.Contains(agent.sent[1], waiting.chips[0].path) {
						t.Fatal("parked file did not travel with its words")
					}
					if !reflect.DeepEqual(a.chips, draftChips) || agent.ConfiguredModel() != "vendor/configured" || config.ChatModelAt(dir) != "vendor/configured" {
						t.Fatal("parked send spent the draft tray or saved the replacement")
					}
					acknowledgeModelChanges(t, a)
					if len(agent.sent) != 2 {
						t.Fatal("the acknowledgment sent the parked message twice")
					}
				})
			}
		}
	}
}

type parkedSwitchModelAgent struct{ *parkedModelAgent }

func (a *parkedSwitchModelAgent) Attach() (<-chan session.Event, bool, func()) {
	return nil, false, func() {}
}

// C1, C4, C5: a model acknowledgment after a real detach/attach releases only
// its owner's parked send; a refusal keeps that owner's complete queue held.
func TestParkedModelAcknowledgmentBelongsToTheConversationThatQueuedIt(t *testing.T) {
	for _, refused := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "refused"}[refused], func(t *testing.T) {
			agent := &parkedSwitchModelAgent{&parkedModelAgent{&heldModelAgent{
				modelForNowAgent: &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/replacement"}, configured: "vendor/configured"},
				entered:          make(chan struct{}), release: make(chan struct{}),
			}}}
			defer agent.finish()
			if refused {
				agent.err = errors.New("connection unavailable")
			}
			rows := []Model{{ID: "vendor/replacement"}, {ID: "vendor/other"}}
			a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), RequireListedModel: true,
				Sources: modelsource.NewSet(testDefaultService("key")), Models: func() []Model { return rows },
			})
			a.file = filepath.Join(t.TempDir(), "held.jsonl")
			spend(t, a, a.submit("first message"))
			a.park("the owning conversation's message", nil)
			rows = append(rows, Model{ID: "vendor/configured"})
			agent.finish()
			_, pending := a.Update(streamClosedMsg{gen: a.gen})
			select {
			case <-agent.entered:
			case <-time.After(time.Second):
				close(agent.release)
				t.Fatal("the model door never started")
			}
			conv, side := a.front(), a.detachConversation()
			stowed := a.stow(conv, side)
			key := a.convKey(conv.SessionFile)
			held := a.behind[key]
			t.Cleanup(held.watch.stop)
			front := &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/other"}, configured: "vendor/other"}
			attached := a.attachConversation(Conversation{Agent: front, SessionFile: filepath.Join(t.TempDir(), "front.jsonl")}, nil)
			a.parks = []parked{{text: "the front conversation's message"}}
			if a.sendBehindParked(key, held) != nil || len(side.parks) != 1 || len(agent.sent) != 1 {
				close(agent.release)
				t.Fatal("switching chats bypassed the pending model acknowledgment")
			}
			close(agent.release)
			spend(t, a, tea.Batch(pending, stowed, attached))
			if len(front.sent) != 0 || len(a.parks) != 1 || front.ConfiguredModel() != "vendor/other" {
				t.Fatal("the old acknowledgment released or changed the front conversation")
			}
			if refused {
				if len(agent.sent) != 1 || len(side.parks) != 1 || a.sendBehindParked(key, held) != nil {
					t.Fatal("refusal released the hidden conversation's queue")
				}
				agent.err = nil
				held.watch.stop()
				delete(a.behind, key)
				a.detachConversation()
				spend(t, a, a.attachConversation(conv, side))
				acknowledgeModelChanges(t, a)
			}
			if len(agent.sent) != 2 || agent.sent[1] != "the owning conversation's message" || len(agent.submitted) != 2 || agent.submitted[1] != "vendor/configured" {
				t.Fatalf("owning send=%q models=%q", agent.sent, agent.submitted)
			}
		})
	}
}

// C1, C4: coming back before the original reply keeps its model gate and
// resumes the parked send once, despite the intervening front generations.
func TestReturningBeforeModelAcknowledgmentResumesTheParkedSend(t *testing.T) {
	agent := &parkedModelAgent{&heldModelAgent{
		modelForNowAgent: &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/replacement"}, configured: "vendor/configured"},
		entered:          make(chan struct{}), release: make(chan struct{}),
	}}
	defer agent.finish()
	rows := []Model{{ID: "vendor/replacement"}, {ID: "vendor/other"}}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), RequireListedModel: true,
		Sources: modelsource.NewSet(testDefaultService("key")), Models: func() []Model { return rows },
	})
	spend(t, a, a.submit("first message"))
	a.park("the returning conversation's message", nil)
	rows = append(rows, Model{ID: "vendor/configured"})
	agent.finish()
	_, pending := a.Update(streamClosedMsg{gen: a.gen})
	conv, side := a.front(), a.detachConversation()
	away := a.attachConversation(Conversation{Agent: &fakeAgent{model: "vendor/other"}}, nil)
	a.detachConversation()
	back := a.attachConversation(conv, side)
	if !a.modelChangePending() || len(a.parks) != 1 || len(agent.sent) != 1 {
		close(agent.release)
		t.Fatal("returning lost the pending model gate or its waiting message")
	}
	close(agent.release)
	spend(t, a, tea.Batch(pending, away, back))
	if a.modelChangePending() || len(agent.sent) != 2 || agent.sent[1] != "the returning conversation's message" || len(agent.submitted) != 2 || agent.submitted[1] != "vendor/configured" || len(a.parks) != 0 {
		t.Fatalf("returning acknowledgment: sent=%q models=%q parks=%+v", agent.sent, agent.submitted, a.parks)
	}
}
