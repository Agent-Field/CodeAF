package crewagent

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{}

var _ svc.CrewAgent = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) Badge(context.Context, svc.Scope) (bridge.Badge, error) {
	return bridge.Badge{}, bridge.NotBuilt("crewagent")
}
func (*Service) Candidates(context.Context, svc.Caller) (bridge.CrewAgentCandidatesResult, error) {
	return bridge.CrewAgentCandidatesResult{}, bridge.NotBuilt("crewagent")
}
func (*Service) Ask(context.Context, svc.CrewAsk) (svc.CrewAnswer, error) {
	return svc.CrewAnswer{}, bridge.NotBuilt("crewagent")
}
