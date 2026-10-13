package gateway

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Keep the wave-1 dispatch table independently testable while extending the session table.
func spacesMethodTable(base map[string]method) map[string]method {
	methods := make(map[string]method, len(base)+24)
	for name, entry := range base {
		methods[name] = entry
	}
	registerSpaces(methods)
	return methods
}

func registerSpaces(methods map[string]method) {
	methods[bridge.MethodSpaceInfo] = method{global: true, run: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.Spaces.Info(ctx, scope.Caller)
	}}
	methods[bridge.MethodSpacesList] = method{global: true, run: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.Spaces.List(ctx, scope.Caller)
	}}
	methods[bridge.MethodSpacesJoin] = method{global: true, run: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SpaceJoinParams) (bridge.SpaceJoinResult, error) {
		return all.Spaces.Join
	})}
	methods[bridge.MethodSpacesConnect] = method{global: true, run: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SpaceConnectParams) (bridge.SpaceConnection, error) {
		return all.Spaces.Connect
	})}
	methods[bridge.MethodSpacesNew] = method{global: true, run: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SpaceNewParams) (bridge.SpaceJoinResult, error) {
		return all.Spaces.New
	})}
	methods[bridge.MethodSpacesForget] = method{global: true, run: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.SpaceRef) (bridge.Empty, error) {
		return all.Spaces.Forget
	})}
	methods[bridge.MethodMembersList] = method{global: true, run: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.Members.List(ctx, scope.Caller)
	}}
	methods[bridge.MethodMembersInvite] = method{global: true, run: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.InviteParams) (bridge.Invite, error) {
		return all.Members.Invite
	})}
	methods[bridge.MethodMembersInviteCancel] = method{global: true, run: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.InviteRef) (bridge.Empty, error) {
		return all.Members.CancelInvite
	})}
	methods[bridge.MethodMembersRole] = method{global: true, run: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.MemberRoleParams) (bridge.Member, error) {
		return all.Members.SetRole
	})}
	methods[bridge.MethodMembersRemove] = method{global: true, run: withCaller(func(all *svc.Services) func(context.Context, svc.Caller, bridge.MemberRef) (bridge.Empty, error) {
		return all.Members.Remove
	})}
	methods[bridge.MethodMembersLeave] = method{global: true, run: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.Members.Leave(ctx, scope.Caller)
	}}
	methods[bridge.MethodAppRolesList] = method{run: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		if err := decode(raw, &bridge.Empty{}); err != nil {
			return nil, err
		}
		return all.AppRoles.List(ctx, scope)
	}}
	methods[bridge.MethodAppRolesSet] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AppRoleSetParams) (bridge.AppMembers, error) {
		return all.AppRoles.Set
	})}
	methods[bridge.MethodAppRolesAsk] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AskEditorParams) (bridge.Empty, error) {
		return all.AppRoles.AskEditor
	})}
	methods[bridge.MethodSignoffRoles] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.SignoffChainRef) (bridge.SignoffRoles, error) {
		return all.Signoff.Roles
	})}
	methods[bridge.MethodSignoffRolesSet] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.SignoffRoles) (bridge.SignoffRoles, error) {
		return all.Signoff.SetRoles
	})}
	methods[bridge.MethodSignoffState] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.SignoffRecordRef) (bridge.SignoffState, error) {
		return all.Signoff.State
	})}
	methods[bridge.MethodSignoffSign] = method{run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.SignoffSignParams) (bridge.SignoffState, error) {
		return all.Signoff.Sign
	})}
	methods[bridge.MethodAuditList] = method{global: true, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AuditListParams) (bridge.AuditListResult, error) {
		return all.Audit.List
	})}
	methods[bridge.MethodActivityList] = method{global: true, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.ActivityListParams) (bridge.ActivityListResult, error) {
		return all.Audit.Activity
	})}
	methods[bridge.MethodSpacesSignin] = method{global: true, run: notBuiltSpaces[bridge.SpacesSigninParams](bridge.MethodSpacesSignin)}
	methods[bridge.MethodMembersInviteResend] = method{global: true, run: notBuiltSpaces[bridge.InviteRef](bridge.MethodMembersInviteResend)}
	methods[bridge.MethodAppsMove] = method{global: false, run: notBuiltSpaces[bridge.MoveAppParams](bridge.MethodAppsMove)}
}

func withCaller[P, R any](pick func(*svc.Services) func(context.Context, svc.Caller, P) (R, error)) handler {
	return with(func(all *svc.Services) func(context.Context, svc.Scope, P) (R, error) {
		return func(ctx context.Context, scope svc.Scope, p P) (R, error) { return pick(all)(ctx, scope.Caller, p) }
	})
}

func notBuiltSpaces[P any](name string) handler {
	return func(_ context.Context, _ *svc.Services, _ svc.Scope, raw json.RawMessage) (any, error) {
		var p P
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		return nil, bridge.NotBuilt(name)
	}
}

func sessionContext(parent, request context.Context) context.Context {
	caller, ok := svc.CallerFrom(request)
	if !ok {
		caller = svc.PersonalCaller()
	}
	return svc.WithCaller(parent, caller)
}

func (s *session) scope(app string) (svc.Scope, error) {
	caller, ok := svc.CallerFrom(s.ctx)
	if !ok {
		caller = svc.PersonalCaller()
	}
	scope := svc.Scope{App: app, Caller: caller, Data: svc.DataSample}
	if app != "" && s.all.AppRoles != nil {
		role, err := s.all.AppRoles.Role(s.ctx, app, caller.Member)
		scope.Caller.Role = role
		return scope, err
	}
	return scope, nil
}

func (s *session) spacesTopicScope(in bridge.Envelope, method string) (svc.Scope, bool, error) {
	if !strings.HasPrefix(in.Topic, "space/") {
		return svc.Scope{}, false, nil
	}
	switch in.Topic {
	case bridge.TopicSpaceMembers, bridge.TopicSpaceActivity, bridge.TopicSpaceWaiting:
	default:
		return svc.Scope{}, true, bridge.Errorf(bridge.CodeInvalid, "unknown space topic")
	}
	if in.App != "" {
		return svc.Scope{}, true, bridge.Errorf(bridge.CodeInvalid, "space topics take no app")
	}
	scope, err := s.scope("")
	if err == nil {
		raw, _ := json.Marshal(map[string]string{"topic": in.Topic})
		err = s.all.Caps.Allow(s.ctx, scope, method, raw)
	}
	return scope, true, err
}
