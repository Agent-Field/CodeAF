package grants

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Spaces is the local adapter until the spaces track supplies its role and member seams.
// App roles already belong to the gateway and are never inferred here.
type Spaces interface {
	RoleOf(svc.Caller) string
	NameOf(svc.Caller) string
	Members(context.Context, string) ([]string, error)
}

type personalSpace struct{}

func (personalSpace) RoleOf(svc.Caller) string   { return "owner" }
func (personalSpace) NameOf(c svc.Caller) string { return c.Member }
func (personalSpace) Members(_ context.Context, _ string) ([]string, error) {
	return []string{svc.Owner}, nil
}

// AuditLine deliberately contains no credential or raw request parameters.
type AuditLine struct {
	Space, App, Member, Action, Object, Decision, Activity string
}
type auditor interface {
	Append(context.Context, AuditLine) error
}
type noAudit struct{}

func (noAudit) Append(context.Context, AuditLine) error { return nil }

// Options isolates the pending spaces wiring and the personal credential store.
// Space is the server's identity, never a client-supplied export destination.
type Options struct {
	Space       string
	Spaces      Spaces
	Audit       auditor
	Connections Connections
}

func (s *Service) roleOf(c svc.Caller) string { return s.spaces.RoleOf(c) }
func (s *Service) admin(c svc.Caller) error {
	switch s.roleOf(c) {
	case "owner", "admin":
		return nil
	}
	return reason(bridge.CodeDenied, bridge.ReasonAdminOnly, "Only an Owner or Admin can change workspace connections")
}
func (s *Service) space() string   { return s.serverSpace }
func ownDesktop(c svc.Caller) bool { return c.Member == svc.Owner && !c.Agent }

// ExportRule is registered by the composition lane with Caps.AddRule.
// The handler also enforces it, so no app or agent can obtain a secret before wiring lands.
func ExportRule(scope svc.Scope, method string, _ json.RawMessage) (bool, error) {
	if method != bridge.MethodConnectCredentialExport {
		return false, nil
	}
	if scope.App != "" || !ownDesktop(scope.Caller) {
		return true, bridge.Errorf(bridge.CodeDenied, "Only your desktop session can export a connection")
	}
	return true, nil
}

func (s *Service) audit(ctx context.Context, caller svc.Caller, app, action, object, decision, activity string) error {
	return s.auditWriter.Append(ctx, AuditLine{Space: s.space(), App: app, Member: caller.Member, Action: action, Object: object, Decision: decision, Activity: activity})
}
func reason(code bridge.ErrorCode, why, message string) error {
	return &bridge.Error{Code: code, Message: message, Details: map[string]any{"reason": why}}
}
