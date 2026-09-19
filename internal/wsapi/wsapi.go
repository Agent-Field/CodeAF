// Package wsapi is the typed workspace service the TUI and session tools call.
//
// Storage stays in internal/workspace. This package projects membership into
// folder snapshots, unique conversation counts, and why-here, and it never
// imports session, tui3, provider, or run. Open binds *workspace.Store
// directly so provenance, atomic Move, WhyHere, and Events are the real store's.
package wsapi

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// Inventory is the conversation world injected from cmd/codeaf. Titles are
// whatever the session side already shows; this package does not open journals.
type Inventory interface {
	ConversationIDs() []string
	Title(id string) string
}

// Folder is one collection as the home panel and tools see it.
// ParentIDs empty means the folder hangs off virtual Root.
type Folder struct {
	ID, Name, Purpose, Lifecycle string
	Revision                     int
	ParentIDs                    []string
	MemberCount                  int // unique conversation IDs, not graph paths
}

// Placement is one membership edge plus the other folders that also hold it.
type Placement struct {
	CollectionID string
	Ref          workspace.Ref
	Title        string
	AlsoIn       []string // other collection names
}

// Why is the latest membership event for an edge, active or last.
type Why struct {
	Event workspace.MembershipEvent
}

// RootView is virtual Root: parentless collections and unfiled conversations.
// Root is never a stored collection and never a CLI list entry.
type RootView struct {
	Folders  []Folder
	Unfiled  []Placement
	Revision int // root_state.revision; zero until the store exposes RootState
}

// Service holds a store, an optional inventory, and a clock.
type Service struct {
	store store
	inv   Inventory
	now   func() time.Time
}

// Open wraps workspace.Open. A corrupt, foreign, or unreadable store is an
// error: the service is not constructed, and callers must not invent an empty
// RootView. The returned service talks to *workspace.Store directly.
func Open(path string) (*Service, error) {
	inner, err := workspace.Open(path)
	if err != nil {
		return nil, err
	}
	return &Service{store: inner, now: time.Now}, nil
}

// Close releases the underlying store.
func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

// SetInventory replaces the conversation world used for titles and unfiled rows.
func (s *Service) SetInventory(inv Inventory) {
	if s == nil {
		return
	}
	s.inv = inv
}

// CreateFolder makes a logical folder. It is a child of Root until placed.
func (s *Service) CreateFolder(ctx context.Context, name string) (Folder, error) {
	if err := s.ready(ctx); err != nil {
		return Folder{}, err
	}
	created, err := s.store.Create(ctx, name)
	if err != nil {
		return Folder{}, wrapStoreError(err)
	}
	return s.folderFrom(ctx, created)
}

// RenameFolder changes a folder's display name. Membership is untouched.
func (s *Service) RenameFolder(ctx context.Context, id, name string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return wrapStoreError(s.store.Rename(ctx, id, name))
}

// AddPlacement files a ref in a folder. A second call with the same edge is
// success: membership is binary, not counted.
func (s *Service) AddPlacement(ctx context.Context, collectionID string, ref workspace.Ref, p workspace.Provenance) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return wrapStoreError(s.store.AddWith(ctx, collectionID, ref, normalize(p)))
}

// RemovePlacement drops one edge. The referenced conversation stays itself.
func (s *Service) RemovePlacement(ctx context.Context, collectionID string, ref workspace.Ref, p workspace.Provenance) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return wrapStoreError(s.store.RemoveWith(ctx, collectionID, ref, normalize(p)))
}

// MovePlacement adds the destination and removes the source in the store's
// one writer transaction. Other placements of the same ref stay.
func (s *Service) MovePlacement(ctx context.Context, fromID, toID string, ref workspace.Ref, p workspace.Provenance) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return wrapStoreError(s.store.Move(ctx, fromID, toID, ref, normalize(p)))
}

// WhyHere is the latest event for that active or last edge.
func (s *Service) WhyHere(ctx context.Context, collectionID string, ref workspace.Ref) (Why, error) {
	if err := s.ready(ctx); err != nil {
		return Why{}, err
	}
	event, err := s.store.WhyHere(ctx, collectionID, ref)
	if err != nil {
		return Why{}, wrapStoreError(err)
	}
	return Why{Event: event}, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s == nil || s.store == nil {
		return wrapStoreError(workspace.ErrInvalid)
	}
	return ctx.Err()
}

func normalize(p workspace.Provenance) workspace.Provenance {
	if p.Origin == "" {
		p.Origin = workspace.OriginPerson
	}
	return p
}
