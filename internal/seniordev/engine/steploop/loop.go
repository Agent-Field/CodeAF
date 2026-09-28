//go:build !windows

package steploop

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// Loop wires the outer state machine's service seams.
type Loop struct {
	Store    Store
	Client   LLMClient
	Models   ModelResolver
	Executor ToolExecutor
	Tasks    TaskController

	// ProcessorWaitTimeout bounds the tool drain after an aborted stream.
	// Zero means 250ms.
	ProcessorWaitTimeout time.Duration
}

// Run drives the step loop until its natural exit or a processor stop.
func (l *Loop) Run(ctx context.Context, opts RunOptions) (msgmodel.Assistant, error) {
	if l == nil || l.Store == nil {
		return msgmodel.Assistant{}, errors.New("steploop: Store is required")
	}
	if l.Client == nil {
		return msgmodel.Assistant{}, errors.New("steploop: Client is required")
	}
	if l.Models == nil {
		return msgmodel.Assistant{}, errors.New("steploop: Models is required")
	}
	models := l.Models

	step := 0
loop:
	for {
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
		if scan.LastFinished != nil && !boolValue(scan.LastFinished.Summary) &&
			!CompactedAfter(msgs, *scan.LastFinished) && l.Tasks != nil {
			overflow, err := l.Tasks.IsOverflow(ctx, *scan.LastFinished, model)
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
				overflow, overflowErr := l.Tasks.IsOverflow(ctx, current, model)
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
		// Pruning runs in the background; its result is not awaited.
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
		Cost:        float64(0),
		Tokens: msgmodel.Tokens{
			Input: 0, Output: 0, Reasoning: 0,
			Cache: msgmodel.TokenCache{Read: 0, Write: 0},
		},
		Variant: user.Model.Variant,
	}
}
