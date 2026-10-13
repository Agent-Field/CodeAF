package runs

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the stub runs engine. B4 replaces this file.
type Service struct{}

var _ svc.Runs = (*Service)(nil)

func New(svc.Config, *svc.Services) *Service { return &Service{} }

func (*Service) Start(context.Context, svc.Scope, bridge.StartRunParams) (bridge.RunState, error) {
	return bridge.RunState{}, bridge.NotBuilt("runs")
}

func (*Service) Get(context.Context, svc.Scope, bridge.GetRunParams) (bridge.RunState, error) {
	return bridge.RunState{}, bridge.NotBuilt("runs")
}

func (*Service) List(context.Context, svc.Scope, bridge.ListRunsParams) (bridge.ListRunsResult, error) {
	return bridge.ListRunsResult{}, bridge.NotBuilt("runs")
}
