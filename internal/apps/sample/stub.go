package sample

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the stub sample generator. B2 replaces this file.
type Service struct{}

var _ svc.Sample = (*Service)(nil)

func New(svc.Config, *svc.Services) *Service { return &Service{} }

func (*Service) Generate(context.Context, *spec.Bundle) (map[string][]bridge.Record, error) {
	return nil, bridge.NotBuilt("sample")
}
