package svc

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// DataDraft selects the calling member's scratch records, never records.db.
const DataDraft DataMode = "draft"

type Shape interface {
	Open(context.Context, Scope, bridge.ShapeOpenParams) (bridge.ShapeOpenResult, error)
	Get(context.Context, Scope, bridge.ShapeGetParams) (bridge.ShapeGetResult, error)
	Request(context.Context, Scope, bridge.ShapeRequestParams) (bridge.ShapeRequestResult, error)
	EditPlan(context.Context, Scope, bridge.ShapePlanEditParams) (bridge.ShapePlanEditResult, error)
	FixChange(context.Context, Scope, bridge.ShapeChangeFixParams) (bridge.ShapeChangeFixResult, error)
	CancelChange(context.Context, Scope, bridge.ShapeChangeCancelParams) (bridge.ShapeChangeCancelResult, error)
	Answer(context.Context, Scope, bridge.ShapeAnswerParams) (bridge.ShapeAnswerResult, error)
	Diff(context.Context, Scope, bridge.ShapeDiffParams) (bridge.ShapeDiffResult, error)
	Preview(context.Context, Scope, bridge.ShapePreviewParams) (bridge.ShapePreviewResult, error)
	Apply(context.Context, Scope, bridge.ShapeApplyParams) (bridge.ShapeApplyResult, error)
	Discard(context.Context, Scope, bridge.ShapeDiscardParams) (bridge.ShapeDiscardResult, error)
	DismissReplay(context.Context, Scope, bridge.ShapeReplayDismissParams) (bridge.ShapeReplayDismissResult, error)
	Ask(context.Context, Scope, bridge.ShapeAskParams) (bridge.ShapeAskResult, error)
	ListVersions(context.Context, Scope, bridge.VersionsListParams) (bridge.VersionsListResult, error)
	DiffVersion(context.Context, Scope, bridge.VersionsDiffParams) (bridge.VersionsDiffResult, error)
	RestoreVersion(context.Context, Scope, bridge.VersionsRestoreParams) (bridge.VersionsRestoreResult, error)
	Health(context.Context, Scope, bridge.AppsHealthParams) (bridge.AppsHealthResult, error)
}

type Shaper interface {
	Shape(ctx context.Context, in ShapeInput) (ShapeOutput, error)
}
type ShapeInput struct {
	App     string
	Files   spec.Files
	Request string
	Report  *bridge.CheckReport
	Round   int
	Sample  map[string][]bridge.Record
}
type ShapeOutput struct {
	Files    spec.Files
	Summary  string
	Question *bridge.ShapeQuestion
	Cost     float64
}

// ParseDraftApp removes only the draft suffix; app validation stays at the service door.
func ParseDraftApp(app string) (id string, draft bool) {
	return strings.CutSuffix(app, "@draft")
}
