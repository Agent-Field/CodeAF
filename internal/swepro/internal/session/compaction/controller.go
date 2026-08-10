// Controller adapts the compaction service to the existing step-loop seam.
// The subtask branch is outside compaction.ts and remains one narrow injected
// dependency rather than being ported into this package.
//
// Source parity:
//   - src/session/compaction.ts:364-384
//   - src/session/compaction.ts:869-888
package compaction

import (
	"context"
	"errors"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
)

type SubtaskHandler interface {
	HandleSubtask(
		ctx context.Context,
		input steploop.TaskInput,
		task msgmodel.SubtaskPart,
	) error
}

type SubtaskHandlerFunc func(
	ctx context.Context,
	input steploop.TaskInput,
	task msgmodel.SubtaskPart,
) error

func (f SubtaskHandlerFunc) HandleSubtask(
	ctx context.Context,
	input steploop.TaskInput,
	task msgmodel.SubtaskPart,
) error {
	return f(ctx, input, task)
}

type Controller struct {
	Compaction *Service
	Subtasks   SubtaskHandler
}

func (c Controller) HandleSubtask(
	ctx context.Context,
	input steploop.TaskInput,
	task msgmodel.SubtaskPart,
) error {
	if c.Subtasks == nil {
		return errors.New("compaction: subtask handler is required")
	}
	return c.Subtasks.HandleSubtask(ctx, input, task)
}

func (c Controller) ProcessCompaction(
	ctx context.Context,
	input steploop.TaskInput,
	task msgmodel.CompactionPart,
) (steploop.Result, error) {
	if c.Compaction == nil {
		return steploop.ResultStop, errors.New("compaction: nil service")
	}
	return c.Compaction.Process(ctx, ProcessInput{
		ParentID: task.MessageID, Messages: input.Messages,
		SessionID: input.SessionID, Auto: task.Auto, Overflow: task.Overflow,
	})
}

func (c Controller) IsOverflow(
	ctx context.Context,
	assistant msgmodel.Assistant,
	model steploop.Model,
	options steploop.OverflowOptions,
) (bool, error) {
	if c.Compaction == nil {
		return false, errors.New("compaction: nil service")
	}
	compactionModel := Model{Message: model.Message, Overflow: model.Calc}
	drift := options.Drift
	if drift == nil && options.Messages != nil {
		scan, err := c.Compaction.ShouldScanDrift(ctx, assistant.Tokens, compactionModel)
		if err != nil {
			return false, err
		}
		if scan {
			value := WorkingSetDrift(options.Messages)
			drift = &value
		}
	}
	return c.Compaction.IsOverflow(
		ctx, assistant.Tokens, compactionModel,
		OverflowOptions{Agent: options.Agent, Drift: drift},
	)
}

func (c Controller) CreateCompaction(
	ctx context.Context,
	sessionID string,
	user msgmodel.User,
	overflowed bool,
) error {
	if c.Compaction == nil {
		return errors.New("compaction: nil service")
	}
	return c.Compaction.Create(ctx, CreateInput{
		SessionID: sessionID, Agent: user.Agent,
		Model: ModelRef{
			ProviderID: user.Model.ProviderID, ModelID: user.Model.ModelID,
		},
		Auto: true, Overflow: &overflowed,
	})
}

func (c Controller) Prune(ctx context.Context, sessionID string) error {
	if c.Compaction == nil {
		return errors.New("compaction: nil service")
	}
	return c.Compaction.Prune(ctx, sessionID)
}

var _ steploop.TaskController = Controller{}
