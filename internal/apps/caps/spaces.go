package caps

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() {
	AddDenyRule(func(_ context.Context, _ svc.Caller, scope svc.Scope, method string, raw json.RawMessage) error {
		if err := allowSpaces(scope, method); err != nil {
			return err
		}
		return denyWorkspaceApp(scope, method, raw)
	})
}

func allowSpaces(scope svc.Scope, method string) error {
	if method == bridge.MethodAppsMove && scope.Caller.Space != "" && scope.Caller.Space != svc.PersonalSpace {
		return denied("Move to workspace is for Personal apps")
	}
	switch method {
	case bridge.MethodMembersInvite, bridge.MethodMembersInviteCancel, bridge.MethodMembersInviteResend,
		bridge.MethodMembersRole, bridge.MethodMembersRemove, bridge.MethodAuditList:
		if !scope.Caller.IsAdmin() {
			return denied("this call needs workspace Owner or Admin")
		}
	}
	return allowSpacesAppRole(scope.Caller, method)
}

func allowSpacesAppRole(caller svc.Caller, method string) error {
	switch method {
	case bridge.MethodAppRolesAsk:
		if !caller.Agent && (caller.Role == svc.RoleUser || caller.Role == svc.RoleViewer || caller.IsAdmin()) {
			return nil
		}
	case bridge.MethodAppRolesSet:
		if !caller.Agent && (caller.Role == svc.RoleEditor || caller.IsAdmin()) {
			return nil
		}
	case bridge.MethodSignoffRolesSet, bridge.MethodAppsMove:
		if caller.CanShape() {
			return nil
		}
	case bridge.MethodSignoffSign:
		if caller.Role.AtLeast(svc.RoleUser) {
			return nil
		}
	default:
		return nil
	}
	return denied("the app role does not allow this call")
}

// SpacesAllow grants the spaces seam only after shared checks and deny rules have run.
func SpacesAllow(global bool) AllowFunc {
	return func(_ context.Context, caller svc.Caller, scope svc.Scope, _ json.RawMessage) error {
		if global {
			return nil
		}
		if scope.App == "" {
			return bridge.Errorf(bridge.CodeInvalid, "this method needs app")
		}
		if !caller.Agent && caller.IsAdmin() {
			return nil
		}
		switch caller.Role {
		case svc.RoleEditor, svc.RoleUser, svc.RoleViewer:
			return nil
		}
		return bridge.Errorf(bridge.CodeDenied, "the app role does not allow this call")
	}
}

// AllowSpacesTopic validates exact topics behind the registered spaces prefixes.
func AllowSpacesTopic(ctx context.Context, _ svc.Caller, scope svc.Scope, raw json.RawMessage) error {
	var p struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return bridge.Errorf(bridge.CodeInvalid, "topic must be a string")
	}
	if strings.HasPrefix(p.Topic, "space/") {
		if scope.App != "" {
			return bridge.Errorf(bridge.CodeInvalid, "space topics take no app")
		}
		switch p.Topic {
		case bridge.TopicSpaceMembers, bridge.TopicSpaceActivity, bridge.TopicSpaceWaiting:
			return nil
		}
	} else if scope.App != "" && (p.Topic == bridge.TopicRoles(scope.App) || p.Topic == bridge.TopicSignoff(scope.App)) {
		return SpacesAllow(false)(ctx, scope.Caller, scope, raw)
	}
	return bridge.Errorf(bridge.CodeInvalid, "unknown space topic")
}

// A deny rule can constrain a registered permission but cannot replace wave-1 allows.
func denyWorkspaceApp(scope svc.Scope, method string, raw json.RawMessage) error {
	caller := scope.Caller
	if caller.Space == "" || caller.Space == svc.PersonalSpace {
		return nil
	}
	app := scope.App
	if method == "sub" || method == "unsub" {
		var topic struct {
			Topic string `json:"topic"`
		}
		if err := json.Unmarshal(object(raw), &topic); err != nil {
			return bridge.Errorf(bridge.CodeInvalid, "topic must be a string")
		}
		parts := strings.Split(topic.Topic, "/")
		if len(parts) >= 3 && parts[0] == "app" {
			app = parts[1]
		}
	}
	if app == "" {
		return denyWorkspaceAction(caller, method)
	}
	// Members is the repair door for administrators who have no role on an app.
	if strings.HasPrefix(method, "approles.") && caller.IsAdmin() && !caller.Agent {
		return nil
	}
	if !caller.Role.AtLeast(svc.RoleViewer) {
		return denied("you do not have access to this app")
	}
	if method == "sub" || method == "unsub" {
		return denyWorkspaceTopic(caller, raw)
	}
	if err := denyWorkspaceAction(caller, method); err != nil {
		return err
	}
	if caller.Agent {
		return nil
	}
	if editorAction(method) && !caller.CanShape() {
		return denied("only Editors can change the app")
	}
	if caller.Role == svc.RoleViewer && !viewerAction(method) {
		return denied("Viewers can read and ask the app agent")
	}
	return nil
}

