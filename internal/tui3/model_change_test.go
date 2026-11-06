package tui3

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

type heldModelAgent struct {
	*modelForNowAgent
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	err      error
	atSubmit chan string
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

func (a *heldModelAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	a.atSubmit <- a.Model()
	return a.modelForNowAgent.Submit(ctx, text)
}

func (a *heldModelAgent) FollowUp(text string) (<-chan session.Event, error) {
	return a.Submit(context.Background(), text)
}

func modelOrderingApp(t *testing.T, refused bool) (*app, *heldModelAgent, func()) {
	t.Helper()
	agent := &heldModelAgent{
		modelForNowAgent: &modelForNowAgent{fakeAgent: &fakeAgent{model: "vendor/configured"}, configured: "vendor/configured"},
		entered:          make(chan struct{}), release: make(chan struct{}), atSubmit: make(chan string, 8),
	}
	if refused {
		agent.err = errors.New("connection unavailable")
	}
	unblock := sync.OnceFunc(func() { close(agent.release) })
	t.Cleanup(unblock)
	t.Cleanup(agent.finish)
	dir := t.TempDir()
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: dir,
		Sources:   modelsource.NewSet(testDefaultService("key")),
		Models:    func() []Model { return []Model{{ID: "vendor/replacement"}, {ID: "vendor/chosen"}} },
		SaveModel: func(id string) error { return config.WriteChatModel(dir, id) },
	})
	a.requireListedModel = true
	return a, agent, unblock
}

func beginOrderedSend(t *testing.T, agent *heldModelAgent, cmd tea.Cmd) <-chan []tea.Msg {
	t.Helper()
	select {
	case <-agent.entered:
	case <-time.After(time.Second):
		t.Fatal("model door never started")
	}
	done := make(chan []tea.Msg, 1)
	go func() { done <- runCmd(cmd) }()
	select {
	case model := <-agent.atSubmit:
		t.Fatalf("Submit passed the unfinished model door on %q", model)
	case <-done:
		t.Fatal("send command finished before the model door")
	case <-time.After(30 * time.Millisecond):
	}
	return done
}

func finishOrderedSend(t *testing.T, a *app, done <-chan []tea.Msg) {
	t.Helper()
	select {
	case msgs := <-done:
		drive(t, a, msgs...)
	case <-time.After(time.Second):
		t.Fatal("send did not resume after the model door")
	}
}

// C1, C4: display changes immediately; only the command waits for selection,
// without requiring its UI acknowledgment.
func TestRunOnlySelectionOrdersTheSendOffTheLoop(t *testing.T) {
	a, agent, unblock := modelOrderingApp(t, false)
	if !a.ensureAvailableModel() || a.model != "vendor/replacement" {
		t.Fatal("selection did not display immediately")
	}
	a.input.insert("send this draft")
	done := beginOrderedSend(t, agent, a.enterLine())
	if a.input.String() != "" || a.state != stateWorking {
		t.Fatal("surface held the send")
	}
	unblock()
	finishOrderedSend(t, a, done)
	acknowledgeModelChanges(t, a)
	if len(agent.sent) != 1 || agent.sent[0] != "send this draft" || agent.submitted[0] != "vendor/replacement" || agent.ConfiguredModel() != "vendor/configured" {
		t.Fatalf("sent=%q models=%q", agent.sent, agent.submitted)
	}
}

// C4: a newer catalog can restore the configured model behind a pending
// replacement, and the next send waits for both asks without a UI fold.
func TestCatalogRecoverySupersedesAPendingReplacement(t *testing.T) {
	a, agent, unblock := modelOrderingApp(t, false)
	a.ensureAvailableModel()
	a.models = func() []Model { return []Model{{ID: "vendor/configured"}} }
	a.serviceModelsLanded(modelsource.DefaultID, config.DefaultBaseURL)
	if a.model != "vendor/configured" {
		t.Fatal("recovery did not display immediately")
	}
	done := beginOrderedSend(t, agent, a.submit("use the recovered model"))
	unblock()
	finishOrderedSend(t, a, done)
	acknowledgeModelChanges(t, a)
	if len(agent.sent) != 1 || agent.submitted[0] != "vendor/configured" || len(agent.temporary) != 2 {
		t.Fatalf("sent=%q models=%q selections=%q", agent.sent, agent.submitted, agent.temporary)
	}
}

