package main

import (
	"context"
	"errors"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"math"
	"sync/atomic"
	"testing"
	"time"

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
		meter.SpendMemory(.02)
		c.Fail(factoryrun.SpendStop{Job: true, Memory: true, Said: meter.Refusal()})
	}}
	stage := factory.Stage{Name: "work", Kind: factory.StageChat, On: true, Until: "done"}
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

func TestMemoryCapKeepsFinishedPhase(t *testing.T) {
	for _, last := range []bool{false, true} {
		t.Run(map[bool]string{false: "next admission", true: "last step"}[last], func(t *testing.T) {
			floor, _ := chatSpendFloor(t)
			item, err := floor.Create(factory.Item{Repo: "acme/api", Cap: 1, ChatSpent: .99, State: factory.StateNew,
				Stages: []factory.Stage{{Name: "work", Kind: factory.StageChat, On: true, Until: "done"}, {Name: "review", Kind: factory.StageChat, On: true, Until: "done"}}})
			if err != nil {
				t.Fatal(err)
			}
			if last {
				if err := floor.Annotate(item.ID, func(it *factory.Item) error { it.Stages = it.Stages[:1]; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			var rounds atomic.Int64
			runner := factoryrun.New(factoryrun.Options{Store: floor, Exec: map[factory.StageKind]factoryrun.Executor{
				factory.StageChat: factoryrun.ExecutorFunc(func(ctx context.Context, job factoryrun.Job) (factory.StageResult, error) {
					rounds.Add(1)
					meter := &stageBudget{floor: floor, item: job.Item}
					meter.SpendMemory(.02)
					return factory.StageResult{Done: true, Output: "completed work", Spent: .02, SpendRecorded: true}, nil
				}),
			}})
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
				if last && got.State == factory.StateLanded {
					if got.QKind != "" || got.Stream.Phases[0].State != factory.PhaseDone || math.Abs(factory.JobSpent(got)-1.01) > .000001 {
						t.Fatalf("last step lost completion or true spend: %+v", got)
					}
					return
				}
				if got.QKind == "cap" {
					if last {
						t.Fatal("last step asked for unused budget")
					}
					if got.Stream == nil || got.Stream.Phases[0].State != factory.PhaseDone || got.Stream.Phases[0].Result == nil || !got.Stream.Phases[0].Result.Done {
						t.Fatalf("memory cap parked completed work instead of next-stage admission: %+v", got.Stream)
					}
					if got.Stream.Phases[1].State != factory.PhaseWaiting || rounds.Load() != 1 {
						t.Fatalf("cap retried successful work: cur=%d rounds=%d", got.Stream.Cur, rounds.Load())
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("next stage did not observe memory charge: %+v", got)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

// A daily refusal is not a memory charge: memory crossing the job cap at the
// same time must not let an early report hide that the day's limit stopped it.
func TestMemoryCapDoesNotHideADailyRefusal(t *testing.T) {
	floor, _ := chatSpendFloor(t)
	item, err := floor.Create(factory.Item{Repo: "acme/api", Cap: 1, ChatSpent: .99})
	if err != nil {
		t.Fatal(err)
	}
	meter := &stageBudget{floor: floor, item: item}
	meter.SpendMemory(.02)
	done := make(chan struct{})
	close(done)
	daily := factoryrun.SpendStop{Daily: true, Said: "daily limit reached"}
	conv := &stageConversation{budget: meter, done: done, failure: daily}
	var stop factoryrun.SpendStop
	if err := conv.Wait(context.Background()); !errors.As(err, &stop) {
		t.Fatalf("Wait = %v, want a spend stop", err)
	}
	if stop.Memory {
		t.Fatalf("a daily refusal reads as memory: %+v", stop)
	}
}

// With no other refusal, memory crossing the cap still reads as memory.
func TestMemoryCapReadsAsMemoryWithoutADailyRefusal(t *testing.T) {
	floor, _ := chatSpendFloor(t)
	item, err := floor.Create(factory.Item{Repo: "acme/api", Cap: 1, ChatSpent: .99})
	if err != nil {
		t.Fatal(err)
	}
	meter := &stageBudget{floor: floor, item: item}
	meter.SpendMemory(.02)
	done := make(chan struct{})
	close(done)
	conv := &stageConversation{budget: meter, done: done}
	var stop factoryrun.SpendStop
	if err := conv.Wait(context.Background()); !errors.As(err, &stop) || !stop.Memory {
		t.Fatalf("Wait = %v (%+v), want a memory stop", err, stop)
	}
}
