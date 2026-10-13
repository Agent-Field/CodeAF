package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type sessionRoles struct {
	svc.AppRoles
	role   svc.Role
	err    error
	app    string
	member string
	calls  int
}

func (r *sessionRoles) Role(_ context.Context, app, member string) (svc.Role, error) {
	r.app, r.member = app, member
	r.calls++
	return r.role, r.err
}

func TestSessionCallerAndRoles(t *testing.T) {
	caller := svc.Caller{Member: "raj", Space: "acme", WS: svc.WorkspaceMember, Role: svc.RoleUser}
	for _, tt := range []struct {
		name   string
		caller *svc.Caller
		app    string
		role   svc.Role
		err    error
	}{
		{"personal default", nil, "", "", nil},
		{"workspace session", &caller, "", "", nil},
		{"app viewer", &caller, "crm", svc.RoleViewer, nil},
		{"app editor", &caller, "crm", svc.RoleEditor, nil},
		{"role failure", &caller, "crm", "", bridge.NotBuilt("roles")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := context.Background()
			want := svc.PersonalCaller()
			if tt.caller != nil {
				request = svc.WithCaller(request, *tt.caller)
				want = *tt.caller
			}
			roles := &sessionRoles{role: tt.role, err: tt.err}
			s := newSession(sessionContext(context.Background(), request), nil, &svc.Services{SpacesServices: svc.SpacesServices{AppRoles: roles}})
			defer s.cancel()
			scope, err := s.scope(tt.app)
			if !errors.Is(err, tt.err) {
				t.Fatalf("scope error = %v, want %v", err, tt.err)
			}
			if tt.app != "" {
				want.Role = tt.role
				if roles.app != tt.app || roles.member != want.Member || roles.calls != 1 {
					t.Fatalf("Role lookup = %+v", roles)
				}
				roles.role = svc.RoleUser
				next, _ := s.scope(tt.app)
				if next.Caller.Role != svc.RoleUser || roles.calls != 2 {
					t.Fatal("app role was cached between calls")
				}
			} else if roles.calls != 0 {
				t.Fatal("global call looked up an app role")
			}
			if scope.Caller != want || scope.App != tt.app || scope.Data != svc.DataSample {
				t.Fatalf("scope = %+v, want caller %+v", scope, want)
			}
		})
	}
}

func TestSpacesMethodsRegistered(t *testing.T) {
	for _, tt := range []struct {
		name   string
		global bool
	}{
		{bridge.MethodSpaceInfo, true}, {bridge.MethodSpacesList, true}, {bridge.MethodSpacesJoin, true},
		{bridge.MethodSpacesSignin, true}, {bridge.MethodSpacesConnect, true}, {bridge.MethodSpacesNew, true}, {bridge.MethodSpacesForget, true},
		{bridge.MethodMembersList, true}, {bridge.MethodMembersInvite, true}, {bridge.MethodMembersInviteCancel, true},
		{bridge.MethodMembersInviteResend, true}, {bridge.MethodMembersRole, true}, {bridge.MethodMembersRemove, true}, {bridge.MethodMembersLeave, true},
		{bridge.MethodAppRolesList, false}, {bridge.MethodAppRolesSet, false}, {bridge.MethodAppRolesAsk, false},
		{bridge.MethodSignoffRoles, false}, {bridge.MethodSignoffRolesSet, false}, {bridge.MethodSignoffState, false}, {bridge.MethodSignoffSign, false},
		{bridge.MethodAuditList, true}, {bridge.MethodActivityList, true}, {bridge.MethodAppsMove, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := sessionMethods[tt.name]
			if !ok || entry.run == nil || entry.global != tt.global {
				t.Fatalf("registered = %v, entry = %+v", ok, entry)
			}
		})
	}
}
