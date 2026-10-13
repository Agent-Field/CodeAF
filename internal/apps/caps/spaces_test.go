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
