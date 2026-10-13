package shape

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type versionStore interface {
	Versions(context.Context, string) ([]store.Version, error)
	VersionRequests(context.Context, string, int) ([]string, error)
	GetVersion(context.Context, string, int) (bridge.AppDetail, error)
	CommitShape(context.Context, string, int, spec.Files, []string, string) (bridge.AppSummary, error)
}
type shapeMigrator interface {
	WithShapeMigration(context.Context, svc.Scope, spec.Files, func() error) error
}

func detailFiles(detail bridge.AppDetail) spec.Files {
	files := spec.Files{}
	for name, text := range detail.Files {
		files[name] = []byte(text)
	}
	return files
}

func (s *Service) versionStore() (versionStore, error) {
	versions, ok := s.all.Store.(versionStore)
	if !ok {
		return nil, bridge.NotBuilt("store shape versions")
	}
	return versions, nil
}

func (s *Service) candidateDir(scope svc.Scope) (string, error) {
	if err := s.ready(context.Background(), scope); err != nil {
		return "", err
	}
	return filepath.Join(s.cfg.Root, scope.App, "drafts", scope.Caller.Member), nil
}

// Callers hold s.mu so the journal and its checked files stay together through Apply.
func (s *Service) applyCandidate(ctx context.Context, scope svc.Scope) (bridge.AppDetail, bridge.Draft, spec.Files, error) {
	if err := s.ready(ctx, scope); err != nil {
		return bridge.AppDetail{}, bridge.Draft{}, nil, err
	}
	detail, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return detail, bridge.Draft{}, nil, err
	}
	d, err := s.draftFor(scope)
	if err != nil {
		return detail, bridge.Draft{}, nil, err
	}
	if d.Base != detail.Version {
		return detail, d.snapshot(), nil, bridge.Errorf(bridge.CodeConflict, "the app version changed; replay your draft")
	}
	files, err := d.Files()
	if err == nil {
		files, err = annotatedRenames(detailFiles(detail), files, d.Changes)
	}
	return detail, d.snapshot(), files, err
}

func (s *Service) Apply(ctx context.Context, scope svc.Scope, p bridge.ShapeApplyParams) (bridge.ShapeApplyResult, error) {
	if s.cfg.Root == "" {
		return bridge.ShapeApplyResult{}, bridge.NotBuilt("shape")
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	s.mu.Lock()
	summary, err := s.applyLocked(ctx, scope, p)
	s.mu.Unlock()
	if err == nil {
		s.replayOthers(scope.App, summary.Version)
	}
	return summary, err
}

func (s *Service) applyLocked(ctx context.Context, scope svc.Scope, p bridge.ShapeApplyParams) (bridge.ShapeApplyResult, error) {
	if err := s.ready(ctx, scope); err != nil {
		return bridge.AppSummary{}, err
	}
	current, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if p.Base != current.Version {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeConflict, "the app version changed; replay your draft")
	}
	_, draft, files, err := s.applyCandidate(ctx, scope)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if draft.State != "ready" || len(draft.Changes) == 0 {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeConflict, "the draft is not ready to apply")
	}
	requests := make([]string, 0, len(draft.Changes))
	for _, change := range draft.Changes {
		if change.State == "cancelled" {
			continue
		}
		if change.State != "passed" || change.Check == nil || !change.Check.Pass {
			return bridge.AppSummary{}, bridge.Errorf(bridge.CodeConflict, "every change must pass Check before Apply")
		}
		requests = append(requests, change.Request)
	}
	if len(requests) == 0 {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeConflict, "the draft has no passed changes")
	}
	summary, err := s.commitCandidate(ctx, scope, current, files, requests)
	if err != nil {
		return summary, err
	}
	lock := records.DraftOverlayLock(s.cfg.Root, scope)
	lock.Lock()
	dir, _ := s.candidateDir(scope)
	err = os.RemoveAll(dir)
	lock.Unlock()
	if err != nil {
		return summary, err
	}
	delete(s.drafts, draftKey(scope))
	at := s.now().UTC().Format(time.RFC3339Nano)
	version := &bridge.Version{
		N: summary.Version, Requests: requests, By: scope.Caller.Member, At: at,
		Changes: len(requests), Current: true, Restorable: true,
	}
	s.publish(bridge.TopicShape(scope.App, scope.Caller.Member), bridge.ShapeEvent{Kind: "applied", Version: version, At: at})
	return summary, nil
}

func (s *Service) commitCandidate(ctx context.Context, scope svc.Scope, current bridge.AppDetail, files spec.Files, requests []string) (bridge.AppSummary, error) {
	versions, err := s.versionStore()
	if err != nil {
		return bridge.AppSummary{}, err
	}
	migrator, ok := s.all.Records.(shapeMigrator)
	if !ok {
		return bridge.AppSummary{}, bridge.NotBuilt("records shape migration")
	}
	var summary bridge.AppSummary
	err = migrator.WithShapeMigration(ctx, scope, files, func() error {
		var err error
		summary, err = versions.CommitShape(ctx, scope.App, current.Version, files, requests, scope.Caller.Member)
		return err
	})
	if err != nil {
		return summary, err
	}
	request := strings.Join(requests, "\n")
	event := bridge.VersionEvent{Version: summary.Version, By: scope.Caller.Member, Request: request, At: s.now().UTC().Format(time.RFC3339Nano)}
	s.publish(bridge.TopicVersion(scope.App), event)
	s.publish("app/"+scope.App+"/activity", map[string]any{"who": scope.Caller.Member, "verb": "shaped", "app": scope.App, "version": summary.Version, "request": request, "undo": true})
	live, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return summary, err
	}
	if err := s.checkHealth(context.WithoutCancel(ctx), scope.App, summary.Version, current.Version, request, scope.Caller.Member, detailFiles(live)); err != nil {
		return summary, err
	}
	return summary, nil
}

func (s *Service) replayOthers(app string, version int) {
	if s.replayHook != nil {
		s.replayHook(app, version)
	}
}

func (s *Service) publish(topic string, data any) {
	if s.all.Hub != nil {
		s.all.Hub.Publish(topic, data)
	}
}
