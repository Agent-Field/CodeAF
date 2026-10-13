package shape

import (
	"bytes"
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) ListVersions(ctx context.Context, scope svc.Scope, _ bridge.VersionsListParams) (bridge.VersionsListResult, error) {
	detail, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return bridge.VersionsListResult{}, err
	}
	versions, err := s.versionStore()
	if err != nil {
		return bridge.VersionsListResult{}, err
	}
	rows, err := versions.Versions(ctx, scope.App)
	if err != nil {
		return bridge.VersionsListResult{}, err
	}
	result := bridge.VersionsListResult{Versions: []bridge.Version{}}
	for _, row := range rows {
		requests, err := versions.VersionRequests(ctx, scope.App, row.Version)
		if err != nil {
			return bridge.VersionsListResult{}, err
		}
		result.Versions = append(result.Versions, bridge.Version{N: row.Version, Requests: requests, By: row.By, At: row.At, Changes: len(requests), Current: row.Version == detail.Version, Restorable: true})
	}
	return result, nil
}

func (s *Service) DiffVersion(ctx context.Context, scope svc.Scope, p bridge.VersionsDiffParams) (bridge.VersionsDiffResult, error) {
	if s.cfg.Root == "" {
		return bridge.VersionsDiffResult{}, bridge.NotBuilt("shape")
	}
	versions, err := s.versionStore()
	if err != nil {
		return bridge.VersionsDiffResult{}, err
	}
	next, err := versions.GetVersion(ctx, scope.App, p.Version)
	if err != nil {
		return bridge.VersionsDiffResult{}, err
	}
	previous := spec.Files{}
	if p.Version > 1 {
		old, err := versions.GetVersion(ctx, scope.App, p.Version-1)
		if err != nil {
			return bridge.VersionsDiffResult{}, err
		}
		previous = detailFiles(old)
	}
	return bridge.VersionsDiffResult{Files: versionFileDiffs(previous, detailFiles(next))}, nil
}

func versionFileDiffs(before, after spec.Files) []bridge.FileDiff {
	names := map[string]bool{}
	for name := range before {
		names[name] = true
	}
	for name := range after {
		names[name] = true
	}
	result := []bridge.FileDiff{}
	for _, name := range sortedNames(names) {
		a, aExists := before[name]
		b, bExists := after[name]
		if aExists && bExists && bytes.Equal(a, b) {
			continue
		}
		op := "modify"
		if !aExists {
			op = "add"
		} else if !bExists {
			op = "delete"
		}
		result = append(result, bridge.FileDiff{Path: name, Op: op, Before: string(a), After: string(b)})
	}
	return result
}

func (s *Service) RestoreVersion(ctx context.Context, scope svc.Scope, p bridge.VersionsRestoreParams) (bridge.VersionsRestoreResult, error) {
	if s.cfg.Root == "" {
		return bridge.VersionsRestoreResult{}, bridge.NotBuilt("shape")
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	versions, err := s.versionStore()
	if err != nil {
		return bridge.AppSummary{}, err
	}
	current, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	target, err := versions.GetVersion(ctx, scope.App, p.Version)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	files := detailFiles(target)
	migration, _, err := s.migration(ctx, scope.App, detailFiles(current), files, current.Version)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	for _, line := range migration {
		if line.Op == "remove" && line.Affected > 0 && !p.Confirm {
			return bridge.AppSummary{}, &bridge.Error{Code: bridge.CodeConflict, Message: "Restore hides fields with stored values; confirm the migration", Details: map[string]any{"migration": migration}}
		}
	}
	summary, err := s.commitCandidate(ctx, scope, current, files, []string{fmt.Sprintf("restore of v%d", p.Version)})
	if err == nil {
		s.replayOthers(scope.App, summary.Version)
	}
	return summary, err
}
