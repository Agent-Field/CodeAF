package grants

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{}

var _ svc.Grants = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) Accounts(context.Context, svc.Caller) (bridge.AccountsResult, error) {
	return bridge.AccountsResult{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.accounts is not built yet")
}

func (*Service) Allow(context.Context, svc.Scope, bridge.AllowParams) (bridge.Grant, error) {
	return bridge.Grant{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.allow is not built yet")
}

func (*Service) AllowSheet(context.Context, svc.Scope, bridge.AllowSheetParams) (bridge.AllowSheet, error) {
	return bridge.AllowSheet{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.allow.sheet is not built yet")
}

func (*Service) Revoke(context.Context, svc.Caller, bridge.RevokeParams) error {
	return bridge.Errorf(bridge.CodeNotBuilt, "connect.revoke is not built yet")
}

func (*Service) Workspace(context.Context, svc.Caller) (bridge.WorkspaceConnectionsResult, error) {
	return bridge.WorkspaceConnectionsResult{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.workspace.list is not built yet")
}

func (*Service) AddWorkspace(context.Context, svc.Caller, bridge.AddWorkspaceConnectionParams) (bridge.WorkspaceConnection, error) {
	return bridge.WorkspaceConnection{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.workspace.add is not built yet")
}

func (*Service) SetWho(context.Context, svc.Caller, bridge.SetWhoParams) (bridge.WorkspaceConnection, error) {
	return bridge.WorkspaceConnection{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.workspace.who is not built yet")
}

func (*Service) RemovalImpact(context.Context, svc.Caller, string) (bridge.RemovalImpact, error) {
	return bridge.RemovalImpact{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.workspace.impact is not built yet")
}

func (*Service) RemoveWorkspace(context.Context, svc.Caller, string) error {
	return bridge.Errorf(bridge.CodeNotBuilt, "connect.workspace.remove is not built yet")
}

func (*Service) Needs(context.Context, svc.Scope) (bridge.ConnectionNeeds, error) {
	return bridge.ConnectionNeeds{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.needs is not built yet")
}

func (*Service) Resolve(context.Context, svc.Scope, string) (svc.Credential, error) {
	return svc.Credential{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.resolve is not built yet")
}

func (*Service) Waits(context.Context, svc.Caller) (bridge.ConnectWaitsResult, error) {
	return bridge.ConnectWaitsResult{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.waits is not built yet")
}

func (*Service) Fakes(context.Context, svc.Scope, bridge.ConnectFakesParams) (bridge.ConnectFakesResult, error) {
	return bridge.ConnectFakesResult{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.fakes is not built yet")
}
