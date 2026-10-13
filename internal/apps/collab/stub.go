package collab

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service reserves the surface service until its implementation lands.
type Service struct{}

var _ svc.Collab = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) Lock(context.Context, svc.Scope, bridge.LockPartParams) (bridge.Lock, error) {
	return bridge.Lock{}, bridge.NotBuilt("collab")
}

func (*Service) Takeover(context.Context, svc.Scope, bridge.TakeoverPartParams) (bridge.Lock, error) {
	return bridge.Lock{}, bridge.NotBuilt("collab")
}

func (*Service) Answer(context.Context, svc.Scope, bridge.AnswerPartParams) (bridge.Lock, error) {
	return bridge.Lock{}, bridge.NotBuilt("collab")
}

func (*Service) Release(context.Context, svc.Scope, bridge.ReleasePartParams) (bridge.Empty, error) {
	return bridge.Empty{}, bridge.NotBuilt("collab")
}

func (*Service) SetPresence(context.Context, svc.Scope, bridge.SetPresenceParams) (bridge.Empty, error) {
	return bridge.Empty{}, bridge.NotBuilt("collab")
}

func (*Service) ListComments(context.Context, svc.Scope, bridge.ListCommentsParams) (bridge.ListCommentsResult, error) {
	return bridge.ListCommentsResult{}, bridge.NotBuilt("collab")
}

func (*Service) AddComment(context.Context, svc.Scope, bridge.AddCommentParams) (bridge.Comment, error) {
	return bridge.Comment{}, bridge.NotBuilt("collab")
}

func (*Service) ResolveComment(context.Context, svc.Scope, bridge.ResolveCommentParams) (bridge.Comment, error) {
	return bridge.Comment{}, bridge.NotBuilt("collab")
}

func (*Service) Comment(context.Context, svc.Scope, bridge.SendCommentParams) (bridge.Comment, error) {
	return bridge.Comment{}, bridge.NotBuilt("collab")
}

func (*Service) Holds(svc.Scope, string, string) bool { return false }

func (*Service) Dropped(string) {}
