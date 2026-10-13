package caps

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() {
	AddDenyRule(func(_ context.Context, _ svc.Caller, scope svc.Scope, method string, _ json.RawMessage) error {
		if scope.Data == svc.DataDraft && method == bridge.MethodAgentSend {
			return denied("the app agent is not available in draft mode")
		}
		return nil
	})
}
