package appapi

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// appsTransport keeps the service handle with this server's guarded handler,
// so independent servers need no process-wide registry or cleanup hook.
type appsTransport struct {
	http.Handler
	services *svc.Services
}

// SetAppsServices binds apps before serving requests and returns the same handle
// for Mount. Later crew handlers reach their peers through AppsServices.
func (s *Server) SetAppsServices(all *svc.Services) *svc.Services {
	if bound, ok := s.guarded.(*appsTransport); ok {
		bound.services = all
	} else {
		s.guarded = &appsTransport{Handler: s.guarded, services: all}
	}
	return all
}

func (s *Server) AppsServices() *svc.Services {
	if bound, ok := s.guarded.(*appsTransport); ok {
		return bound.services
	}
	return nil
}
