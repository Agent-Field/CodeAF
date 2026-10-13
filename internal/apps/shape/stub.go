package shape

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var _ svc.Shape = (*Service)(nil)

func (*Service) Request(context.Context, svc.Scope, bridge.ShapeRequestParams) (bridge.ShapeRequestResult, error) {
	return bridge.ShapeRequestResult{}, bridge.NotBuilt("shape")
}

func (*Service) EditPlan(context.Context, svc.Scope, bridge.ShapePlanEditParams) (bridge.ShapePlanEditResult, error) {
	return bridge.ShapePlanEditResult{}, bridge.NotBuilt("shape")
}

func (*Service) FixChange(context.Context, svc.Scope, bridge.ShapeChangeFixParams) (bridge.ShapeChangeFixResult, error) {
	return bridge.ShapeChangeFixResult{}, bridge.NotBuilt("shape")
}

func (*Service) Answer(context.Context, svc.Scope, bridge.ShapeAnswerParams) (bridge.ShapeAnswerResult, error) {
	return bridge.ShapeAnswerResult{}, bridge.NotBuilt("shape")
}

func (*Service) DismissReplay(context.Context, svc.Scope, bridge.ShapeReplayDismissParams) (bridge.ShapeReplayDismissResult, error) {
	return bridge.ShapeReplayDismissResult{}, bridge.NotBuilt("shape")
}

func (*Service) Ask(context.Context, svc.Scope, bridge.ShapeAskParams) (bridge.ShapeAskResult, error) {
	return bridge.ShapeAskResult{}, bridge.NotBuilt("shape")
}
