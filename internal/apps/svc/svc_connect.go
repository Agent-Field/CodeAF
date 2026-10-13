package svc

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// ConnectServices groups this track's services on svc.Services.
type ConnectServices struct {
	Grants  Grants
	Imports Imports
}

// Grants owns Allow and workspace connections.
type Grants interface {
	Accounts(ctx context.Context, caller Caller) (bridge.AccountsResult, error)
	Allow(ctx context.Context, scope Scope, p bridge.AllowParams) (bridge.Grant, error)
	Revoke(ctx context.Context, caller Caller, p bridge.RevokeParams) error
	Workspace(ctx context.Context, caller Caller) (bridge.WorkspaceConnectionsResult, error)
	AddWorkspace(ctx context.Context, caller Caller, p bridge.AddWorkspaceConnectionParams) (bridge.WorkspaceConnection, error)
	SetWho(ctx context.Context, caller Caller, p bridge.SetWhoParams) (bridge.WorkspaceConnection, error)
	RemovalImpact(ctx context.Context, caller Caller, id string) (bridge.RemovalImpact, error)
	RemoveWorkspace(ctx context.Context, caller Caller, id string) error
	Needs(ctx context.Context, scope Scope) (bridge.ConnectionNeeds, error)
	// Resolve picks an eligible workspace credential or a personal one with Allow.
	// Errors distinguish not_allowed_for_app, needs_allow and expired.
	Resolve(ctx context.Context, scope Scope, service string) (Credential, error)
}

// Credential is what the door hands an adapter. Tokens never leave the server.
type Credential struct {
	ID      string
	Service string
	Account string
	Level   string
	Member  string
}

// Imports owns one-time imports.
type Imports interface {
	Sources(ctx context.Context, scope Scope, p bridge.ImportSourcesParams) (bridge.ImportSourcesResult, error)
	Preview(ctx context.Context, scope Scope, p bridge.ImportPreviewParams) (bridge.ImportPreview, error)
	Run(ctx context.Context, scope Scope, p bridge.ImportRunParams) (bridge.ImportResult, error)
	Undo(ctx context.Context, scope Scope, p bridge.ImportUndoParams) (bridge.ImportUndoResult, error)
}
