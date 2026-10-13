package lifecycle

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type decoratedStore struct {
	svc.Store
	lifecycle *Service
}

// Wrap filters lifecycle state while leaving every other store operation on B1's door.
func Wrap(store svc.Store) svc.Store {
	if store == nil {
		return nil
	}
	if _, ok := store.(*decoratedStore); ok {
		return store
	}
	return &decoratedStore{Store: store}
}

func (w *decoratedStore) state(app string) (*entry, error) {
	if w.lifecycle == nil {
		return nil, nil
	}
	w.lifecycle.mu.Lock()
	defer w.lifecycle.mu.Unlock()
	return w.lifecycle.load(app)
}
func (w *decoratedStore) List(ctx context.Context, caller svc.Caller) ([]bridge.AppSummary, error) {
	apps, err := w.Store.List(ctx, caller)
	if err != nil {
		return nil, err
	}
	visible := make([]bridge.AppSummary, 0, len(apps))
	for _, app := range apps {
		state, err := w.state(app.ID)
		if err != nil {
			return nil, err
		}
		if state == nil && app.State != bridge.StateArchived && app.State != bridge.StateDeleted {
			visible = append(visible, app)
		}
	}
	return visible, nil
}
func (w *decoratedStore) Get(ctx context.Context, app string) (bridge.AppDetail, error) {
	state, err := w.state(app)
	if err != nil {
		return bridge.AppDetail{}, err
	}
	if state != nil && state.State == bridge.StateDeleted {
		return bridge.AppDetail{}, notFound(app)
	}
	detail, err := w.Store.Get(ctx, app)
	if err == nil && detail.State == bridge.StateDeleted {
		return bridge.AppDetail{}, notFound(app)
	}
	return detail, err
}
func (w *decoratedStore) Definition(ctx context.Context, app string) (*spec.Bundle, error) {
	state, err := w.state(app)
	if err != nil {
		return nil, err
	}
	if state != nil && state.State == bridge.StateDeleted {
		return nil, notFound(app)
	}
	if state == nil {
		detail, err := w.Store.Get(ctx, app)
		if err == nil && detail.State == bridge.StateDeleted {
			return nil, notFound(app)
		}
	}
	return w.Store.Definition(ctx, app)
}
