package session

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestIdleRelease_R5_1_PersistentChoicesRefuseBeforeMutation(t *testing.T) {
	want := &executor.WorkRefusal{Line: "laptop continued this chat", HolderID: "B"}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Seat = executor.Gated(c.Seat, func() error { return nil }, nil, nil, func() error { return want })
	})
	model, effort := a.Model(), a.ConversationEffort()
	for _, check := range []struct {
		name string
		run  func() error
	}{
		{"model", func() error { return a.SetModelChecked("new/model") }},
		{"effort", func() error { _, err := a.SetConversationEffortChecked("high"); return err }},
		{"models", func() error { _, err := a.SetConversationModels(ConversationModels{Main: "new/model"}); return err }},
		{"automatic models", func() error { return a.SetAutomaticModels(true) }},
		{"approval", func() error { return a.SetApprovalPosture(PostureAsk) }},
		{"refer place", func() error { _, err := a.ReferPlace(t.TempDir(), PlaceSaid); return err }},
		{"remove place", func() error { return a.RemovePlace("kept") }},
		{"place mode", func() error { return a.SetPlaceMode("kept", "read") }},
		{"task effort", func() error { return a.SetTaskEffort(1, "high") }},
		{"memory", func() error { _, err := a.Remember("keep my requested words"); return err }},
	} {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, want) {
				t.Fatalf("setting refusal=%v", err)
			}
		})
	}
	if a.Model() != model || a.ConversationEffort() != effort {
		t.Fatal("refused choice mutated the conversation")
	}
	a.SetModel("another/model")
	if !errors.Is(a.SettingError(), want) {
		t.Fatal("void model door lost the refusal")
	}
	if a.SetConversationEffort("high") || !errors.Is(a.SettingError(), want) {
		t.Fatal("bool effort door lost the refusal")
	}
}

func TestIdleRelease_R5_1_SettingAdmissionRetainsTheHoldWhileBusy(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	watch := &cellstore.SealWatch{}
	watch.Waiting()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Seals = watch
		c.Seat = executor.Gated(c.Seat, func() error { return nil }, nil, nil, func() error { close(entered); <-release; return nil })
	})
	a.SettleWrites()
	done := make(chan bool, 1)
	go func() { done <- a.SetConversationEffort("high") }()
	defer func() { close(release); <-done }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("setting did not enter admission")
	}
	if watch.IdleReady() {
		t.Fatal("setting admission still owns the choice, but the chat looks idle")
	}
}
