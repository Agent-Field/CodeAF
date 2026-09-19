package wsapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// store matches the frozen workspace.Store methods so tests can inject a fake
// while storage lives on another worktree. Open wraps today's *workspace.Store
// through storeAdapter until AddWith, Move, and WhyHere exist on the real type.
type store interface {
	Close() error
	Create(ctx context.Context, name string) (workspace.Collection, error)
	Rename(ctx context.Context, id, name string) error
	Collections(ctx context.Context) ([]workspace.Collection, error)
	Members(ctx context.Context, id string) ([]workspace.Ref, error)
	CollectionsFor(ctx context.Context, ref workspace.Ref) ([]workspace.Collection, error)
	Add(ctx context.Context, id string, ref workspace.Ref) error
	Remove(ctx context.Context, id string, ref workspace.Ref) error
	AddWith(ctx context.Context, id string, ref workspace.Ref, p Provenance) error
	RemoveWith(ctx context.Context, id string, ref workspace.Ref, p Provenance) error
	Move(ctx context.Context, fromID, toID string, ref workspace.Ref, p Provenance) error
	WhyHere(ctx context.Context, id string, ref workspace.Ref) (MembershipEvent, error)
	Events(ctx context.Context, id string, ref workspace.Ref) ([]MembershipEvent, error)
	SchemaVersion() int
}

// storeAdapter keeps Open compiling against a v1 Store. Provenance is accepted
// and discarded: Add is person origin with an empty reason. Move is Add then
// Remove, not one writer transaction. WhyHere has no events to read.
//
// INTEGRATION BINDS THE REAL STORE. Replace this adapter once *workspace.Store
// carries AddWith, RemoveWith, Move, WhyHere, Events, and workspace.Provenance.
type storeAdapter struct {
	inner *workspace.Store
}

func (a *storeAdapter) Close() error { return a.inner.Close() }

func (a *storeAdapter) Create(ctx context.Context, name string) (workspace.Collection, error) {
	return a.inner.Create(ctx, name)
}

func (a *storeAdapter) Rename(ctx context.Context, id, name string) error {
	return a.inner.Rename(ctx, id, name)
}

func (a *storeAdapter) Collections(ctx context.Context) ([]workspace.Collection, error) {
	return a.inner.Collections(ctx)
}

func (a *storeAdapter) Members(ctx context.Context, id string) ([]workspace.Ref, error) {
	return a.inner.Members(ctx, id)
}

func (a *storeAdapter) CollectionsFor(ctx context.Context, ref workspace.Ref) ([]workspace.Collection, error) {
	return a.inner.CollectionsFor(ctx, ref)
}

func (a *storeAdapter) Add(ctx context.Context, id string, ref workspace.Ref) error {
	return a.inner.Add(ctx, id, ref)
}

func (a *storeAdapter) Remove(ctx context.Context, id string, ref workspace.Ref) error {
	return a.inner.Remove(ctx, id, ref)
}

func (a *storeAdapter) AddWith(ctx context.Context, id string, ref workspace.Ref, _ Provenance) error {
	return a.inner.Add(ctx, id, ref)
}

func (a *storeAdapter) RemoveWith(ctx context.Context, id string, ref workspace.Ref, _ Provenance) error {
	return a.inner.Remove(ctx, id, ref)
}

func (a *storeAdapter) Move(ctx context.Context, fromID, toID string, ref workspace.Ref, _ Provenance) error {
	if err := a.inner.Add(ctx, toID, ref); err != nil {
		return err
	}
	return a.inner.Remove(ctx, fromID, ref)
}

func (a *storeAdapter) WhyHere(context.Context, string, workspace.Ref) (MembershipEvent, error) {
	return MembershipEvent{}, fmt.Errorf("%w: why-here needs membership events", workspace.ErrNotFound)
}

func (a *storeAdapter) Events(context.Context, string, workspace.Ref) ([]MembershipEvent, error) {
	return []MembershipEvent{}, nil
}

func (a *storeAdapter) SchemaVersion() int { return 1 }

// wrapStoreError keeps expected-revision refusals on workspace.ErrInvalid with
// a stable "revision" mention until storage adds ErrConflict.
func wrapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if !mentionsRevision(err) {
		return err
	}
	if errors.Is(err, workspace.ErrInvalid) {
		return err
	}
	return fmt.Errorf("%w: revision: %v", workspace.ErrInvalid, err)
}

func mentionsRevision(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "revision")
}