// C4, C6: alternate surface turn doors share the command wait even when
// their start closure does not go through submitStart.
func TestAlternateTurnStartFollowsTheOrderedSelection(t *testing.T) {
	a, agent, unblock := modelOrderingApp(t, false)
	a.ensureAvailableModel()
	cmd := a.submitting("alternate turn", func() (<-chan session.Event, error) {
		return agent.Submit(t.Context(), "alternate turn")
	})
	done := beginOrderedSend(t, agent, cmd)
	unblock()
	finishOrderedSend(t, a, done)
	acknowledgeModelChanges(t, a)
	if len(agent.sent) != 1 || agent.submitted[0] != "vendor/replacement" {
		t.Fatal("alternate turn passed the model selection")
	}
}

// C4, C6: a follow-up on an idle engine starts a turn, so its separate call
// must also follow the pending switch rather than overtaking plain sends.
func TestIdleFollowUpFollowsTheOrderedSelection(t *testing.T) {
	a, agent, unblock := modelOrderingApp(t, false)
	a.ensureAvailableModel()
	a.input.insert("start this follow-up")
	done := beginOrderedSend(t, agent, a.followUp())
	unblock()
	finishOrderedSend(t, a, done)
	acknowledgeModelChanges(t, a)
	if len(agent.sent) != 1 || agent.sent[0] != "start this follow-up" || agent.submitted[0] != "vendor/replacement" {
		t.Fatal("idle follow-up bypassed the ordered model selection")
	}
}

