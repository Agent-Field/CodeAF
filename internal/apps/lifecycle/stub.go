package lifecycle

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{}

var _ svc.Lifecycle = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) Archive(context.Context, svc.Scope) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("lifecycle")
}
func (*Service) Restore(context.Context, svc.Scope) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("lifecycle")
}
func (*Service) Delete(context.Context, svc.Scope, bridge.DeleteAppParams) error {
	return bridge.NotBuilt("lifecycle")
}
func (*Service) Copy(context.Context, svc.Scope, bridge.CopyAppParams) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("lifecycle")
}
func (*Service) List(context.Context, svc.Caller) (bridge.LifecycleResult, error) {
	return bridge.LifecycleResult{}, bridge.NotBuilt("lifecycle")
}

// Wrap preserves the store until lifecycle filtering is built.
func Wrap(store svc.Store) svc.Store { return store }
