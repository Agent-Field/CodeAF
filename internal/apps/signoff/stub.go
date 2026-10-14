package signoff

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{}

func New(_ svc.Config, _ *svc.Services) *Service { return &Service{} }

var _ svc.Signoff = (*Service)(nil)

func (*Service) Roles(context.Context, svc.Scope, bridge.SignoffChainRef) (bridge.SignoffRoles, error) {
	return bridge.SignoffRoles{}, bridge.NotBuilt("signoff")
}
func (*Service) SetRoles(context.Context, svc.Scope, bridge.SignoffRoles) (bridge.SignoffRoles, error) {
	return bridge.SignoffRoles{}, bridge.NotBuilt("signoff")
}
func (*Service) State(context.Context, svc.Scope, bridge.SignoffRecordRef) (bridge.SignoffState, error) {
	return bridge.SignoffState{}, bridge.NotBuilt("signoff")
}
func (*Service) Sign(context.Context, svc.Scope, bridge.SignoffSignParams) (bridge.SignoffState, error) {
	return bridge.SignoffState{}, bridge.NotBuilt("signoff")
}