func denyWorkspaceAction(caller svc.Caller, method string) error {
	switch method {
	case "workspace.pay", "workspace.plan", "workspace.delete", "space.delete":
		if caller.Agent || caller.WS != svc.WorkspaceOwner {
			return denied("this call needs workspace Owner")
		}
	case "connections.add", "connections.remove", "workspace.connections.add", "workspace.connections.remove", "budget.set", "apps.budget.set", "apps.delete":
		if caller.Agent || !caller.IsAdmin() {
			return denied("this call needs workspace Owner or Admin")
		}
	}
	return nil
}

func editorAction(method string) bool {
	if definitionMethod(method) && method != bridge.MethodAppsCreate {
		return true
	}
	if strings.HasPrefix(method, "shape.") {
		return method != bridge.MethodShapeAsk
	}
	if strings.HasPrefix(method, "triggers.") || strings.HasPrefix(method, "needs.") {
		return true
	}
	switch method {
	case bridge.MethodVersionsRestore, bridge.MethodSignoffRolesSet, bridge.MethodAppRolesSet, bridge.MethodAppsMove,
		"apps.archive", "apps.restore", "apps.undo", "apps.apply", "members.signatories.set":
		return true
	}
	return false
}

func viewerAction(method string) bool {
	switch method {
	case bridge.MethodAppsGet, bridge.MethodRecordsQuery, bridge.MethodRecordsGet,
		bridge.MethodRunsGet, bridge.MethodRunsList, bridge.MethodApprovalsList, bridge.MethodAgentSend,
		bridge.MethodAppRolesList, bridge.MethodAppRolesAsk, bridge.MethodSignoffRoles, bridge.MethodSignoffState,
		bridge.MethodShapeAsk, bridge.MethodVersionsList, bridge.MethodVersionsDiff, bridge.MethodAppsHealth,
		"content.get", "content.list", "parts.get", "parts.list", "comments.list", "apps.export", "export.pdf", "export.pptx", "export.csv", "apps.present", "apps.budget.set", "budget.set", "apps.delete":
		return true
	}
	return false
}

func denyWorkspaceTopic(caller svc.Caller, raw json.RawMessage) error {
	if caller.Agent {
		return nil
	}
	var p struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(object(raw), &p); err != nil {
		return bridge.Errorf(bridge.CodeInvalid, "topic must be a string")
	}
	parts := strings.Split(p.Topic, "/")
	if len(parts) < 3 || parts[0] != "app" {
		return nil
	}
	switch parts[2] {
	case "build", "shape", "*":
		if !caller.CanShape() {
			return denied("this topic needs an Editor")
		}
	}
	return nil
}

// AllowWorkspaceAgent is the per-method gateway permission for agent.send.
// The method owner must register it: deny-only rules cannot grant a Viewer read access.
func AllowWorkspaceAgent(_ context.Context, caller svc.Caller, scope svc.Scope, _ json.RawMessage) error {
	if scope.App == "" {
		return bridge.Errorf(bridge.CodeInvalid, "this method needs app")
	}
	if caller.Space == "" || caller.Space == svc.PersonalSpace || caller.Agent {
		return allowRole(caller, bridge.MethodAgentSend)
	}
	if !caller.Role.AtLeast(svc.RoleViewer) {
		return denied("you do not have access to this app")
	}
	return nil
}

// AllowWorkspaceApps is the global apps.list/apps.create permission for the method owner.
// Socket callers have no app role; their workspace membership grants these two actions.
func AllowWorkspaceApps(method string) AllowFunc {
	return func(_ context.Context, caller svc.Caller, _ svc.Scope, _ json.RawMessage) error {
		if method != bridge.MethodAppsList && method != bridge.MethodAppsCreate {
			return denied("this permission only covers listing or making apps")
		}
		if caller.Space == "" || caller.Space == svc.PersonalSpace || caller.Agent {
			return allowRole(caller, method)
		}
		if validWorkspaceMember(caller.WS) {
			return nil
		}
		return denied("workspace membership is required")
	}
}

func validWorkspaceMember(role svc.WorkspaceRole) bool {
	return role == svc.WorkspaceOwner || role == svc.WorkspaceAdmin || role == svc.WorkspaceMember
}
