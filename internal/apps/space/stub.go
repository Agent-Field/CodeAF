package space

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// MemberService is the personal space: one member, and no invites.
type MemberService struct{}

func (*MemberService) Authenticate(context.Context, string) (svc.Caller, error) {
	return svc.Caller{}, bridge.NotBuilt("members.authenticate")
}
func (*MemberService) List(context.Context, svc.Caller) (bridge.MembersListResult, error) {
	return bridge.MembersListResult{Members: []bridge.Member{personalMember()}, Invites: []bridge.Invite{}}, nil
}
func (*MemberService) Invite(context.Context, svc.Caller, bridge.InviteParams) (bridge.Invite, error) {
	return bridge.Invite{}, bridge.NotBuilt("members.invite")
}
func (*MemberService) CancelInvite(context.Context, svc.Caller, bridge.InviteRef) (bridge.Empty, error) {
	return bridge.Empty{}, bridge.NotBuilt("members.invite.cancel")
}
func (*MemberService) SetRole(context.Context, svc.Caller, bridge.MemberRoleParams) (bridge.Member, error) {
	return bridge.Member{}, bridge.NotBuilt("members.role")
}
func (*MemberService) Remove(context.Context, svc.Caller, bridge.MemberRef) (bridge.Empty, error) {
	return bridge.Empty{}, bridge.NotBuilt("members.remove")
}
func (*MemberService) Leave(context.Context, svc.Caller) (bridge.Empty, error) {
	return bridge.Empty{}, bridge.NotBuilt("members.leave")
}
