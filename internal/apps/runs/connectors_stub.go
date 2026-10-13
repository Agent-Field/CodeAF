package runs

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Connectors is the stub connector door. B4 replaces this file with fakes.
type Connectors struct{}

var _ svc.Connectors = (*Connectors)(nil)

func NewConnectors(svc.Config, *svc.Services) *Connectors { return &Connectors{} }

func (*Connectors) Call(context.Context, svc.Scope, string, map[string]any) (any, error) {
	return nil, bridge.NotBuilt("connectors")
}

// Outbound is conservative until built: every tool counts as outbound.
func (*Connectors) Outbound(string) bool { return true }
