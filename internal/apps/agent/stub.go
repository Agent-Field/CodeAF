package agent

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the stub app agent. B7 replaces this file.
type Service struct{}

var _ svc.Agent = (*Service)(nil)

func New(svc.Config, *svc.Services) *Service { return &Service{} }

func (*Service) Send(context.Context, svc.Scope, bridge.AgentSendParams) (bridge.AgentSendResult, error) {
	return bridge.AgentSendResult{}, bridge.NotBuilt("agent")
}
