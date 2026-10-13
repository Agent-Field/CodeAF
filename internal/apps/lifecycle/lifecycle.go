// Package lifecycle keeps archived and deleted apps out of the live store.
package lifecycle

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const retention = 30 * 24 * time.Hour
const purgeInterval = 24 * time.Hour

// Clock supplies both timestamps and daily wakeups so retention tests need no sleeps.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type Service struct {
	cfg   svc.Config
	all   *svc.Services
	store svc.Store
	clock Clock
	mu    sync.Mutex
}

var _ svc.Lifecycle = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service {
	return NewWithClock(cfg, all, wallClock{})
}

func NewWithClock(cfg svc.Config, all *svc.Services, clock Clock) *Service {
	s := &Service{cfg: cfg, all: all, clock: clock}
	all.Store = Wrap(all.Store)
	if wrapped, ok := all.Store.(*decoratedStore); ok {
		s.store = wrapped.Store
		wrapped.lifecycle = s
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.Purge(ctx); err != nil {
		slog.Error("apps lifecycle purge", "error", err)
	}
	if cfg.Ctx != nil {
		go s.daily(cfg.Ctx)
	}
	return s
}

func (s *Service) daily(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(purgeInterval):
			if err := s.Purge(ctx); err != nil {
				slog.Error("apps lifecycle purge", "error", err)
			}
		}
	}
}

func (s *Service) Archive(ctx context.Context, scope svc.Scope) (bridge.AppSummary, error) {
	app, changed, err := s.archive(ctx, scope)
	if err != nil || !changed {
		return app, err
	}
	return app, s.finish(ctx, scope, app, "archived "+app.Name+" · Restore")
}

func (s *Service) archive(ctx context.Context, scope svc.Scope) (bridge.AppSummary, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	detail, state, err := s.current(ctx, scope)
	if err != nil {
		return bridge.AppSummary{}, false, err
	}
	if err := canEdit(scope.Caller, detail.AppSummary); err != nil {
		return bridge.AppSummary{}, false, err
	}
	if state != nil {
		if state.State == bridge.StateDeleted {
			return bridge.AppSummary{}, false, notFound(scope.App)
		}
		return state.summary(), false, nil
	}
	state = &entry{App: detail.AppSummary, State: bridge.StateArchived, At: stamp(s.clock.Now()), By: scope.Caller.Member}
	if err := s.save(scope.App, state); err != nil {
		return bridge.AppSummary{}, false, err
	}
	return state.summary(), true, nil
}

func (s *Service) Restore(ctx context.Context, scope svc.Scope) (bridge.AppSummary, error) {
	app, err := s.restore(ctx, scope)
	if err != nil {
		return app, err
	}
	return app, s.finish(ctx, scope, app, "restored "+app.Name)
}

func (s *Service) restore(ctx context.Context, scope svc.Scope) (bridge.AppSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	detail, state, err := s.current(ctx, scope)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if state == nil {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeConflict, "app is not archived or deleted")
	}
	if state.State == bridge.StateDeleted {
		if state.expired(s.clock.Now()) {
			return bridge.AppSummary{}, notFound(scope.App)
		}
		err = canDelete(scope.Caller, detail.AppSummary)
	} else {
		err = canEdit(scope.Caller, detail.AppSummary)
	}
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if err := s.clear(scope.App); err != nil {
		return bridge.AppSummary{}, err
	}
	return detail.AppSummary, nil
}

func (s *Service) Delete(ctx context.Context, scope svc.Scope, p bridge.DeleteAppParams) error {
	app, changed, err := s.delete(ctx, scope, p)
	if err != nil || !changed {
		return err
	}
	return s.finish(ctx, scope, app, "deleted "+app.Name+" · Restore")
}

