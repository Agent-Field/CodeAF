//go:build !windows

// Controller adapts the compaction service to the step-loop task seam.
package compaction

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

type Controller struct {
	Compaction *Service
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
) (bool, error) {
	if c.Compaction == nil {
		return false, errors.New("compaction: nil service")
	}
	return c.Compaction.IsOverflow(
		ctx, assistant.Tokens, Model{Message: model.Message, Overflow: model.Calc},
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
