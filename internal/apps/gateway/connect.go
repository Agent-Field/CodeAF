package gateway

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() {
	Register(bridge.MethodConnectCredentialExport, Method{Allow: caps.ConnectAllow(bridge.MethodConnectCredentialExport), Global: true, Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.CredentialExportParams) (bridge.CredentialExportResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.CredentialExportParams) (bridge.CredentialExportResult, error) {
			if exporter, ok := all.Connect.Grants.(interface {
				ExportCredential(context.Context, svc.Scope, bridge.CredentialExportParams) (bridge.CredentialExportResult, error)
			}); ok {
				return exporter.ExportCredential(ctx, scope, p)
			}
			return bridge.CredentialExportResult{}, bridge.Errorf(bridge.CodeDenied, "credential export is unavailable")
		}
	})})
	Register(bridge.MethodConnectAccounts, Method{Allow: caps.ConnectAllow(bridge.MethodConnectAccounts), Global: true, Handle: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.Empty) (bridge.AccountsResult, error) {
		return func(ctx context.Context, caller svc.Caller, _ bridge.Empty) (bridge.AccountsResult, error) {
			return all.Connect.Grants.Accounts(ctx, caller)
		}
	})})
	Register(bridge.MethodConnectAllow, Method{Allow: caps.ConnectAllow(bridge.MethodConnectAllow), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AllowParams) (bridge.Grant, error) {
		return all.Connect.Grants.Allow
	})})
	Register(bridge.MethodConnectAllowSheet, Method{Allow: caps.ConnectAllow(bridge.MethodConnectAllowSheet), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AllowSheetParams) (bridge.AllowSheet, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.AllowSheetParams) (bridge.AllowSheet, error) {
			if sheets, ok := all.Connect.Grants.(connectAllowSheets); ok {
				return sheets.AllowSheet(ctx, scope, p)
			}
			return bridge.AllowSheet{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.allow.sheet is not built yet")
		}
	})})
	Register(bridge.MethodConnectRevoke, Method{Allow: caps.ConnectAllow(bridge.MethodConnectRevoke), Global: true, Handle: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.RevokeParams) (bridge.Empty, error) {
		return func(ctx context.Context, caller svc.Caller, p bridge.RevokeParams) (bridge.Empty, error) {
			return bridge.Empty{}, all.Connect.Grants.Revoke(ctx, caller, p)
		}
	})})
	Register(bridge.MethodConnectWorkspaceList, Method{Allow: caps.ConnectAllow(bridge.MethodConnectWorkspaceList), Global: true, Handle: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.Empty) (bridge.WorkspaceConnectionsResult, error) {
		return func(ctx context.Context, caller svc.Caller, _ bridge.Empty) (bridge.WorkspaceConnectionsResult, error) {
			return all.Connect.Grants.Workspace(ctx, caller)
		}
	})})
	Register(bridge.MethodConnectWorkspaceAdd, Method{Allow: caps.ConnectAllow(bridge.MethodConnectWorkspaceAdd), Global: true, Handle: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.AddWorkspaceConnectionParams) (bridge.WorkspaceConnection, error) {
		return all.Connect.Grants.AddWorkspace
	})})
	Register(bridge.MethodConnectWorkspaceWho, Method{Allow: caps.ConnectAllow(bridge.MethodConnectWorkspaceWho), Global: true, Handle: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SetWhoParams) (bridge.WorkspaceConnection, error) {
		return all.Connect.Grants.SetWho
	})})
	Register(bridge.MethodConnectWorkspaceImpact, Method{Allow: caps.ConnectAllow(bridge.MethodConnectWorkspaceImpact), Global: true, Handle: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.WorkspaceConnectionParams) (bridge.RemovalImpact, error) {
		return func(ctx context.Context, caller svc.Caller, p bridge.WorkspaceConnectionParams) (bridge.RemovalImpact, error) {
			return all.Connect.Grants.RemovalImpact(ctx, caller, p.ID)
		}
	})})
	Register(bridge.MethodConnectWorkspaceRemove, Method{Allow: caps.ConnectAllow(bridge.MethodConnectWorkspaceRemove), Global: true, Handle: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.WorkspaceConnectionParams) (bridge.Empty, error) {
		return func(ctx context.Context, caller svc.Caller, p bridge.WorkspaceConnectionParams) (bridge.Empty, error) {
			return bridge.Empty{}, all.Connect.Grants.RemoveWorkspace(ctx, caller, p.ID)
		}
	})})
	Register(bridge.MethodConnectNeeds, Method{Allow: caps.ConnectAllow(bridge.MethodConnectNeeds), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.ConnectionNeeds, error) {
		return func(ctx context.Context, scope svc.Scope, _ bridge.Empty) (bridge.ConnectionNeeds, error) {
			return all.Connect.Grants.Needs(ctx, scope)
		}
	})})
	Register(bridge.MethodConnectWaits, Method{Allow: caps.ConnectAllow(bridge.MethodConnectWaits), Global: true, Handle: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.Empty) (bridge.ConnectWaitsResult, error) {
		return func(ctx context.Context, caller svc.Caller, _ bridge.Empty) (bridge.ConnectWaitsResult, error) {
			if activity, ok := all.Connectors.(connectActivity); ok {
				return activity.Waits(ctx, caller)
			}
			return bridge.ConnectWaitsResult{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.waits is not built yet")
		}
	})})
	Register(bridge.MethodConnectFakes, Method{Allow: caps.ConnectAllow(bridge.MethodConnectFakes), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ConnectFakesParams) (bridge.ConnectFakesResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.ConnectFakesParams) (bridge.ConnectFakesResult, error) {
			if activity, ok := all.Connectors.(connectActivity); ok {
				return activity.Fakes(ctx, scope, p)
			}
			return bridge.ConnectFakesResult{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.fakes is not built yet")
		}
	})})
	Register(bridge.MethodImportsSources, Method{Allow: caps.ConnectAllow(bridge.MethodImportsSources), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ImportSourcesParams) (bridge.ImportSourcesResult, error) {
		return all.Connect.Imports.Sources
	})})
	Register(bridge.MethodImportsPreview, Method{Allow: caps.ConnectAllow(bridge.MethodImportsPreview), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ImportPreviewParams) (bridge.ImportPreview, error) {
		return all.Connect.Imports.Preview
	})})
	Register(bridge.MethodImportsRun, Method{Allow: caps.ConnectAllow(bridge.MethodImportsRun), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ImportRunParams) (bridge.ImportResult, error) {
		return all.Connect.Imports.Run
	})})
	Register(bridge.MethodImportsUndo, Method{Allow: caps.ConnectAllow(bridge.MethodImportsUndo), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ImportUndoParams) (bridge.ImportUndoResult, error) {
		return all.Connect.Imports.Undo
	})})
}

// The contract's Grants seam omits the sheet query, so it stays an optional extension.
type connectAllowSheets interface {
	AllowSheet(context.Context, svc.Scope, bridge.AllowSheetParams) (bridge.AllowSheet, error)
}

// The connector door owns fake calls and durable connection waits.
type connectActivity interface {
	Waits(context.Context, svc.Caller) (bridge.ConnectWaitsResult, error)
	Fakes(context.Context, svc.Scope, bridge.ConnectFakesParams) (bridge.ConnectFakesResult, error)
}

func withConnectCaller[P, R any](pick func(*svc.Services) func(context.Context, svc.Caller, P) (R, error)) Handler {
	return with(func(all *svc.Services) func(context.Context, svc.Scope, P) (R, error) {
		return func(ctx context.Context, scope svc.Scope, params P) (R, error) {
			return pick(all)(ctx, scope.Caller, params)
		}
	})
}
