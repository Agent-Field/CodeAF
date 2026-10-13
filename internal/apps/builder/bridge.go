package builder

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const MethodRunFix = "builder.run.fix"

type RunFixParams struct {
	File string `json:"file"`
}

func init() {
	gateway.Register(MethodRunFix, gateway.Method{
		Allow: func(_ context.Context, caller svc.Caller, _ svc.Scope, _ json.RawMessage) error {
			if caller.Agent || caller.Role != svc.RoleEditor {
				return bridge.Errorf(bridge.CodeDenied, "only an editor may fix an automation")
			}
			return nil
		},
		Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
			var p RunFixParams
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, bridge.Errorf(bridge.CodeInvalid, "supply an automation file")
			}
			builder, ok := all.Builder.(interface {
				RetryRun(context.Context, svc.Scope, string) error
			})
			if !ok {
				return nil, bridge.NotBuilt("automation fixes")
			}
			return bridge.Empty{}, builder.RetryRun(ctx, scope, p.File)
		},
	})
}
