package runs

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type permitKey struct{}

func withPermit(ctx context.Context) context.Context {
	return context.WithValue(ctx, permitKey{}, true)
}

func permitted(ctx context.Context) bool {
	ok, _ := ctx.Value(permitKey{}).(bool)
	return ok
}

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
	if outboundVerb(tool) && !permitted(ctx) {
		return nil, bridge.Errorf(bridge.CodeDenied, "outbound needs approval")
	}
	if args == nil {
		args = map[string]any{}
	}
	copied := resolveMap(args, map[string]any{})
	e.mu.Lock()
	e.calls = append(e.calls, recorded{App: scope.App, Tool: tool, Args: copied})
	reply, ok := e.replies[tool]
	e.mu.Unlock()
	if ok {
		return reply, nil
	}
	return canned(tool), nil
}

func canned(tool string) any {
	switch tool {
	case "gmail.search", "gmail.read":
		return map[string]any{"messages": []any{}, "thread": "thread-fake"}
	case "gmail.draft":
		return map[string]any{"id": "draft-fake", "fake": true}
	case "gmail.send":
		return map[string]any{"id": "msg-fake", "thread": "thread-fake", "fake": true}
	default:
		return map[string]any{"ok": true, "fake": true, "tool": tool}
	}
}
