package caps

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestWorkspaceRoleGate(t *testing.T) {
	methods := []string{bridge.MethodMembersInvite, bridge.MethodMembersInviteCancel, bridge.MethodMembersInviteResend,
		bridge.MethodMembersRole, bridge.MethodMembersRemove, bridge.MethodAuditList}
	for _, role := range []svc.WorkspaceRole{svc.WorkspaceOwner, svc.WorkspaceAdmin, svc.WorkspaceMember, ""} {
		for _, method := range methods {
			t.Run(string(role)+"/"+method, func(t *testing.T) {
				scope := svc.Scope{Caller: svc.Caller{Member: "raj", WS: role, Role: svc.RoleEditor}}
				err := allowSpaces(scope, method)
				wantDenied := role != svc.WorkspaceOwner && role != svc.WorkspaceAdmin
				var typed *bridge.Error
				if (err != nil) != wantDenied || (err != nil && (!errors.As(err, &typed) || typed.Code != bridge.CodeDenied)) {
					t.Fatalf("error = %v, want denied=%v", err, wantDenied)
				}
			})
		}
	}
}

func TestSpacesCheckerPersonalAndMember(t *testing.T) {
	checker := New(svc.Config{Root: t.TempDir()}, &svc.Services{})
	for _, tt := range []struct {
		name   string
		caller svc.Caller
		method string
		raw    json.RawMessage
		code   bridge.ErrorCode
	}{
		{"personal audit", svc.PersonalCaller(), bridge.MethodAuditList, nil, ""},
		{"member audit", svc.Caller{Member: "raj", WS: svc.WorkspaceMember}, bridge.MethodAuditList, nil, bridge.CodeDenied},
		{"member info", svc.Caller{Member: "raj", WS: svc.WorkspaceMember}, bridge.MethodSpaceInfo, nil, ""},
		{"member list", svc.Caller{Member: "raj", WS: svc.WorkspaceMember}, bridge.MethodMembersList, nil, ""},
		{"bad params", svc.PersonalCaller(), bridge.MethodSpaceInfo, json.RawMessage(`[]`), bridge.CodeInvalid},
		{"space topic", svc.PersonalCaller(), "sub", json.RawMessage(`{"topic":"space/activity"}`), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := checker.Allow(context.Background(), svc.Scope{Caller: tt.caller}, tt.method, tt.raw)
			var typed *bridge.Error
			if tt.code == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.As(err, &typed) || typed.Code != tt.code {
				t.Fatalf("error = %v, want %s", err, tt.code)
			}
		})
	}
}

func TestSpacesDenyRulesPrecedeMethodPermission(t *testing.T) {
	for _, tt := range []struct {
		name   string
		method string
		role   svc.Role
		ws     svc.WorkspaceRole
		agent  bool
		denied bool
	}{
		{"member invite", bridge.MethodMembersInvite, svc.RoleEditor, svc.WorkspaceMember, false, true},
		{"admin invite", bridge.MethodMembersInvite, svc.RoleViewer, svc.WorkspaceAdmin, false, false},
		{"viewer sign", bridge.MethodSignoffSign, svc.RoleViewer, svc.WorkspaceMember, false, true},
		{"user sign", bridge.MethodSignoffSign, svc.RoleUser, svc.WorkspaceMember, false, false},
		{"user assign signatories", bridge.MethodSignoffRolesSet, svc.RoleUser, svc.WorkspaceMember, false, true},
		{"editor assign signatories", bridge.MethodSignoffRolesSet, svc.RoleEditor, svc.WorkspaceMember, false, false},
		{"agent assign signatories", bridge.MethodSignoffRolesSet, svc.RoleEditor, svc.WorkspaceMember, true, true},
		{"viewer set roles", bridge.MethodAppRolesSet, svc.RoleViewer, svc.WorkspaceMember, false, true},
		{"admin repair roles", bridge.MethodAppRolesSet, svc.RoleViewer, svc.WorkspaceAdmin, false, false},
		{"admin shape", bridge.MethodSignoffRolesSet, svc.RoleViewer, svc.WorkspaceAdmin, false, true},
		{"user move", bridge.MethodAppsMove, svc.RoleUser, svc.WorkspaceOwner, false, true},
		{"editor move", bridge.MethodAppsMove, svc.RoleEditor, svc.WorkspaceOwner, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			// The startup deny rule must win even when a method grants permission.
			_, rules := registeredPermission(tt.method, nil)
			resetPermissions(t)
			for _, rule := range rules {
				AddDenyRule(rule)
			}
			RegisterAllow(tt.method, func(context.Context, svc.Caller, svc.Scope, json.RawMessage) error { called = true; return nil })
			checker := New(svc.Config{Root: t.TempDir()}, &svc.Services{})
			scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Role: tt.role, WS: tt.ws, Agent: tt.agent}}
			err := checker.Allow(context.Background(), scope, tt.method, nil)
			if tt.denied {
				if errorCode(err) != bridge.CodeDenied || called {
					t.Fatalf("deny did not win: error=%v allow called=%v", err, called)
				}
			} else if err != nil || !called {
				t.Fatalf("error=%v allow called=%v", err, called)
			}
		})
	}
}

func TestSpacesTopicPermissions(t *testing.T) {
	checker := New(svc.Config{Root: t.TempDir()}, &svc.Services{})
	for _, method := range []string{"sub", "unsub"} {
		for _, tt := range []struct {
			topic string
			app   string
			role  svc.Role
			code  bridge.ErrorCode
		}{
			{bridge.TopicSpaceMembers, "", "", "allowed"},
			{bridge.TopicSpaceActivity, "", "", "allowed"},
			{bridge.TopicSpaceWaiting, "", "", "allowed"},
			{"space/unknown", "", "", bridge.CodeInvalid},
			{bridge.TopicSpaceMembers, "crm", svc.RoleViewer, bridge.CodeInvalid},
			{bridge.TopicRoles("crm"), "crm", svc.RoleViewer, "allowed"},
			{bridge.TopicSignoff("crm"), "crm", svc.RoleUser, "allowed"},
			{bridge.TopicRoles("other"), "crm", svc.RoleViewer, bridge.CodeInvalid},
			{"app/crm/roles/extra", "crm", svc.RoleViewer, bridge.CodeInvalid},
			{bridge.TopicRoles("crm"), "crm", "", bridge.CodeDenied},
		} {
			t.Run(method+"/"+tt.topic+"/"+tt.app+"/"+string(tt.role), func(t *testing.T) {
				raw, _ := json.Marshal(map[string]string{"topic": tt.topic})
				scope := svc.Scope{App: tt.app, Caller: svc.Caller{Member: "raj", Space: "acme", WS: svc.WorkspaceMember, Role: tt.role}}
				if err := checker.Allow(context.Background(), scope, method, raw); errorCode(err) != tt.code {
					t.Fatalf("error = %v, want %s", err, tt.code)
				}
			})
		}
	}
}
