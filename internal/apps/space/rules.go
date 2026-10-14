package space

import (
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// b2 has not landed approles.CanSetWorkspaceRole. These are the same rules
// the board states, kept here until that function exists: only an Owner makes
// or unmakes an Owner, and the last Owner cannot be moved off Owner.

func canInvite(actor svc.WorkspaceRole, role svc.WorkspaceRole) error {
	if actor != svc.WorkspaceOwner && actor != svc.WorkspaceAdmin {
		return denied("this call needs workspace Owner or Admin")
	}
	if role == svc.WorkspaceOwner {
		return invalid("invite someone as Admin or Member")
	}
	if role != svc.WorkspaceAdmin && role != svc.WorkspaceMember {
		return invalid("role must be admin or member")
	}
	return nil
}

func canSetRole(actor, from, to svc.WorkspaceRole, owners int, space string) error {
	if actor != svc.WorkspaceOwner && actor != svc.WorkspaceAdmin {
		return denied("this call needs workspace Owner or Admin")
	}
	switch to {
	case svc.WorkspaceOwner, svc.WorkspaceAdmin, svc.WorkspaceMember:
	default:
		return invalid("role must be owner, admin, or member")
	}
	if from == to {
		return nil
	}
	if (from == svc.WorkspaceOwner || to == svc.WorkspaceOwner) && actor != svc.WorkspaceOwner {
		return denied("only an Owner can make or unmake an Owner")
	}
	if from == svc.WorkspaceOwner && to != svc.WorkspaceOwner && owners <= 1 {
		return errLastOwnerOf(space)
	}
	return nil
}

func canRemove(actor, target svc.WorkspaceRole, owners int, self bool, space string) error {
	if self {
		return nil
	}
	if actor != svc.WorkspaceOwner && actor != svc.WorkspaceAdmin {
		return denied("this call needs workspace Owner or Admin")
	}
	if target == svc.WorkspaceOwner && actor != svc.WorkspaceOwner {
		return denied("only an Owner can make or unmake an Owner")
	}
	if target == svc.WorkspaceOwner && owners <= 1 {
		return errLastOwnerOf(space)
	}
	return nil
}

func errLastOwnerOf(name string) *bridge.Error {
	return conflict("You're the last Owner. Make someone else Owner first, or delete " + name + " in General.")
}
