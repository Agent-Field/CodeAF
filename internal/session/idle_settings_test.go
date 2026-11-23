package session

import (
	"errors"
	"path/filepath"
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

func TestIdleRelease_R8_DurableWritesOutsideTurnsRefuseBeforeMutation(t *testing.T) {
	want := &executor.WorkRefusal{Line: "laptop continued this chat", HolderID: "B"}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		c.Seat = executor.Gated(c.Seat, func() error { return nil }, nil, nil, func() error { return want })
	})
	for _, check := range []struct {
		name string
		run  func() error
	}{
		{"rename", func() error { return a.RenameByPerson("person's title") }},
		{"rewind", func() error { _, err := a.Rewind(); return err }},
		{"rewind at", func() error { _, err := a.RewindAt(0); return err }},
		{"rewind keep", func() error { _, err := a.RewindKeep(0); return err }},
		{"undo rewind", func() error { return a.UndoRewind("token") }},
		{"compact", func() error { return a.Compact(t.Context()) }},
		{"observed compact", func() error { return a.CompactObserved(t.Context(), nil) }},
		{"anchor", func() error { _, err := a.AnchorWorkspace(t.TempDir()); return err }},
		{"move memory", func() error { _, err := a.MoveMemory("id", "project"); return err }},
		{"plan note", func() error { return a.PlanNote("id", "person's note") }},
		{"delete task", func() error { return a.DeleteTask("id") }},
	} {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, want) {
				t.Fatalf("durable write refusal=%v", err)
			}
		})
	}
}

func TestIdleRelease_R8_MoreDurableWritesOutsideTurnsRefuse(t *testing.T) {
	want := &executor.WorkRefusal{Line: "laptop continued this chat", HolderID: "B"}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Seat = executor.Gated(c.Seat, func() error { return nil }, nil, nil, func() error { return want })
	})
	for _, check := range []struct {
		name string
		run  func() error
	}{
		{"resolve unverified", func() error { return a.ResolveUnverified(1, TaskAccept, "accepted") }},
		{"review root", func() error { _, err := a.ReviewTaskWorkspaceRoot(t.Context(), 1, "slot"); return err }},
		{"apply root", func() error { _, err := a.ApplyTaskWorkspaceRoot(t.Context(), 1, "slot"); return err }},
		{"task model", func() error { _, err := a.RetargetTask(1, "new/model"); return err }},
		{"conflict resolution", func() error { return a.ResolveConflict(1) }},
		{"land", func() error { _, err := a.Land("folder"); return err }},
		{"autonomy", func() error { return a.SetAutonomy(AskConfirmation, Policy{Kind: PolicyAsk}) }},
	} {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, want) {
				t.Fatalf("durable write refusal=%v", err)
			}
		})
	}
}
