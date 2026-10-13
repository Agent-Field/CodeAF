package runs

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Connectors is the wave-1 fake door. Every call is recorded and answered
// with a canned reply. Outbound verbs refuse unless the run held an approval.
type Connectors struct {
	eng *engine
}

var _ svc.Connectors = (*Connectors)(nil)

// NewConnectors returns the fake connector door.
func NewConnectors(cfg svc.Config, all *svc.Services) *Connectors {
	return &Connectors{eng: shared(cfg, all)}
}

// Call records the call and returns the canned reply.
func (c *Connectors) Call(ctx context.Context, scope svc.Scope, tool string, args map[string]any) (any, error) {
	return c.eng.callConnector(ctx, scope, tool, args)
}

// Outbound reports send, post and pay. Those wait on a send gate.
func (c *Connectors) Outbound(tool string) bool { return outboundVerb(tool) }

func (e *engine) callConnector(ctx context.Context, scope svc.Scope, tool string, args map[string]any) (any, error) {
	return e.connectorCall(ctx, scope, tool, args)
}
