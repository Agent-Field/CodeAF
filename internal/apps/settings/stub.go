package settings

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"time"
)

type Service struct{}

var _ svc.Settings = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) ListTriggers(context.Context, svc.Scope) (bridge.ListTriggersResult, error) {
	return bridge.ListTriggersResult{}, bridge.NotBuilt("settings")
}
func (*Service) PauseTrigger(context.Context, svc.Scope, bridge.PauseTriggerParams) (bridge.TriggerInfo, error) {
	return bridge.TriggerInfo{}, bridge.NotBuilt("settings")
}
func (*Service) GetBudget(context.Context, svc.Scope) (bridge.BudgetInfo, error) {
	return bridge.BudgetInfo{}, bridge.NotBuilt("settings")
}
func (*Service) SetBudget(context.Context, svc.Scope, bridge.SetBudgetParams) (bridge.BudgetInfo, error) {
	return bridge.BudgetInfo{}, bridge.NotBuilt("settings")
}

var _ svc.TriggerGate = (*Service)(nil)
var _ svc.BudgetCap = (*Service)(nil)

func (*Service) Allow(string, string, time.Time) bool { return true }
func (*Service) Month(string) (float64, bool)         { return 0, false }
