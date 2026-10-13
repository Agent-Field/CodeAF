package gateway

import "github.com/Agent-Field/codeaf/internal/apps/svc"

func (s *session) appScope(app string) svc.Scope {
	id, draft := svc.ParseDraftApp(app)
	scope := s.ownerScope(id)
	if draft {
		scope.Data = svc.DataDraft
	}
	return scope
}
