package tui3

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

type heldModelAgent struct {
	*modelForNowAgent
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	err     error
}

func (a *heldModelAgent) UseModelForNow(id string) error {
	a.once.Do(func() { close(a.entered) })
	select {
	case <-a.release:
	case <-time.After(2 * time.Second):
		return errors.New("model door fixture timed out")
	}
	if a.err != nil {
		return a.err
	}
	return a.modelForNowAgent.UseModelForNow(id)
}

// C1, C4, C5: catalog selection returns to the loop while its door waits;
// the draft and alternate send gates stay closed until a successful reply.
func TestRunOnlySelectionWaitsForAcknowledgmentOffTheLoop(t *testing.T) {
	for _, refused := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "refused"}[refused], func(t *testing.T) {
			agent := &heldModelAgent{
				modelForNowAgent: &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/configured"}, configured: "vendor/configured"},
				entered:          make(chan struct{}), release: make(chan struct{}),
			}
			if refused {
				agent.err = errors.New("connection unavailable")
			}
			a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir(), Sources: modelsource.NewSet(testDefaultService("key")),
				Models: func() []Model { return []Model{{ID: "vendor/replacement"}} },
			})
			a.requireListedModel = true
			a.input.insert("keep this draft")
			if a.ensureAvailableModel() || a.model != "vendor/configured" {
				t.Fatal("unacknowledged selection opened the send gate")
			}
			select {
			case <-agent.entered:
			case <-time.After(time.Second):
				close(agent.release)
				t.Fatal("model door never started")
			}
			called := false
			cmd := a.submitting("blocked", func() (<-chan session.Event, error) { called = true; return nil, nil })
			if called || a.input.String() != "keep this draft" || a.state == stateWorking {
				t.Fatal("pending selection submitted or consumed the draft")
			}
			close(agent.release)
			spend(t, a, cmd)
			if refused {
				if a.model != "vendor/configured" || agent.ConfiguredModel() != "vendor/configured" {
					t.Fatal("refused selection changed the model")
				}
				if got := lastNote(t, a); got != "could not change model · connection unavailable" {
					t.Fatalf("refusal=%q", got)
				}
				if a.ensureAvailableModel() {
					t.Fatal("failed model selection opened the send gate")
				}
				acknowledgeModelChanges(t, a)
				return
			}
			if !a.ensureAvailableModel() || a.model != "vendor/replacement" || agent.ConfiguredModel() != "vendor/configured" {
				t.Fatal("successful selection did not resume reconciliation")
			}
			spend(t, a, a.enterLine())
			if len(agent.submitted) != 1 || agent.submitted[0] != "vendor/replacement" {
				t.Fatalf("submitted=%v", agent.submitted)
			}
		})
	}
}

// C5: an explicit pick queued behind a temporary selection wins in wire order
// and keeps its ordinary profile write and model receipt.
func TestExplicitPickSupersedesAnUnacknowledgedRunOnlySelection(t *testing.T) {
	agent := &heldModelAgent{
		modelForNowAgent: &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/configured"}, configured: "vendor/configured"},
		entered:          make(chan struct{}), release: make(chan struct{}),
	}
	dir := t.TempDir()
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: dir, SaveModel: func(id string) error { return config.WriteChatModel(dir, id) }})
	a.useModelForNow("vendor/replacement", 0)
	a.switchModel("vendor/configured", 0)
	close(agent.release)
	acknowledgeModelChanges(t, a)
	if agent.Model() != "vendor/configured" || agent.ConfiguredModel() != "vendor/configured" || config.ChatModelAt(dir) != "vendor/configured" {
		t.Fatal("the temporary selection displaced the explicit pick")
	}
	if got := noteSaying(t, a, "model ·"); got != "model · vendor/configured" {
		t.Fatalf("explicit receipt=%q", got)
	}
}

type heldLegacyModelAgent struct {
	*fakeAgent
	release chan struct{}
}

// C5: leaving a conversation before its queued explicit pick is acknowledged
// does not lose its saved preference or apply its display to the next chat.
func TestPendingExplicitPickSavesAfterLeavingTheConversation(t *testing.T) {
	for _, chooseAgain := range []bool{false, true} {
		t.Run(map[bool]string{false: "leave", true: "choose in next chat"}[chooseAgain], func(t *testing.T) {
			agent := &heldModelAgent{
				modelForNowAgent: &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/configured"}, configured: "vendor/configured"},
				entered:          make(chan struct{}), release: make(chan struct{}),
			}
			dir := t.TempDir()
			a := newApp(t.Context(), Options{Agent: agent, ProfileDir: dir, SaveModel: func(id string) error { return config.WriteChatModel(dir, id) }})
			a.useModelForNow("vendor/replacement", 0)
			a.switchModel("vendor/chosen", 0)
			a.frontGen++
			a.agent, a.model = &fakeAgent{model: "other/model"}, "other/model"
			wantSaved, wantLive := "vendor/chosen", "other/model"
			if chooseAgain {
				a.switchModel("other/chosen", 0)
				wantSaved, wantLive = "other/chosen", "other/chosen"
			}
			close(agent.release)
			acknowledgeModelChanges(t, a)
			if agent.ConfiguredModel() != "vendor/chosen" || config.ChatModelAt(dir) != wantSaved || a.model != wantLive {
				t.Fatal("leaving the conversation lost a choice or applied choices out of order")
			}
		})
	}
}

func (a *heldLegacyModelAgent) SetModel(id string) {
	select {
	case <-a.release:
	case <-time.After(2 * time.Second):
		return
	}
	a.fakeAgent.SetModel(id)
}

// C6: an older engine's SetModel fallback also waits off the update loop.
func TestLegacyRunOnlySelectionUsesTheOffLoopDoor(t *testing.T) {
	agent := &heldLegacyModelAgent{fakeAgent: &fakeAgent{model: "vendor/configured"}, release: make(chan struct{})}
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir()})
	a.useModelForNow("vendor/replacement", 0)
	if !a.modelChangePending() || a.model != "vendor/configured" {
		t.Fatal("the legacy model change did not wait for its acknowledgment")
	}
	close(agent.release)
	acknowledgeModelChanges(t, a)
	if a.model != "vendor/replacement" || agent.Model() != a.model {
		t.Fatal("legacy model acknowledgment was lost")
	}
}
