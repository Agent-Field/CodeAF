package space

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{ all *svc.Services }

func New(_ svc.Config, all *svc.Services) *Service { return &Service{all: all} }

var _ svc.Spaces = (*Service)(nil)
var _ svc.Members = (*MemberService)(nil)

func personalMember() bridge.Member {
	return bridge.Member{ID: svc.Owner, Name: svc.Owner, Role: string(svc.WorkspaceOwner)}
}
func (s *Service) Info(ctx context.Context, _ svc.Caller) (bridge.SpaceInfo, error) {
	apps, err := s.all.Store.List(ctx, svc.PersonalCaller())
	return bridge.SpaceInfo{ID: svc.PersonalSpace, Name: "Personal", Kind: "personal", Me: personalMember(), Apps: len(apps)}, err
}
func (s *Service) List(ctx context.Context, caller svc.Caller) (bridge.SpacesListResult, error) {
	info, err := s.Info(ctx, caller)
	return bridge.SpacesListResult{Spaces: []bridge.SpaceEntry{{ID: info.ID, Name: info.Name, Address: info.Address, Role: string(svc.WorkspaceOwner), Apps: info.Apps}}}, err
}

// MemberService keeps the Members seam separate because Spaces.List has a different result.
type MemberService struct{}

func (s *Service) Members() *MemberService { return &MemberService{} }

func (*MemberService) Authenticate(context.Context, string) (svc.Caller, error) {
	return svc.Caller{}, bridge.NotBuilt("members.authenticate")
}
func (*MemberService) List(context.Context, svc.Caller) (bridge.MembersListResult, error) {
	return bridge.MembersListResult{Members: []bridge.Member{personalMember()}, Invites: []bridge.Invite{}}, nil
}
func (*Service) Join(context.Context, svc.Caller, bridge.SpaceJoinParams) (bridge.SpaceJoinResult, error) {
	return bridge.SpaceJoinResult{}, bridge.NotBuilt("spaces.join")
}
func (*Service) Connect(context.Context, svc.Caller, bridge.SpaceConnectParams) (bridge.SpaceConnection, error) {
	return bridge.SpaceConnection{}, bridge.NotBuilt("spaces.connect")
}
func (*Service) New(context.Context, svc.Caller, bridge.SpaceNewParams) (bridge.SpaceJoinResult, error) {
	return bridge.SpaceJoinResult{}, bridge.NotBuilt("spaces.new")
}
func (*Service) Forget(context.Context, svc.Caller, bridge.SpaceRef) (bridge.Empty, error) {
	return bridge.Empty{}, bridge.NotBuilt("spaces.forget")
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
