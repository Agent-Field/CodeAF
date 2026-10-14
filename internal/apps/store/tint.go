package store

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var _ svc.TintStore = (*Service)(nil)

// SetTint uses a new version so kept reads and immutable history stay consistent.
func (s *Service) SetTint(ctx context.Context, caller svc.Caller, p bridge.SetTintParams) (bridge.AppSummary, error) {
	if err := s.start(ctx); err != nil {
		return bridge.AppSummary{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if caller.Member != svc.Owner || caller.Agent {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeDenied, "only the owner may change a personal app's tint")
	}
	m, err := s.lookup(ctx, p.App)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if !slices.Contains(spec.Tints, p.Tint) {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeInvalid, "tint must be one of "+strings.Join(spec.Tints, ", "))
	}
	if m.State != bridge.StateKept {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeConflict, "only a kept app can change tint")
	}
	files, err := s.files(m)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(files[spec.AppFile], &definition); err != nil {
		return bridge.AppSummary{}, err
	}
	definition["tint"], _ = json.Marshal(p.Tint)
	files[spec.AppFile], err = json.MarshalIndent(definition, "", "  ")
	if err != nil {
		return bridge.AppSummary{}, err
	}
	m.Version++
	if err := s.commitVersion(ctx, &m, files, "Change tint", p.Tint); err != nil {
		return bridge.AppSummary{}, err
	}
	s.publish(m.AppSummary)
	return m.AppSummary, nil
}
