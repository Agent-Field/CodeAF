package gateway

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() { registerConnect(methods) }

func registerConnect(table map[string]method) {
	table[bridge.MethodConnectCredentialExport] = method{global: true, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.CredentialExportParams) (bridge.CredentialExportResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.CredentialExportParams) (bridge.CredentialExportResult, error) {
			if exporter, ok := all.Connect.Grants.(interface {
				ExportCredential(context.Context, svc.Scope, bridge.CredentialExportParams) (bridge.CredentialExportResult, error)
			}); ok {
				return exporter.ExportCredential(ctx, scope, p)
			}
			return bridge.CredentialExportResult{}, bridge.Errorf(bridge.CodeDenied, "credential export is unavailable")
		}
	})}
	table[bridge.MethodConnectAccounts] = method{global: true, run: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.Empty) (bridge.AccountsResult, error) {
		return func(ctx context.Context, caller svc.Caller, _ bridge.Empty) (bridge.AccountsResult, error) {
			return all.Connect.Grants.Accounts(ctx, caller)
		}
	})}
	table[bridge.MethodConnectAllow] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AllowParams) (bridge.Grant, error) {
		return all.Connect.Grants.Allow
	})}
	table[bridge.MethodConnectAllowSheet] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AllowSheetParams) (bridge.AllowSheet, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.AllowSheetParams) (bridge.AllowSheet, error) {
			if sheets, ok := all.Connect.Grants.(connectAllowSheets); ok {
				return sheets.AllowSheet(ctx, scope, p)
			}
			return bridge.AllowSheet{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.allow.sheet is not built yet")
		}
	})}
	table[bridge.MethodConnectRevoke] = method{global: true, run: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.RevokeParams) (bridge.Empty, error) {
		return func(ctx context.Context, caller svc.Caller, p bridge.RevokeParams) (bridge.Empty, error) {
			return bridge.Empty{}, all.Connect.Grants.Revoke(ctx, caller, p)
		}
	})}
	table[bridge.MethodConnectWorkspaceList] = method{global: true, run: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.Empty) (bridge.WorkspaceConnectionsResult, error) {
		return func(ctx context.Context, caller svc.Caller, _ bridge.Empty) (bridge.WorkspaceConnectionsResult, error) {
			return all.Connect.Grants.Workspace(ctx, caller)
		}
	})}
	table[bridge.MethodConnectWorkspaceAdd] = method{global: true, run: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.AddWorkspaceConnectionParams) (bridge.WorkspaceConnection, error) {
		return all.Connect.Grants.AddWorkspace
	})}
	table[bridge.MethodConnectWorkspaceWho] = method{global: true, run: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SetWhoParams) (bridge.WorkspaceConnection, error) {
		return all.Connect.Grants.SetWho
	})}
	table[bridge.MethodConnectWorkspaceImpact] = method{global: true, run: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.WorkspaceConnectionParams) (bridge.RemovalImpact, error) {
		return func(ctx context.Context, caller svc.Caller, p bridge.WorkspaceConnectionParams) (bridge.RemovalImpact, error) {
			return all.Connect.Grants.RemovalImpact(ctx, caller, p.ID)
		}
	})}
	table[bridge.MethodConnectWorkspaceRemove] = method{global: true, run: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.WorkspaceConnectionParams) (bridge.Empty, error) {
		return func(ctx context.Context, caller svc.Caller, p bridge.WorkspaceConnectionParams) (bridge.Empty, error) {
			return bridge.Empty{}, all.Connect.Grants.RemoveWorkspace(ctx, caller, p.ID)
		}
	})}
	table[bridge.MethodConnectNeeds] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.ConnectionNeeds, error) {
		return func(ctx context.Context, scope svc.Scope, _ bridge.Empty) (bridge.ConnectionNeeds, error) {
			return all.Connect.Grants.Needs(ctx, scope)
		}
	})}
	table[bridge.MethodConnectWaits] = method{global: true, run: withConnectCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.Empty) (bridge.ConnectWaitsResult, error) {
		return func(ctx context.Context, caller svc.Caller, _ bridge.Empty) (bridge.ConnectWaitsResult, error) {
			if activity, ok := all.Connect.Grants.(connectActivity); ok {
				return activity.Waits(ctx, caller)
			}
			return bridge.ConnectWaitsResult{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.waits is not built yet")
		}
	})}
	table[bridge.MethodConnectFakes] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ConnectFakesParams) (bridge.ConnectFakesResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.ConnectFakesParams) (bridge.ConnectFakesResult, error) {
			if activity, ok := all.Connect.Grants.(connectActivity); ok {
				return activity.Fakes(ctx, scope, p)
			}
			return bridge.ConnectFakesResult{}, bridge.Errorf(bridge.CodeNotBuilt, "connect.fakes is not built yet")
		}
	})}
	table[bridge.MethodImportsSources] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ImportSourcesParams) (bridge.ImportSourcesResult, error) {
		return all.Connect.Imports.Sources
	})}
	table[bridge.MethodImportsPreview] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ImportPreviewParams) (bridge.ImportPreview, error) {
		return all.Connect.Imports.Preview
	})}
	table[bridge.MethodImportsRun] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ImportRunParams) (bridge.ImportResult, error) {
		return all.Connect.Imports.Run
	})}
	table[bridge.MethodImportsUndo] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ImportUndoParams) (bridge.ImportUndoResult, error) {
		return all.Connect.Imports.Undo
	})}
}