// C6: cancellation bounds a send already waiting for a model door, without
// submitting or canceling the independently ordered model change.
func TestCancelingSendEndsTheModelWait(t *testing.T) {
	a, agent, unblock := modelOrderingApp(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a.ctx = ctx
	a.ensureAvailableModel()
	done := beginOrderedSend(t, agent, a.submit("canceled while waiting"))
	cancel()
	var msgs []tea.Msg
	select {
	case msgs = <-done:
	case <-time.After(time.Second):
		t.Fatal("send context did not end the model wait")
	}
	found := false
	for _, msg := range msgs {
		if submitted, ok := msg.(submittedMsg); ok && errors.Is(submitted.err, context.Canceled) {
			found = true
		}
	}
	if !found || len(agent.atSubmit) != 0 {
		t.Fatal("canceled wait reached Submit or lost its cancellation")
	}
	unblock()
	drive(t, a, msgs...)
	acknowledgeModelChanges(t, a)
	if len(agent.sent) != 0 {
		t.Fatal("model acknowledgment resurrected a canceled send")
	}
}

// C1, C4: refusal releases one message on the current model; reconciliation retries.
func TestRefusedRunOnlySelectionStillSendsOnce(t *testing.T) {
	a, agent, unblock := modelOrderingApp(t, true)
	a.ensureAvailableModel()
	done := beginOrderedSend(t, agent, a.submit("send despite refusal"))
	unblock()
	finishOrderedSend(t, a, done)
	if len(agent.sent) != 1 || agent.sent[0] != "send despite refusal" || agent.submitted[0] != "vendor/configured" {
		t.Fatalf("sent=%q models=%q", agent.sent, agent.submitted)
	}
	if got := lastNote(t, a); got != "could not change model · connection unavailable" {
		t.Fatalf("refusal=%q", got)
	}
	agent.err = nil
	a.settle()
	acknowledgeModelChanges(t, a)
	if agent.Model() != "vendor/replacement" || len(agent.sent) != 1 {
		t.Fatal("retry failed or repeated the send")
	}
}

// C5: the explicit pick after a pending temporary selection owns the next send,
// profile write and immediate receipt.
func TestExplicitPickSupersedesAnUnacknowledgedRunOnlySelection(t *testing.T) {
	a, agent, unblock := modelOrderingApp(t, false)
	a.useModelForNow("vendor/replacement", 0)
	a.switchModel("vendor/chosen", 0)
	if a.model != "vendor/chosen" || noteSaying(t, a, "model ·") != "model · vendor/chosen" {
		t.Fatal("explicit pick did not display immediately")
	}
	done := beginOrderedSend(t, agent, a.submit("use the explicit choice"))
	unblock()
	finishOrderedSend(t, a, done)
	acknowledgeModelChanges(t, a)
	if agent.ConfiguredModel() != "vendor/chosen" || config.ChatModelAt(a.profileDir) != "vendor/chosen" || len(agent.sent) != 1 || agent.submitted[0] != "vendor/chosen" {
		t.Fatal("temporary selection displaced the explicit pick or its send")
	}
}

// C5: picks made after leaving a conversation keep preference writes ordered.
func TestPendingExplicitPickSavesAfterLeavingTheConversation(t *testing.T) {
	for _, chooseAgain := range []bool{false, true} {
		t.Run(map[bool]string{false: "leave", true: "choose in next chat"}[chooseAgain], func(t *testing.T) {
			a, agent, unblock := modelOrderingApp(t, false)
			a.useModelForNow("vendor/replacement", 0)
			a.switchModel("vendor/chosen", 0)
			a.frontGen++
			a.agent, a.model = &fakeAgent{model: "other/model"}, "other/model"
			wantSaved, wantLive := "vendor/chosen", "other/model"
			if chooseAgain {
				a.switchModel("other/chosen", 0)
				wantSaved, wantLive = "other/chosen", "other/chosen"
			}
			unblock()
			acknowledgeModelChanges(t, a)
			if agent.ConfiguredModel() != "vendor/chosen" || config.ChatModelAt(a.profileDir) != wantSaved || a.model != wantLive {
				t.Fatal("leaving lost a choice or reordered preferences")
			}
		})
	}
}

type heldLegacyModelAgent struct {
	*fakeAgent
	release chan struct{}
}

func (a *heldLegacyModelAgent) SetModel(id string) {
	select {
	case <-a.release:
	case <-time.After(2 * time.Second):
		return
	}
	a.fakeAgent.SetModel(id)
}

// C6: legacy selection displays immediately, precedes Submit without a UI fold,
// and a canceled send context ends the wait without submitting.
func TestLegacyRunOnlySelectionUsesTheOffLoopDoor(t *testing.T) {
	agent := &heldLegacyModelAgent{fakeAgent: &fakeAgent{model: "vendor/configured"}, release: make(chan struct{})}
	unblock := sync.OnceFunc(func() { close(agent.release) })
	t.Cleanup(unblock)
	a := newApp(t.Context(), Options{Agent: agent, ProfileDir: t.TempDir()})
	a.useModelForNow("vendor/replacement", 0)
	if a.model != "vendor/replacement" {
		t.Fatal("legacy selection did not display immediately")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := submitStart(agent, ctx, "canceled send")(); !errors.Is(err, context.Canceled) || len(agent.sent) != 0 {
		t.Fatalf("canceled wait submitted: sent=%q err=%v", agent.sent, err)
	}
	unblock()
	if _, err := submitStart(agent, t.Context(), "legacy send")(); err != nil {
		t.Fatal(err)
	}
	acknowledgeModelChanges(t, a)
	if agent.Model() != "vendor/replacement" || len(agent.sent) != 1 {
		t.Fatal("legacy selection did not precede its send")
	}
	agent.finish()
}
