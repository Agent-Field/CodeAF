package main

import (
	"context"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"math"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestMemoryRecognizesOldJobOwnership(t *testing.T) {
	places, profile, conversation := bringBackFixture(t)
	floor, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := conversation("old-stage")
	conversation("ordinary")
	if err := markFactoryChat(floor, factory.Item{ID: 41, Repo: "acme/api"}, old); err != nil {
		t.Fatal(err)
	}
	journals, err := session.KnowledgeHistory(places, profile).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, journal := range journals {
		if journal.ID == old {
			if !journal.Job {
				t.Fatal("old job transcript with factory-owner.json was admitted as ordinary chat")
			}
			return
		}
	}
	t.Fatal("old job transcript missing from inventory")
}

func TestMemoryChargeCannotUndoSuccessfulStage(t *testing.T) {
	floor, _ := chatSpendFloor(t)
	item, err := floor.Create(factory.Item{Repo: "acme/api", Cap: 1, ChatSpent: .99})
	if err != nil {
		t.Fatal(err)
	}
	meter := &stageBudget{floor: floor, item: item}
	maker := &factoryrun.FakeMaker{Script: func(ctx context.Context, c *factoryrun.FakeConversation) {
		if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "completed work"}); err != nil {
			t.Error(err)
		}
		// The successful work is already reported. Its deferred memory receipt
		// arrives before Wait returns, taking this job from $0.99 to $1.01.
		meter.Spend(.02)
		c.Fail(factoryrun.SpendStop{Job: true, Said: meter.Refusal()})
	}}
	stage := factory.Stage{Name: "write", Kind: factory.StageChat, On: true, Until: "done"}
	result, err := factoryrun.NewChatExecutor(maker).Run(context.Background(), factoryrun.Job{Item: item, Stage: stage, Round: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Done || result.JobBudgetStopped || result.Output != "completed work" {
		t.Fatalf("memory charge undid successful stage: %+v", result)
	}
	charged, err := floor.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := factory.JobSpent(charged); math.Abs(got-1.01) > .000001 {
		t.Fatalf("memory charge missing: %.4f", got)
	}
	next := &stageBudget{floor: floor, item: charged}
	if next.Refusal() == "" {
		t.Fatal("next stage ignored memory spending")
	}
}
