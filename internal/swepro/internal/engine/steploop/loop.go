package steploop

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Loop wires the outer state machine's service seams.
type Loop struct {
	Store     Store
	Client    LLMClient
	Models    ModelResolver
	Executor  ToolExecutor
	Scheduler Scheduler
	ExitGuard ExitGuard
	Tasks     TaskController

	// ProcessorWaitTimeout is zero for the TS 250ms default.
	ProcessorWaitTimeout time.Duration
}

// Run drives prompt.ts:runLoop until its natural exit or a processor stop.
func (l *Loop) Run(ctx context.Context, opts RunOptions) (msgmodel.Assistant, error) {
	if l == nil || l.Store == nil {
		return msgmodel.Assistant{}, errors.New("steploop: Store is required")
	}
	if l.Client == nil {
		return msgmodel.Assistant{}, errors.New("steploop: Client is required")
	}
	models := l.Models
	if models == nil {
		models = StaticModelResolver{}
	}

	step := 0
loop:
	for {
		if err := l.schedulerPump(ctx, opts, step); err != nil {
			return msgmodel.Assistant{}, err
		}

		chronological, err := l.Store.Messages(ctx, opts.SessionID)
		if err != nil {
			return msgmodel.Assistant{}, err
		}
		msgs := msgmodel.FilterCompacted(newestFirst(chronological))
		scan := BackScan(msgs)
		if scan.LastUser == nil {
			return msgmodel.Assistant{}, errors.New("No user message found in stream. This should never happen.")
		}

		if ShouldExit(scan.LastUser, scan.LastAssistant, msgs) {
			nudged, guardErr := l.exitGuard(ctx, opts, msgs)
			if guardErr != nil {
				return msgmodel.Assistant{}, guardErr
			}
			if nudged {
				// prompt.ts:1679 — no step increment.
				continue
			}
			break
		}

		step++
		model, err := models.Resolve(ctx, *scan.LastUser)
		if err != nil {
			return msgmodel.Assistant{}, err
		}
		taskInput := TaskInput{SessionID: opts.SessionID, Messages: msgs, User: *scan.LastUser, Model: model}
		if len(scan.Tasks) > 0 {
			task := scan.Tasks[len(scan.Tasks)-1] // tasks.pop()
			switch value := task.(type) {
			case msgmodel.SubtaskPart:
				if l.Tasks == nil {
					return msgmodel.Assistant{}, errors.New("steploop: TaskController is required for a subtask")
				}
				if err := l.Tasks.HandleSubtask(ctx, taskInput, value); err != nil {
					return msgmodel.Assistant{}, err
				}
				continue
			case msgmodel.CompactionPart:
				if l.Tasks == nil {
					return msgmodel.Assistant{}, errors.New("steploop: TaskController is required for compaction")
				}
				result, err := l.Tasks.ProcessCompaction(ctx, taskInput, value)
				if err != nil {
					return msgmodel.Assistant{}, err
				}
				if result == ResultStop {
					break loop
				}
				continue
			}
		}
		if scan.LastFinished != nil && !boolValue(scan.LastFinished.Summary) && l.Tasks != nil {
			agent := scan.LastUser.Agent
			overflow, err := l.Tasks.IsOverflow(ctx, *scan.LastFinished, model, OverflowOptions{
				Agent: &agent, Messages: msgs,
			})
			if err != nil {
				return msgmodel.Assistant{}, err
			}
			if overflow {
				if err := l.Tasks.CreateCompaction(ctx, opts.SessionID, *scan.LastUser, false); err != nil {
					return msgmodel.Assistant{}, err
				}
				continue
			}
		}
		if opts.InjectReminders != nil {
			msgs, err = opts.InjectReminders(ctx, msgs, *scan.LastUser)
			if err != nil {
				return msgmodel.Assistant{}, err
			}
		}
		if step > 1 && scan.LastFinished != nil {
			WrapLateUserText(msgs, *scan.LastFinished)
		}

		assistant := newAssistant(opts, *scan.LastUser, model)
		if err := l.Store.UpdateMessage(ctx, assistant); err != nil {
			return msgmodel.Assistant{}, err
		}

		prompt, err := msgmodel.ToModelMessages(msgs, model.Message, nil)
		if err != nil {
			return msgmodel.Assistant{}, err
		}
		if float64(step) >= opts.maxSteps() {
			prompt = append(prompt, msgmodel.ModelMessage{Role: "assistant", Content: MaxStepsPrompt})
		}

		params := model.Request
		params.ModelID = model.Message.ID
		params.Prompt = prompt
		if params.MaxOutputTokens == nil {
			maxOutput := calc.MaxOutputTokens(model.Calc)
			params.MaxOutputTokens = &maxOutput
		}
		params.Tools = nil
		for _, tool := range opts.Tools {
			params.Tools = append(params.Tools, tool.Provider)
		}

		processor := NewProcessor(ProcessorOptions{
			Store:       l.Store,
			Assistant:   assistant,
			Model:       model,
			Tools:       opts.Tools,
			Executor:    l.Executor,
			WaitTimeout: l.ProcessorWaitTimeout,
		})
		outcome, processErr := func() (Result, error) {
			if opts.AfterAssistant != nil {
				defer opts.AfterAssistant(ctx, assistant.ID)
			}
			stream, streamErr := l.Client.Stream(ctx, params)
			if streamErr != nil {
				stream = &SliceStream{Failure: streamErr}
			}
			return processor.Process(ctx, stream)
		}()
		if opts.AfterTurn != nil {
			parts, partsErr := assistantParts(ctx, l.Store, opts.SessionID, assistant.ID)
			if partsErr != nil {
				return msgmodel.Assistant{}, partsErr
			}
			if hookErr := opts.AfterTurn(ctx, processor.Message(), parts); hookErr != nil {
				return msgmodel.Assistant{}, hookErr
			}
		}
		if processErr != nil {
			return msgmodel.Assistant{}, processErr
		}
		if outcome == ResultStop {
			break
		}
		if outcome == ResultContinue && l.Tasks != nil {
			current := processor.Message()
			if current.Finish != nil && !boolValue(current.Summary) {
				agent := scan.LastUser.Agent
				overflow, overflowErr := l.Tasks.IsOverflow(ctx, current, model, OverflowOptions{
					Agent: &agent, Messages: msgs,
				})
				if overflowErr != nil {
					return msgmodel.Assistant{}, overflowErr
				}
				if overflow {
					outcome = ResultCompact
				}
			}
		}
		if outcome == ResultCompact && l.Tasks != nil {
			current := processor.Message()
			if err := l.Tasks.CreateCompaction(ctx, opts.SessionID, *scan.LastUser, current.Finish == nil); err != nil {
				return msgmodel.Assistant{}, err
			}
		}
	}

	if l.Tasks != nil {
		// prompt.ts:1862 forks and ignores pruning.
		go func(tasks TaskController) {
			_ = tasks.Prune(ctx, opts.SessionID)
		}(l.Tasks)
	}
	chronological, err := l.Store.Messages(ctx, opts.SessionID)
	if err != nil {
		return msgmodel.Assistant{}, err
	}
	for i := len(chronological) - 1; i >= 0; i-- {
		if assistant, ok := chronological[i].Info.(msgmodel.Assistant); ok {
			return assistant, nil
		}
	}
	return msgmodel.Assistant{}, errors.New("Impossible")
}

