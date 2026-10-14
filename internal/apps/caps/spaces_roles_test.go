package caps

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// These permissions are the role table's deny layer; app capability checks still run afterwards.
func TestAppRoleActionTable(t *testing.T) {
	rows := []struct {
		action  string
		methods []string
		minimum svc.Role
	}{
		{"open and read", []string{bridge.MethodAppsGet, bridge.MethodRecordsGet, bridge.MethodRecordsQuery, "content.get", "content.list"}, svc.RoleViewer},
		{"create and edit records", []string{bridge.MethodRecordsCreate, bridge.MethodRecordsUpdate, bridge.MethodRecordsDelete}, svc.RoleUser},
		{"Ask the app agent", []string{bridge.MethodAgentSend}, svc.RoleViewer},
		{"Ask Nudge Comment on parts", []string{"parts.ask", "parts.nudge", "comments.create", "comments.update", "comments.delete"}, svc.RoleUser},
		{"Export and Present", []string{"apps.export", "export.pdf", "export.pptx", "export.csv", "apps.present"}, svc.RoleViewer},
		{"Share link", []string{"apps.share"}, svc.RoleUser},
		{"approve own actions", []string{bridge.MethodApprovalsDecide}, svc.RoleUser},
		{"Shape", []string{bridge.MethodShapeOpen, bridge.MethodShapeGet, bridge.MethodShapeRequest, bridge.MethodShapePlanEdit, bridge.MethodShapeChangeFix, bridge.MethodShapeChangeCancel, bridge.MethodShapeAnswer, bridge.MethodShapeDiff, bridge.MethodShapePreview, bridge.MethodShapeDiscard, bridge.MethodShapeReplayDismiss, bridge.MethodBuilderWrite, bridge.MethodBuilderPlanEdit}, svc.RoleEditor},
		{"Apply Undo restore version", []string{bridge.MethodShapeApply, bridge.MethodVersionsRestore, "apps.apply", "apps.undo"}, svc.RoleEditor},
		{"Needs", []string{"needs.add", "needs.remove"}, svc.RoleEditor},
		{"Triggers", []string{"triggers.set", "triggers.pause", "triggers.resume"}, svc.RoleEditor},
		{"Members", []string{bridge.MethodAppRolesSet, bridge.MethodSignoffRolesSet}, svc.RoleEditor},
		{"archive restore", []string{"apps.archive", "apps.restore"}, svc.RoleEditor},
		{"move", []string{bridge.MethodAppsMove}, svc.RoleEditor},
		{"make my copy", []string{"apps.copy"}, svc.RoleUser},
		{"runs", []string{bridge.MethodRunsStart}, svc.RoleUser},
		{"read run approvals roles signoff versions", []string{bridge.MethodRunsGet, bridge.MethodRunsList, bridge.MethodApprovalsList, bridge.MethodAppRolesList, bridge.MethodSignoffRoles, bridge.MethodSignoffState, bridge.MethodVersionsList, bridge.MethodVersionsDiff}, svc.RoleViewer},
	}
	for _, row := range rows {
		for _, method := range row.methods {
			for _, role := range []svc.Role{svc.RoleEditor, svc.RoleUser, svc.RoleViewer, ""} {
				t.Run(row.action+"/"+method+"/"+string(role), func(t *testing.T) {
					scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Space: "acme", WS: svc.WorkspaceMember, Role: role}}
					if method == bridge.MethodAppsMove {
						scope.Caller.Space = svc.PersonalSpace
					}
					// A per-method allow must not bypass the app-role denial.
					_, denies := registeredPermission(method, nil)
					resetPermissions(t)
					for _, deny := range denies {
						AddDenyRule(deny)
					}
					reached := false
					RegisterAllow(method, func(context.Context, svc.Caller, svc.Scope, json.RawMessage) error { reached = true; return nil })
					checker := New(svc.Config{Root: t.TempDir()}, nil)
					t.Cleanup(func() {
						if checker.audit.fallback != nil {
							_ = checker.audit.fallback.Close()
						}
					})
					err := checker.Allow(context.Background(), scope, method, nil)
					wantAllowed := role.AtLeast(row.minimum)
					if wantAllowed && (err != nil || !reached) {
						t.Fatalf("error=%v reached=%v", err, reached)
					}
					if !wantAllowed && (errorCode(err) != bridge.CodeDenied || reached) {
						t.Fatalf("error=%v reached=%v, want denied", err, reached)
					}
				})
			}
		}
	}
}

func TestWorkspaceActionsTable(t *testing.T) {
	rows := []struct {
		methods []string
		minimum svc.WorkspaceRole
		app     bool
	}{
		{[]string{"workspace.pay", "workspace.plan", "workspace.delete"}, svc.WorkspaceOwner, false},
		{[]string{bridge.MethodMembersInvite, bridge.MethodMembersInviteCancel, bridge.MethodMembersInviteResend, bridge.MethodMembersRole, bridge.MethodMembersRemove}, svc.WorkspaceAdmin, false},
		{[]string{"connections.add", "connections.remove"}, svc.WorkspaceAdmin, false},
		{[]string{"budget.set", bridge.MethodAuditList}, svc.WorkspaceAdmin, false},
		{[]string{"apps.budget.set", "budget.set", "apps.delete"}, svc.WorkspaceAdmin, true},
		{[]string{bridge.MethodAppsCreate}, svc.WorkspaceMember, false},
		{[]string{bridge.MethodMembersList, "connections.allow"}, svc.WorkspaceMember, false},
	}
	for _, row := range rows {
		for _, method := range row.methods {
			for _, role := range []svc.WorkspaceRole{svc.WorkspaceOwner, svc.WorkspaceAdmin, svc.WorkspaceMember} {
				for _, appRole := range []svc.Role{svc.RoleEditor, svc.RoleUser, svc.RoleViewer} {
					t.Run(method+"/"+string(role)+"/"+string(appRole), func(t *testing.T) {
						scope := svc.Scope{Caller: svc.Caller{Member: "raj", Space: "acme", WS: role, Role: appRole}}
						if row.app {
							scope.App = "crm"
						}
						err := allowSpaces(scope, method)
						if err == nil {
							err = denyWorkspaceApp(scope, method, nil)
						}
						wantAllowed := role == svc.WorkspaceOwner || row.minimum == svc.WorkspaceMember || role == row.minimum
						if (err == nil) != wantAllowed {
							t.Fatalf("error=%v wantAllowed=%v", err, wantAllowed)
						}
					})
				}
			}
		}
	}
}