// The contract's Grants seam omits the sheet query, so it stays an optional extension.
type connectAllowSheets interface {
	AllowSheet(context.Context, svc.Scope, bridge.AllowSheetParams) (bridge.AllowSheet, error)
}

// Lane b1 fills these queries; they route to Grants until the connector door lands.
type connectActivity interface {
	Waits(context.Context, svc.Caller) (bridge.ConnectWaitsResult, error)
	Fakes(context.Context, svc.Scope, bridge.ConnectFakesParams) (bridge.ConnectFakesResult, error)
}

func withConnectCaller[P, R any](pick func(*svc.Services) func(context.Context, svc.Caller, P) (R, error)) handler {
	return with(func(all *svc.Services) func(context.Context, svc.Scope, P) (R, error) {
		return func(ctx context.Context, scope svc.Scope, params P) (R, error) {
			return pick(all)(ctx, scope.Caller, params)
		}
	})
}

func (s *session) topicScope(in bridge.Envelope, method string) (svc.Scope, error) {
	if scope, handled, err := s.connectTopicScope(in, method); handled {
		return scope, err
	}
	return s.wave1TopicScope(in, method)
}

// connectTopicScope sends connect subscriptions through the same checker and hub path.
func (s *session) connectTopicScope(in bridge.Envelope, method string) (svc.Scope, bool, error) {
	global := strings.HasPrefix(in.Topic, "connect/")
	app, valid := topicApp(in.Topic)
	if !global && (!valid || in.Topic != bridge.TopicFakes(app)) {
		return svc.Scope{}, false, nil
	}
	if global {
		app = ""
	}
	scope := ownerScope(app)
	if in.App != "" && in.App != app {
		return scope, true, bridge.Errorf(bridge.CodeInvalid, "topic and app disagree")
	}
	if global && in.Topic != bridge.TopicConnectWorkspace() && in.Topic != bridge.TopicConnectAllow(scope.Caller.Member) && in.Topic != bridge.TopicConnectWaits(scope.Caller.Member) {
		return scope, true, bridge.Errorf(bridge.CodeDenied, "this connection topic is not yours")
	}
	raw, _ := json.Marshal(map[string]string{"topic": in.Topic})
	if err := s.all.Caps.Allow(s.ctx, scope, method, raw); err != nil {
		return scope, true, err
	}
	scope, err := s.resolveScope(scope, global)
	return scope, true, err
}
