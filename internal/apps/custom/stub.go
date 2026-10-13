package custom

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{}

var _ svc.Custom = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) Bundle(context.Context, svc.Scope, bridge.CustomBundleParams) (bridge.CustomBundleResult, error) {
	return bridge.CustomBundleResult{}, bridge.NotBuilt("custom")
}