func assistantParts(
	ctx context.Context, store Store, sessionID, messageID string,
) (msgmodel.Parts, error) {
	messages, err := store.Messages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	for index := len(messages) - 1; index >= 0; index-- {
		assistant, ok := messages[index].Info.(msgmodel.Assistant)
		if ok && assistant.ID == messageID {
			return messages[index].Parts, nil
		}
	}
	return nil, nil
}

func newAssistant(opts RunOptions, user msgmodel.User, model Model) msgmodel.Assistant {
	return msgmodel.Assistant{
		MessageBase: msgmodel.MessageBase{ID: nextID("msg"), SessionID: opts.SessionID},
		Time:        msgmodel.AssistantTime{Created: currentNow()},
		ParentID:    user.ID,
		ModelID:     model.Message.ID,
		ProviderID:  model.Message.ProviderID,
		Mode:        user.Agent,
		Agent:       user.Agent,
		Path:        msgmodel.AssistantPath{Cwd: opts.Workspace, Root: opts.Worktree},
		Cost:        jscompat.JSNumber(0),
		Tokens: msgmodel.Tokens{
			Input: 0, Output: 0, Reasoning: 0,
			Cache: msgmodel.TokenCache{Read: 0, Write: 0},
		},
		Variant: user.Model.Variant,
	}
}

