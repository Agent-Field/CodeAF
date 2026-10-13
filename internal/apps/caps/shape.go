package caps

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) allowWithShape(ctx context.Context, scope svc.Scope, method string, params json.RawMessage) error {
	if scope.Data == svc.DataDraft && method == bridge.MethodAgentSend {
		return denied("the app agent is not available in draft mode")
	}
	if shapeMethod(method) {
		if err := validateShapeCall(ctx, scope, params); err != nil {
			return err
		}
		return allowShapeRole(scope.Caller, method)
	}
	if method == "sub" || method == "unsub" {
		if handled, err := allowShapeTopic(ctx, scope, params); handled {
			return err
		}
	}
	return s.allow(ctx, scope, method, params)
}

func shapeMethod(method string) bool {
	return strings.HasPrefix(method, "shape.") || method == bridge.MethodVersionsList || method == bridge.MethodVersionsDiff || method == bridge.MethodVersionsRestore || method == bridge.MethodAppsHealth
}

func validateShapeCall(ctx context.Context, scope svc.Scope, params json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if scope.Caller.Member == "" {
		return denied("a member is required")
	}
	switch scope.Caller.Role {
	case svc.RoleEditor, svc.RoleUser, svc.RoleViewer:
	default:
		return denied("the app role does not allow this call")
	}
	if len(params) != 0 && string(params) != "null" && (!json.Valid(params) || !strings.HasPrefix(strings.TrimSpace(string(params)), "{")) {
		return bridge.Errorf(bridge.CodeInvalid, "params must be an object")
	}
	if scope.App == "" {
		return bridge.Errorf(bridge.CodeInvalid, "this method needs app")
	}
	return nil
}

func allowShapeRole(caller svc.Caller, method string) error {
	if caller.Agent && method != bridge.MethodAppsHealth {
		return denied("the app agent cannot change or read app definitions")
	}
	switch method {
	case bridge.MethodVersionsList, bridge.MethodVersionsDiff, bridge.MethodAppsHealth:
		return nil
	case bridge.MethodShapeAsk:
		if caller.Role != svc.RoleEditor {
			return nil
		}
	default:
		if caller.Role == svc.RoleEditor {
			return nil
		}
	}
	return denied("the app role does not allow this call")
}

func allowShapeTopic(ctx context.Context, scope svc.Scope, params json.RawMessage) (bool, error) {
	var p struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(object(params), &p); err != nil {
		return false, nil
	}
	parts := strings.Split(p.Topic, "/")
	if len(parts) < 3 {
		return false, nil
	}
	if parts[2] != "shape" && parts[2] != "version" && parts[2] != "health" && parts[2] != "*" {
		return false, nil
	}
	if err := validateShapeCall(ctx, scope, params); err != nil {
		return true, err
	}
	if parts[0] != "app" || parts[1] != scope.App {
		return true, bridge.Errorf(bridge.CodeInvalid, "topic must belong to this app")
	}
	switch parts[2] {
	case "shape":
		if len(parts) != 4 || parts[3] == "" || strings.ContainsAny(parts[3], "*\\") {
			return true, bridge.Errorf(bridge.CodeInvalid, "unknown app topic")
		}
		if parts[3] != scope.Caller.Member {
			return true, denied("a shape draft belongs to its member")
		}
		return true, nil
	case "version", "health":
		if len(parts) == 3 {
			return true, nil
		}
	case "*":
		// Client app wildcards would also expose other members' private shape events.
		// Internal subscribers still use the hub wildcard directly for record triggers.
		return true, denied("subscribe to individual app topics")
	}
	return true, bridge.Errorf(bridge.CodeInvalid, "unknown app topic")
}
