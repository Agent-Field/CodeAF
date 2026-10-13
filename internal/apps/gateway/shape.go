package gateway

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() {
	Register(bridge.MethodShapeOpen, Method{Allow: shapeAllow(bridge.MethodShapeOpen), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeOpenParams) (bridge.ShapeOpenResult, error) {
		return s.Shape.Open
	})})
	Register(bridge.MethodShapeGet, Method{Allow: shapeAllow(bridge.MethodShapeGet), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeGetParams) (bridge.ShapeGetResult, error) {
		return s.Shape.Get
	})})
	Register(bridge.MethodShapeRequest, Method{Allow: shapeAllow(bridge.MethodShapeRequest), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeRequestParams) (bridge.ShapeRequestResult, error) {
		return s.Shape.Request
	})})
	Register(bridge.MethodShapePlanEdit, Method{Allow: shapeAllow(bridge.MethodShapePlanEdit), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapePlanEditParams) (bridge.ShapePlanEditResult, error) {
		return s.Shape.EditPlan
	})})
	Register(bridge.MethodShapeChangeFix, Method{Allow: shapeAllow(bridge.MethodShapeChangeFix), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeChangeFixParams) (bridge.ShapeChangeFixResult, error) {
		return s.Shape.FixChange
	})})
	Register(bridge.MethodShapeChangeCancel, Method{Allow: shapeAllow(bridge.MethodShapeChangeCancel), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeChangeCancelParams) (bridge.ShapeChangeCancelResult, error) {
		return s.Shape.CancelChange
	})})
	Register(bridge.MethodShapeAnswer, Method{Allow: shapeAllow(bridge.MethodShapeAnswer), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeAnswerParams) (bridge.ShapeAnswerResult, error) {
		return s.Shape.Answer
	})})
	Register(bridge.MethodShapeDiff, Method{Allow: shapeAllow(bridge.MethodShapeDiff), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeDiffParams) (bridge.ShapeDiffResult, error) {
		return s.Shape.Diff
	})})
	Register(bridge.MethodShapePreview, Method{Allow: shapeAllow(bridge.MethodShapePreview), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapePreviewParams) (bridge.ShapePreviewResult, error) {
		return s.Shape.Preview
	})})
	Register(bridge.MethodShapeApply, Method{Allow: shapeAllow(bridge.MethodShapeApply), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeApplyParams) (bridge.ShapeApplyResult, error) {
		return s.Shape.Apply
	})})
	Register(bridge.MethodShapeDiscard, Method{Allow: shapeAllow(bridge.MethodShapeDiscard), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeDiscardParams) (bridge.ShapeDiscardResult, error) {
		return s.Shape.Discard
	})})
	Register(bridge.MethodShapeReplayDismiss, Method{Allow: shapeAllow(bridge.MethodShapeReplayDismiss), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeReplayDismissParams) (bridge.ShapeReplayDismissResult, error) {
		return s.Shape.DismissReplay
	})})
	Register(bridge.MethodShapeAsk, Method{Allow: shapeAllow(bridge.MethodShapeAsk), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeAskParams) (bridge.ShapeAskResult, error) {
		return s.Shape.Ask
	})})
	Register(bridge.MethodVersionsList, Method{Allow: shapeAllow(bridge.MethodVersionsList), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.VersionsListParams) (bridge.VersionsListResult, error) {
		return s.Shape.ListVersions
	})})
	Register(bridge.MethodVersionsDiff, Method{Allow: shapeAllow(bridge.MethodVersionsDiff), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.VersionsDiffParams) (bridge.VersionsDiffResult, error) {
		return s.Shape.DiffVersion
	})})
	Register(bridge.MethodVersionsRestore, Method{Allow: shapeAllow(bridge.MethodVersionsRestore), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.VersionsRestoreParams) (bridge.VersionsRestoreResult, error) {
		return s.Shape.RestoreVersion
	})})
	Register(bridge.MethodAppsHealth, Method{Allow: shapeAllow(bridge.MethodAppsHealth), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AppsHealthParams) (bridge.AppsHealthResult, error) {
		return s.Shape.Health
	})})
	caps.RegisterTopic("app/*/shape/", allowShapeTopic)
	caps.RegisterTopic("app/*/version", allowShapeTopic)
	caps.RegisterTopic("app/*/health", allowShapeTopic)
	caps.RegisterTopic("app/*/*", allowShapeTopic)
}

func shapeAllow(method string) AllowFunc {
	return func(_ context.Context, caller svc.Caller, scope svc.Scope, _ json.RawMessage) error {
		if err := validateShapeScope(scope); err != nil {
			return err
		}
		if caller.Agent && method != bridge.MethodAppsHealth {
			return bridge.Errorf(bridge.CodeDenied, "the app agent cannot change or read app definitions")
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
		return bridge.Errorf(bridge.CodeDenied, "the app role does not allow this call")
	}
}

func validateShapeScope(scope svc.Scope) error {
	switch scope.Caller.Role {
	case svc.RoleEditor, svc.RoleUser, svc.RoleViewer:
	default:
		return bridge.Errorf(bridge.CodeDenied, "the app role does not allow this call")
	}
	if scope.App == "" {
		return bridge.Errorf(bridge.CodeInvalid, "this method needs app")
	}
	return nil
}

func allowShapeTopic(_ context.Context, caller svc.Caller, scope svc.Scope, params json.RawMessage) error {
	if err := validateShapeScope(scope); err != nil {
		return err
	}
	var p struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return bridge.Errorf(bridge.CodeInvalid, "topic must be a string")
	}
	parts := strings.Split(p.Topic, "/")
	if len(parts) < 3 || parts[0] != "app" || parts[1] != scope.App {
		return bridge.Errorf(bridge.CodeInvalid, "topic must belong to this app")
	}
	switch parts[2] {
	case "shape":
		if len(parts) != 4 || parts[3] == "" || strings.ContainsAny(parts[3], "*\\") {
			return bridge.Errorf(bridge.CodeInvalid, "unknown app topic")
		}
		if parts[3] != caller.Member {
			return bridge.Errorf(bridge.CodeDenied, "a shape draft belongs to its member")
		}
		return nil
	case "version", "health":
		if len(parts) == 3 {
			return nil
		}
	case "*":
		// Client wildcards would expose other members' private shape events.
		return bridge.Errorf(bridge.CodeDenied, "subscribe to individual app topics")
	}
	return bridge.Errorf(bridge.CodeInvalid, "unknown app topic")
}
