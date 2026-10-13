package prebuilt

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{}

var _ svc.Prebuilt = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) List(context.Context, svc.Caller) (bridge.PrebuiltResult, error) {
	return bridge.PrebuiltResult{}, bridge.NotBuilt("prebuilt")
}