func TestWorkspaceTopicRoleTable(t *testing.T) {
	for _, method := range []string{"sub", "unsub"} {
		for _, topic := range []string{"app/crm/records/deal", "app/crm/runs", "app/crm/agent/t1", "app/crm/roles", "app/crm/build", "app/crm/shape/raj", "app/crm/*"} {
			for _, role := range []svc.Role{"", svc.RoleViewer, svc.RoleUser, svc.RoleEditor} {
				t.Run(method+"/"+topic+"/"+string(role), func(t *testing.T) {
					raw, _ := json.Marshal(map[string]string{"topic": topic})
					scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Space: "acme", WS: svc.WorkspaceMember, Role: role}}
					err := denyWorkspaceApp(scope, method, raw)
					writeOnly := topic == "app/crm/build" || topic == "app/crm/shape/raj" || topic == "app/crm/*"
					wantDenied := role == "" || writeOnly && role != svc.RoleEditor
					if (err != nil) != wantDenied {
						t.Fatalf("error=%v wantDenied=%v", err, wantDenied)
					}
				})
			}
		}
	}
}

func TestAdminMembersRepairWithoutAppRole(t *testing.T) {
	for _, ws := range []svc.WorkspaceRole{svc.WorkspaceOwner, svc.WorkspaceAdmin} {
		for _, method := range []string{bridge.MethodAppRolesList, bridge.MethodAppRolesSet, bridge.MethodAppRolesAsk} {
			scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "mona", Space: "acme", WS: ws}}
			if err := allowSpaces(scope, method); err != nil {
				t.Fatal(err)
			}
			if err := denyWorkspaceApp(scope, method, nil); err != nil {
				t.Fatal(err)
			}
			if err := SpacesAllow(false)(context.Background(), scope.Caller, scope, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestWorkspaceAgentPermissionPersonalAndAgentsUnchanged(t *testing.T) {
	for _, space := range []string{"", svc.PersonalSpace, "acme"} {
		for _, role := range []svc.Role{svc.RoleEditor, svc.RoleUser, svc.RoleViewer, ""} {
			for _, agent := range []bool{false, true} {
				scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Space: space, Role: role, Agent: agent}}
				got := AllowWorkspaceAgent(context.Background(), scope.Caller, scope, nil)
				expected := allowRole(scope.Caller, bridge.MethodAgentSend)
				if space == "acme" && !agent && role.AtLeast(svc.RoleViewer) {
					expected = nil
				}
				if (got == nil) != (expected == nil) {
					t.Fatalf("space=%s role=%s agent=%v got=%v expected=%v", space, role, agent, got, expected)
				}
			}
		}
	}
}

func TestNoRoleDeniedBeforeAnyMethodPermission(t *testing.T) {
	for _, method := range []string{bridge.MethodRecordsQuery, bridge.MethodAppsGet, "future.track.method"} {
		_, denies := registeredPermission(method, nil)
		resetPermissions(t)
		for _, deny := range denies {
			AddDenyRule(deny)
		}
		called := false
		RegisterAllow(method, func(context.Context, svc.Caller, svc.Scope, json.RawMessage) error { called = true; return nil })
		checker := New(svc.Config{Root: t.TempDir()}, nil)
		t.Cleanup(func() {
			if checker.audit.fallback != nil {
				_ = checker.audit.fallback.Close()
			}
		})
		scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Space: "acme", WS: svc.WorkspaceMember}}
		if err := checker.Allow(context.Background(), scope, method, nil); errorCode(err) != bridge.CodeDenied || called {
			t.Fatal(err, called)
		}
	}
}

func TestWorkspaceGlobalAppsPermission(t *testing.T) {
	for _, method := range []string{bridge.MethodAppsList, bridge.MethodAppsCreate} {
		for _, ws := range []svc.WorkspaceRole{svc.WorkspaceOwner, svc.WorkspaceAdmin, svc.WorkspaceMember, ""} {
			caller := svc.Caller{Member: "raj", Space: "acme", WS: ws}
			err := AllowWorkspaceApps(method)(context.Background(), caller, svc.Scope{Caller: caller}, nil)
			if (err == nil) != (ws != "") {
				t.Fatal(method, ws, err)
			}
		}
		for _, role := range []svc.Role{svc.RoleEditor, svc.RoleUser, svc.RoleViewer, ""} {
			for _, agent := range []bool{false, true} {
				caller := svc.Caller{Member: svc.Owner, Space: svc.PersonalSpace, Role: role, Agent: agent}
				got := AllowWorkspaceApps(method)(context.Background(), caller, svc.Scope{Caller: caller}, nil)
				expected := allowRole(caller, method)
				if (got == nil) != (expected == nil) {
					t.Fatal(method, role, agent, got, expected)
				}
			}
		}
	}
}