func (s *Service) delete(ctx context.Context, scope svc.Scope, p bridge.DeleteAppParams) (bridge.AppSummary, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	detail, state, err := s.current(ctx, scope)
	if err != nil {
		return bridge.AppSummary{}, false, err
	}
	if err := canDelete(scope.Caller, detail.AppSummary); err != nil {
		return bridge.AppSummary{}, false, err
	}
	if p.Confirm != detail.Name {
		return bridge.AppSummary{}, false, bridge.Errorf(bridge.CodeInvalid, "type the app name exactly to confirm")
	}
	if detail.Version < 1 {
		return bridge.AppSummary{}, false, bridge.Errorf(bridge.CodeConflict, "only a kept app can be deleted")
	}
	if state != nil && state.State == bridge.StateDeleted {
		return bridge.AppSummary{}, false, nil
	}
	at := s.clock.Now()
	state = &entry{App: detail.AppSummary, State: bridge.StateDeleted, At: stamp(at), By: scope.Caller.Member, PurgeAt: stamp(at.Add(retention))}
	if err := s.save(scope.App, state); err != nil {
		return bridge.AppSummary{}, false, err
	}
	return state.summary(), true, nil
}

func (s *Service) List(ctx context.Context, caller svc.Caller) (bridge.LifecycleResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := bridge.LifecycleResult{Archived: []bridge.LifecycleApp{}, Deleted: []bridge.LifecycleApp{}}
	if err := canOpen(caller); err != nil {
		return result, err
	}
	apps, err := s.store.List(ctx, caller)
	if err != nil {
		return result, err
	}
	for _, app := range apps {
		state, err := s.load(app.ID)
		if err != nil {
			return result, err
		}
		if state == nil {
			continue
		}
		row := bridge.LifecycleApp{App: state.summary(), At: state.At, By: state.By, PurgeAt: state.PurgeAt}
		if state.State == bridge.StateArchived {
			result.Archived = append(result.Archived, row)
		}
		if state.State == bridge.StateDeleted && !state.expired(s.clock.Now()) && canDelete(caller, app) == nil {
			result.Deleted = append(result.Deleted, row)
		}
	}
	return result, nil
}

func (s *Service) current(ctx context.Context, scope svc.Scope) (bridge.AppDetail, *entry, error) {
	if err := ctx.Err(); err != nil {
		return bridge.AppDetail{}, nil, err
	}
	if err := canOpen(scope.Caller); err != nil {
		return bridge.AppDetail{}, nil, err
	}
	state, err := s.load(scope.App)
	if err != nil {
		return bridge.AppDetail{}, nil, err
	}
	detail, err := s.store.Get(ctx, scope.App)
	return detail, state, err
}

func canOpen(caller svc.Caller) error {
	if caller.Member != "" && !caller.Agent {
		switch svc.RoleOf(caller) {
		case svc.RoleEditor, svc.RoleUser, svc.RoleViewer:
			return nil
		}
	}
	return bridge.Errorf(bridge.CodeDenied, "an app member is required")
}
func canEdit(caller svc.Caller, app bridge.AppSummary) error {
	if err := canOpen(caller); err != nil {
		return err
	}
	if app.Space == "personal" || svc.RoleOf(caller) == svc.RoleEditor {
		return nil
	}
	return bridge.Errorf(bridge.CodeDenied, "Editor or Admin required")
}
func canDelete(caller svc.Caller, app bridge.AppSummary) error {
	if err := canOpen(caller); err != nil {
		return err
	}
	// An app role never proves workspace ownership. R6 grants every role in Personal.
	if app.Space == "personal" {
		return nil
	}
	return bridge.Errorf(bridge.CodeDenied, "Owner or Admin required")
}

// Writers may read the decorated store, so external callbacks run after the state lock is released.
func (s *Service) finish(ctx context.Context, scope svc.Scope, app bridge.AppSummary, action string) error {
	s.publish(app)
	return s.activity(ctx, scope, action)
}
func (s *Service) activity(ctx context.Context, scope svc.Scope, action string) error {
	if s.all.Crews.Activity == nil {
		return nil
	}
	return s.all.Crews.Activity.Write(ctx, scope, "@"+scope.Caller.Member+" "+action)
}
func (s *Service) publish(app bridge.AppSummary) {
	if s.all.Hub != nil {
		s.all.Hub.Publish(bridge.TopicState(app.ID), app)
	}
}
func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }
func notFound(app string) error { return bridge.Errorf(bridge.CodeNotFound, "no app "+app) }
