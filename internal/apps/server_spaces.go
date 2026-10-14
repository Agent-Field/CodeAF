package apps

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/apps/approles"
	"github.com/Agent-Field/codeaf/internal/apps/audit"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/signoff"
	"github.com/Agent-Field/codeaf/internal/apps/space"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// The gateway resolver has no service context, so wiring publishes the current services.
var spacesServices atomic.Pointer[svc.Services]

func init() {
	gateway.SetCallerResolver(resolveSpacesCaller)
}

func resolveSpacesCaller(ctx context.Context, r *http.Request, app string) svc.Caller {
	caller := svc.PersonalCaller()
	if r != nil {
		if member, ok := svc.CallerFrom(r.Context()); ok {
			caller = member
		}
	}
	if app == "" {
		return caller
	}
	caller.Role = ""
	all := spacesServices.Load()
	if all != nil && all.AppRoles != nil {
		role, err := all.AppRoles.Role(ctx, app, caller.Member)
		if err == nil {
			caller.Role = role
			return caller
		}
	}
	if caller.Space == svc.PersonalSpace {
		caller.Role = svc.RoleEditor
	}
	return caller
}

func wireSpaces(cfg svc.Config, all *svc.Services) {
	spaces := space.New(cfg, all)
	all.Spaces = spaces
	all.Members = spaces.Members()
	all.AppRoles = approles.New(cfg, all)
	all.Audit = audit.New(cfg, all)
	all.Signoff = signoff.New(cfg, all)
	spacesServices.Store(all)
}
