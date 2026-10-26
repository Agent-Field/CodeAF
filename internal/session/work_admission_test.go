package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		"adaptive run":     func() error { _, err := agent.RunOrchestrate(ctx, "refused goal", "", 0); return err },
		"task redo":        func() error { _, _, err := agent.RedoStronger(ctx, 1); return err },
		"task steer":       func() error { _, err := agent.SteerTask(1, "refused task correction"); return err },
		"keyed task steer": func() error { _, err := agent.SteerTaskFrom(1, "refused task correction", SteerSource{}); return err },
		"task action":      func() error { return agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "start"}) },
		"quick promotion": func() error {
			_, _, err := agent.AppQuickTaskAction(AppTaskIntent{ID: "1", Action: "promote"})
			return err
		},
		"routing":  func() error { _, err := agent.TaskRoutingAct(ctx, "1", TaskRoutingAction{}); return err },
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

// C1's answer amendment: an existing consent can be approved or declined
// after a move, through either resolver. Approval still meets the tool gate.
func TestC1SessionViewerAcceptsPendingConsentAnswers(t *testing.T) {
	for _, door := range []string{"legacy", "remember", "question"} {
		for _, allow := range []bool{true, false} {
			t.Run(door+map[bool]string{true: "/allow", false: "/decline"}[allow], func(t *testing.T) {
				var viewer atomic.Bool
				refusal := &executor.WorkRefusal{Line: "laptop continued this chat; this window now only shows it"}
				agent, _ := newTestAgent(t, &scriptedCompleter{steps: toolCallTurn("touch")}, func(cfg *Config) {
					cfg.AskConsent, cfg.Interactive = true, true
					cfg.ApprovalPolicy = promptAll()
					cfg.Seat = executor.Gated(executor.Host, func() error {
						if viewer.Load() {
							return refusal
						}
						return nil
					})
				})
				runs := make(chan string, 1)
				agent.tools = append(agent.tools, countingTool("touch", runs, nil))
				answered := false
				events := drainAnswering(t, mustSubmit(t, agent, "touch the file"), func(request Event) {
					answered = true
					viewer.Store(true)
					switch door {
					case "legacy":
						agent.ResolveConsent(request.ID, allow)
					case "remember":
						agent.ResolveConsentRemember(request.ID, allow, ConsentToolSession)
					case "question":
						key := "1"
						if !allow {
							key = "3"
						}
						if err := agent.ResolveQuestion(Answer{Kind: QuestionConsent, ID: request.ID, Key: key}); err != nil {
							t.Fatal(err)
						}
					}
				})
				if !answered || len(runs) != 0 {
					t.Fatalf("pending answer = %v, tool executions = %d", answered, len(runs))
				}
				if allow {
					failed, ok := firstOfKind(events, EventToolFailed)
					if !ok || !strings.Contains(failed.Output, refusal.Line) {
						t.Fatalf("approved tool escaped ownership gate: %+v", events)
					}
				}
				if _, err := agent.Submit(context.Background(), "new work"); !errors.Is(err, refusal) {
					t.Fatalf("answer admitted a later message: %v", err)
				}
			})
		}
	}
}

// C1's answer amendment applies to the legacy task, harness, connect and
// intake resolvers as well as ResolveQuestion. A decline must also release
// the pending waiter, rather than silently leaving the card stranded.
func TestC1SessionViewerLegacyAnswersReleaseTheirWaiters(t *testing.T) {
	refusal := &executor.WorkRefusal{Line: "laptop continued this chat"}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.Seat = executor.Gated(executor.Host, func() error { return refusal })
	})
	for _, allow := range []bool{true, false} {
		task := make(chan TaskAnswer, 1)
		harness := make(chan harnessAnswer, 1)
		connect := make(chan connectAnswer, 1)
		key := make(chan connectAnswer, 1)
		intake := make(chan subharnessConsent, 1)
		agent.mu.Lock()
		agent.taskAnswers = map[uint64]*taskQuestion{1: {answer: task}}
		agent.harnessAsks = map[uint64]harnessAsk{2: {answers: harness}}
		agent.connectAsks = map[string]connectAsk{"browser": {answers: connect}, "key": {answers: key, needsKey: true}}
		agent.subharnessOffers = map[uint64]*subharnessOffer{3: {answers: intake}}
		agent.mu.Unlock()
		agent.ResolveTask(1, TaskAnswer{Approved: allow})
		agent.ResolveHarness(2, allow, "chosen")
		agent.ResolveConnect("browser", allow)
		words := ""
		if allow {
			words = "fixture-key"
		}
		agent.ResolveConnectKey("key", words)
		agent.ResolveSubharness(3, allow, json.RawMessage(`{"test":"fixture"}`))
		if len(task) != 1 || len(harness) != 1 || len(connect) != 1 || len(key) != 1 || len(intake) != 1 {
			t.Fatal("an answer left its pending waiter blocked")
		}
		if (<-task).Approved != allow || (<-harness).run != allow || (<-connect).approved != allow || (<-key).approved != allow || (<-intake).run != allow {
			t.Fatal("the resolver changed the answer")
		}
	}
	proposal := make(chan TaskAnswer, 1)
	agent.mu.Lock()
	agent.taskAnswers[4] = &taskQuestion{answer: proposal}
	agent.mu.Unlock()
	if err := agent.AppTaskAction(AppTaskIntent{ID: "4", Action: "start"}); err != nil {
		t.Fatal(err)
	}
	if len(proposal) != 1 || !(<-proposal).Approved {
		t.Fatal("starting an offered proposal did not answer its pending question")
	}
}

