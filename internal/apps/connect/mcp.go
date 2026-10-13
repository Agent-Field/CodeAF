package connect

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	native "github.com/Agent-Field/codeaf/internal/connect"
	"strings"
)

type mcpAdapter struct{ clients *nativeClients }

func (a mcpAdapter) Call(ctx context.Context, c svc.Credential, verb string, args map[string]any) (any, error) {
	manager, err := a.clients.manager(ctx, c)
	if err != nil {
		return nil, err
	}
	return callMCP(ctx, manager, c, verb, args, a.clients.s.rememberMCP)
}

type mcpPeer interface {
	MCPService(string) bool
	MCPTools(context.Context, string) ([]native.MCPTool, error)
	MCPCall(context.Context, string, string, json.RawMessage) (string, error)
}

func callMCP(ctx context.Context, manager mcpPeer, c svc.Credential, verb string, args map[string]any, observe func(string, []native.MCPTool)) (any, error) {
	service := mcpService(manager, c.Service)
	if service == "" {
		return nil, bridge.Errorf(bridge.CodeInvalid, "no adapter")
	}
	tools, err := manager.MCPTools(ctx, service)
	if err != nil {
		return nil, err
	}
	if observe != nil {
		observe(c.Service, tools)
	}
	for _, tool := range tools {
		if tool.Name != verb {
			continue
		}
		if !tool.ReadOnly && !Permitted(ctx) {
			return nil, bridge.Errorf(bridge.CodeDenied, "outbound needs approval")
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		return manager.MCPCall(ctx, service, verb, raw)
	}
	return nil, noVerb(verb)
}
func mcpService(manager mcpPeer, service string) string {
	for _, id := range []string{service, "mcp:" + strings.TrimPrefix(service, "mcp:")} {
		if manager.MCPService(id) {
			return id
		}
	}
	return ""
}

func (s *Service) rememberMCP(service string, tools []native.MCPTool) {
	for _, tool := range tools {
		s.mcpOutbound.Store(service+"."+tool.Name, !tool.ReadOnly)
	}
}
