package check

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the stub Check. B6 replaces this file.
type Service struct{}

var _ svc.Check = (*Service)(nil)

func New(svc.Config, *svc.Services) *Service { return &Service{} }

func (*Service) Run(context.Context, string, spec.Files, int) (bridge.CheckReport, error) {
	return bridge.CheckReport{}, bridge.NotBuilt("check")
}
