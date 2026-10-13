// Package connect is the app connector door: non-real data always uses fakes.
package connect

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type adapter interface {
	Call(context.Context, svc.Credential, string, map[string]any) (any, error)
}

type Service struct {
	cfg         svc.Config
	all         *svc.Services
	now         func() time.Time
	adapters    map[string]adapter
	fakes       map[string]func(string) any
	mu          sync.Mutex
	calls       []bridge.FakeCall
	waits       *waitStore
	mcpOutbound sync.Map
	resumeMu    sync.Mutex
}

var _ svc.Connectors = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return NewWithClock(cfg, all, time.Now) }

// NewWithClock lets callers advance durable deadlines without sleeping.
func NewWithClock(cfg svc.Config, all *svc.Services, now func() time.Time) *Service {
	s := &Service{cfg: cfg, all: all, now: now, calls: []bridge.FakeCall{}, waits: newWaitStore(cfg.Root)}
	s.fakes = fakeRegistry()
	native := &nativeClients{s: s}
	s.adapters = map[string]adapter{"gmail": gmailAdapter{native}, "calendar": calendarAdapter{native}, "slack": slackAdapter{native}}
	remote := mcpAdapter{native}
	for _, name := range []string{"mcp", "github", "stripe", "airtable", "hubspot"} {
		s.adapters[name] = remote
	}
	if cfg.Ctx != nil {
		go s.watch(cfg.Ctx)
	}
	return s
}

func (s *Service) Call(ctx context.Context, scope svc.Scope, tool string, args map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, bridge.Errorf(bridge.CodeInvalid, "connector arguments must be JSON")
	}
	if s.all != nil && s.all.Caps != nil {
		if err := s.all.Caps.Allow(ctx, scope, tool, raw); err != nil {
			return nil, err
		}
	}
	service, verb, ok := strings.Cut(tool, ".")
	if !ok || service == "" || verb == "" {
		return nil, bridge.Errorf(bridge.CodeInvalid, "connector tool must name a service and verb")
	}
	annotation, known := s.mcpOutbound.Load(tool)
	if (outboundTool(tool) || known && annotation.(bool)) && !Permitted(ctx) {
		return nil, bridge.Errorf(bridge.CodeDenied, "outbound needs approval")
	}
	if scope.Data != svc.DataReal {
		return s.fake(ctx, scope, service, verb, tool, args)
	}
	if s.all == nil || s.all.Connect.Grants == nil {
		return nil, bridge.Errorf(bridge.CodeInternal, "connections are unavailable")
	}
	credential, err := s.all.Connect.Grants.Resolve(ctx, scope, service)
	if err != nil {
		return nil, s.waitError(ctx, scope, service, err)
	}
	if credential.Service == "" {
		credential.Service = service
	}
	target := s.adapters[serviceKey(service)]
	if target == nil {
		return nil, bridge.Errorf(bridge.CodeInvalid, "no adapter")
	}
	out, err := target.Call(context.WithValue(ctx, scopeKey{}, scope), credential, verb, args)
	if expiredError(err) {
		return nil, s.waitError(ctx, scope, service, connectionError(bridge.ReasonExpired))
	}
	return out, err
}

func serviceKey(service string) string {
	if strings.HasPrefix(service, "mcp:") {
		return "mcp"
	}
	return service
}

var outbound = map[string]bool{"send": true, "post": true, "reply": true, "pay": true, "charge": true, "create": true, "create-external": true}

func outboundTool(tool string) bool {
	_, verb, _ := strings.Cut(tool, ".")
	_, last, _ := strings.Cut(verb, ".")
	return outbound[verb] || outbound[last]
}

func (s *Service) Outbound(tool string) bool {
	if outboundTool(tool) {
		return true
	}
	if act, known := s.mcpOutbound.Load(tool); known {
		return act.(bool)
	}
	service, _, _ := strings.Cut(tool, ".")
	remote := map[string]bool{"mcp": true, "github": true, "stripe": true, "airtable": true, "hubspot": true}
	// Unknown MCP tools retain the act floor until their annotations have been read.
	return remote[serviceKey(service)]
}

type scopeKey struct{}
type permitKey struct{}
type invocationKey struct{}
type Invocation struct{ RunID, Step string }

// WithPermit is used only after the runs engine has a send approval.
func WithPermit(ctx context.Context) context.Context {
	return context.WithValue(ctx, permitKey{}, true)
}
func Permitted(ctx context.Context) bool { yes, _ := ctx.Value(permitKey{}).(bool); return yes }
func WithInvocation(ctx context.Context, runID, step string) context.Context {
	return context.WithValue(ctx, invocationKey{}, Invocation{runID, step})
}
func invocation(ctx context.Context) Invocation {
	v, _ := ctx.Value(invocationKey{}).(Invocation)
	return v
}
