package grants

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// CredentialExportRule lets caps enforce the server's identity before dispatching an export.
func (s *Service) CredentialExportRule(scope svc.Scope, method string, params json.RawMessage) (bool, error) {
	return ExportRule(scope, method, params, s.space())
}
