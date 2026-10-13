package apps

import (
	"github.com/Agent-Field/codeaf/internal/apps/crewagent"
	"github.com/Agent-Field/codeaf/internal/apps/crewlink"
	"github.com/Agent-Field/codeaf/internal/apps/custom"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/lifecycle"
	"github.com/Agent-Field/codeaf/internal/apps/prebuilt"
	"github.com/Agent-Field/codeaf/internal/apps/settings"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func wireCrews(cfg svc.Config, all *svc.Services) {
	all.Store = lifecycle.Wrap(all.Store)
	preferences := settings.New(cfg, all)
	all.Crews = svc.CrewsServices{
		Lifecycle:   lifecycle.New(cfg, all),
		Settings:    preferences,
		Links:       crewlink.New(cfg, all),
		Agent:       crewagent.New(cfg, all),
		Prebuilt:    prebuilt.New(cfg, all),
		Custom:      custom.New(cfg, all),
		TriggerGate: preferences,
		BudgetCap:   preferences,
	}
	all.Caps = gateway.CrewsCaps(all.Caps, all)
}
