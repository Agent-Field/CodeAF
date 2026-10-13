package gateway

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Keep the wave-1 dispatch table intact so tracks can register their own methods.
var shapeMethods = map[string]method{}

func init() { registerShape(shapeMethods) }

func lookupShapeMethod(name string) (method, bool) {
	if entry, ok := methods[name]; ok {
		return entry, true
	}
	entry, ok := shapeMethods[name]
	return entry, ok
}

func (s *session) shapeCall(in bridge.Envelope) (method, svc.Scope, error) {
	scope := shapeScope(in.App)
	err := s.all.Caps.Allow(s.ctx, scope, in.Method, in.Params)
	entry, ok := lookupShapeMethod(in.Method)
	// Unknown wire methods remain invalid even when internal tool capabilities
	// recognise the same verb for runs or the app agent.
	if !ok {
		return entry, scope, bridge.Errorf(bridge.CodeInvalid, "unknown method "+in.Method)
	}
	if err == nil {
		scope, err = s.resolveShapeScope(scope, entry.global)
	}
	return entry, scope, err
}

func registerShape(m map[string]method) {
	m[bridge.MethodShapeOpen] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeOpenParams) (bridge.ShapeOpenResult, error) {
		return s.Shape.Open
	})}
	m[bridge.MethodShapeGet] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeGetParams) (bridge.ShapeGetResult, error) {
		return s.Shape.Get
	})}
	m[bridge.MethodShapeRequest] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeRequestParams) (bridge.ShapeRequestResult, error) {
		return s.Shape.Request
	})}
	m[bridge.MethodShapePlanEdit] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapePlanEditParams) (bridge.ShapePlanEditResult, error) {
		return s.Shape.EditPlan
	})}
	m[bridge.MethodShapeChangeFix] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeChangeFixParams) (bridge.ShapeChangeFixResult, error) {
		return s.Shape.FixChange
	})}
	m[bridge.MethodShapeChangeCancel] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeChangeCancelParams) (bridge.ShapeChangeCancelResult, error) {
		return s.Shape.CancelChange
	})}
	m[bridge.MethodShapeAnswer] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeAnswerParams) (bridge.ShapeAnswerResult, error) {
		return s.Shape.Answer
	})}
	m[bridge.MethodShapeDiff] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeDiffParams) (bridge.ShapeDiffResult, error) {
		return s.Shape.Diff
	})}
	m[bridge.MethodShapePreview] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapePreviewParams) (bridge.ShapePreviewResult, error) {
		return s.Shape.Preview
	})}
	m[bridge.MethodShapeApply] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeApplyParams) (bridge.ShapeApplyResult, error) {
		return s.Shape.Apply
	})}
	m[bridge.MethodShapeDiscard] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeDiscardParams) (bridge.ShapeDiscardResult, error) {
		return s.Shape.Discard
	})}
	m[bridge.MethodShapeReplayDismiss] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeReplayDismissParams) (bridge.ShapeReplayDismissResult, error) {
		return s.Shape.DismissReplay
	})}
	m[bridge.MethodShapeAsk] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ShapeAskParams) (bridge.ShapeAskResult, error) {
		return s.Shape.Ask
	})}
	m[bridge.MethodVersionsList] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.VersionsListParams) (bridge.VersionsListResult, error) {
		return s.Shape.ListVersions
	})}
	m[bridge.MethodVersionsDiff] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.VersionsDiffParams) (bridge.VersionsDiffResult, error) {
		return s.Shape.DiffVersion
	})}
	m[bridge.MethodVersionsRestore] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.VersionsRestoreParams) (bridge.VersionsRestoreResult, error) {
		return s.Shape.RestoreVersion
	})}
	m[bridge.MethodAppsHealth] = method{run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AppsHealthParams) (bridge.AppsHealthResult, error) {
		return s.Shape.Health
	})}
}

func shapeScope(app string) svc.Scope {
	id, draft := svc.ParseDraftApp(app)
	scope := ownerScope(id)
	if draft {
		scope.Data = svc.DataDraft
	}
	return scope
}

func (s *session) resolveShapeScope(scope svc.Scope, global bool) (svc.Scope, error) {
	resolved, err := s.resolveScope(scope, global)
	if scope.Data == svc.DataDraft {
		resolved.Data = svc.DataDraft
	}
	return resolved, err
}
