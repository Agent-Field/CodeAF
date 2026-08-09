package replangate

import (
	"context"

	"github.com/Agent-Field/swe-pro-go/internal/session/scheduler"
)

// SchedulerService implements scheduler.Replanner.
type SchedulerService struct {
	Dependencies Dependencies
}

var _ scheduler.Replanner = (*SchedulerService)(nil)

func (service *SchedulerService) ShouldReplan(
	_ context.Context, workspace string,
) bool {
	return ShouldReplan(workspace, service.Dependencies.LookupEnv)
}

func (service *SchedulerService) Replan(
	ctx context.Context, input scheduler.ReplanInput,
) (scheduler.ReplanResult, error) {
	sessionKey := input.SessionKey
	dispatched, err := DispatchReplanner(ctx, DispatchReplannerInput{
		Workspace: input.Workspace, DBPath: input.DBPath,
		ParentSessionID: input.ParentSessionID, PromptOps: input.PromptOps,
		UserGoal: input.UserGoal, EscalatedTaskIDs: []string{input.TaskID},
		SessionKey: &sessionKey,
	}, service.Dependencies)
	if err != nil {
		return scheduler.ReplanResult{}, err
	}
	applied := ApplyReplanDecision(
		dispatched.Data, input.Workspace, input.DBPath, service.Dependencies,
	)
	return scheduler.ReplanResult{Abort: applied.Abort, Summary: applied.Summary}, nil
}
