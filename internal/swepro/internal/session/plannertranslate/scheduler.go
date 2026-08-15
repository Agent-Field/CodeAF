package plannertranslate

import (
	"context"
	"path/filepath"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
)

// SchedulerService implements scheduler.PlannerTranslator.
type SchedulerService struct {
	AgentJSON agentjson.Dependencies
	PlanDB    PlanDBRunner
}

var _ scheduler.PlannerTranslator = (*SchedulerService)(nil)

// RunFrontierTick dispatches and applies one frontier tranche.
func (service *SchedulerService) RunFrontierTick(
	ctx context.Context,
	input scheduler.FrontierTickInput,
) (scheduler.FrontierTickResult, error) {
	outputPath := filepath.Join(
		input.Workspace, ".codeaf", "plan", "frontier-tick-"+itoa(input.Tick)+".json",
	)
	frontierContext := input.FrontierContext
	tick := float64(input.Tick)
	dispatched, err := DispatchPlannerTranslate(ctx, DispatchPlannerTranslateInput{
		Workspace:       input.Workspace,
		ParentSessionID: input.ParentSessionID,
		PromptOps:       input.PromptOps,
		OutputPath:      &outputPath,
		FrontierContext: &frontierContext,
		FrontierTick:    &tick,
	}, service.AgentJSON)
	if err != nil {
		return scheduler.FrontierTickResult{}, err
	}
	runner := service.PlanDB
	if runner == nil {
		runner = nativePlanDBRunner{}
	}
	applied := ApplyTranslatedDAGWithRunner(ApplyTranslatedDAGInput{
		DAG: dispatched.Data, Workspace: input.Workspace, DBPath: input.DBPath,
		ProjectID: input.ProjectID, RootTaskID: input.RootTaskID,
	}, runner)
	status := "partial-apply"
	if applied.OK {
		status = "applied"
	}
	return scheduler.FrontierTickResult{
		Status: status, TaskCount: applied.TaskCount,
		ResidualUpdated: residualContent(dispatched.Data) != "",
	}, nil
}
