package main

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestStageOwnBudgetRejectsEarlyReport(t *testing.T) {
	maker := &factoryrun.FakeMaker{Script: func(ctx context.Context, c *factoryrun.FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "early report"})
		c.Fail(factoryrun.SpendStop{Job: true, Said: "job budget reached"})
	}}
	result, err := factoryrun.NewChatExecutor(maker).Run(context.Background(), factoryrun.Job{Stage: factory.Stage{Name: "write", Kind: factory.StageChat, On: true, Until: "done"}, Round: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Done || !result.JobBudgetStopped {
		t.Fatalf("own budget accepted early completion: %+v", result)
	}
}

func TestStageOwnSpendAsksAtCompletion(t *testing.T) {
	floor, _ := chatSpendFloor(t)
	item, err := floor.Create(factory.Item{Repo: "acme/api", Cap: 1, State: factory.StateNew, Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true, Until: "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	runner := factoryrun.New(factoryrun.Options{Store: floor, Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: factoryrun.ExecutorFunc(func(context.Context, factoryrun.Job) (factory.StageResult, error) {
		return factory.StageResult{Done: true, Output: "completed work", Spent: 1.5}, nil
	})}})
	t.Cleanup(func() { _ = runner.Stop(item.ID) })
	if err := runner.Launch(item.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := floor.Get(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.QKind == "cap" {
			return
		}
		if got.State == factory.StateLanded || time.Now().After(deadline) {
			t.Fatalf("own spend did not ask at cap: state=%s question=%q spend=%v", got.State, got.QKind, factory.JobSpent(got))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