// C1's answer amendment: a replacement or clarification of a real pending
// permission reaches the model on a viewer. It does not authorize a tool.
func TestC1SessionViewerAcceptsReplacementAndClarification(t *testing.T) {
	for _, clarify := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement", true: "clarification"}[clarify], func(t *testing.T) {
			var viewer atomic.Bool
			refusal := &executor.WorkRefusal{Line: "laptop continued this chat"}
			agent, dir := newTestAgent(t, &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("pending", "write", `{"path":"held.txt","content":"held"}`), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Explanation."), nil },
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Finished."), nil },
			}}, func(cfg *Config) {
				cfg.AskConsent, cfg.Interactive = true, true
				cfg.ApprovalPolicy = promptAll()
				cfg.Seat = executor.Gated(executor.Host, func() error {
					if viewer.Load() {
						return refusal
					}
					return nil
				})
			})
			watch, stop := agent.WatchQuestions()
			defer stop()
			main := mustSubmit(t, agent, "write held.txt")
			q := discussionWait(t, watch, func(ev Event) bool { return ev.Kind == EventQuestion }).Question
			viewer.Store(true)
			if clarify {
				if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Clarify: true, AskedBack: []Exchange{{Asked: "why this file?"}}}); err != nil {
					t.Fatal(err)
				}
				discussionWait(t, watch, func(ev Event) bool {
					return ev.Discussion != nil && ev.Discussion.Event != nil && ev.Discussion.Event.Kind == EventTurnDone
				})
				if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: "1"}); err != nil {
					t.Fatal(err)
				}
				collect(t, main)
			} else {
				stream, err := agent.ReplaceQuestion(context.Background(), Answer{Kind: q.Kind, ID: q.ID, Change: "explain instead"})
				if err != nil {
					t.Fatal(err)
				}
				collect(t, stream)
			}
			if _, err := os.Stat(filepath.Join(dir, "held.txt")); !os.IsNotExist(err) {
				t.Fatalf("the pending tool ran: %v", err)
			}
		})
	}
}

// C1's answer amendment also covers a question whose tool already carried
// on. Delivering that answer may continue the conversation, while unrelated
// wakes and new messages remain refused.
func TestC1SessionViewerAcceptsAnAnswerAfterTheAskingTurnEnds(t *testing.T) {
	var viewer atomic.Bool
	refusal := &executor.WorkRefusal{Line: "laptop continued this chat"}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Ready."), nil },
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			found := false
			for _, message := range messages {
				found = found || strings.Contains(messageContentText(message), askAnsweredWord)
			}
			if !found {
				t.Error("the continued turn did not receive the existing question's answer")
			}
			return textResponse("Answer received."), nil
		},
	}}, func(cfg *Config) {
		cfg.Interactive = true
		cfg.Seat = executor.Gated(executor.Host, func() error {
			if viewer.Load() {
				return refusal
			}
			return nil
		})
	})
	collect(t, mustSubmit(t, agent, "start here"))
	_, stopQuestions := agent.WatchQuestions()
	defer stopQuestions()
	if _, failed, err := agent.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil || failed {
		t.Fatalf("ask = %v, %v", failed, err)
	}
	q := waitForOneQuestion(t, agent)
	wakes, stopWakes := agent.WatchWakes()
	defer stopWakes()
	viewer.Store(true)
	if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case stream := <-wakes:
		collect(t, stream)
	case <-time.After(10 * time.Second):
		t.Fatal("the accepted answer never reached the model")
	}
	if agent.enqueueNote(wakeNote("unrelated new work")) {
		t.Fatal("an unrelated wake bypassed ownership after an answer")
	}
}

// C1's answer amendment: asking back about a standing question reaches the
// conversation's own model on a viewer without settling the decision or
// allowing its tools to act here.
func TestC1SessionViewerAcceptsAnAskBackAfterTheAskingTurnEnds(t *testing.T) {
	var viewer atomic.Bool
	refusal := &executor.WorkRefusal{Line: "laptop continued this chat"}
	const words = "what does the second option mean?"
	agent, dir := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Ready."), nil },
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			found := false
			for _, message := range messages {
				text := messageContentText(message)
				found = found || (strings.Contains(text, askBackNoteLead) && strings.Contains(text, words))
			}
			if !found {
				t.Error("the conversation's model did not receive the ask-back about its question")
			}
			return toolResponse("held", "write", `{"path":"held.txt","content":"held"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Here is the explanation."), nil
		},
	}}, func(cfg *Config) {
		cfg.Interactive = true
		cfg.Seat = executor.Gated(executor.Host, func() error {
			if viewer.Load() {
				return refusal
			}
			return nil
		})
	})
	collect(t, mustSubmit(t, agent, "start here"))
	_, stopQuestions := agent.WatchQuestions()
	defer stopQuestions()
	if _, failed, err := agent.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil || failed {
		t.Fatalf("ask = %v, %v", failed, err)
	}
	q := waitForOneQuestion(t, agent)
	wakes, stopWakes := agent.WatchWakes()
	defer stopWakes()
	viewer.Store(true)
	if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, AskedBack: []Exchange{{Asked: words}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case stream := <-wakes:
		failed, ok := firstOfKind(collect(t, stream), EventToolFailed)
		if !ok || !strings.Contains(failed.Output, refusal.Line) {
			t.Fatalf("the ask-back's tool escaped ownership: %+v", failed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the accepted ask-back never reached the conversation's model")
	}
	if open := agent.OpenQuestions(); len(open) != 1 || open[0].ID != q.ID {
		t.Fatalf("the ask-back settled or replaced the standing question: %+v", open)
	}
	if _, err := os.Stat(filepath.Join(dir, "held.txt")); !os.IsNotExist(err) {
		t.Fatalf("the viewer's tool wrote a file: %v", err)
	}
	if agent.enqueueNote(wakeNote("unrelated new work")) {
		t.Fatal("an unrelated wake bypassed ownership after an ask-back")
	}
}
