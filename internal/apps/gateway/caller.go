package gateway

import (
	"context"
	"net/http"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// CallerResolver resolves socket membership for app "", and the role for a bare app id.
type CallerResolver func(ctx context.Context, r *http.Request, app string) svc.Caller

var callers = struct {
	sync.RWMutex
	resolve CallerResolver
	set     bool
}{resolve: PersonalCaller}

// PersonalCaller preserves the single-member Personal space's owner and editor role.
func PersonalCaller(_ context.Context, _ *http.Request, _ string) svc.Caller {
	return svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}
}

// SetCallerResolver is the spaces track's startup seam; only one track owns identity.
func SetCallerResolver(f CallerResolver) {
	callers.Lock()
	defer callers.Unlock()
	if callers.set {
		panic("gateway: caller resolver set twice")
	}
	callers.resolve, callers.set = f, true
}

func resolveCaller(ctx context.Context, r *http.Request, app string) svc.Caller {
	callers.RLock()
	resolve := callers.resolve
	callers.RUnlock()
	return resolve(ctx, r, app)
}

func (s *session) ownerScope(app string) svc.Scope {
	caller := s.caller
	if app != "" {
		caller = resolveCaller(s.ctx, s.request, app)
	}
	return svc.Scope{App: app, Caller: caller, Data: svc.DataSample}
}
