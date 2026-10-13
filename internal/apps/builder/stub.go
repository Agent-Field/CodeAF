package builder

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the stub Builder. B5 replaces this file.
type Service struct{}

var _ svc.Builder = (*Service)(nil)

func New(svc.Config, *svc.Services) *Service { return &Service{} }

func (*Service) Create(context.Context, svc.Caller, bridge.CreateAppParams) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("builder")
}

func (*Service) EditPlan(context.Context, svc.Scope, bridge.PlanEditParams) (bridge.PlanEditResult, error) {
	return bridge.PlanEditResult{}, bridge.NotBuilt("builder")
}

func (*Service) Write(context.Context, svc.Scope) error  { return bridge.NotBuilt("builder") }
func (*Service) Cancel(context.Context, svc.Scope) error { return bridge.NotBuilt("builder") }
