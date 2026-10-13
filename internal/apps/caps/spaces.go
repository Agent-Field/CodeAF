package caps

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func allowSpaces(scope svc.Scope, method string) error {
	switch method {
	case bridge.MethodMembersInvite, bridge.MethodMembersInviteCancel, bridge.MethodMembersInviteResend,
		bridge.MethodMembersRole, bridge.MethodMembersRemove, bridge.MethodAuditList:
		if !scope.Caller.IsAdmin() {
			return denied("this call needs workspace Owner or Admin")
		}
	}
	return nil
}

// New seams bypass wave-1 capability lookup; their services remain Personal stubs.
func checkSpaces(scope svc.Scope, method string, raw json.RawMessage) (bool, error) {
	if !spacesMethod(method) && !spacesTopic(scope, method, raw) {
		return false, nil
	}
	if err := allowSpaces(scope, method); err != nil {
		return true, err
	}
	var params map[string]json.RawMessage
	if len(raw) != 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &params); err != nil {
			return true, bridge.Errorf(bridge.CodeInvalid, "params must be an object")
		}
	}
	return true, nil
}

func spacesMethod(method string) bool {
	switch method {
	case bridge.MethodSpaceInfo, bridge.MethodSpacesList, bridge.MethodSpacesJoin,
		bridge.MethodSpacesSignin, bridge.MethodSpacesConnect, bridge.MethodSpacesNew, bridge.MethodSpacesForget,
		bridge.MethodMembersList, bridge.MethodMembersInvite, bridge.MethodMembersInviteCancel,
		bridge.MethodMembersInviteResend, bridge.MethodMembersRole, bridge.MethodMembersRemove, bridge.MethodMembersLeave,
		bridge.MethodAppRolesList, bridge.MethodAppRolesSet, bridge.MethodAppRolesAsk,
		bridge.MethodSignoffRoles, bridge.MethodSignoffRolesSet, bridge.MethodSignoffState, bridge.MethodSignoffSign,
		bridge.MethodAuditList, bridge.MethodActivityList, bridge.MethodAppsMove:
		return true
	}
	return false
}

func spacesTopic(scope svc.Scope, method string, raw json.RawMessage) bool {
	if method != "sub" && method != "unsub" {
		return false
	}
	var p struct {
		Topic string `json:"topic"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	if scope.App != "" {
		return p.Topic == bridge.TopicRoles(scope.App) || p.Topic == bridge.TopicSignoff(scope.App)
	}
	return p.Topic == bridge.TopicSpaceMembers || p.Topic == bridge.TopicSpaceActivity || p.Topic == bridge.TopicSpaceWaiting
}
