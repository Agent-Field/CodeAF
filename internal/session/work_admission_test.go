package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// C1: every user-work producer refuses at the session boundary, including
// doors which do not submit a message through the app.
func TestC1SessionViewerRefusesUserWorkBeforeEffects(t *testing.T) {
	refusal := &executor.WorkRefusal{Line: "laptop continued this chat; this window now only shows it", HolderID: "device", HolderName: "laptop"}
	var calls atomic.Int32
	script := &routedCompleter{parent: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		calls.Add(1)
		return textResponse("unexpected model work"), nil
	}}}
	journal := filepath.Join(t.TempDir(), "viewer.jsonl")
	agent, _ := newTestAgent(t, script, func(cfg *Config) {
		cfg.SessionFile = journal
		cfg.Seat = executor.Gated(executor.Host, func() error { return refusal })
	})
	before, _ := os.ReadFile(journal)
	entriesBefore := len(agent.Transcript())
	ctx := context.Background()
	for name, work := range map[string]func() error{
		"submit": func() error { _, err := agent.Submit(ctx, "refused draft"); return err },
		"marked submit": func() error {
			_, err := agent.SubmitMessage(ctx, Submission{Text: "refused draft", ClientID: "viewer"})
			return err
		},
		"composed submit": func() error {
			_, _, err := agent.SubmitComposed(ctx, Submission{Text: "refused composed draft"})
			return err
		},
		"image submit": func() error {
			_, err := agent.SubmitImage(ctx, "refused image", []Image{{Path: "absent.png"}})
			return err
		},
		"steer":            func() error { _, err := agent.Steer("refused correction"); return err },
		"follow-up":        func() error { _, err := agent.FollowUp("refused follow-up"); return err },
		"task":             func() error { _, _, _, err := agent.StartTask(ctx, "refused task", true); return err },
		"task effort":      func() error { _, _, _, err := agent.StartTaskEffort(ctx, "refused task", true, "best"); return err },
		"program":          func() error { _, _, _, err := agent.StartDelegate(ctx, "missing", "refused program"); return err },
		"harness":          func() error { _, err := agent.RunHarnessRequest(ctx, "missing", "refused harness", ""); return err },
		"move":             agent.MoveTurnToTask,
		"task retry":       func() error { return agent.RetryTask(1) },
		"task continue":    func() error { return agent.ContinueTask(1, "refused continuation") },
		"run continue":     func() error { _, err := agent.ContinueRun(ctx, 1); return err },
		"parked retry":     func() error { return agent.RetryParkedTask(1) },
		"resolve conflict": func() error { return agent.ResolveConflict(1) },
		"hand decision":    func() error { return agent.HandUnverifiedToModel(1) },
		"adaptive run":     func() error { _, err := agent.RunOrchestrate(ctx, "refused goal", "", 0); return err },
		"adaptive answer":  func() error { _, err := agent.ResolveOrchestrate("1", "continue"); return err },
		"task redo":        func() error { _, _, err := agent.RedoStronger(ctx, 1); return err },
		"task steer":       func() error { _, err := agent.SteerTask(1, "refused task correction"); return err },
		"keyed task steer": func() error { _, err := agent.SteerTaskFrom(1, "refused task correction", SteerSource{}); return err },
		"task action":      func() error { return agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "start"}) },
		"quick promotion": func() error {
			_, _, err := agent.AppQuickTaskAction(AppTaskIntent{ID: "1", Action: "promote"})
			return err
		},
		"routing": func() error { _, err := agent.TaskRoutingAct(ctx, "1", TaskRoutingAction{}); return err },
		"answer":  func() error { return agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: 1, Change: "refused answer"}) },
		"replace question": func() error {
			_, err := agent.ReplaceQuestion(ctx, Answer{Kind: QuestionAsk, ID: 1, Change: "refused revision"})
			return err
		},
		"side ask": func() error { _, err := agent.SideAsk(ctx, "refused side ask"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := work(); !errors.Is(err, refusal) {
				t.Fatalf("ownership refusal = %v", err)
			}
		})
	}
	if accepted, line := agent.teamWakeWith("refused crew message"); accepted || line != refusal.Line {
		t.Fatalf("crew wake = %v, %q", accepted, line)
	}
	if agent.ResumeAfterQuit() || agent.ResumeAfterClose() {
		t.Fatal("viewer accepted a recovery wake")
	}
	if calls.Load() != 0 || len(agent.Transcript()) != entriesBefore {
		t.Fatal("viewer called the model or recorded a user entry")
	}
	after, _ := os.ReadFile(journal)
	if string(after) != string(before) {
		t.Fatal("viewer changed its journal")
	}
}

// C1: messages accepted before a move are checked again when the current
// turn ends. Every queued stream gets the refusal, with no user journal entry.
func TestC1SessionQueuedFollowUpsRecheckOwnership(t *testing.T) {
	refusal := &executor.WorkRefusal{Line: "laptop continued this chat; this window now only shows it"}
	var viewer atomic.Bool
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	script := &routedCompleter{parent: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			return textResponse("done"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}}
	agent, _ := newTestAgent(t, script, func(cfg *Config) {
		cfg.SessionFile = filepath.Join(t.TempDir(), "follow-ups.jsonl")
		cfg.Seat = executor.Gated(executor.Host, func() error {
			if viewer.Load() {
				return refusal
			}
			return nil
		})
	})
	opening, err := agent.Submit(context.Background(), "first message")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the opening turn did not reach the model")
	}
	var queued []<-chan Event
	for _, words := range []string{"refused queued one", "refused queued two"} {
		stream, err := agent.FollowUp(words)
		if err != nil {
			t.Fatal(err)
		}
		queued = append(queued, stream)
	}
	viewer.Store(true)
	close(release)
	collect(t, opening)
	for _, stream := range queued {
		events := collect(t, stream)
		if len(events) != 1 || events[0].Kind != EventError || !errors.Is(events[0].Err, refusal) {
			t.Fatalf("queued refusal = %+v", events)
		}
	}
	if calls.Load() != 1 || agent.UserMessageCount() != 1 {
		t.Fatalf("viewer started work: model calls %d, user messages %d", calls.Load(), agent.UserMessageCount())
	}
}
