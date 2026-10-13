package crewlink

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{}

var _ svc.CrewLinks = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) Add(context.Context, svc.Scope, bridge.AddLinkParams) (bridge.CrewLink, error) {
	return bridge.CrewLink{}, bridge.NotBuilt("crewlink")
}
func (*Service) Remove(context.Context, svc.Scope, bridge.RemoveLinkParams) error {
	return bridge.NotBuilt("crewlink")
}
func (*Service) ForRecord(context.Context, svc.Scope, bridge.LinksForRecordParams) (bridge.ListLinksResult, error) {
	return bridge.ListLinksResult{}, bridge.NotBuilt("crewlink")
}
func (*Service) ForCrew(context.Context, svc.Caller, bridge.LinksForCrewParams) (bridge.ListLinksResult, error) {
	return bridge.ListLinksResult{}, bridge.NotBuilt("crewlink")
}