func (l *Loop) schedulerPump(ctx context.Context, opts RunOptions, step int) error {
	if step <= 0 || opts.ParentID != "" || l.Scheduler == nil {
		return nil
	}
	recent, err := l.Store.Messages(ctx, opts.SessionID)
	if err != nil {
		// prompt.ts:1498-1500 degrades to [].
		return nil
	}
	if len(recent) > 50 {
		recent = recent[len(recent)-50:]
	}
	plan := PlanDBInfoFromMessages(recent)
	agent := latestAssistantAgent(recent, "")
	if plan == nil || (agent != "" && agent != "root-orchestrator" && agent != "orchestrator") {
		return nil
	}
	summary, err := l.Scheduler.Pump(ctx, SchedulerInput{SessionID: opts.SessionID, Plan: *plan})
	if err != nil {
		// run.ts:1313-1345 only crosses the quiet-to-audit boundary after
		// proving that PlanDB has no active work. A failed state check cannot
		// be treated as an ordinary quiet cycle.
		return err
	}
	if summary == "" {
		return nil
	}
	model, err := lastModel(ctx, opts, recent)
	if err != nil || model == nil {
		return nil
	}
	return l.persistSyntheticUser(ctx, opts.SessionID, latestAssistantAgent(recent, "orchestrator"), *model, summary)
}

func (l *Loop) exitGuard(ctx context.Context, opts RunOptions, msgs []msgmodel.WithParts) (bool, error) {
	if l.ExitGuard == nil {
		return false, nil
	}
	plan := PlanDBInfoFromMessages(msgs)
	if plan == nil {
		return false, nil
	}
	open, err := l.ExitGuard.FindOpenCapExhaustFailures(ctx, ExitGuardQuery{
		Workspace: opts.Workspace, DBPath: plan.DBPath, RootTaskID: plan.RootTaskID,
	})
	if err != nil || len(open) == 0 {
		// prompt.ts:1628-1634 degrades discovery failure to [].
		return false, nil
	}
	model, err := lastModel(ctx, opts, msgs)
	if err != nil || model == nil {
		// prompt.ts:1636-1639: failure skips the guard.
		return false, nil
	}
	agent := latestAssistantAgent(msgs, "orchestrator")
	if err := l.persistSyntheticUser(ctx, opts.SessionID, agent, *model, RenderRecoveryReminder(open)); err != nil {
		return false, err
	}
	for _, failure := range open {
		l.ExitGuard.MarkFailureNudged(plan.RootTaskID, failure.TaskID)
	}
	return true, nil
}

func lastModel(ctx context.Context, opts RunOptions, msgs []msgmodel.WithParts) (*msgmodel.UserModel, error) {
	if opts.LastModel != nil {
		return opts.LastModel(ctx, opts.SessionID, msgs)
	}
	return defaultLastModel(msgs), nil
}

func (l *Loop) persistSyntheticUser(ctx context.Context, sessionID, agent string, model msgmodel.UserModel, text string) error {
	user := msgmodel.User{
		MessageBase: msgmodel.MessageBase{ID: nextID("msg"), SessionID: sessionID},
		Time:        msgmodel.TimeCreated{Created: currentNow()},
		Agent:       agent,
		Model:       model,
	}
	if err := l.Store.UpdateMessage(ctx, user); err != nil {
		return err
	}
	return l.Store.UpdatePart(ctx, syntheticTextPart(sessionID, user.ID, text))
}
