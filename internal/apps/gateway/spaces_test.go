package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestSessionCallerAndRoles(t *testing.T) {
	resetCallerResolver(t)
	caller := svc.Caller{Member: "raj", Space: "acme", WS: svc.WorkspaceMember}
	role := svc.RoleViewer
	var apps []string
	request := httptest.NewRequest(http.MethodGet, bridge.Path, nil)
	request = request.WithContext(svc.WithCaller(request.Context(), caller))
	SetCallerResolver(func(_ context.Context, r *http.Request, app string) svc.Caller {
		if r != request {
			t.Fatal("resolver lost the upgrade request")
		}
		resolved, _ := svc.CallerFrom(r.Context())
		apps = append(apps, app)
		if app != "" {
			resolved.Role = role
		}
		return resolved
	})
	s := newSession(context.Background(), nil, &svc.Services{}, request)
	defer s.cancel()
	if s.caller != caller {
		t.Fatalf("socket caller = %+v", s.caller)
	}
	for _, tt := range []struct {
		app  string
		role svc.Role
		data svc.DataMode
	}{
		{"crm", svc.RoleViewer, svc.DataSample},
		{"crm@draft", svc.RoleUser, svc.DataDraft},
		{"support", svc.RoleEditor, svc.DataSample},
	} {
		role = tt.role
		scope := s.appScope(tt.app)
		want := caller
		want.Role = role
		if scope.Caller != want || scope.Data != tt.data {
			t.Fatalf("scope = %+v, want caller %+v and data %s", scope, want, tt.data)
		}
	}
	if !reflect.DeepEqual(apps, []string{"", "crm", "crm", "support"}) {
		t.Fatalf("resolver apps = %v", apps)
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
			entry, ok := lookupMethod(tt.name)
			if !ok || entry.Handle == nil || entry.Global != tt.global {
				t.Fatalf("registered = %v, entry = %+v", ok, entry)
			}
		})
	}
}
