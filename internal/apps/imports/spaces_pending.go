package imports

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Spaces is the temporary member and Activity seam until the spaces track lands.
// Its adapter is confined here so the service does not depend on workspace internals.
type Spaces interface {
	Members(context.Context, svc.Scope) ([]Member, error)
	Append(context.Context, ActivityEntry) error
}

type Member struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}
type ActivityEntry struct {
	Scope                                    svc.Scope
	Action, Object, Text, Detail, UndoMethod string
	UndoParams                               map[string]any
}

// UseSpaces installs the workspace member directory and Activity writer at wiring time.
func (s *Service) UseSpaces(spaces Spaces) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spaces = spaces
}

func defaultSpaces(*svc.Services) Spaces { return personal{} }

type personal struct{}

func (personal) Members(_ context.Context, scope svc.Scope) ([]Member, error) {
	email := ""
	if strings.Contains(scope.Caller.Member, "@") {
		email = scope.Caller.Member
	}
	return []Member{{ID: scope.Caller.Member, Email: email}}, nil
}
func (personal) Append(context.Context, ActivityEntry) error { return nil }
