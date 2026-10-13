package connect

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func fakeRegistry() map[string]func(string) any {
	registry := map[string]func(string) any{}
	for _, service := range []string{"gmail", "calendar", "slack", "mcp", "github", "stripe", "airtable", "hubspot"} {
		registry[service] = canned
	}
	return registry
}

func canned(tool string) any {
	replies := map[string]any{
		"gmail.search":  map[string]any{"messages": []any{}, "thread": "thread-fake"},
		"gmail.read":    map[string]any{"messages": []any{}, "thread": "thread-fake"},
		"gmail.draft":   map[string]any{"id": "draft-fake", "fake": true},
		"gmail.send":    map[string]any{"id": "msg-fake", "thread": "thread-fake", "fake": true},
		"calendar.list": map[string]any{"events": []any{}, "fake": true},
		"slack.search":  map[string]any{"messages": []any{}, "fake": true},
		"airtable.list": map[string]any{"rows": []any{}, "fake": true},
		"hubspot.list":  map[string]any{"rows": []any{}, "fake": true},
	}
	if reply, ok := replies[tool]; ok {
		return reply
	}
	return map[string]any{"ok": true, "fake": true, "tool": tool}
}

func (s *Service) fake(ctx context.Context, scope svc.Scope, service, verb, tool string, args map[string]any) (any, error) {
	reply := s.fakes[serviceKey(service)]
	if reply == nil {
		return nil, bridge.Errorf(bridge.CodeInvalid, "no adapter")
	}
	call := bridge.FakeCall{App: scope.App, RunID: invocation(ctx).RunID, Tool: tool, VerbLabel: verbLabel(verb), Summary: summary(args), At: s.now().UTC().Format(time.RFC3339Nano), Fake: true}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()
	s.publish(bridge.TopicFakes(scope.App), call)
	return reply(tool), nil
}

func verbLabel(verb string) string {
	labels := map[string]string{"send": "Would send", "post": "Would post", "reply": "Would reply", "pay": "Would pay", "charge": "Would charge", "create": "Would create", "draft": "Would draft"}
	if label := labels[verb]; label != "" {
		return label
	}
	return "Would read"
}

func summary(args map[string]any) string {
	var parts []string
	for _, key := range []string{"subject", "title", "text", "body", "query", "amount", "to", "channel", "id"} {
		if value, ok := args[key]; ok && fmt.Sprint(value) != "" {
			parts = append(parts, fmt.Sprint(value))
		}
	}
	if len(parts) == 0 {
		keys := make([]string, 0, len(args))
		for key := range args {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, key+": "+fmt.Sprint(args[key]))
		}
	}
	return strings.Join(parts, " · ")
}

func (s *Service) Fakes(_ context.Context, scope svc.Scope, p bridge.ConnectFakesParams) (bridge.ConnectFakesResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := bridge.ConnectFakesResult{Calls: []bridge.FakeCall{}}
	for _, call := range s.calls {
		if call.App == scope.App && (p.RunID == "" || call.RunID == p.RunID) {
			result.Calls = append(result.Calls, call)
		}
	}
	return result, nil
}

func (s *Service) publish(topic string, event any) {
	if s.all != nil && s.all.Hub != nil {
		s.all.Hub.Publish(topic, event)
	}
}
