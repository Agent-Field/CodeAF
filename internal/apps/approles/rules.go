package approles

import (
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// WorkspaceRoleTarget carries the owner count read under b1's membership transaction.
type WorkspaceRoleTarget struct {
	Member string
	Owners int
}

// CanSetWorkspaceRole is pure so b1 can check it inside the membership write transaction.
func CanSetWorkspaceRole(actor svc.Caller, target WorkspaceRoleTarget, from, to svc.WorkspaceRole) error {
	if !validWorkspaceRole(from) || !validWorkspaceRole(to) || target.Member == "" {
		return bridge.Errorf(bridge.CodeInvalid, "member and workspace roles are required")
	}
	if actor.Member == "" || actor.Agent || !actor.IsAdmin() {
		return bridge.Errorf(bridge.CodeDenied, "this call needs workspace Owner or Admin")
	}
	if actor.WS != svc.WorkspaceOwner && (from == svc.WorkspaceOwner || to == svc.WorkspaceOwner) {
		return bridge.Errorf(bridge.CodeDenied, "only an Owner can make or unmake an Owner")
	}
	if from == svc.WorkspaceOwner && to != svc.WorkspaceOwner && target.Owners <= 1 {
		return bridge.Errorf(bridge.CodeConflict, "the last Owner cannot be demoted")
	}
	return nil
}

func validWorkspaceRole(role svc.WorkspaceRole) bool {
	return role == svc.WorkspaceOwner || role == svc.WorkspaceAdmin || role == svc.WorkspaceMember
}
