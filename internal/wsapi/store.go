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
// and Events are the real methods, and provenance is never discarded.
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
}

var _ store = (*workspace.Store)(nil)

// rootStater is the RootState seam. Storage's CAS remediation may still be
// landing this method; when *workspace.Store grows it, RootSnapshot fills
// Revision from the same handle Open already bound.
type rootStater interface {
	RootState(ctx context.Context) (revision int, purpose, updatedAt string, err error)
}

func rootRevision(ctx context.Context, s store) (int, error) {
	rs, ok := s.(rootStater)
	if !ok {
		return 0, nil
	}
	revision, _, _, err := rs.RootState(ctx)
	return revision, wrapStoreError(err)
}

// wrapStoreError keeps expected-revision refusals on workspace.ErrInvalid with
// a stable "revision" mention. ErrConflict wrapping ErrInvalid (when storage
// grows it) already satisfies errors.Is(..., ErrInvalid) and is left intact.
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
