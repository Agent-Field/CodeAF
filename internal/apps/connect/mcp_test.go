package connect

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	native "github.com/Agent-Field/codeaf/internal/connect"
	"golang.org/x/oauth2"
)

type mcpStub struct {
	configured    string
	tools         []native.MCPTool
	calls         int
	service, verb string
	args          json.RawMessage
}

func (m *mcpStub) MCPService(id string) bool                                  { return id == m.configured }
func (m *mcpStub) MCPTools(context.Context, string) ([]native.MCPTool, error) { return m.tools, nil }
func (m *mcpStub) MCPCall(_ context.Context, service, verb string, args json.RawMessage) (string, error) {
	m.calls++
	m.service, m.verb, m.args = service, verb, args
	return "MCP reply", nil
}
func TestMCPAnnotationsRequirePermitForActs(t *testing.T) {
	for _, service := range []string{"mcp:custom", "github", "stripe", "airtable", "hubspot"} {
		for _, readOnly := range []bool{true, false} {
			for _, permit := range []bool{true, false} {
				t.Run(service+"/"+map[bool]string{true: "read", false: "act"}[readOnly]+"/"+map[bool]string{true: "permit", false: "no-permit"}[permit], func(t *testing.T) {
					configured := "mcp:" + service
					if service == "mcp:custom" {
						configured = service
					}
					m := &mcpStub{configured: configured, tools: []native.MCPTool{{Name: "tool", ReadOnly: readOnly}}}
					ctx := context.Background()
					if permit {
						ctx = WithPermit(ctx)
					}
					out, err := callMCP(ctx, m, svc.Credential{Service: service}, "tool", map[string]any{"value": "team"}, nil)
					if !readOnly && !permit {
						var typed *bridge.Error
						if !errors.As(err, &typed) || typed.Code != bridge.CodeDenied || m.calls != 0 {
							t.Fatalf("err=%v calls=%d", err, m.calls)
						}
						return
					}
					if err != nil || out != "MCP reply" || m.calls != 1 || m.service != configured || m.verb != "tool" || string(m.args) != `{"value":"team"}` {
						t.Fatalf("out=%v err=%v peer=%+v", out, err, m)
					}
				})
			}
		}
	}
}
func TestMCPMissingConfigurationHasNoAdapter(t *testing.T) {
	for _, service := range []string{"github", "stripe", "airtable", "hubspot", "mcp:absent"} {
		t.Run(service, func(t *testing.T) {
			m := &mcpStub{}
			_, err := callMCP(context.Background(), m, svc.Credential{Service: service}, "read", nil, nil)
			var typed *bridge.Error
			if !errors.As(err, &typed) || typed.Code != bridge.CodeInvalid || typed.Message != "no adapter" || m.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, m.calls)
			}
		})
	}
}
func TestNativeExpiryShapes(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		expired bool
	}{
		{"client expiry", errors.New("Google has to be connected again"), true},
		{"MCP expiry", errors.New("Notion is not connected"), true},
		{"Slack revoked", errors.New("Slack has to be connected again"), true},
		{"renewal refused", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}, true},
		{"API expired", errors.New("401 Unauthorized: invalid token"), true},
		{"forbidden", errors.New("403 Forbidden: missing scope"), false},
		{"network", errors.New("connection refused"), false},
		{"healthy", nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := expiredError(tt.err); got != tt.expired {
				t.Fatalf("expired=%v want %v", got, tt.expired)
			}
		})
	}
}

func TestMCPOutboundUsesKnownAnnotations(t *testing.T) {
	s, _, _, _, _ := testDoor(t)
	s.rememberMCP("mcp:custom", []native.MCPTool{{Name: "read", ReadOnly: true}, {Name: "erase"}})
	for _, tt := range []struct {
		tool     string
		outbound bool
	}{{"mcp:custom.read", false}, {"mcp:custom.erase", true}, {"mcp:custom.unknown", true}, {"gmail.send", true}, {"gmail.read", false}} {
		t.Run(tt.tool, func(t *testing.T) {
			if got := s.Outbound(tt.tool); got != tt.outbound {
				t.Fatalf("outbound=%v want %v", got, tt.outbound)
			}
		})
	}
	_, err := s.Call(context.Background(), testScope(svc.DataSample), "mcp:custom.erase", nil)
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != bridge.CodeDenied {
		t.Fatal(err)
	}
}
