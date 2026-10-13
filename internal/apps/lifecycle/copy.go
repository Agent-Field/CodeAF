package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) Copy(ctx context.Context, scope svc.Scope, p bridge.CopyAppParams) (bridge.AppSummary, error) {
	// Copy uses the filtered store: deleted apps cannot be copied through a retained definition.
	if err := canOpen(scope.Caller); err != nil {
		return bridge.AppSummary{}, err
	}
	source, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	name := p.Name
	if name == "" {
		name = source.Name + " (my copy)"
	}
	if strings.TrimSpace(name) == "" {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeInvalid, "copy name is required")
	}
	if s.all.Sample == nil || s.all.Records == nil {
		return bridge.AppSummary{}, bridge.NotBuilt("sample records")
	}
	def, err := s.all.Store.Definition(ctx, scope.App)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	copy, err := s.store.Create(ctx, scope.Caller, name, source.Description)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	kept, err := s.fillCopy(ctx, copy, def)
	if err != nil {
		// A failed copy must not leave an app that looks ready to keep.
		cleanup := s.store.Discard(context.WithoutCancel(ctx), copy.ID)
		return bridge.AppSummary{}, errors.Join(err, cleanup)
	}
	copyScope := svc.Scope{App: kept.ID, Caller: scope.Caller, Data: svc.DataReal}
	return kept, s.finish(ctx, copyScope, kept, "copied "+source.Name+" · "+kept.Name)
}

func (s *Service) fillCopy(ctx context.Context, copy bridge.AppSummary, source *spec.Bundle) (bridge.AppSummary, error) {
	files, err := copyFiles(source.Files, copy)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if err := s.store.WriteFiles(ctx, copy.ID, files); err != nil {
		return bridge.AppSummary{}, err
	}
	def, err := s.store.Definition(ctx, copy.ID)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	rows, err := s.all.Sample.Generate(ctx, def)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if err := s.all.Records.Seed(ctx, copy.ID, rows); err != nil {
		return bridge.AppSummary{}, err
	}
	if err := s.store.SetState(ctx, copy.ID, bridge.StatePreview); err != nil {
		return bridge.AppSummary{}, err
	}
	return s.store.Keep(ctx, copy.ID)
}
func copyFiles(source spec.Files, copy bridge.AppSummary) (spec.Files, error) {
	files := make(spec.Files, len(source))
	for name, data := range source {
		files[name] = append([]byte(nil), data...)
	}
	// Patch only identity; preserving unknown app fields keeps other tracks' definitions intact.
	var app map[string]json.RawMessage
	if err := json.Unmarshal(files[spec.AppFile], &app); err != nil {
		return nil, err
	}
	for key, value := range map[string]any{"id": copy.ID, "name": copy.Name, "space": "personal", "version": 1} {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		app[key] = data
	}
	data, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return nil, err
	}
	files[spec.AppFile] = append(data, '\n')
	return files, nil
}
