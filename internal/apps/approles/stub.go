package approles

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{}

func New(_ svc.Config, _ *svc.Services) *Service { return &Service{} }

var _ svc.AppRoles = (*Service)(nil)

func (*Service) Role(context.Context, string, string) (svc.Role, error) { return svc.RoleEditor, nil }
func (*Service) List(context.Context, svc.Scope) (bridge.AppMembers, error) {
	return bridge.AppMembers{Everyone: string(svc.RoleUser), People: []bridge.AppMember{{Member: svc.Owner, Role: string(svc.RoleEditor), From: "set", Maker: true}}}, nil
}
func (*Service) Seed(context.Context, string, string) error { return nil }
func (*Service) Set(context.Context, svc.Scope, bridge.AppRoleSetParams) (bridge.AppMembers, error) {
	return bridge.AppMembers{}, bridge.NotBuilt("approles.set")
}
func (*Service) AskEditor(context.Context, svc.Scope, bridge.AskEditorParams) (bridge.Empty, error) {
	return bridge.Empty{}, bridge.NotBuilt("approles.ask")
}
