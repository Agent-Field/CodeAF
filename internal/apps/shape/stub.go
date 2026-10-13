package shape

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{ all *svc.Services }

var _ svc.Shape = (*Service)(nil)

func New(_ svc.Config, all *svc.Services) *Service { return &Service{all: all} }

func (*Service) Open(context.Context, svc.Scope, bridge.ShapeOpenParams) (bridge.ShapeOpenResult, error) {
	return bridge.ShapeOpenResult{}, bridge.NotBuilt("shape")
}

func (*Service) Get(context.Context, svc.Scope, bridge.ShapeGetParams) (bridge.ShapeGetResult, error) {
	return bridge.ShapeGetResult{}, bridge.NotBuilt("shape")
}

func (*Service) Request(context.Context, svc.Scope, bridge.ShapeRequestParams) (bridge.ShapeRequestResult, error) {
	return bridge.ShapeRequestResult{}, bridge.NotBuilt("shape")
}

func (*Service) EditPlan(context.Context, svc.Scope, bridge.ShapePlanEditParams) (bridge.ShapePlanEditResult, error) {
	return bridge.ShapePlanEditResult{}, bridge.NotBuilt("shape")
}

func (*Service) FixChange(context.Context, svc.Scope, bridge.ShapeChangeFixParams) (bridge.ShapeChangeFixResult, error) {
	return bridge.ShapeChangeFixResult{}, bridge.NotBuilt("shape")
}

func (*Service) CancelChange(context.Context, svc.Scope, bridge.ShapeChangeCancelParams) (bridge.ShapeChangeCancelResult, error) {
	return bridge.ShapeChangeCancelResult{}, bridge.NotBuilt("shape")
}

func (*Service) Answer(context.Context, svc.Scope, bridge.ShapeAnswerParams) (bridge.ShapeAnswerResult, error) {
	return bridge.ShapeAnswerResult{}, bridge.NotBuilt("shape")
}

func (*Service) Diff(context.Context, svc.Scope, bridge.ShapeDiffParams) (bridge.ShapeDiffResult, error) {
	return bridge.ShapeDiffResult{}, bridge.NotBuilt("shape")
}

func (*Service) Preview(context.Context, svc.Scope, bridge.ShapePreviewParams) (bridge.ShapePreviewResult, error) {
	return bridge.ShapePreviewResult{}, bridge.NotBuilt("shape")
}

func (*Service) Apply(context.Context, svc.Scope, bridge.ShapeApplyParams) (bridge.ShapeApplyResult, error) {
	return bridge.ShapeApplyResult{}, bridge.NotBuilt("shape")
}

func (*Service) Discard(context.Context, svc.Scope, bridge.ShapeDiscardParams) (bridge.ShapeDiscardResult, error) {
	return bridge.ShapeDiscardResult{}, bridge.NotBuilt("shape")
}

func (*Service) DismissReplay(context.Context, svc.Scope, bridge.ShapeReplayDismissParams) (bridge.ShapeReplayDismissResult, error) {
	return bridge.ShapeReplayDismissResult{}, bridge.NotBuilt("shape")
}

func (*Service) Ask(context.Context, svc.Scope, bridge.ShapeAskParams) (bridge.ShapeAskResult, error) {
	return bridge.ShapeAskResult{}, bridge.NotBuilt("shape")
}

func (s *Service) ListVersions(ctx context.Context, scope svc.Scope, _ bridge.VersionsListParams) (bridge.VersionsListResult, error) {
	detail, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return bridge.VersionsListResult{}, err
	}
	// The store has no historical metadata seam yet, so only its current row is known.
	return bridge.VersionsListResult{Versions: []bridge.Version{{N: detail.Version, Requests: []string{}, At: detail.UpdatedAt, Current: true, Restorable: true}}}, nil
}

func (*Service) DiffVersion(context.Context, svc.Scope, bridge.VersionsDiffParams) (bridge.VersionsDiffResult, error) {
	return bridge.VersionsDiffResult{}, bridge.NotBuilt("shape")
}

func (*Service) RestoreVersion(context.Context, svc.Scope, bridge.VersionsRestoreParams) (bridge.VersionsRestoreResult, error) {
	return bridge.VersionsRestoreResult{}, bridge.NotBuilt("shape")
}

func (*Service) Health(context.Context, svc.Scope, bridge.AppsHealthParams) (bridge.AppsHealthResult, error) {
	return bridge.AppsHealthResult{}, bridge.NotBuilt("shape")
}
