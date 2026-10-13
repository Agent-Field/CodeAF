package caps

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (a *auditLog) append(ctx context.Context, all *svc.Services, scope svc.Scope, method string, params json.RawMessage, err error) {
	a.appendSpaces(ctx, all, scope, method, params, err)
}
