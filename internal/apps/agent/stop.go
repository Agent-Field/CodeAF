package agent

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() {
	gateway.Register(bridge.MethodAgentStop, gateway.Method{
		Allow: func(_ context.Context, caller svc.Caller, _ svc.Scope, _ json.RawMessage) error {
			if caller.Agent {
				return bridge.Errorf(bridge.CodeDenied, "the agent cannot stop itself")
			}
			return nil
		},
		Handle: func(_ context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
			var p bridge.AgentStopParams
			if err := json.Unmarshal(raw, &p); err != nil || !threadID.MatchString(p.Thread) {
				return nil, bridge.Errorf(bridge.CodeInvalid, "thread is required")
			}
			agent, ok := all.Agent.(*Service)
			if !ok {
				return nil, bridge.NotBuilt("stopping the agent")
			}
			agent.Stop(scope, p.Thread)
			return bridge.Empty{}, nil
		},
	})
}

// Stop cancels the thread's turn in flight. The turn ends with "Stopped."
// and done, and what was said so far stays in the thread.
func (s *Service) Stop(scope svc.Scope, thread string) { s.turns.stop(scope.App, thread) }

// stopped reports whether err is agent.stop rather than a failure: the turn's
// own context ended while the server's did not. A provider may not wrap the
// cancellation, so the contexts decide, not the error's type.
func (t *turn) stopped(err error) bool {
	return err != nil && t.ctx.Err() != nil && t.svc.ctx.Err() == nil
}
