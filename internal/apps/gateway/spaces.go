package gateway

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() {
	Register(bridge.MethodSpaceInfo, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.Spaces.Info(ctx, scope.Caller)
	}})
	Register(bridge.MethodSpacesList, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.Spaces.List(ctx, scope.Caller)
	}})
	Register(bridge.MethodSpacesJoin, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SpaceJoinParams) (bridge.SpaceJoinResult, error) {
		return all.Spaces.Join
	})})
	Register(bridge.MethodSpacesConnect, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SpaceConnectParams) (bridge.SpaceConnection, error) {
		return all.Spaces.Connect
	})})
	Register(bridge.MethodSpacesNew, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SpaceNewParams) (bridge.SpaceJoinResult, error) {
		return all.Spaces.New
	})})
	Register(bridge.MethodSpacesForget, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SpaceRef) (bridge.Empty, error) {
		return all.Spaces.Forget
	})})
	Register(bridge.MethodMembersList, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.Members.List(ctx, scope.Caller)
	}})
	Register(bridge.MethodMembersInvite, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.InviteParams) (bridge.Invite, error) {
		return all.Members.Invite
	})})
	Register(bridge.MethodMembersInviteCancel, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.InviteRef) (bridge.Empty, error) {
		return all.Members.CancelInvite
	})})
	Register(bridge.MethodMembersRole, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.MemberRoleParams) (bridge.Member, error) {
		return all.Members.SetRole
	})})
	Register(bridge.MethodMembersRemove, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.MemberRef) (bridge.Empty, error) {
		return all.Members.Remove
	})})
	Register(bridge.MethodMembersLeave, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.Members.Leave(ctx, scope.Caller)
	}})
	Register(bridge.MethodAppRolesList, Method{Allow: caps.SpacesAllow(false), Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.AppRoles.List(ctx, scope)
	}})
	Register(bridge.MethodAppRolesSet, Method{Allow: caps.SpacesAllow(false), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AppRoleSetParams) (bridge.AppMembers, error) {
		return all.AppRoles.Set
	})})
	Register(bridge.MethodAppRolesAsk, Method{Allow: caps.SpacesAllow(false), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AskEditorParams) (bridge.Empty, error) {
		return all.AppRoles.AskEditor
	})})
	Register(bridge.MethodSignoffRoles, Method{Allow: caps.SpacesAllow(false), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.SignoffChainRef) (bridge.SignoffRoles, error) {
		return all.Signoff.Roles
	})})
	Register(bridge.MethodSignoffRolesSet, Method{Allow: caps.SpacesAllow(false), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.SignoffRoles) (bridge.SignoffRoles, error) {
		return all.Signoff.SetRoles
	})})
	Register(bridge.MethodSignoffState, Method{Allow: caps.SpacesAllow(false), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.SignoffRecordRef) (bridge.SignoffState, error) {
		return all.Signoff.State
	})})
	Register(bridge.MethodSignoffSign, Method{Allow: caps.SpacesAllow(false), Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.SignoffSignParams) (bridge.SignoffState, error) {
		return all.Signoff.Sign
	})})
	Register(bridge.MethodAuditList, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AuditListParams) (bridge.AuditListResult, error) {
		return all.Audit.List
	})})
	Register(bridge.MethodActivityList, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ActivityListParams) (bridge.ActivityListResult, error) {
		return all.Audit.Activity
	})})
	Register(bridge.MethodSpacesSignin, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: notBuiltSpaces[bridge.SpacesSigninParams](bridge.MethodSpacesSignin)})
	Register(bridge.MethodMembersInviteResend, Method{Allow: caps.SpacesAllow(true), Global: true, Handle: notBuiltSpaces[bridge.InviteRef](bridge.MethodMembersInviteResend)})
	Register(bridge.MethodAppsMove, Method{Allow: caps.SpacesAllow(false), Global: false, Handle: notBuiltSpaces[bridge.MoveAppParams](bridge.MethodAppsMove)})
	caps.RegisterTopic("space/", caps.AllowSpacesTopic)
	caps.RegisterTopic("app/*/roles", caps.AllowSpacesTopic)
	caps.RegisterTopic("app/*/signoff", caps.AllowSpacesTopic)
}

func withCaller[P, R any](pick func(*svc.Services) func(context.Context, svc.Caller, P) (R, error)) Handler {
	return with(func(all *svc.Services) func(context.Context, svc.Scope, P) (R, error) {
		return func(ctx context.Context, scope svc.Scope, p P) (R, error) { return pick(all)(ctx, scope.Caller, p) }
	})
}

func notBuiltSpaces[P any](name string) Handler {
	return func(_ context.Context, _ *svc.Services, _ svc.Scope, raw json.RawMessage) (any, error) {
		var p P
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		return nil, bridge.NotBuilt(name)
	}
}
