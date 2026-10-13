package imports

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{}

var _ svc.Imports = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) Sources(context.Context, svc.Scope, bridge.ImportSourcesParams) (bridge.ImportSourcesResult, error) {
	return bridge.ImportSourcesResult{}, bridge.Errorf(bridge.CodeNotBuilt, "imports.sources is not built yet")
}

func (*Service) Preview(context.Context, svc.Scope, bridge.ImportPreviewParams) (bridge.ImportPreview, error) {
	return bridge.ImportPreview{}, bridge.Errorf(bridge.CodeNotBuilt, "imports.preview is not built yet")
}

func (*Service) Run(context.Context, svc.Scope, bridge.ImportRunParams) (bridge.ImportResult, error) {
	return bridge.ImportResult{}, bridge.Errorf(bridge.CodeNotBuilt, "imports.run is not built yet")
}

func (*Service) Undo(context.Context, svc.Scope, bridge.ImportUndoParams) (bridge.ImportUndoResult, error) {
	return bridge.ImportUndoResult{}, bridge.Errorf(bridge.CodeNotBuilt, "imports.undo is not built yet")
}
