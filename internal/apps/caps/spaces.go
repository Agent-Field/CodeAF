package caps

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() {
	AddDenyRule(func(_ context.Context, _ svc.Caller, scope svc.Scope, method string, _ json.RawMessage) error {
		return allowSpaces(scope, method)
	})
}

func allowSpaces(scope svc.Scope, method string) error {
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
