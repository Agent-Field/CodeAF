package approles

import (
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"testing"
)

func TestWorkspaceRoleMatrix(t *testing.T) {
	roles := []svc.WorkspaceRole{svc.WorkspaceOwner, svc.WorkspaceAdmin, svc.WorkspaceMember}
	for _, actorRole := range roles {
		for _, from := range roles {
			for _, to := range roles {
				for _, owners := range []int{1, 2} {
					name := string(actorRole) + "/" + string(from) + "/" + string(to) + "/owners=" + string(rune('0'+owners))
					t.Run(name, func(t *testing.T) {
						actor := svc.Caller{Member: "mona", WS: actorRole}
						code := bridge.ErrorCode("")
						if actorRole == svc.WorkspaceMember || (actorRole == svc.WorkspaceAdmin && (from == svc.WorkspaceOwner || to == svc.WorkspaceOwner)) {
							code = bridge.CodeDenied
						} else if from == svc.WorkspaceOwner && to != svc.WorkspaceOwner && owners == 1 {
							code = bridge.CodeConflict
						}
						requireCode(t, CanSetWorkspaceRole(actor, WorkspaceRoleTarget{Member: "raj", Owners: owners}, from, to), code)
					})
				}
			}
		}
	}
}

func TestWorkspaceRoleInvalidAndAgent(t *testing.T) {
	for _, tt := range []struct {
		actor    svc.Caller
		target   WorkspaceRoleTarget
		from, to svc.WorkspaceRole
		code     bridge.ErrorCode
	}{
		{svc.PersonalCaller(), WorkspaceRoleTarget{Member: "raj", Owners: 1}, "", svc.WorkspaceMember, bridge.CodeInvalid},
		{svc.PersonalCaller(), WorkspaceRoleTarget{}, svc.WorkspaceMember, svc.WorkspaceAdmin, bridge.CodeInvalid},
		{svc.Caller{Member: "agent", WS: svc.WorkspaceOwner, Agent: true}, WorkspaceRoleTarget{Member: "raj"}, svc.WorkspaceMember, svc.WorkspaceAdmin, bridge.CodeDenied},
		{svc.Caller{WS: svc.WorkspaceOwner}, WorkspaceRoleTarget{Member: "raj"}, svc.WorkspaceMember, svc.WorkspaceAdmin, bridge.CodeDenied},
	} {
		requireCode(t, CanSetWorkspaceRole(tt.actor, tt.target, tt.from, tt.to), tt.code)
	}
}
