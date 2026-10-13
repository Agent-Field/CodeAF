package caps

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/grants"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() {
	AddDenyRule(func(_ context.Context, _ svc.Caller, scope svc.Scope, method string, params json.RawMessage) error {
		_, err := grants.ExportRule(scope, method, params)
		return err
	})
	RegisterTopic("connect/*/allow", allowConnectTopic)
	RegisterTopic("connect/*/waits", allowConnectTopic)
	RegisterTopic("connect/workspace", allowConnectTopic)
	RegisterTopic("app/*/fakes", allowConnectTopic)
}

// ConnectAllow keeps shell queries independent of app capabilities, while imports
// require a role that may write records. The services enforce connection ownership.
func ConnectAllow(method string) AllowFunc {
	return func(_ context.Context, caller svc.Caller, scope svc.Scope, _ json.RawMessage) error {
		switch method {
		case bridge.MethodConnectAllow, bridge.MethodConnectAllowSheet, bridge.MethodConnectNeeds, bridge.MethodConnectFakes,
			bridge.MethodImportsSources, bridge.MethodImportsPreview, bridge.MethodImportsRun, bridge.MethodImportsUndo:
			if scope.App == "" {
				return bridge.Errorf(bridge.CodeInvalid, "this method needs app")
			}
			if err := allowRole(caller, "sub"); err != nil {
				return err
			}
		}
		if strings.HasPrefix(method, "imports.") && caller.Role != svc.RoleEditor && caller.Role != svc.RoleUser {
			return denied("the app role does not allow this call")
		}
		return nil
	}
}

func allowConnectTopic(_ context.Context, caller svc.Caller, scope svc.Scope, params json.RawMessage) error {
	var p struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return bridge.Errorf(bridge.CodeInvalid, "topic must be a string")
	}
	parts := strings.Split(p.Topic, "/")
	if len(parts) == 2 && p.Topic == bridge.TopicConnectWorkspace() && scope.App == "" {
		return nil
	}
	if len(parts) == 3 && parts[0] == "connect" && (parts[2] == "allow" || parts[2] == "waits") {
		if scope.App != "" || parts[1] != caller.Member || strings.ContainsAny(parts[1], "*\\") {
			return denied("this connection topic is not yours")
		}
		return nil
	}
	if len(parts) == 3 && parts[0] == "app" && parts[1] == scope.App && parts[1] != "" && parts[2] == "fakes" {
		return allowRole(caller, "sub")
	}
	return bridge.Errorf(bridge.CodeInvalid, "unknown connection topic")
}
