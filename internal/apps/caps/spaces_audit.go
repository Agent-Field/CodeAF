package caps

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/audit"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type auditLog struct {
	root     string
	once     sync.Once
	fallback *audit.Service
}

func (a *auditLog) appendSpaces(ctx context.Context, all *svc.Services, scope svc.Scope, method string, params json.RawMessage, callErr error) {
	if callErr == nil && auditRead(method) {
		return
	}
	decision := "allow"
	if callErr != nil {
		decision = "deny"
	}
	entry := bridge.AuditEntry{Space: scope.Caller.Space, App: scope.App,
		Member: scope.Caller.Member, Agent: scope.Caller.Agent,
		Action: method, Object: auditObject(method, params), Decision: decision}
	writer := a.writer(all)
	if _, err := writer.Append(context.WithoutCancel(ctx), entry); err != nil {
		slog.Error("apps audit append failed", "error", err, "action", method, "decision", decision, "code", errorCode(callErr))
	}
}

// Standalone capability checkers use the same SQLite store when wiring has no audit service.
func (a *auditLog) writer(all *svc.Services) svc.AuditWriter {
	if all != nil && all.Audit != nil {
		return all.Audit
	}
	a.once.Do(func() {
		a.fallback = audit.New(svc.Config{Root: a.root}, all)
	})
	return a.fallback
}

// Reveals are intentionally absent here, even when their method ends in get or list.
func auditRead(method string) bool {
	if strings.Contains(method, "reveal") {
		return false
	}
	if strings.HasSuffix(method, ".list") || strings.HasSuffix(method, ".get") || strings.HasSuffix(method, ".query") {
		return true
	}
	switch method {
	case "sub", "unsub", bridge.MethodSpaceInfo, bridge.MethodSignoffRoles, bridge.MethodSignoffState:
		return true
	}
	return false
}

// Only identifiers reach the audit trail; values, credentials and prompts stay in the call.
func auditObject(method string, raw json.RawMessage) string {
	var params map[string]json.RawMessage
	if json.Unmarshal(raw, &params) != nil {
		return ""
	}
	value := func(key string) string {
		var text string
		_ = json.Unmarshal(params[key], &text)
		return text
	}
	if strings.HasPrefix(method, "records.") {
		if kind, id := value("type"), value("id"); kind != "" && id != "" {
			return kind + "/" + id
		}
	}
	for _, field := range []struct{ key, prefix string }{
		{"member", "member/"}, {"invite", "invite/"}, {"approval_id", "approval/"},
		{"run_id", "run/"}, {"run", "run/"}, {"trigger", "trigger/"},
		{"version", ""}, {"id", ""},
	} {
		if id := value(field.key); id != "" {
			return field.prefix + id
		}
	}
	return ""
}
