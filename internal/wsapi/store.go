package wsapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// store matches the frozen workspace.Store methods so tests can inject a fake.
// Open assigns *workspace.Store directly: AddWith, RemoveWith, Move, WhyHere,
// Events, RootState, and Expected* on Provenance are the real methods.
type store interface {
	Close() error
	Create(ctx context.Context, name string) (workspace.Collection, error)
	Rename(ctx context.Context, id, name string) error
	Collections(ctx context.Context) ([]workspace.Collection, error)
	Members(ctx context.Context, id string) ([]workspace.Ref, error)
	CollectionsFor(ctx context.Context, ref workspace.Ref) ([]workspace.Collection, error)
	Add(ctx context.Context, id string, ref workspace.Ref) error
	Remove(ctx context.Context, id string, ref workspace.Ref) error
	AddWith(ctx context.Context, id string, ref workspace.Ref, p workspace.Provenance) error
	RemoveWith(ctx context.Context, id string, ref workspace.Ref, p workspace.Provenance) error
	Move(ctx context.Context, fromID, toID string, ref workspace.Ref, p workspace.Provenance) error
	WhyHere(ctx context.Context, id string, ref workspace.Ref) (workspace.MembershipEvent, error)
	Events(ctx context.Context, id string, ref workspace.Ref) ([]workspace.MembershipEvent, error)
	SchemaVersion() int
	RootState(ctx context.Context) (revision int, purpose, updatedAt string, err error)
	PutGuidance(ctx context.Context, g workspace.Guidance) (workspace.Guidance, error)
	ListGuidance(ctx context.Context, scopeID string) ([]workspace.Guidance, error)
	PutProposal(ctx context.Context, p workspace.Proposal) (workspace.Proposal, error)
	Suppress(ctx context.Context, collectionID string, ref workspace.Ref, evidenceHash string, p workspace.Provenance) error
	IsSuppressed(ctx context.Context, collectionID string, ref workspace.Ref, evidenceHash string) (bool, error)
}

var _ store = (*workspace.Store)(nil)

func rootRevision(ctx context.Context, s store) (int, error) {
	revision, _, _, err := s.RootState(ctx)
	if errors.Is(err, workspace.ErrNotFound) {
		// A blank file has no root_state row until the first write. That is
		// empty, not corrupt: RootSnapshot still returns folders/unfiled.
		return 0, nil
	}
	return revision, wrapStoreError(err)
}

// wrapStoreError keeps expected-revision refusals on workspace.ErrInvalid with
// a stable "revision" mention. ErrConflict already wraps ErrInvalid.
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
