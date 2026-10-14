package svc

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

type WorkspaceRole string

const (
	WorkspaceOwner  WorkspaceRole = "owner"
	WorkspaceAdmin  WorkspaceRole = "admin"
	WorkspaceMember WorkspaceRole = "member"
	PersonalSpace                 = "personal"
)

func (c Caller) IsAdmin() bool  { return c.WS == WorkspaceOwner || c.WS == WorkspaceAdmin }
func (c Caller) CanShape() bool { return c.Role == RoleEditor && !c.Agent }
func (r Role) AtLeast(other Role) bool {
	ranks := map[Role]int{RoleViewer: 1, RoleUser: 2, RoleEditor: 3}
	return ranks[r] != 0 && ranks[other] != 0 && ranks[r] >= ranks[other]
}
func PersonalCaller() Caller {
	return Caller{Member: Owner, Role: RoleEditor, Space: PersonalSpace, WS: WorkspaceOwner}
}

type callerKey struct{}

func WithCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	return caller, ok
}

type SpacesServices struct {
	Spaces   Spaces
	Members  Members
	AppRoles AppRoles
	Audit    Audit
	Signoff  Signoff
}

// AuditWriter is what every track calls after a write, reveal, send,
// approval, denial, Apply, migration or role change (rule: append-only).
type AuditWriter interface {
	Append(ctx context.Context, e bridge.AuditEntry) (bridge.AuditEntry, error)
}

type Audit interface {
	AuditWriter
	List(ctx context.Context, scope Scope, p bridge.AuditListParams) (bridge.AuditListResult, error)           // owner/admin
	Activity(ctx context.Context, scope Scope, p bridge.ActivityListParams) (bridge.ActivityListResult, error) // members
}

type AppRoles interface {
	// Role is the caller's effective app role: own row, else Everyone, else
	// editor for the app's maker. Personal: always editor.
	Role(ctx context.Context, app, member string) (Role, error)
	List(ctx context.Context, scope Scope) (bridge.AppMembers, error)
	Set(ctx context.Context, scope Scope, p bridge.AppRoleSetParams) (bridge.AppMembers, error)
	// Seed makes the maker Editor and Everyone User; Store calls it on Keep
	// and on the first Apply (shape) and on Move.
	Seed(ctx context.Context, app, maker string) error
	AskEditor(ctx context.Context, scope Scope, p bridge.AskEditorParams) (bridge.Empty, error)
}

type Spaces interface {
	Info(ctx context.Context, caller Caller) (bridge.SpaceInfo, error)
	// Personal only: the spaces this desktop has joined and how to reach them.
	List(ctx context.Context, caller Caller) (bridge.SpacesListResult, error)
	Join(ctx context.Context, caller Caller, p bridge.SpaceJoinParams) (bridge.SpaceJoinResult, error)
	Connect(ctx context.Context, caller Caller, p bridge.SpaceConnectParams) (bridge.SpaceConnection, error)
	New(ctx context.Context, caller Caller, p bridge.SpaceNewParams) (bridge.SpaceJoinResult, error)
	Forget(ctx context.Context, caller Caller, p bridge.SpaceRef) (bridge.Empty, error)
}

type Members interface {
	// Authenticate maps a bearer token to the member (workspace mode).
	Authenticate(ctx context.Context, token string) (Caller, error)
	List(ctx context.Context, caller Caller) (bridge.MembersListResult, error)
	Invite(ctx context.Context, caller Caller, p bridge.InviteParams) (bridge.Invite, error)
	CancelInvite(ctx context.Context, caller Caller, p bridge.InviteRef) (bridge.Empty, error)
	SetRole(ctx context.Context, caller Caller, p bridge.MemberRoleParams) (bridge.Member, error)
	Remove(ctx context.Context, caller Caller, p bridge.MemberRef) (bridge.Empty, error)
	Leave(ctx context.Context, caller Caller) (bridge.Empty, error)
}

type Signoff interface {
	Roles(ctx context.Context, scope Scope, p bridge.SignoffChainRef) (bridge.SignoffRoles, error)
	SetRoles(ctx context.Context, scope Scope, p bridge.SignoffRoles) (bridge.SignoffRoles, error)
	State(ctx context.Context, scope Scope, p bridge.SignoffRecordRef) (bridge.SignoffState, error)
	Sign(ctx context.Context, scope Scope, p bridge.SignoffSignParams) (bridge.SignoffState, error)
}
